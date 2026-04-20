# CPA Usage Keeper

[中文说明](./README.zh-CN.md)

CPA Usage Keeper is a standalone usage persistence and dashboard service for CPA (CLI Proxy API).

It requires `cli-proxy-api` as the backend source of CPA usage data and is designed to add persistence and statistics capabilities on top of CPA. It periodically pulls CPA `usage/export` data, stores normalized events in SQLite, exposes aggregated APIs, and serves a built-in web dashboard for usage, pricing, request health, and model/API breakdowns.

![cpa-usage-keeper](https://images.bitskyline.com/i/2026/04/u903kd.png)

## Features

- Periodic CPA usage sync with SQLite persistence
- Raw export backup retention on local disk
- Aggregated usage and pricing APIs
- Built-in React dashboard served by the Go backend
- Optional password-based login gate
- Configurable pricing persistence for used models only
- Docker and Docker Compose deployment support

## Project Structure

```text
cmd/                 Application entrypoint
internal/api/        HTTP routes and handlers
internal/app/        App wiring and startup
internal/auth/       In-memory session auth
internal/backup/     Raw export backup management
internal/config/     Environment config loading
internal/cpa/        CPA client and types
internal/models/     GORM models
internal/poller/     Background sync loop
internal/repository/ SQLite access and aggregations
internal/service/    Sync, usage, and pricing services
web/                 React + TypeScript frontend
```

## Configuration

Copy the example file and fill in your values:

```bash
cp .env.example .env
```

Key variables:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `APP_PORT` | No | `8080` | HTTP listen port |
| `CPA_BASE_URL` | Yes | - | CPA server base URL |
| `CPA_MANAGEMENT_KEY` | Yes | - | CPA management key |
| `POLL_INTERVAL` | No | `5m` | Usage sync interval |
| `SQLITE_PATH` | Yes | - | SQLite database path |
| `BACKUP_ENABLED` | No | `true` | Enable raw export backups |
| `BACKUP_DIR` | No | `/data/backups` | Backup directory |
| `BACKUP_INTERVAL` | No | `1h` | Minimum interval between backup writes |
| `BACKUP_RETENTION_DAYS` | No | `30` | Backup retention days |
| `REQUEST_TIMEOUT` | No | `30s` | CPA request timeout |
| `LOG_LEVEL` | No | `info` | Log level |
| `AUTH_ENABLED` | No | `false` | Enable login protection |
| `LOGIN_PASSWORD` | When auth is enabled | - | Login password |
| `AUTH_SESSION_TTL` | No | `168h` | Session lifetime |

When backups are enabled, the service writes at most one raw export backup per `BACKUP_INTERVAL`. Every sync still records a snapshot run and persists usage events.

## Development

### Prerequisites

- Go 1.22+
- Node.js 22+
- npm
- A running `cli-proxy-api` instance

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

```bash
go test ./...
npm --prefix ./web run build
```

## Docker

Build the image from the repository root:

```bash
docker build -t cpa-usage-keeper .
```

Run the container:

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --env-file .env \
  cpa-usage-keeper
```

Notes:
- Set `SQLITE_PATH=/data/app.db`
- Set `BACKUP_DIR=/data/backups`
- The Go server serves the built frontend from `web/dist`

## Docker Compose

1. Copy the root env template:

```bash
cp .env.example .env
```

2. Edit `.env` with your CPA credentials and runtime settings.

3. Start the stack:

```bash
docker compose -f docker-compose.example.yml --env-file .env up -d --build
```

4. Stop the stack:

```bash
docker compose -f docker-compose.example.yml --env-file .env down
```

The compose file bind-mounts `data` to `/data` for SQLite and backup persistence.
