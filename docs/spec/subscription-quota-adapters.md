# Specification: Subscription Quota Adapters & UI Extension for CPA Usage Keeper

## 1. Overview & Destination
Extend CPA Usage Keeper (`darkrei08/cpa-usage-keeper`) with a provider-neutral subscription quota adapter architecture and Keeper-like dashboard interface.

This specification consolidates the decisions established across the Wayfinder map (`#1`) and its child tickets (`#2` through `#6`):
- **Quota Model (`#2`)**: Canonical multi-window observation envelope with explicit freshness, reset semantics, confidence, and structured safe errors.
- **Endpoint Policy (`#3`)**: Server-side-only credential acquisition via CPA `auth_index`; fail-closed private endpoint protection; zero raw tokens in logs/responses; bounded redaction.
- **Persistence & API/UI Seams (`#4`)**: Reuse GORM `quota_cycles` and `quota_percent_segments`; additive migrations only; backward-compatible API; reuse existing credential cards and drawers.
- **Provider Capability Matrix (`#5`)**: Initial guaranteed slice for `claude`, `codex`, `antigravity`, and `gemini-cli`; same-provider-only fallback; explicit partial success; stale retention.
- **Verification & Slices (`#6`)**: Strict security leak gates; RED/GREEN invariant tests; WCAG AA dark theme with zero CLS; 6 chained reviewable work units (<400 lines).

---

## 2. Domain Model & Invariants

### 2.1 Separation of Evidence Domains
1. **Subscription Quota Telemetry**: Dynamic snapshots of provider limits and consumption (`used`, `remaining`, `limit`, `percent`, `resetAt`).
2. **CPA Request/Token Telemetry**: Micro-level proxy events in `usage_events` (requests, input/output tokens, cost, latency).
3. **Static Plan Limits**: Static facts about catalog limits; never mixed into observed telemetry.

### 2.2 Canonical Envelope Schema
```go
type SubscriptionObservation struct {
    Provider       string             `json:"provider"`
    IdentityType   string             `json:"identity_type"`
    AuthIndex      string             `json:"auth_index"`
    AccountID      *string            `json:"account_id,omitempty"`
    Source         string             `json:"source"`
    ObservedAt     time.Time          `json:"observed_at"`
    LastSuccessAt  *time.Time         `json:"last_success_at,omitempty"`
    ResetAt        *time.Time         `json:"reset_at,omitempty"`
    ResetSemantics ResetSemantics     `json:"reset_semantics"` // absolute | relative | continuous | unknown
    Freshness      FreshnessState     `json:"freshness"`       // fresh | stale | unknown
    Confidence     float64            `json:"confidence"`      // 0.0 to 1.0 (evidence quality)
    Status         ObservationStatus  `json:"status"`          // complete | partial | failed
    Error          *ObservationError  `json:"error,omitempty"`
    Windows        []QuotaWindowItem  `json:"windows"`
}

type QuotaWindowItem struct {
    Key            string         `json:"key"`
    Label          string         `json:"label"`
    Used           *float64       `json:"used,omitempty"`
    Remaining      *float64       `json:"remaining,omitempty"`
    Limit          *float64       `json:"limit,omitempty"`
    Unit           string         `json:"unit"`
    Percent        *float64       `json:"percent,omitempty"`
    ResetAt        *time.Time     `json:"reset_at,omitempty"`
    ResetSemantics ResetSemantics `json:"reset_semantics"`
}
```

### 2.3 Critical Invariants
- **Missing Values**: Must remain `nil`/omitted; never defaulted or coerced to `0` or `100%`.
- **Confidence**: Expresses evidence reliability, not percentage remaining.
- **Fail-Closed**: Private/undocumented upstream endpoints fail closed unless explicitly authorized in configuration.
- **No Cross-Provider Fallback**: A failure in Claude never cascades to Codex or Gemini.
- **Non-Retryable Resets**: Any mutating reset endpoint is attempted once; zero automatic retries.

---

## 3. Step-by-Step Implementation Task Graph (6 Chained Work Units)

