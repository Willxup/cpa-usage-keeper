# CPA Usage Keeper

[中文说明](./README.md)

CPA Usage Keeper is a standalone CPA usage persistence and dashboard service.

It relies on [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) as the backend CPA data source and adds persistent storage and statistical analysis capabilities on top of CPA. The service periodically pulls CPA data, writes normalized events to SQLite, exposes aggregation APIs, and serves a built-in web dashboard for usage, pricing, request health, and model/API statistics.

![cpa-usage-keeper-screenshot](https://images.bitskyline.com/i/2026/04/h9se9f.png)

## Features

- CPA usage persistence in SQLite
- Aggregated usage and pricing APIs
- Built-in React dashboard
- Optional password login protection
- Local raw data backups with retention
- Docker / Docker Compose deployment

## Project Structure

```text
cmd/                 Application entrypoint
internal/api/        HTTP routes and handlers
internal/app/        App wiring and startup
internal/auth/       In-memory session auth
internal/backup/     Raw data backup management
internal/config/     Environment config loading
internal/cpa/        CPA client and types
internal/models/     GORM models
internal/poller/     Background sync loop
internal/repository/ SQLite access and aggregations
internal/service/    Sync, usage, and pricing services
web/                 React + TypeScript frontend
```

## Configuration

Copy the example config:

```bash
cp .env.example .env
```

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `CPA_BASE_URL` | Yes | - | CPA server URL |
| `CPA_MANAGEMENT_KEY` | Yes | - | CPA management key |
| `AUTH_ENABLED` | No | `false` | Enable login protection |
| `LOGIN_PASSWORD` | When auth is enabled | - | Login password |
| `AUTH_SESSION_TTL` | No | `168h` | Session lifetime |
| `APP_PORT` | No | `8080` | HTTP listen port |
| `APP_BASE_PATH` | No | root path | Subpath prefix such as `/cpa`; empty means `/` |
| `TZ` | No | `Asia/Shanghai` | Project business timezone; affects Today, daily aggregation, daily 03:00 cleanup, and log timestamps |
| `USAGE_SYNC_MODE` | No | `auto` | Sync mode: `auto` probes at startup and then fixes the process to `redis` or `legacy_export`; can also be set explicitly to `redis` or `legacy_export` |
| `REDIS_QUEUE_ADDR` | No | `CPA_BASE_URL` hostname + `8317` | CPA Redis/RESP TCP address; set `host:port` for non-default ports |
| `REDIS_QUEUE_BATCH_SIZE` | No | `1000` | Maximum queue records per pull |
| `REDIS_QUEUE_IDLE_INTERVAL` | No | `1s` | Empty queue check interval |
| `POLL_INTERVAL` | No | `5m` | Pull interval for `legacy_export` |
| `REQUEST_TIMEOUT` | No | `30s` | CPA request timeout |
| `WORK_DIR` | No | `./data` | Application work directory; database, logs, and backups default to `app.db`, `logs/`, and `backups/` under it |
| `LOG_LEVEL` | No | `info` | Log level |
| `LOG_FILE_ENABLED` | No | `true` | Write persistent log files |
| `LOG_RETENTION_DAYS` | No | `7` | Log retention days; `0` disables cleanup |
| `BACKUP_ENABLED` | No | `true` | Enable raw backups |
| `BACKUP_INTERVAL` | No | `1h` | Minimum interval between backup writes |
| `BACKUP_RETENTION_DAYS` | No | `30` | Backup retention days |

`APP_BASE_PATH` must be empty or start with `/`; for example `/cpa`. `/cpa/` is normalized to `/cpa`.

Security and data notes:

- SQLite and raw backups store original data pulled from CPA, and backup files are not encrypted.
- Browser-facing APIs redact key-like source/lookup fields or map them to stable public identifiers, but raw database values are unchanged.
- For public deployments, enable `AUTH_ENABLED=true` and terminate HTTPS at your reverse proxy.
- Login sessions are stored in process memory and become invalid after restart.
- Redis inbox raw messages are cleaned up automatically: successful rows are kept until the end of the current day, and failed rows are kept for 7 days.

## Development

### Prerequisites

- Go 1.22+
- Node.js 22+
- npm
- A running [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) instance

### Run locally

1. Create your local config:

```bash
cp .env.example .env
```

2. Start the backend:

```bash
go run ./cmd/server/main.go
```

3. In another terminal, install frontend dependencies and start the dev server:

```bash
npm --prefix ./web ci
npm --prefix ./web run dev -- --host 127.0.0.1
```

4. Build the frontend for production:

```bash
npm --prefix ./web run build
```

### Tests

Run the full local verification baseline:

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

## Docker

If CPA is already running on the host:

```bash
# TZ sets the container timezone; log timestamps are displayed in this timezone.
docker run -d \
  --name cpa-usage-keeper \
  --add-host=host.docker.internal:host-gateway \
  -p 8080:8080 \
  -v "$(pwd)/keeper/data:/data" \
  -e TZ=Asia/Shanghai \
  -e CPA_BASE_URL=http://host.docker.internal:8317 \
  -e CPA_MANAGEMENT_KEY=replace-with-your-management-key \
  -e REDIS_QUEUE_ADDR=host.docker.internal:8317 \
  -e AUTH_ENABLED=true \
  -e LOGIN_PASSWORD=replace-with-your-login-password \
  ghcr.io/willxup/cpa-usage-keeper:latest
```

`/data` stores the SQLite database, backups, and log files. Mount it to persistent storage.

## Docker Compose

The repository includes a minimal `docker-compose.yaml` example for running CPA and CPA Usage Keeper together:

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
      LOGIN_PASSWORD: replace-with-your-login-password
    volumes:
      - ./keeper:/data
    networks:
      - cpa-network

networks:
  cpa-network:
    driver: bridge
```

Start:

```bash
docker compose up -d
```

Stop:

```bash
docker compose down
```

CPA files are stored under `./cpa`, and CPA Usage Keeper data is stored under `./keeper`.

## Subpath reverse proxy

When serving under `/cpa`, set `APP_BASE_PATH=/cpa` and keep the prefix in your reverse proxy:

```nginx
location /cpa/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```
