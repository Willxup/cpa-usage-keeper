# Wayfinder decision: security, verification, rollout, and PR-slice gates

## Decision
Establish six strict verification gates before any subscription quota adapter code is merged or released: Security Zero-Leak Gate, Functional RED/GREEN Invariant Gate, UI State & Theme Gate, Compatibility & Rollback Gate, Docker/Runtime Operational Gate, and Chained PR Slicing (<400 lines/PR).

---

## 1. Concrete Security Gates

### 1.1 Zero Secret Leaks & Server-Side-Only Credential Seam
- **Server-Side-Only Credentials**: Adapters MUST resolve provider credentials exclusively on the server side via existing CPA `auth_index` lookups and the internal management `api-call` seam (`internal/cpa/client.go:137-155,495-499`).
- **No Client-Side Ingestion**: The frontend MUST NEVER receive, store, or submit raw provider tokens, cookies, or refresh tokens.
- **Payload Sanitization**: API responses (`/api/v1/quota/cache`, `/api/v1/quota/refresh`, `/api/v1/quota/history/:auth_index`) MUST strictly exclude credential fields (`access_token`, `refresh_token`, `session_key`, `cookie`, `authorization`).
- **Automated CI Secret Scanning**: CI pipeline executes automated pattern matching (e.g., `sk-ant-*`, `Bearer *`, `ya29.*`, private keys) against all serialized API test responses, JSON fixtures, and log streams.

### 1.2 Redacting Diagnostics Pipeline
- **Disabled by Default**: Upstream response logging and persistence (`internal/quota/upstream_responses.go`) MUST remain disabled in production (`ENABLE_UPSTREAM_DIAGNOSTICS=false`).
- **Admin-Only & Bounded**: Diagnostic endpoints are restricted to authenticated admin sessions, bounded in time (TTL max 24 hours), and bounded in volume (ring-buffer max 100 entries).
- **Mandatory Redaction**: Headers and body payloads pass through a redaction sanitizer before storage:
  - Headers `Authorization`, `Cookie`, `Set-Cookie`, `x-api-key`, `Proxy-Authorization` stripped or masked to `[REDACTED]`.
  - JSON body fields matching `*token*`, `*key*`, `*secret*`, `*credential*`, `*email*` replaced with SHA-256 fingerprint prefix or `[REDACTED]`.

### 1.3 Fail-Closed Endpoint Enforcement
- Undocumented or private upstream endpoints (Claude OAuth, Codex WHAM, Antigravity/Gemini `v1internal`) MUST fail closed by default unless explicitly enabled via administrative configuration.
- Missing credentials, invalid `auth_index`, or missing GCP `project_id` immediately return structured errors without attempting network calls or retrying.

---

## 2. Functional RED/GREEN Tests (Scenarios & Invariants)

Each scenario MUST be codified as a deterministic unit/integration test in `internal/quota/test/` before implementation:

### 2.1 Model & Envelope Invariants
- **Multi-Window Isolation (`TestQuota_MultiWindow_Independent`)**:
  - *Scenario*: Provider returns 5-hour limit and 7-day limit.
  - *Assertion*: Both windows exist independently; values are not summed, averaged, or overwritten.
- **Missing Values Not Zero (`TestQuota_MissingValues_NotZero`)**:
  - *Scenario*: Upstream response omits `remaining` or `limit`.
  - *Assertion*: Window fields are `nil`/omitted; never defaulted or coerced to `0` or `100%`.
- **Reset Semantics (`TestQuota_ResetSemantics_Classification`)**:
  - *Scenario*: Upstream returns ISO-8601 absolute timestamp, relative TTL seconds, or rolling window.
  - *Assertion*: Correctly classified into `absolute`, `relative`, `continuous`, or `unknown`.
- **Timestamp Separation (`TestQuota_Timestamp_Separation`)**:
  - *Scenario*: Telemetry is collected at `T1`, provider cycle reset is `T2`, previous success was `T0`.
  - *Assertion*: `observedAt == T1`, `resetAt == T2`, `lastSuccessAt == T0`.
- **Confidence Scoring (`TestQuota_Confidence_Scoring`)**:
  - *Scenario*: Parse complete official payload vs degraded/inferred payload.
  - *Assertion*: `confidence` reflects evidence quality (1.0 vs 0.5), independent of quota percentage.

