<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/keeper-logo-dark.svg" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/keeper-logo-light.svg" />
    <img src="./assets/keeper-logo-light.svg" alt="Keeper" width="560" />
  </picture>
</p>

<p align="center">
  <a href="./README.md"><strong>English</strong></a> ｜ <a href="./README.zh.md">简体中文</a>
</p>

<h1 align="center">CPA Usage Keeper</h1>

<p align="center">Every flow leaves a trace.</p>

<p align="center">
  <a href="https://github.com/Willxup/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/github/v/release/Willxup/cpa-usage-keeper?style=flat-square" alt="Latest release" /></a>
  <a href="https://github.com/Willxup/cpa-usage-keeper/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/Willxup/cpa-usage-keeper/ci.yml?branch=main&amp;style=flat-square&amp;label=CI" alt="CI status" /></a>
  <a href="https://github.com/Willxup/cpa-usage-keeper/pkgs/container/cpa-usage-keeper"><img src="https://img.shields.io/badge/Docker-GHCR-2496ED?style=flat-square&amp;logo=docker&amp;logoColor=white" alt="Docker image on GHCR" /></a>
  <a href="https://github.com/Willxup/homebrew-cpa-usage-keeper"><img src="https://img.shields.io/badge/Homebrew-supported-FBB040?style=flat-square&amp;logo=homebrew&amp;logoColor=black" alt="Homebrew supported" /></a>
  <a href="https://github.com/Willxup/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/badge/Linux-FCC624?style=flat-square&amp;logo=linux&amp;logoColor=black" alt="Linux supported" /></a>
  <a href="https://github.com/Willxup/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/badge/macOS-A2AAAD?style=flat-square&amp;logo=apple&amp;logoColor=black" alt="macOS supported" /></a>
  <a href="https://github.com/Willxup/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/badge/Windows-0078D4?style=flat-square&amp;logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZmlsbD0iI2ZmZiIgZD0iTTIgMy41IDExIDJ2OUgyem0xMC0xLjdMMjIgLjNWMTFIMTJ6TTIgMTJoOXY5TDIgMTkuNXptMTAgMGgxMHYxMC43bC0xMC0xLjV6Ii8+PC9zdmc%2B" alt="Windows supported" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/Willxup/cpa-usage-keeper?style=flat-square" alt="MIT License" /></a>
</p>

CPA Usage Keeper is a standalone persistence and analytics dashboard for [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI). It stores CPA usage in SQLite, pulls CPA configuration and credential data, and provides views for usage, cost, request health, quotas, and model/API statistics.

## Screenshots

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/overview-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/overview-light.png" />
    <img src="./assets/screenshots/overview-light.png" alt="CPA Usage Keeper Overview" width="49%" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/analysis-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/analysis-light.png" />
    <img src="./assets/screenshots/analysis-light.png" alt="CPA Usage Keeper Analysis" width="49%" />
  </picture>
</p>
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/auth-files-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/auth-files-light.png" />
    <img src="./assets/screenshots/auth-files-light.png" alt="CPA Usage Keeper Auth Files" width="49%" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/ai-provider-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/ai-provider-light.png" />
    <img src="./assets/screenshots/ai-provider-light.png" alt="CPA Usage Keeper AI Provider" width="49%" />
  </picture>
</p>
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/ranking-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/ranking-light.png" />
    <img src="./assets/screenshots/ranking-light.png" alt="CPA Usage Keeper Ranking" width="49%" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/login-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/login-light.png" />
    <img src="./assets/screenshots/login-light.png" alt="CPA Usage Keeper Login" width="49%" />
  </picture>
</p>

## Features

- Persist CPA usage data in SQLite, with optional scheduled backups
- Track requests, tokens, cost, cache usage, success rate, RPM/TPM, and latency, with filters for time range, model, API Key, source, and result
- Inspect and export request-level events with configurable table columns
- Analyze usage trends, cost composition, model/API Key/AI Provider mix, hourly heatmaps, and latency diagnostics
- Monitor Auth Files and AI Providers with usage metrics, health inspection, and quota refresh
- Opt into community rankings by overall score, tokens, requests, cache rate, average TTFT/latency, or peak TPM/RPM
- Open a read-only usage view scoped to an individual CPA API Key
- Sync CPA Auth Files, API Keys, and AI Providers automatically, and maintain model pricing for cost estimates
- Deploy with Docker/Docker Compose, Homebrew, binaries, or systemd, with optional password protection
- Embed the Keeper dashboard in CPAMC through the CPA plugin

