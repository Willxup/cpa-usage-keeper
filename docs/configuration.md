# Advanced Configuration

[Back to README](../README.md) · [简体中文](./configuration.zh.md)

See the README's [configuration section](../README.md#configuration) for common settings and [`.env.example`](../.env.example) for the complete variable template.

## Web Access And Reverse Proxy

### Listening And Base Paths

`--host` overrides `APP_HOST`; when neither is set, Keeper listens on all available interfaces. Keep `APP_HOST` empty for Docker/Compose and use `127.0.0.1:8080:8080` port mapping for host-only access.

`APP_BASE_PATH` must be empty or start with `/`; trailing `/` is removed. Reverse proxies must preserve the prefix; see the [Nginx example](../README.md#nginx-reverse-proxy).

### CPA Addresses

- `CPA_BASE_URL`: used by the Keeper server; private addresses and Docker service names are valid.
- `CPA_PUBLIC_URL`: used by browsers for "Back to CPA" and cross-origin embedding. Leave it empty for same-origin CPA at `/management.html`; set it when domains, ports, or paths differ.
- Cross-origin embedding requires a complete HTTP/HTTPS URL with a host in `CPA_PUBLIC_URL`; relative paths affect navigation only.

### Proxy Trust

Only local loopback and proxies in `TRUSTED_PROXY_CIDRS` can provide trusted `X-Forwarded-For` values. Local Nginx needs no extra settings; configure actual proxy networks for containers, remote proxies, or CDNs. Do not copy example networks or trust all sources; universal CIDRs are rejected.

## Optional Compose Environment File

The combined CPA + Keeper template can also load settings from `./keeper/.env`; a missing file does not prevent startup. `environment` overrides `env_file`: remove corresponding entries, including empty placeholders, from Compose's `environment` to use the file's values.

## Built-In HTTPS

Usually, terminate HTTPS at a reverse proxy. Use these settings when Keeper must serve HTTPS directly.

For native deployments, relative certificate paths resolve against the environment file read by Keeper. With Compose, mount the certificate and key and use container paths; `env_file` does not mount files.

| Variable | Default | Description |
| --- | --- | --- |
| `TLS_ENABLED` | `false` | Set to `true` to enable built-in HTTPS |
| `TLS_CERT_FILE` | Empty | Required when enabled; certificate file path |
| `TLS_KEY_FILE` | Empty | Required when enabled; private key file path |

## Credentials Quota Refresh

Configure ordinary scheduled refresh in the Credentials inspection dialog. These optional settings adjust concurrency or enable debugging.

| Variable | Default | Description |
| --- | --- | --- |
| `QUOTA_REFRESH_WORKER_LIMIT` | `10` | Shared concurrency limit for manual and scheduled refresh, capped at `100` |
| `QUOTA_UPSTREAM_RESPONSES_ENABLED` | `false` | Cache each credential's latest raw quota responses for browser Network debugging; may contain account data |

## Redis Queue Advanced Settings

All variables below are optional.

| Variable | Default | Description |
| --- | --- | --- |
| `REDIS_QUEUE_ADDR` | Derived from `CPA_BASE_URL` | An explicit value takes precedence. Otherwise, use the hostname and explicit port from `CPA_BASE_URL`, or `8317` if no port is specified. Set `host:port` for a separate address |
| `REDIS_QUEUE_TLS` | `false` | `true` forces TLS; when no queue address is specified, TLS follows the `CPA_BASE_URL` protocol. `false` does not override automatically inferred TLS |
| `REDIS_QUEUE_BATCH_SIZE` | `10000` | Maximum records per pull |
| `REDIS_QUEUE_IDLE_INTERVAL` | `1s` | Empty queue check interval |

## Session And Message Retention

`AUTH_SESSION_TTL` controls login session lifetime. See [OIDC sessions](./oidc.md#sessions-and-sign-out) for OIDC sign-out and revocation rules. Redis inbox messages are retained through the current day after success or for 7 days after failure.

## Storage And Log Details

See the README's [storage, logs, and backups](../README.md#storage-logs-and-backups) section for common variables.

- **Archiving:** Every day at 04:30 in the configured timezone, Keeper archives raw requests older than 90 local calendar days. Normal pages do not query archive details directly.
- **Deletion:** Archives are retained indefinitely by default. With `USAGE_RAW_RETENTION_DAYS>=90`, expired archives are removed by request date without changing the recent 90-day window. Historical aggregates and backups have separate retention policies.
- **Impact:** Deleted raw records cannot support historical recalculation, and the SQLite file may not immediately shrink.

File logs use `cpa-usage-keeper-YYYY-MM-DD.log`. Error, fatal, and panic entries also go to `cpa-usage-keeper-error-YYYY-MM-DD.log`; error logs retain 30 previous local calendar dates plus the current date.