### 2.2 Provider Adapter Capabilities
- **Claude Adapter (`TestQuota_Claude_CoreAndEnrichment`)**:
  - *Scenario A*: Primary usage succeeds, profile/organization call succeeds -> `status: "complete"`.
  - *Scenario B*: Primary usage succeeds, profile/plan call fails (500/timeout) -> `status: "partial"`, core usage preserved.
- **Codex Adapter (`TestQuota_Codex_CoreAndEnrichment`)**:
  - *Scenario A*: Core quota succeeds, reset-credit/subscription expiry succeeds -> `status: "complete"`.
  - *Scenario B*: Core quota succeeds, subscription expiry fails -> `status: "partial"`, core window preserved.
- **Antigravity Adapter (`TestQuota_Antigravity_ProjectIDRequirement`)**:
  - *Scenario A*: Missing `project_id` -> immediate validation failure, no network call.
  - *Scenario B*: Daily tier fails -> fall back to prod tier within Antigravity only.
- **Gemini CLI Adapter (`TestQuota_GeminiCLI_CodeAssistOptional`)**:
  - *Scenario A*: Core quota succeeds, Code Assist fails -> `status: "partial"`, core quota preserved.

### 2.3 Fallback, Timeouts & Error Handling
- **No Cross-Provider Fallback (`TestQuota_Fallback_NoCrossProvider`)**:
  - *Scenario*: Antigravity primary fails.
  - *Assertion*: Never fallback to Gemini CLI or Claude. Refresh marked failed.
- **Core Failure Behavior (`TestQuota_CoreFailure_NoFreshObservation`)**:
  - *Scenario*: Primary endpoint returns 401/502 or network error.
  - *Assertion*: Task marked `failed`; no fresh observation envelope published.
- **Stale Preservation (`TestQuota_StalePreservation_OnFailure`)**:
  - *Scenario*: Cache has valid data from `T0`; new refresh at `T1` fails.
  - *Assertion*: Cache retains `T0` data with `freshness: "stale"`, `lastSuccessAt: T0`, `status: "failed"`, and error details attached.
- **Non-Retryable Mutating Operations (`TestQuota_MutatingOperations_NoRetry`)**:
  - *Scenario*: Quota reset action (`claude_reset.go`, `reset.go`) encounters timeout or 500.
  - *Assertion*: Zero automatic retries; returns explicit error to caller.
- **Strict Bounded Timeouts (`TestQuota_Refresh_TimeoutEnforcement`)**:
  - *Scenario*: Upstream stalls.
  - *Assertion*: Primary call canceled at 20s context deadline; supplemental enrichment canceled at 10s.

### 2.4 Evidence Separation & Persistence
- **Evidence Domain Isolation (`TestQuota_EvidenceSeparation`)**:
  - *Scenario*: Quota observations written to `quota_cycles`.
  - *Assertion*: `usage_events` (CPA token telemetry) remains untouched and unmodified.
- **Serialization Backward Compatibility (`TestQuota_DTO_BackwardCompatibility`)**:
  - *Scenario*: Older client deserializes new `CachedQuotaItem` JSON.
  - *Assertion*: Unmarshals cleanly into legacy `QuotaRow` and `SubscriptionInfo` fields without errors.

---

## 3. UI Verification Gates

Frontend tests in `web/src/components/usage/credentials/test/` MUST verify:

### 3.1 Stale, Partial, and Error State Rendering
- **Stale State Display**:
  - Render explicit "Stale" status badge/pill with amber warning color.
  - Tooltip surfaces `lastSuccessAt` formatted timestamp and failure reason.
  - Quota bar remains visible with desaturated or muted styling; values are not cleared to blank.
- **Partial State Display**:
  - Render "Partial" indicator pill.
  - Popover/tooltip clearly enumerates valid windows vs unavailable enrichments.
- **Error State Display**:
  - Safe error banner or inline message using `error.safe_message` (no upstream stack traces or raw URLs).
  - Failed refresh with existing stale data preserves stale visual state + error indicator; failed refresh with no prior data renders clean empty-state error card.

### 3.2 Dark Theme Alignment & Impeccable Styling
- **Zero Raw Color Literals**: No hardcoded hex values (`#fff`, `#1e293b`). Use semantic CSS tokens (`var(--text-secondary)`, `var(--bg-card)`, or Tailwind dark classes `dark:bg-slate-800`, `dark:text-slate-100`).
- **Accessible Contrast**: All text, progress bars, and status pills achieve WCAG AA contrast ratio (>= 4.5:1 for normal text, >= 3:1 for UI bars) in both light and dark themes.

