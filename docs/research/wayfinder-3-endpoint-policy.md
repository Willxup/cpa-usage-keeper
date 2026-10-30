# Wayfinder decision: credential acquisition and endpoint policy

## Decision
Keeper MUST use an existing CPA credential identified by `auth_index`; it MUST NOT log in, scrape provider pages, read provider credential files, or receive provider access/refresh tokens. Provider secrets stay behind the server-side CPA management `api-call` seam.

Documented public provider APIs are eligible. Existing Claude OAuth, Codex WHAM, and Antigravity/Gemini `v1internal` paths are private/undocumented client protocols and MUST fail closed by default. An official hostname does not make a private path an official contract.

Raw upstream responses remain disabled by default. Optional diagnostics MUST be admin-only, bounded, allowlisted/redacted, and short-lived; no cookies, authorization values, or sensitive body fields may be persisted or logged.

Use the existing bounded refresh deadlines (20 seconds overall, 10 seconds for supplemental calls) with one attempt per endpoint. Do not retry mutating reset operations. Missing credentials, missing project metadata, private/unapproved endpoint, timeout, or primary failure produces no fresh quota. Optional enrichment failure may omit only that enrichment and preserve valid core usage.

## Evidence

- CPA credential and management seams: `internal/quota/service.go:310-317`, `internal/cpa/dto/apicall/api_call.go:8-16`, `internal/cpa/client.go:137-155,495-499`.
- Endpoint definitions and bounded refresh behavior: `internal/quota/config.go`, `internal/quota/refresh.go`, `internal/quota/claude.go`, `internal/quota/antigravity.go`.
- Diagnostic response persistence: `internal/quota/upstream_responses.go`, `README.md:528`.
- Public sources: Anthropic usage report API (https://platform.claude.com/docs/en/api/beta/organization/usage_report/retrieve_messages), Claude Code auth (https://code.claude.com/docs/en/env-vars.md), OpenAI API overview (https://developers.openai.com/api/reference/overview), OpenAI organization usage (https://developers.openai.com/api/reference/resources/admin/subresources/organization/subresources/usage/), Gemini CLI authentication (https://raw.githubusercontent.com/google-gemini/gemini-cli/refs/heads/main/docs/get-started/authentication.mdx), Gemini CLI terms (https://raw.githubusercontent.com/google-gemini/gemini-cli/refs/heads/main/docs/resources/tos-privacy.md), and Google quotas (https://docs.cloud.google.com/gemini/docs/quotas).

## Acceptance criteria

- Endpoint templates are classified public/private and private execution is rejected by default.
- Tests prove server-side-only credential handling, redaction, missing credential/project failure, timeout, optional degradation, and no fresh quota after primary failure.
- Mutating operations have no hidden retry.
- No secret-bearing browser response, log, or routine diagnostic is possible.
