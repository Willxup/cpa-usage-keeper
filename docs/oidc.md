# OIDC Administrator Login

[Back to README](../README.md) · [简体中文](./oidc.zh.md)

OIDC is an optional administrator login method, disabled by default. It is available only on the standalone administrator login page, not for API Key login or CPAMC embedding. Password login remains available.

## Setup

1. Register a confidential server-side client with a secret, enable Authorization Code Flow with PKCE S256, and allow Keeper's requested scopes: `openid profile email`.
2. Register Keeper's public callback URL: `https://keeper.example.com/api/v1/auth/oidc/callback` for a root deployment, or `https://keeper.example.com/keeper/api/v1/auth/oidc/callback` for the `/keeper` base path.
3. Configure the variables below and administrator access, then restart Keeper. Open Keeper at the same public origin as the callback and select OIDC login.

## Configuration

Keep `AUTH_ENABLED=true` and a private `LOGIN_PASSWORD` so password login remains available during provider outages.

| Variable | Requirement when enabled | Description |
| --- | --- | --- |
| `AUTH_OIDC_ENABLED` | Set to `true` | Defaults to `false` |
| `AUTH_OIDC_ISSUER` | Required | Full provider-advertised issuer, including any tenant path |
| `AUTH_OIDC_CLIENT_ID` | Required | Registered client ID |
| `AUTH_OIDC_CLIENT_SECRET` | Required | Client secret |
| `AUTH_OIDC_REDIRECT_URL` | Required | Public callback URL, exactly matching the provider registration |
| `AUTH_OIDC_ALLOWED_SUBJECTS` | Optional | JSON array of exact, case-sensitive `sub` values; defaults to `[]` |
| `AUTH_OIDC_PROMPT_LOGIN` | Optional | Defaults to `true`, sending `prompt=login` to request reauthentication; `false` allows reuse of the provider's login session |

## Restrict Administrator Access

**If `AUTH_OIDC_ALLOWED_SUBJECTS` is omitted or empty, everyone who authenticates for this client becomes a Keeper administrator.** Restrict application access at the provider or configure a subject allowlist:

```env
AUTH_OIDC_ALLOWED_SUBJECTS='["user-123","user-456"]'
```

Use the actual `sub` issued to this client, not an email, display name, or assumed user ID.

A nonempty legacy `AUTH_OIDC_ALLOWED_USERS` prevents startup. Replace it with actual subjects or enforce equivalent access restrictions at the provider before removing the old setting.

## Sessions And Sign-Out

- `AUTH_SESSION_TTL` controls session lifetime. Sessions can be revoked in Session Management.
- Signing out revokes only the Keeper session, not the provider session. If provider session reuse is allowed, the next login may complete automatically.
- Disabling OIDC, changing issuer/client ID, or tightening the subject allowlist revokes affected sessions after restart. Changing only the client secret or `AUTH_OIDC_PROMPT_LOGIN` does not revoke existing sessions.
- Provider-side account removal or access restrictions do not automatically revoke Keeper sessions; revoke them manually or wait for expiry.
- Before downgrading to a Keeper version without OIDC support, revoke all OIDC sessions, then stop the service and replace the binary.

## Networking And Troubleshooting

- **Addresses and connectivity:** The issuer must exactly match Discovery and the ID Token's `iss`; do not substitute a container-internal address. Keeper must reach Discovery, Token, and JWKS endpoints; browsers must reach the authorization endpoint.
- **HTTPS:** Use HTTPS for all related addresses. HTTP is accepted but cannot prevent eavesdropping or tampering with Discovery/JWKS. Internal CAs must be trusted by the service; HTTPS failures never downgrade or bypass verification.
- **Reverse proxies:** Preserve the public `Host` and `X-Forwarded-Proto` (or supply `X-Forwarded-Host`), and configure actual proxy networks in `TRUSTED_PROXY_CIDRS`. Filter callback query parameters from access logs to avoid recording authorization codes and state.
- **Interrupted login:** Login attempts expire after five minutes. Click the login button again after a timeout or service restart.

## Compatibility

Supports one identity provider, Discovery/JWKS, signed ID Tokens, and `client_secret_basic` / `client_secret_post` authentication. Public clients, `form_post`, JWE, JWT client authentication, multi-issuer selection, and global single logout are not supported.