### 3.3 Zero Flashing & Layout Shift
- **Debounced Reconciliation**: Polling in `useQuotaCache.ts` debounces updates and reconciles in-place without unmounting DOM subtrees.
- **No Layout Shift (CLS = 0)**: Quota panel containers maintain fixed minimum heights; skeleton loaders preserve final bar dimensions.
- **Optimistic Retention**: During active polling/refreshing, existing quota bars remain rendered with a subtle refresh spinner; they are never wiped or replaced with skeleton flash.

---

## 4. Compatibility & Rollback Boundaries

### 4.1 API & DTO Compatibility
- Legacy consumers of `/api/v1/quota/cache` and `/api/v1/quota/refresh` receive identically structured response root envelopes (`data.quota`, `data.subscription`).
- New observation envelope fields (`status`, `confidence`, `resetSemantics`, `freshness`, `windows`) are serialized as additive optional properties.
- `/api/v1/quota/history/:auth_index` returns provider-neutral cycle history while keeping existing response key mappings for Codex consumers.

### 4.2 Database Migration & Schema Rollback
- **Strictly Additive**: All GORM auto-migrations (`internal/repository/migration`) add nullable columns only. No column renames, drops, or type alterations.
- **Forward Tolerance**: If application binary is rolled back to prior version, older binary safely ignores newly added columns in `quota_cycles` and `quota_percent_segments`.
- **Down-Migration Script**: Provide explicit SQL down-migration script in `deploy/migrations/` documenting `ALTER TABLE DROP COLUMN` steps if schema rollback is needed.

### 4.3 Operational Rollback & Kill Switches
- **Runtime Kill-Switch**: Support `QUOTA_ADAPTERS_ENABLED=false` environment flag to instantly bypass adapter execution and fall back to baseline CPA pass-through behavior.
- **Private Endpoint Guard**: `ALLOW_PRIVATE_ENDPOINTS=false` (default) guarantees that any unapproved private upstream endpoints are rejected at startup without redeployment.

---

## 5. Docker & Runtime Verification

Before merge, the following automated checks MUST pass in clean environment:

1. **`make verify-backend`**:
   - `go test -race -v -cover ./cmd/... ./internal/...`
   - Test suite coverage >= 80% on new adapter packages (`internal/quota/...`).
2. **`make verify-frontend`**:
   - `npm --prefix ./web ci`
   - `npm --prefix ./web run test` (vitest unit and component tests)
   - `npm --prefix ./web run lint` (eslint clean, zero warnings)
   - `npm --prefix ./web run typecheck` (tsc `--noEmit` clean, zero type errors)
   - `npm --prefix ./web run build` (vite production build succeeds)
3. **`make verify-docker`**:
   - `docker build -t cpa-usage-keeper:ci .`
   - Image builds multi-stage, runs as non-root unprivileged user, passes vulnerability scan.
4. **Compose Readiness & Smoke Verification**:
   - Stand up `deploy/docker-compose.example.yml` and `deploy/docker-compose.full.example.yml`.
   - Validate health check `/api/v1/health` responds 200 OK.
   - Startup gate successfully connects to SQLite/MySQL, executes additive migrations, and initiates bounded refresh worker without panic or deadlock.

---

## 6. Chained PR Slicing (< 400 lines per PR budget)

To maintain high review velocity, impeccable quality, and rigorous test coverage, the implementation is decomposed into 6 independent, sequential PR slices:

```
[PR 1: Core Domain & Types] (~320 lines)
       ↓
[PR 2: Provider Adapters & Mocks] (~360 lines)
       ↓
[PR 3: Security Redaction & Refresh Worker] (~350 lines)
       ↓
[PR 4: Persistence & Provider History API] (~310 lines)
       ↓
[PR 5: Frontend State & UI Indicators] (~370 lines)
       ↓
[PR 6: E2E Integration, Docker & Documentation] (~280 lines)
```

### PR 1: Core Domain Types & Envelope Invariants
- **Scope**: Define canonical observation models (`internal/quota/types.go`, normalization enums, reset semantics).
- **Files**: `internal/quota/types.go`, `internal/quota/normalize.go`, `internal/quota/test/normalizer_test.go`.
- **Budget**: ~320 lines (140 code / 180 tests).
- **Verification**: Invariant unit tests (multi-window, missing!=0, reset semantics, confidence).

