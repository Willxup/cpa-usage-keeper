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

Keep usage history for [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI), understand model costs, and track request performance and credential quotas. CPA Usage Keeper brings overviews, realtime diagnostics, request details, and quota history into one independently deployed dashboard.

<p align="center">
  <strong><a href="#keeper-only">Deploy Keeper</a>&emsp;|&emsp;<a href="#cpa--keeper">Deploy CPA + Keeper</a>&emsp;|&emsp;<a href="#configuration">Configuration</a></strong>
</p>

## Screenshots

<table>
  <tr>
    <td width="50%" align="center">
      <strong>Overview</strong>
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/overview-dark.png" />
        <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/overview-white.png" />
        <img src="./assets/screenshots/overview-white.png" alt="CPA Usage Keeper Overview" width="100%" />
      </picture>
    </td>
    <td width="50%" align="center">
      <strong>Realtime Diagnostics</strong>
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/realtime-dark.png" />
        <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/realtime-white.png" />
        <img src="./assets/screenshots/realtime-white.png" alt="CPA Usage Keeper Realtime Diagnostics" width="100%" />
      </picture>
    </td>
  </tr>
  <tr>
    <td width="50%" align="center">
      <strong>Usage Analysis</strong>
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/analysis-dark.png" />
        <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/analysis-white.png" />
        <img src="./assets/screenshots/analysis-white.png" alt="CPA Usage Keeper Usage Analysis" width="100%" />
      </picture>
    </td>
    <td width="50%" align="center">
      <strong>Request Events</strong>
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/request-events-dark.png" />
        <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/request-events-white.png" />
        <img src="./assets/screenshots/request-events-white.png" alt="CPA Usage Keeper Request Events" width="100%" />
      </picture>
    </td>
  </tr>
  <tr>
    <td width="50%" align="center">
      <strong>Credentials and Quotas</strong>
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/credentials-dark.png" />
        <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/credentials-white.png" />
        <img src="./assets/screenshots/credentials-white.png" alt="CPA Usage Keeper Credentials and Quotas" width="100%" />
      </picture>
    </td>
    <td width="50%" align="center">
      <strong>Quota History</strong>
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/quota-history-dark.png" />
        <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/quota-history-white.png" />
        <img src="./assets/screenshots/quota-history-white.png" alt="CPA Usage Keeper Quota History" width="100%" />
      </picture>
    </td>
  </tr>
</table>

## Features

- **Keep your history**: persist CPA usage in SQLite with scheduled backups.
- **Understand costs**: analyze usage, caching, and estimated costs by model, API Key, and provider.
- **Investigate requests**: inspect and export request details, and explore success rates, time to first token (TTFT), and total latency.
- **Track quotas**: monitor credential health and remaining quotas, refresh quotas, edit priorities, and explore Codex quota history.
- **Share scoped access**: provide a read-only usage view for an individual CPA API Key.

CPA plugin embedding in CPAMC is also available. Deploy with Docker Compose, Homebrew, or binaries; login protection is enabled by default.

## Sponsors and Special Thanks

