# Wayfinder decision: persistence and API/UI integration seams

## Decision
Retain and extend the existing canonical GORM and API seams rather than introducing a parallel quota/history subsystem or second UI.

1. **Persistence & Schema**:
   - Reuse `quota_cycles` and `quota_percent_segments` (already provider-neutral via `entities.QuotaCycle` with `provider`, `auth_index`, `quota_key`, `window_seconds`, `reset_at_source`, and `quota_percent_segments`).
   - Additive migration only: if observation metadata fields (such as `source`, `confidence`, `status`, `reset_semantics`) require persistence beyond the transient refresh cache, add optional columns to `quota_cycles` or an adjacent lightweight table `subscription_quota_observations`. No schema rewrites or table drops.
   - CPA request/token/cost telemetry remains in `usage_events` and `usage_overview_*` tables; do not duplicate telemetry into quota observation tables.

2. **Migration & Backward Compatibility**:
   - Zero-downtime additive migrations following `internal/repository/migration` timestamp conventions.
   - Existing Codex and Claude quota histories remain intact.
   - Backward-compatible JSON serialisation: keep existing `CheckResponse`, `CachedQuotaItem`, and `CodexQuotaHistoryResponse` fields while adding optional typed fields (`status`, `confidence`, `freshness`, `windows`, `resetSemantics`).

3. **API Routes & Response Shapes**:
   - Preserve existing `/api/v1/quota/cache` (POST) and `/api/v1/quota/refresh` (POST) as the primary batch/single read and trigger points.
   - Preserve `/api/v1/quota/refresh/:auth_index` (GET) for polling async task status.
   - Extend `CachedQuotaItem` and `CheckResponse` envelopes with canonical observation fields (`status`, `freshness`, `confidence`, `resetSemantics`) without breaking existing web consumers expecting `quota: QuotaRow[]` and `subscription: SubscriptionInfo`.
   - Generalize `/api/v1/quota/history/:auth_index` (GET) across providers using the provider-neutral `quota_cycles` model, keeping backward-compatible cycle/window field mappings.

4. **Refresh Orchestration**:
   - Use the existing `internal/quota/refresh.go` in-memory task runner and worker pool (`RefreshWorkerLimit`, `refreshTasks` map, debounce and deduplication).
   - Core quota refresh remains bounded (20s overall, 10s for supplemental/subscription enrichment).
   - Same-provider fallback: core failure fails the task (no fake zero observation); enrichment failure yields `status: "partial"`. Stale observations remain explicitly marked `freshness: "stale"`.

5. **UI Surface & Integration Seams**:
   - Do NOT duplicate existing quota/history components.
   - In `CredentialsSection.tsx`, reuse `CredentialQuotaPanel`, `QuotaBar`, and `AntigravityQuotaGroup` to render normalized multi-window rows.
   - In `useQuotaCache.ts`, retain the current authIndex polling and cache reconciliation logic; consume extended observation metadata (status, stale warning, confidence).
   - In `CredentialDetailDrawer.tsx`, reuse the quota history tab and generalize `CodexQuotaHistoryPanel` (or rename/alias it as a unified `QuotaHistoryPanel`) to render efficiency and cycle history for any provider backed by `quota_cycles`.

## Alternatives Rejected

1. **Separate Subscription Table & Endpoints (`/api/v1/subscriptions/...`)**:
   - Rejected: Duplicates existing quota cache, polling lifecycle, and credential UI wiring. Keeper already associates credentials by `auth_index`.
2. **Replacing In-Memory Refresh Queue with Redis / DB Task Table**:
   - Rejected: Adds unnecessary operational complexity. The current bounded in-memory worker with debounce and task deduping in `internal/quota/refresh.go` meets all latency and throughput requirements.
3. **Dedicated UI Views / Second Dashboard for Subscription Observations**:
   - Rejected: Violates Wayfinder map #1 constraint ("Extend CPA Usage Keeper rather than create a new dashboard"). The existing `CredentialsSection` and `CredentialDetailDrawer` are the natural home for credential quota and cycle history.

## Affected Paths

- **Backend Persistence**:
  - `internal/entities/quota_cycle.go`
  - `internal/entities/quota_percent_segment.go`
  - `internal/repository/migration/`
  - `internal/repository/codex_quota_history.go` (and provider-neutral query extensions)
- **Backend Service & API**:
  - `internal/quota/service.go`
  - `internal/quota/refresh.go`
  - `internal/quota/types.go`
  - `internal/quota/normalize.go`
  - `internal/api/quota.go`
  - `internal/api/router.go`
- **Frontend State & UI**:
  - `web/src/lib/types.ts`
  - `web/src/lib/api.ts`
  - `web/src/components/usage/credentials/useQuotaCache.ts`
  - `web/src/components/usage/credentials/CredentialsSection.tsx`
  - `web/src/components/usage/credentials/CredentialDetailDrawer.tsx`
  - `web/src/components/usage/credentials/CodexQuotaHistoryPanel.tsx`

## Acceptance Criteria

- Existing `/api/v1/quota/*` routes remain fully backward-compatible.
- GORM migrations are strictly additive with zero data loss or breaking changes for existing Codex/Claude cycle history.
- Telemetry facts (`usage_events`) and quota observation facts (`quota_cycles`) remain strictly separated.
- Stale, partial, and failed states are correctly represented in API responses and reflected in existing UI indicators without layout regression.
- No second dashboard or duplicated quota panels are introduced.