### PR 2: Provider Adapters & Normalization Suite
- **Scope**: Implement Claude, Codex, Antigravity, and Gemini CLI adapters conforming to canonical envelope.
- **Files**: `internal/quota/{claude,codex,antigravity,gemini_cli}.go`, test fixtures in `internal/quota/test/`.
- **Budget**: ~360 lines (160 code / 200 tests).
- **Verification**: Complete/partial/failed response fixtures; same-provider fallback assertion.

### PR 3: Refresh Worker Integration & Security Redaction
- **Scope**: Integrate adapters into `internal/quota/refresh.go`; implement diagnostic redaction filter and private endpoint fail-closed policy.
- **Files**: `internal/quota/refresh.go`, `internal/quota/upstream_responses.go`, `internal/quota/config.go`, `internal/quota/test/service_refresh_test.go`.
- **Budget**: ~350 lines (150 code / 200 tests).
- **Verification**: Secret-leak scanning test, 20s/10s timeout tests, non-retryable reset assertion.

### PR 4: GORM Persistence & Generalized History API
- **Scope**: Additive schema migrations for `quota_cycles`; generalize `/api/v1/quota/history/:auth_index`.
- **Files**: `internal/entities/quota_cycle.go`, `internal/repository/migration/`, `internal/api/quota.go`, `internal/api/router.go`.
- **Budget**: ~310 lines (130 code / 180 tests).
- **Verification**: Additive migration test, backward-compatible history DTO tests, zero-telemetry contamination assertion.

### PR 5: Frontend State, Dark Theme & No-Flash UI
- **Scope**: Extend TypeScript interfaces; update `useQuotaCache.ts` and credential panels for stale/partial/error states with zero flash and dark theme alignment.
- **Files**: `web/src/lib/types.ts`, `web/src/components/usage/credentials/{useQuotaCache.ts,CredentialQuotaPanel.tsx,QuotaBar.tsx}`, tests in `web/src/components/usage/credentials/test/`.
- **Budget**: ~370 lines (170 code / 200 tests).
- **Verification**: Vitest component tests for stale badge, partial pill, error banner, dark mode styling, and CLS=0.

### PR 6: Full Verification, Docker Compose & Verification Docs
- **Scope**: Docker compose integration smoke tests, CI verification workflow updates, operator runbooks.
- **Files**: `Makefile`, `deploy/docker-compose.*`, `docs/`, CI workflow configs.
- **Budget**: ~280 lines.
- **Verification**: Full `make verify`, `make verify-docker`, Compose boot test.

---

## Alternatives Rejected

1. **Single Monolithic PR**:
   - Rejected: Implementing all adapters, persistence, and UI in one PR would exceed 2,000 lines, violating reviewability and TigerStyle safety discipline.
2. **Client-Side Credential Handling or Token Passing**:
   - Rejected: Severe security risk. Exposing raw provider tokens or allowing client-side auth bypasses CPA security boundaries.
3. **Persisting Raw Upstream Diagnostics in Production by Default**:
   - Rejected: High risk of credential leakage in logs or database. Bounded redaction with opt-in admin debugging is mandatory.
4. **Coercing Missing Windows or Enrichments to Zero**:
   - Rejected: Violates evidence integrity. Missing values must remain missing, and enrichment failure yields explicit `partial` status.

---

## Affected Paths

- `internal/quota/` (`types.go`, `refresh.go`, `normalize.go`, `config.go`, `upstream_responses.go`, `claude.go`, `codex.go`, `antigravity.go`, `gemini_cli.go`)
- `internal/quota/test/` (unit and integration tests for all adapters, timeouts, and invariants)
- `internal/entities/` (`quota_cycle.go`, `quota_percent_segment.go`)
- `internal/repository/migration/` (additive GORM migrations)
- `internal/api/` (`quota.go`, `router.go`)
- `web/src/lib/` (`types.ts`, `api.ts`)
- `web/src/components/usage/credentials/` (`useQuotaCache.ts`, `CredentialsSection.tsx`, `CredentialQuotaPanel.tsx`, `QuotaBar.tsx`, `CodexQuotaHistoryPanel.tsx`)
- `web/src/components/usage/credentials/test/` (vitest component tests)
- `Makefile`, `Dockerfile`, `deploy/docker-compose.*.yml`