## Sponsors and Special Thanks

- Thanks to [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) for providing the upstream CPA foundation and data source this project builds on.
- Thanks to [@YouShouldBetOnMe](https://github.com/YouShouldBetOnMe) for supporting CPA Usage Keeper.
- Thanks to the CPA discussion group for their discussions and feedback.

## Quick Start

> Before using CPA Usage Keeper, make sure CPA usage statistics are enabled: `usage-statistics-enabled: true`.
>
> When multiple usage collectors share one CPA instance, ensure they all use subscription mode; otherwise, collection may stop or become incomplete.

Docker Compose is the recommended deployment method. Use the full stack when deploying CPA and Keeper together, or the Keeper-only stack when CPA already exists.

| Setup | Recommended path | Architectures |
| --- | --- | --- |
| New CPA + Keeper deployment | [Docker Compose: CPA + Keeper](#docker-compose-recommended) | `linux/amd64`, `linux/arm64` |
| Existing CPA deployment | [Docker Compose: Keeper only](#docker-compose-recommended) | `linux/amd64`, `linux/arm64` |
| Existing CPA, Docker CLI preferred | [Docker](#docker-cpa-already-runs-on-the-host) | `linux/amd64`, `linux/arm64` |
| macOS | [Homebrew](#macos-homebrew) | `amd64`, `arm64` |
| Linux without containers | [Linux binary](#linux-binary) | `amd64`, `arm64` |
| Windows | [Windows binary](#windows-binary) | `amd64`, `arm64` |

Login protection is enabled by default. Configure `LOGIN_PASSWORD` before starting Keeper, or explicitly set `AUTH_ENABLED=false` only when access is reliably isolated by the deployment environment.

## Benchmark

Production-style `linux/amd64` capacity measurements for sustained ingestion, Dashboard latency, CPU utilization, and Keeper cgroup peak memory are available in the [Capacity Benchmark Report](./internal/benchmark/REPORT.md).

## Project Structure

```text
cmd/server/              Application entry point
internal/api/            HTTP routes and handlers
internal/app/            Application wiring and startup
internal/auth/           Sessions and access control
internal/poller/         CPA usage and metadata synchronization
internal/repository/     SQLite persistence and aggregations
internal/service/        Usage, pricing, and identity services
internal/quota/          Provider quota refresh and inspection
internal/ranking/        Community ranking aggregation and sync
internal/benchmark/      Capacity suite, reports, manifests, and legacy microbenchmarks
deploy/linux/            systemd service template
web/                     React + TypeScript frontend
```

## Local Development

### Prerequisites

- Go 1.26+
- Node.js 24+
- npm
- A running [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) instance

### Run Locally

1. Copy `.env.example` to `.env`, then set at least `CPA_BASE_URL` and `CPA_MANAGEMENT_KEY`.

```bash
cp .env.example .env
vim .env
```

2. Start the backend.

```bash
go run ./cmd/server/main.go
```

3. In another terminal, install frontend dependencies and start the development server.

```bash
npm --prefix ./web ci
npm --prefix ./web run dev -- --host 127.0.0.1
```

Open `http://127.0.0.1:5173`. The frontend proxies `/api` to `http://127.0.0.1:8080`; override it with `VITE_API_PROXY_TARGET` when the backend uses another port.

### Tests

Run the full verification baseline:

```bash
make verify
```

Or run checks individually:

```bash
go test ./cmd/... ./internal/...
npm --prefix ./web run test
npm --prefix ./web run lint
npm --prefix ./web run typecheck
npm --prefix ./web run build
```

## Deployment

### Docker Compose (Recommended)

Docker Compose is recommended for both a complete CPA + Keeper stack and a Keeper-only deployment.

#### CPA + Keeper

Save the following as `docker-compose.yml`, then replace the management key and login password:

```yaml
services:
  cli-proxy-api:
    image: eceasy/cli-proxy-api:latest
    container_name: cli-proxy-api
    restart: unless-stopped
    ports:
      - "8317:8317"
      - "1455:1455"
    volumes:
      - ./cpa/config.yaml:/CLIProxyAPI/config.yaml
      - ./cpa/auths:/root/.cli-proxy-api
      - ./cpa/logs:/CLIProxyAPI/logs
    networks:
      - cpa-network

  cpa-usage-keeper:
    image: ghcr.io/willxup/cpa-usage-keeper:latest
    container_name: cpa-usage-keeper
    restart: unless-stopped
    depends_on:
      - cli-proxy-api
    ports:
      - "8080:8080"
    environment:
      TZ: Asia/Shanghai # Sets the container timezone; log timestamps use this timezone.
      CPA_BASE_URL: http://cli-proxy-api:8317
      CPA_MANAGEMENT_KEY: replace-with-your-management-key
      REDIS_QUEUE_ADDR: cli-proxy-api:8317
      AUTH_ENABLED: true
      LOGIN_PASSWORD: ${KEEPER_LOGIN_PASSWORD:?set KEEPER_LOGIN_PASSWORD}
    volumes:
      - ./keeper:/data
    networks:
      - cpa-network

networks:
  cpa-network:
    driver: bridge
```

Set `KEEPER_LOGIN_PASSWORD` in the shell or the Compose `.env` file before starting.

Run `docker compose up -d` to start the stack and `docker compose down` to stop it.

CPA data is stored under `./cpa`, and Keeper data is stored under `./keeper`.

#### Keeper Only

When CPA is already deployed, use the repository's Keeper-only Compose template:

```bash
cp docker-compose.example.yml docker-compose.yml
cp .env.example .env
vim .env
```

For CPA running on the Docker host, start with:

```env
CPA_BASE_URL=http://host.docker.internal:8317
CPA_MANAGEMENT_KEY=replace-with-your-management-key
AUTH_ENABLED=true
LOGIN_PASSWORD=
```

Set a private `LOGIN_PASSWORD` before starting the container.

Set `CPA_BASE_URL` to the reachable CPA address for other network layouts. If CPA uses a non-default Redis/RESP address, also set `REDIS_QUEUE_ADDR`.

Run `docker compose up -d` to start Keeper and `docker compose down` to stop it.

Keeper data is stored under `./data` by the provided template.

### Docker (CPA Already Runs On The Host)

Use the same `.env` values as the Keeper-only Compose setup when you prefer `docker run`:

```bash
docker run -d \
  --name cpa-usage-keeper \
  --add-host=host.docker.internal:host-gateway \
  -p 8080:8080 \
  -v "$(pwd)/keeper:/data" \
  --env-file .env \
  ghcr.io/willxup/cpa-usage-keeper:latest
```

### macOS Homebrew

Homebrew is the recommended macOS installation method:

```bash
brew tap Willxup/cpa-usage-keeper
brew install cpa-usage-keeper
```

Set `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`, and a private `LOGIN_PASSWORD`, then start the service:

```bash
vim "$(brew --prefix)/etc/cpa-usage-keeper.env"
brew services start cpa-usage-keeper
```

Upgrade and service commands:

```bash
brew services list
brew services restart cpa-usage-keeper
brew update
brew upgrade cpa-usage-keeper
```

Data is stored under `$(brew --prefix)/var/cpa-usage-keeper`; logs are written under `$(brew --prefix)/var/log/`.

### Linux Binary

Download the `linux_amd64` or `linux_arm64` archive from [Releases](https://github.com/Willxup/cpa-usage-keeper/releases/latest), then extract and run it:

```bash
mkdir -p cpa-usage-keeper
tar -xzf ./cpa-usage-keeper_*_linux_*.tar.gz -C cpa-usage-keeper --strip-components=1
cd cpa-usage-keeper
cp .env.example .env
vim .env
./cpa-usage-keeper
```

#### systemd

The Linux package includes a service template. Run these commands from the extracted package directory:

```bash
sudo cp cpa-usage-keeper.service /etc/systemd/system/cpa-usage-keeper.service
sudo sed -i "s|__CPA_USAGE_KEEPER_DIR__|$(pwd)|g" /etc/systemd/system/cpa-usage-keeper.service
sudo systemctl daemon-reload
sudo systemctl enable --now cpa-usage-keeper
```

```bash
sudo systemctl status cpa-usage-keeper
sudo journalctl -u cpa-usage-keeper -f
sudo systemctl restart cpa-usage-keeper
```

### Command-Line Options

The binary supports optional startup flags:

```bash
cpa-usage-keeper --host 127.0.0.1 # Override APP_HOST for this process.
cpa-usage-keeper -v               # Print the build version and exit; --version is also supported.
```

### Windows Binary

Download the `windows_amd64` or `windows_arm64` ZIP package from [Releases](https://github.com/Willxup/cpa-usage-keeper/releases/latest) and extract it. In PowerShell, open the extracted package directory and run:

```powershell
Copy-Item .env.example .env
notepad .env
.\cpa-usage-keeper.exe
```

Set `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`, and a private `LOGIN_PASSWORD` before starting. Authentication is enabled by default; set `AUTH_ENABLED=false` explicitly only for an isolated deployment.

## Configuration

Copy the example config:

```bash
cp .env.example .env
```

For first-time deployments, start with "Minimum required" and "Web access and reverse proxy". Most other settings can keep their defaults.

### Minimum Required

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `CPA_BASE_URL` | Yes | - | URL used by the Keeper server to call CPA. In Docker Compose this is usually `http://cli-proxy-api:8317`, and it can be a private address or container service name |
| `CPA_MANAGEMENT_KEY` | Yes | - | CPA management key used to read CPA management APIs |

### Web Access And Reverse Proxy

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `APP_HOST` | No | all interfaces | Keeper HTTP listen host; native deployments can set `127.0.0.1` for local-only access |
| `APP_PORT` | No | `8080` | Keeper HTTP listen port |
| `APP_BASE_PATH` | No | root path | Keeper subpath prefix, such as `/keeper`; empty means `/` |
| `CPA_PUBLIC_URL` | No | current browser origin root | Public CPA URL for the "Back to CPA" link and CPAMC frame trust |
| `TRUSTED_PROXY_CIDRS` | No | local loopback only | Additional reverse-proxy CIDRs allowed to provide `X-Forwarded-For`, separated by commas |

- The `--host` startup flag overrides `APP_HOST`. When neither is set, Keeper preserves its existing behavior and listens on all available network interfaces.
- For Docker/Compose, keep `APP_HOST` empty. To restrict access to the Docker host, publish the port as `127.0.0.1:8080:8080`.
- `APP_BASE_PATH` must be empty or start with `/`; `/cpa/` is normalized to `/cpa`.
- `CPA_BASE_URL` is the server-side CPA address and may use a private host or Docker service name.
- `CPA_PUBLIC_URL` controls browser navigation and cross-origin CPAMC frame trust. Leave it empty for same-origin `/management.html`, or set an explicit public CPA URL when domains, ports, or paths differ.
- Keeper trusts `X-Forwarded-For` only from local loopback and `TRUSTED_PROXY_CIDRS`. Direct clients cannot change their login-rate-limit source with this header. Configure only the exact proxy address or network; universal CIDRs are rejected.

For cross-origin CPAMC embedding, `CPA_PUBLIC_URL` must be a complete `http://` or `https://` URL with a host. Relative paths affect navigation only.

### Login Protection

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `AUTH_ENABLED` | No | `true` | Enable login protection |
| `LOGIN_PASSWORD` | When auth is enabled | - | Login password |
| `AUTH_SESSION_TTL` | No | `168h` | Login session lifetime |

### Timezone And Request Behavior

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `TZ` | No | `Asia/Shanghai` | Timezone used for statistics and display; Today, daily totals, page timestamps, log timestamps, and daily cleanup are calculated in this timezone |
| `REQUEST_TIMEOUT` | No | `30s` | Timeout for CPA HTTP requests and Redis queue operations |
| `TLS_SKIP_VERIFY` | No | `false` | Skip TLS certificate verification for CPA HTTPS and Redis queue TLS; enable only with self-signed certificates |

### Auth Files Quota Refresh

Scheduled Auth Files quota refresh is configured from the gear button in the Auth Files inspection dialog. The setting is stored in the local SQLite database and does not require the page to stay open.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `QUOTA_REFRESH_WORKER_LIMIT` | No | `10` | Maximum Auth Files quota refresh concurrency for manual and scheduled refresh, capped at `100` |

### Redis Queue Advanced Settings

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `REDIS_QUEUE_ADDR` | No | `CPA_BASE_URL` hostname + `8317` | CPA Redis/RESP TCP address; normally leave empty. Set `host:port` for non-default ports or separately exposed Redis streams |
| `REDIS_QUEUE_TLS` | No | `false` | Use TLS for Redis queue connection; set `true` when `REDIS_QUEUE_ADDR` is explicit and requires TLS |
| `REDIS_QUEUE_BATCH_SIZE` | No | `10000` | Maximum queue records per pull |
| `REDIS_QUEUE_IDLE_INTERVAL` | No | `1s` | Empty queue check interval |

### Storage, Logs, And Backups

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `WORK_DIR` | No | `./data` | Application work directory; database, logs, and backups default to `app.db`, `logs/`, and `backups/` under it |
| `LOG_LEVEL` | No | `info` | Log level |
| `LOG_FILE_ENABLED` | No | `true` | Write persistent log files |
| `LOG_RETENTION_DAYS` | No | `7` | Combined-log history days, plus the current day; `0` disables cleanup. Error-only logs keep 30 history days plus the current day |
| `BACKUP_ENABLED` | No | `true` | Enable SQLite database backups |
| `BACKUP_INTERVAL` | No | `24h` | Database backup interval |
| `BACKUP_RETENTION_DAYS` | No | `7` | Backup retention days |

Keeper automatically moves raw `usage_events` older than 90 local calendar days into the permanently retained `usage_events_archive` cold table during the daily 04:30 maintenance window. The archive is reserved for future schema-migration rebuilds and is not queried by normal dashboard APIs.

When file logging is enabled, `cpa-usage-keeper-YYYY-MM-DD.log` contains all emitted levels. Error, fatal, and panic entries are also copied to `cpa-usage-keeper-error-YYYY-MM-DD.log`, which keeps the previous 30 local calendar dates plus the current date.

### Built-In HTTPS

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `TLS_ENABLED` | No | `false` | Let Keeper serve HTTPS/TLS directly |
| `TLS_CERT_FILE` | Required when TLS is enabled | - | HTTPS certificate file path |
| `TLS_KEY_FILE` | Required when TLS is enabled | - | HTTPS private key file path |

Usually, HTTPS should be terminated at nginx, Caddy, or another reverse proxy. Set `TLS_ENABLED=true` only when the Keeper process must serve HTTPS directly, and provide `TLS_CERT_FILE` and `TLS_KEY_FILE`; relative paths are resolved against the `.env` file directory.

Security and data notes:

- Browser APIs redact key-like fields, but the SQLite database and its unencrypted backups contain original data.
- Authentication is enabled by default. If it is explicitly disabled, restrict Keeper access at the deployment boundary; terminate public HTTPS at a reverse proxy.
- Login session hashes persist in SQLite until logout or `AUTH_SESSION_TTL` expiry.
- CPAMC uses a separate embed session: an `HttpOnly` cookie when available, or a per-tab header token in browser session storage as a fallback.
- Same-origin embedding works by default. For cross-origin embedding, set `CPA_PUBLIC_URL` to the public CPA/CPAMC origin used for `frame-ancestors`.
- Redis inbox messages are retained through the current day after success or for 7 days after failure.

## Nginx Reverse Proxy

When serving under `/cpa`, set `APP_BASE_PATH=/cpa` and keep the prefix in your reverse proxy:

```nginx
location /cpa/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

The loopback Nginx configuration above works without additional Keeper settings. If the reverse proxy reaches Keeper from a container or another host, add that exact proxy network, for example `TRUSTED_PROXY_CIDRS=172.18.0.0/16`.

When CPA and Keeper share a browser origin, `CPA_PUBLIC_URL` can be omitted and "Back to CPA" uses `/management.html`. For another domain, port, or path, set the public CPA URL:

```env
CPA_PUBLIC_URL=https://cpa.example.com
```

## License

This project is open source under the [MIT License](./LICENSE).