```
[PR 1: Core Domain & Types] (Foundation)
       ↓
[PR 2: Provider Adapters & Normalization]
       ↓
[PR 3: Refresh Worker & Security Redaction]
       ↓
[PR 4: GORM Persistence & Generalized History API]
       ↓
[PR 5: Frontend State & Impeccable Dark UI]
       ↓
[PR 6: Verification, Docker & Documentation]
```

### Work Unit 1: Core Domain Types & Envelope Invariants
- **Goal**: Establish canonical Go structs and normalization methods for provider-neutral subscription observations.
- **Paths**: `internal/quota/types.go`, `internal/quota/normalize.go`, `internal/quota/test/normalizer_test.go`.
- **Tests**: Table-driven tests asserting multi-window isolation, missing values as `nil`, timestamp separation, and reset classification.
- **Budget**: ~320 lines.

### Work Unit 2: Provider Adapters & Normalization Suite
- **Goal**: Implement adapters for Claude, Codex, Antigravity, and Gemini CLI conforming to the canonical envelope.
- **Paths**: `internal/quota/{claude,codex,antigravity,gemini_cli}.go`, `internal/quota/test/*_test.go`.
- **Tests**: Unit tests with recorded fixtures for complete, partial (enrichment failed), and failed responses.
- **Budget**: ~360 lines.

### Work Unit 3: Refresh Worker Integration & Security Redaction
- **Goal**: Wire adapters into `internal/quota/refresh.go`, add redaction filters for diagnostics, and enforce bounded timeouts (20s/10s).
- **Paths**: `internal/quota/refresh.go`, `internal/quota/upstream_responses.go`, `internal/quota/config.go`, `internal/quota/test/service_refresh_test.go`.
- **Tests**: Secret-leak scanning assertions, timeout enforcement tests, and non-retryable reset verification.
- **Budget**: ~350 lines.

### Work Unit 4: GORM Persistence & Generalized History API
- **Goal**: Additive migrations for `quota_cycles`; generalize `/api/v1/quota/history/:auth_index` across providers while preserving backward compatibility.
- **Paths**: `internal/entities/quota_cycle.go`, `internal/repository/migration/`, `internal/api/quota.go`, `internal/api/router.go`.
- **Tests**: Migration tests, backward-compatible DTO serialization tests, zero-telemetry contamination assertions.
- **Budget**: ~310 lines.

### Work Unit 5: Frontend State, Dark Theme & No-Flash UI
- **Goal**: Extend React types, update `useQuotaCache.ts` and credential components for stale/partial/error states, dark theme WCAG AA contrast, and zero layout shift.
- **Paths**: `web/src/lib/types.ts`, `web/src/components/usage/credentials/{useQuotaCache.ts,CredentialQuotaPanel.tsx,QuotaBar.tsx,CodexQuotaHistoryPanel.tsx}`.
- **Tests**: Vitest component tests covering stale badge, partial pill, error banner, dark mode styling, and CLS=0.
- **Budget**: ~370 lines.

### Work Unit 6: Full Verification, Docker Compose & Documentation
- **Goal**: Run complete test suites (`make verify`, `make verify-docker`), add Compose smoke verification, and document operator runbooks.
- **Paths**: `Makefile`, `deploy/docker-compose.*.yml`, `docs/`.
- **Verification**: Zero failed checks in backend tests, frontend tests/lint/build, and container builds.
- **Budget**: ~280 lines.

---

## 4. Verification & Operational Acceptance Criteria

1. **Secret Scanning**: Zero raw credentials in API responses, logs, or diagnostic ring buffers.
2. **Quality Gates**:
   - `make verify-backend`: Go tests with `-race` pass with >= 80% coverage on new packages.
   - `make verify-frontend`: `npm test`, `npm run lint`, `npm run typecheck`, and `npm run build` all pass cleanly.
   - `make verify-docker`: Container image builds and passes unprivileged user check.
3. **Rollback Safety**:
   - `QUOTA_ADAPTERS_ENABLED=false` provides instant operational bypass.
   - Database migrations are strictly additive with forward compatibility.
