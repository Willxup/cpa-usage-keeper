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
| `APP_BASE_PATH` | No | root path | App base path such as `/cpa`; leave empty for root deployment |
| `CPA_BASE_URL` | Yes | - | CPA server base URL |
| `CPA_MANAGEMENT_KEY` | Yes | - | CPA management key |
| `POLL_INTERVAL` | No | `5m` | Usage sync interval |
| `SQLITE_PATH` | No | `/data/app.db` | SQLite database path |
| `BACKUP_ENABLED` | No | `true` | Enable raw export backups |
| `BACKUP_DIR` | No | `/data/backups` | Backup directory |
| `BACKUP_INTERVAL` | No | `1h` | Minimum interval between backup writes |
| `BACKUP_RETENTION_DAYS` | No | `30` | Backup retention days |
| `REQUEST_TIMEOUT` | No | `30s` | CPA request timeout |
| `LOG_LEVEL` | No | `info` | Log level |
| `AUTH_ENABLED` | No | `false` | Enable login protection |
| `LOGIN_PASSWORD` | When auth is enabled | - | Login password |
| `AUTH_SESSION_TTL` | No | `168h` | Session lifetime |

`APP_BASE_PATH` rules:
- Leave it empty to serve from `/`
- For subpath deployment, it must start with `/`, for example `/cpa`
- A trailing slash like `/cpa/` is accepted and normalized to `/cpa`
- A value without the leading slash such as `cpa` is invalid

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

### Publish image with GitHub Actions and GHCR

This repository can publish a Docker image to GitHub Container Registry (GHCR):

- GitHub repository stores the source code
- GitHub Actions builds and publishes the image automatically
- GHCR stores the built image at `ghcr.io/willxup/cpa-usage-keeper`

After adding `.github/workflows/docker-publish.yml`, GitHub Actions is effectively enabled for this repository, but you may still need to do two things in GitHub:

1. Open the repository `Actions` tab and enable Actions if GitHub asks you to.
2. After the first successful publish, open the package page and make the image public if you want anonymous `docker pull` access.

The workflow publishes automatically when you:
- push to `main`
- push a version tag such as `v1.0.0`

On pull requests, it only verifies that the Docker image can be built.

### Use the published image

Using a `.env` file is optional for Docker deployment. You can either:
- copy `.env.example` to `.env` and pass `--env-file .env`
- or provide the needed `-e` flags directly on the command line

1. Optional: copy the env template:

```bash
cp .env.example .env
```

2. If you use `.env`, edit it and fill in at least:
- `CPA_BASE_URL`
- `CPA_MANAGEMENT_KEY`
- `SQLITE_PATH=/data/app.db` (optional; defaults to `/data/app.db`)

3. Pull the image:

```bash
docker pull ghcr.io/willxup/cpa-usage-keeper:latest
```

4. Run the container.

If you use `.env`:

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --env-file .env \
  ghcr.io/willxup/cpa-usage-keeper:latest
```

Or without `.env`:

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  -e CPA_BASE_URL=http://127.0.0.1:8317 \
  -e CPA_MANAGEMENT_KEY=replace-with-your-management-key \
  ghcr.io/willxup/cpa-usage-keeper:latest
```

5. Verify it is running:

```bash
curl -i http://127.0.0.1:8080/healthz
```

Notes:
- `APP_BASE_PATH` is a runtime environment variable, not a Docker build argument
- The same image can run either at `/` or a subpath such as `/cpa`
- `BACKUP_DIR` should normally be `/data/backups`
- `SQLITE_PATH` is optional for Docker deployment and defaults to `/data/app.db`
- The image does not include your runtime secrets; all deployment-specific settings stay in `.env` or runtime environment variables
- Persist `./data:/data` or your SQLite database and backups will be ephemeral

### Build locally

If you still want to build locally from the repository root:

```bash
docker build -t cpa-usage-keeper .
```

Then run:

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --env-file .env \
  cpa-usage-keeper
```

## Docker Compose

Using a `.env` file is optional for Docker Compose deployment.

- If a `.env` file exists in the repository root, Docker Compose will load it automatically.
- You can also pass `--env-file .env` explicitly.
- If you do not use a `.env` file, set the required variables in your shell before running Compose.

1. Optional: copy the root env template:

```bash
cp .env.example .env
```

2. If you use `.env`, edit it with your CPA credentials and runtime settings.

3. Pull the published image:

```bash
docker compose -f docker-compose.example.yml --env-file .env pull
```

4. Start the stack:

```bash
docker compose -f docker-compose.example.yml --env-file .env up -d
```

5. Stop the stack:

```bash
docker compose -f docker-compose.example.yml --env-file .env down
```

If you do not want to use `.env`, you can run Compose like this instead:

```bash
CPA_BASE_URL=http://127.0.0.1:8317 \
CPA_MANAGEMENT_KEY=replace-with-your-management-key \
docker compose -f docker-compose.example.yml up -d
```

By default, `docker-compose.example.yml` pulls `ghcr.io/willxup/cpa-usage-keeper:latest` instead of building from the local Dockerfile.

The compose file bind-mounts `data` to `/data` for SQLite and backup persistence.

If you want to keep using local image builds for development, replace the `image:` line with a `build:` block again.

When `APP_BASE_PATH=/cpa` is set, access the app at `/cpa/` and keep that prefix in your Nginx reverse proxy instead of rewriting it away.

## Nginx subpath reverse proxy

If the app runs under a subpath such as `/cpa`, set `APP_BASE_PATH=/cpa` and keep the same prefix in Nginx:

```nginx
location /cpa/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

Do not rewrite `/cpa` away before proxying.