- Thanks to [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) for providing the upstream CPA foundation and data source this project builds on.
- Thanks to [@YouShouldBetOnMe](https://github.com/YouShouldBetOnMe) for supporting CPA Usage Keeper.
- Thanks to the CPA discussion group for their discussions and feedback.

## Quick Start

> Keeper supports CPA v8.0.12+. Enable usage statistics by setting `observability.usage.usage-statistics-enabled` to `true`.<br>
> When multiple collectors share one CPA instance, all must use subscription mode to avoid interrupted collection or missing data.

Docker Compose is recommended; no Go, Node.js, or source build is needed. Choose a deployment below. For an existing CPA, have its address and management key ready, and choose a Keeper login password.

| Setup | Recommended path | Architectures |
| --- | --- | --- |
| New CPA + Keeper deployment | [Docker Compose: CPA + Keeper](#cpa--keeper) | `linux/amd64`, `linux/arm64` |
| Existing CPA deployment | [Docker Compose: Keeper only](#keeper-only) | `linux/amd64`, `linux/arm64` |
| Existing CPA, Docker CLI preferred | [Docker](#docker-cpa-already-runs-on-the-host) | `linux/amd64`, `linux/arm64` |
| macOS | [Homebrew](#macos-homebrew) | `amd64`, `arm64` |
| Linux without containers | [Linux binary](#linux-binary) | `amd64`, `arm64` |
| Windows | [Windows binary](#windows-binary) | `amd64`, `arm64` |

Login protection is enabled by default. Configure `LOGIN_PASSWORD` before starting Keeper, or explicitly set `AUTH_ENABLED=false` only when access is reliably isolated by the deployment environment.

## Benchmark

Production-style `linux/amd64` capacity measurements for sustained ingestion, Dashboard latency, CPU utilization, and Keeper cgroup peak memory are available in the [Capacity Benchmark Report](./internal/benchmark/reports/capacity/README.md).

## Developer Reference

<details>
<summary>Project structure, local setup, and tests</summary>

### Project Structure

```text
cmd/server/              Application entry point
internal/api/            HTTP routes and handlers
internal/app/            Application wiring and startup
internal/auth/           Sessions and access control
internal/poller/         CPA usage and metadata synchronization
internal/repository/     SQLite persistence and aggregations
internal/service/        Usage, pricing, and identity services
internal/quota/          Provider quota refresh and inspection
internal/benchmark/      Capacity suite, reports, manifests, and legacy microbenchmarks
deploy/                  Deployment templates
web/                     React + TypeScript frontend
```

### Local Development

#### Prerequisites

- Go 1.26.10+
- Node.js 24.22.1+
- npm
- A running [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) instance

If you use nvm, run `nvm install` and `nvm use` in the repository root to select the version in `.nvmrc`.

#### Run Locally

1. Copy `.env.example` to `.env`, then set at least `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`, and a private `LOGIN_PASSWORD`.

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

#### Tests

Run the full verification baseline:

```bash
make verify
```

See the [development guide](./docs/development.md) for individual test commands and chart time-handling conventions.

</details>

## Deployment

After startup, open `http://your-server-address:8080` (`http://127.0.0.1:8080` for a local deployment) and sign in with your Keeper login password. Adjust the URL if you configure a different port, HTTPS, or a base path.

### Docker Compose (Recommended)

#### CPA + Keeper

**1. Prepare the configuration files**

First, download the configuration example from the [official CPA repository](https://github.com/router-for-me/CLIProxyAPI) into `./cpa/config.yaml` in your deployment directory:

```bash
mkdir -p cpa/auths cpa/logs keeper
curl -fL https://raw.githubusercontent.com/router-for-me/CLIProxyAPI/main/config.example.yaml \
  -o cpa/config.yaml
```

In the same directory, download the [CPA + Keeper Compose template](./deploy/docker-compose.full.example.yml):

```bash
curl -fL https://raw.githubusercontent.com/Willxup/cpa-usage-keeper/main/deploy/docker-compose.full.example.yml \
  -o docker-compose.yml
```

Use this download command for a new deployment only; edit an existing configuration directly to avoid overwriting it.

**2. Configure CPA and Keeper**

Edit the existing settings in `cpa/config.yaml` as listed below, preserving YAML nesting and indentation. Dots in the table denote nested paths, not literal YAML keys to add:

| Setting | What to configure |
| --- | --- |
| `management.allow-remote` | Set to `true` so Keeper can access CPA management features from another container. |
| `management.secret-key` | Set a private management key and use the same original key for `CPA_MANAGEMENT_KEY` in `docker-compose.yml`. |
| `observability.usage.usage-statistics-enabled` | Set to `true` to enable usage statistics. |
| `access.api-keys` | Replace the example keys with your own client API keys; these are separate from the CPA management key and Keeper login password. |

Keep `server.host` as an empty string, `server.port` as `8317`, and `oauth.auth-dir` as `"~/.cli-proxy-api"` to match the container network and volume mounts in the template.

Edit the downloaded `docker-compose.yml` and fill in two values under Keeper's `environment`:

- `CPA_MANAGEMENT_KEY`: use the same original management key configured in CPA.
- `LOGIN_PASSWORD`: replace the empty string `""` with your own Keeper login password.

For optional environment-file setup, see [advanced configuration](./docs/configuration.md#optional-compose-environment-file).

**3. Start and open Keeper**

```bash
docker compose up -d
```

Open `http://your-server-address:8080` and sign in with your Keeper login password.

For a new deployment, add model credentials in CPA and make a model request to generate usage records.

CPA data is stored under `./cpa`, and Keeper data is stored under `./keeper`.

#### Keeper Only

**1. Prepare the configuration files**

When CPA is already deployed, download the Keeper-only Compose template and environment configuration into a new deployment directory:

```bash
curl -fL https://raw.githubusercontent.com/Willxup/cpa-usage-keeper/main/deploy/docker-compose.example.yml \
  -o docker-compose.yml
curl -fL https://raw.githubusercontent.com/Willxup/cpa-usage-keeper/main/.env.example -o .env
vim .env
```

For an existing deployment, edit the existing files directly to avoid overwriting your configuration.

**2. Set the connection details and login password**

For CPA running on the Docker host, start with:

```env
CPA_BASE_URL=http://host.docker.internal:8317
CPA_MANAGEMENT_KEY=replace-with-your-management-key
AUTH_ENABLED=true
LOGIN_PASSWORD=
```

Set a private `LOGIN_PASSWORD` before starting the container.

Whether CPA runs on the Docker host or another machine, it must allow remote management and listen on an address reachable from the Keeper container; a listener bound only to `127.0.0.1` is not reachable. Set `management.allow-remote: true` and check `server.host`.

For other network layouts, set `CPA_BASE_URL` to a CPA address reachable from the container. Set `REDIS_QUEUE_ADDR` only when the Redis/RESP address differs from the automatically derived address.

**3. Start and open Keeper**

```bash
docker compose up -d
```

Keeper data is stored under `./data` by the provided template.

#### Access And Routine Management

For either Compose setup, sign in at `http://your-server-address:8080` with your Keeper password and run `docker compose down` to stop it. View logs from the deployment directory:

```bash
docker compose logs --tail=100 -f cpa-usage-keeper
```

To update Keeper, keep your configuration and data directories and run:

```bash
docker compose pull cpa-usage-keeper
docker compose up -d cpa-usage-keeper
```

#### No Data After Startup?

- Check that CPA usage statistics are enabled: `observability.usage.usage-statistics-enabled` must be `true`.
- Ensure Keeper can reach `CPA_BASE_URL` and that `CPA_MANAGEMENT_KEY` matches the CPA management key.
- Check that CPA has received new model requests. If data is still missing, use the log command above to look for connection or authentication errors.

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
cpa-usage-keeper --env /path/to/keeper.env # Use a specific environment file.
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

Set `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`, and a private `LOGIN_PASSWORD` before starting.

## Configuration

Edit your existing `.env` or Compose `environment`, depending on the deployment method; do not overwrite existing settings with a fresh template.

For a first deployment, set the CPA address, CPA management key, and Keeper login password. Most other settings can keep their defaults. Read the relevant sections when you need a domain, HTTPS, or a base path.

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

For Docker/Compose, keep `APP_HOST` empty; publish `127.0.0.1:8080:8080` for host-only access. Set a browser-accessible `CPA_PUBLIC_URL` when CPA and Keeper use different domains, ports, or paths. See [advanced configuration](./docs/configuration.md#web-access-and-reverse-proxy) for details and the reverse-proxy example below.

### Login Protection

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `AUTH_ENABLED` | No | `true` | Enable login protection |
| `LOGIN_PASSWORD` | When auth is enabled | - | Login password |
| `CPA_REQUEST_LOG_ACCESS_ENABLED` | No | `false` | Allow administrators to view and download CPA request logs through Keeper; corresponding logs must exist in CPA and may contain request or response data |
| `AUTH_SESSION_TTL` | No | `168h` | Login session lifetime |

### Timezone And Request Behavior

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `TZ` | No | `Asia/Shanghai` | Timezone used for statistics and display; Today, daily totals, page timestamps, log timestamps, and daily cleanup are calculated in this timezone |
| `REQUEST_TIMEOUT` | No | `30s` | Timeout for CPA HTTP requests and Redis queue operations |
| `TLS_SKIP_VERIFY` | No | `false` | Skip TLS certificate verification for CPA HTTPS and Redis queue TLS; enable only with self-signed certificates |

### Storage, Logs, And Backups

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `WORK_DIR` | No | `./data` | Application work directory; database, logs, and backups default to `app.db`, `logs/`, and `backups/` under it |
| `LOG_LEVEL` | No | `info` | Log level |
| `LOG_FILE_ENABLED` | No | `true` | Write persistent log files |
| `LOG_RETENTION_DAYS` | No | `7` | Combined-log history days, plus the current day; `0` disables cleanup. Error-only logs keep 30 history days plus the current day |
| `BACKUP_ENABLED` | No | `true` | Enable scheduled database backups |
| `BACKUP_INTERVAL` | No | `24h` | Database backup interval |
| `BACKUP_RETENTION_DAYS` | No | `7` | Backup retention days |
| `USAGE_RAW_RETENTION_DAYS` | No | `0` | Total raw-request retention in days; only archives are deleted. `0` retains archives forever; `>=90` enables cleanup. Negative values and `1–89` fall back to `0` with a warning without preventing startup |

Raw requests are archived after 90 days and retained indefinitely by default; set `USAGE_RAW_RETENTION_DAYS>=90` to remove expired archives. Deleted records cannot support historical recalculation, and deletion may not immediately shrink the database file. See [storage and log details](./docs/configuration.md#storage-and-log-details).

### Rolling Back A Failed v1-to-v2 Upgrade

Keeper backs up the database before migration. If it fails, follow the [failed migration rollback guide](./docs/upgrading.md). **Do not open a partially migrated database with v1.**

### Optional Features And Advanced Configuration

- [OIDC administrator login](./docs/oidc.md): disabled by default; keep password login configured. **Without subject restrictions, all users authenticated for the client become administrators; restrict application access at the provider.**
- [Quota refresh](./docs/configuration.md#credentials-quota-refresh): configure schedules from the gear button in the Credentials inspection dialog; the page need not stay open after saving. The guide covers concurrency and debugging settings.
- [Redis queue](./docs/configuration.md#redis-queue-advanced-settings): separate addresses, TLS, and polling settings; defaults usually suffice.
- [Built-in HTTPS](./docs/configuration.md#built-in-https): for Keeper to serve HTTPS directly; usually, use nginx, Caddy, or another reverse proxy instead.

### Security Notes

- Browser APIs redact key-like fields, but the SQLite database and its unencrypted backups contain original data.
- Authentication is enabled by default. If it is explicitly disabled, restrict Keeper access at the deployment boundary; terminate public HTTPS at a reverse proxy.

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

Local Nginx needs no extra proxy trust settings. For containers, remote proxies, or CDNs, configure only the actual proxy networks in `TRUSTED_PROXY_CIDRS`; never trust all sources.

## License

This project is open source under the [MIT License](./LICENSE).

## Star History

<a href="https://star-history.dera.page/Willxup/cpa-usage-keeper">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://star-history.dera.page/svg?repos=Willxup/cpa-usage-keeper&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://star-history.dera.page/svg?repos=Willxup/cpa-usage-keeper" />
   <img alt="Star History Chart" src="https://star-history.dera.page/svg?repos=Willxup/cpa-usage-keeper" />
 </picture>
</a>
