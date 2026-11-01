# Wayfinder decision: canonical subscription quota observation model

## Decision
Use one provider-neutral observation envelope with:

- `provider`, `identityType`, and optional `accountId`/CPA `authIndex` identity;
- `source`, `observedAt`, `lastSuccessAt`, `resetAt`, `resetSemantics`, `freshness`, `confidence`;
- `status`: `complete`, `partial`, or `failed`;
- structured `error` (code, message safe for UI, retryable);
- zero or more quota windows, each containing `key`, `label`, `used`, `remaining`, `limit`, `unit`, `percent`, `resetAt`, `resetSemantics`, and window metadata.

`resetSemantics` MUST distinguish `absolute`, `relative`, `continuous`, and `unknown`. Missing values remain missing; they MUST NOT be converted to zero. A failed refresh MAY retain the last valid observation only when it is marked stale and keeps the failure metadata.

## Examples

- Complete: all required identity/source/timestamps exist and every advertised window is parseable.
- Partial: the core window is valid but optional profile/plan/enrichment is absent; status is `partial` and the missing field is explicit.
- Stale: the last valid observation remains visible with `freshness=stale`, `lastSuccessAt`, current error, and confidence; it is never displayed as current.
- Failed: no valid observation exists; status is `failed`, windows are empty, and the structured error is retained.

## Invariants

1. Subscription observations remain separate from CPA request/token/cost telemetry and static plan facts.
2. The model supports multiple independent windows without summing or interpolating missing windows.
3. `observedAt` describes collection time; `resetAt` describes the provider window and may be unknown.
4. Confidence describes evidence quality, not remaining percentage.
5. Provider adapters never fabricate a fresh observation from a private or unavailable endpoint.

## Evidence

- Existing seam: `internal/quota/registry.go`, `internal/quota/service.go`, `internal/quota/types.go`.
- Existing multi-window and reset structures: `internal/entities/quota_cycle.go`, `internal/repository/dto/quota_window.go`.
- Existing stale/cache behavior: `internal/quota/refresh.go` and `web/src/components/usage/credentials/useQuotaCache.ts`.
- Provider quota semantics are dynamic and windowed: OpenAI Codex plan limits (https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan), Anthropic Claude Code limits (https://support.claude.com/en/articles/11145838-use-claude-code-with-your-pro-or-max-plan), and Google Gemini quotas (https://docs.cloud.google.com/gemini/docs/quotas).
