# Wayfinder decision: initial provider capability and fallback matrix

## First slice
Cover `claude`, `codex`, `antigravity`, and `gemini-cli`. `quota` is the only guaranteed capability, and only when the primary endpoint responds with a parseable payload. Profile, plan, reset credits, Code Assist, and subscription enrichment are best-effort. Existing Kimi and xAI runtime support remains outside this slice.

| Provider | Canonical ID / aliases | Core | Optional behavior |
| --- | --- | --- | --- |
| Claude | `claude` | usage | profile/plan failure preserves usage |
| Codex | `codex` | usage | reset credit/subscription expiry failure preserves usage |
| Antigravity | `antigravity` | quota with `project_id` | subscription fallback daily -> prod |
| Gemini CLI | `gemini-cli` | quota with `project_id` | Code Assist failure preserves quota |

Do not treat `gemini` or `gemini-cli-code-assist` as quota aliases without CPA evidence. Kimi aliases (`kimi`, `kimi-ai`, `kimi.ai`, `kimi.com`) and xAI remain governed by their existing runtime handlers.

## Fallback and partial-success rules

- Fallback is only between endpoints of the same capability and provider; never cross-provider.
- Publish valid components, never turn missing data into zero or synthesize windows.
- If core quota fails, the refresh fails and no fresh observation is published.
- If enrichment fails, publish core quota with explicit partial status.
- Preserve the last valid observation on refresh failure only as stale, with `observedAt`, `lastSuccessAt`, error, and confidence. Do not show stale data as current.
- Add a future adapter only when CPA identity is verified, credentials remain server-side, core capability and endpoint policy are defined, normalized schema/freshness/error semantics exist, and empty/partial/error payload tests are present.

## Evidence

- Registry and alias resolution: `internal/quota/registry.go`, `internal/quota/service.go`, `internal/quota/test/registry_test.go`.
- Provider behavior: `internal/quota/{claude,codex,antigravity,gemini_cli,kimi,xai}.go`.
- Cache behavior: `internal/quota/refresh.go`, `web/src/components/usage/credentials/useQuotaCache.ts`.
- Official quota context: OpenAI Codex (https://help.openai.com/en/articles/11369540-using-codex-with-your-chatgpt-plan), Anthropic Claude Code (https://support.claude.com/en/articles/11145838-use-claude-code-with-your-pro-or-max-plan), Google Gemini (https://docs.cloud.google.com/gemini/docs/quotas), xAI FAQ (https://docs.x.ai/grok/faq), and Kimi membership rules (https://www.kimi.com/en/help/membership/membership-update-rules).
