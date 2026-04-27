# CPA Usage Keeper

[English README](./README.en.md)

`CPA Usage Keeper` 是一个独立的 CPA 用量持久化与可视化服务。

它依赖 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 作为后端 CPA 数据来源，目标是在 CPA 之上补充持久化存储与统计分析能力。服务会定时拉取 CPA 数据，将规范化后的事件写入 SQLite，暴露聚合 API，并提供内置 Web Dashboard 用于查看 usage、pricing、request health 和 model/API 维度的统计信息。

## 与 CLIProxyAPI 的关系

这个项目是 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 的配套服务，不是它的替代品。

- 数据来自 CLIProxyAPI（CPA）。
- CPA Usage Keeper 依赖一个正在运行的 CPA 实例及其 management API。
- 没有 CPA，这个项目无法采集或刷新 usage 数据。

如果你正在评估或部署本仓库，建议先部署 CLIProxyAPI，再在需要持久化、历史分析或独立 Dashboard 时叠加使用 CPA Usage Keeper。

![cpa-usage-keeper-screenshot](https://images.bitskyline.com/i/2026/04/h9se9f.png)

## 功能特性

- 定时同步 CPA usage 数据并持久化到 SQLite
- 原始 export JSON 本地备份与保留策略
- usage 聚合 API 与 pricing API
- 由 Go 后端直接托管的内置 React Dashboard
- 可选的密码登录保护
- 仅允许对已使用模型进行价格持久化配置
- 支持 Docker 与 Docker Compose 部署

## 项目结构

```text
cmd/                 应用入口
internal/api/        HTTP 路由与处理器
internal/app/        应用装配与启动
internal/auth/       内存 session 鉴权
internal/backup/     原始 export 备份管理
internal/config/     环境配置加载
internal/cpa/        CPA 客户端与类型定义
internal/models/     GORM 模型
internal/poller/     后台同步轮询
internal/repository/ SQLite 访问与聚合逻辑
internal/service/    同步、usage 与 pricing 服务
web/                 React + TypeScript 前端
```

## 配置

先复制配置模板：

```bash
cp .env.example .env
```

关键环境变量：

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `CPA_BASE_URL` | 是 | - | CPA 服务地址 |
| `CPA_MANAGEMENT_KEY` | 是 | - | CPA management key |
| `AUTH_ENABLED` | 否 | `false` | 是否启用登录保护 |
| `LOGIN_PASSWORD` | 鉴权启用时必填 | - | 登录密码 |
| `AUTH_SESSION_TTL` | 否 | `168h` | Session 生命周期 |
| `APP_PORT` | 否 | `8080` | HTTP 监听端口 |
| `APP_BASE_PATH` | 否 | 根路径 | 应用子路径前缀，例如 `/cpa`；留空表示部署在根路径 |
| `USAGE_SYNC_MODE` | 否 | `auto` | usage 同步模式：`auto`、`redis` 或 `legacy_export` |
| `REDIS_QUEUE_ADDR` | 否 | `CPA_BASE_URL` 主机名 + `8317` | CPA management data stream 的 Redis/RESP TCP 地址；nginx stream 使用非 8317 端口时请显式设置 |
| `REDIS_QUEUE_BATCH_SIZE` | 否 | `1000` | 每次 Redis `LPOP` 最多拉取的队列记录数 |
| `REDIS_QUEUE_IDLE_INTERVAL` | 否 | `1s` | Redis 队列为空时的检查间隔 |
| `POLL_INTERVAL` | 否 | `30s`（`legacy_export` 为 `5m`） | legacy export 同步周期；`auto` 模式下也用于 Redis 不可用时的旧方式回退节流 |
| `REQUEST_TIMEOUT` | 否 | `30s` | CPA 请求超时 |
| `SQLITE_PATH` | 否 | `/data/app.db` | SQLite 数据库路径 |
| `LOG_LEVEL` | 否 | `info` | 日志级别 |
| `BACKUP_ENABLED` | 否 | `true` | 是否启用原始备份 |
| `BACKUP_DIR` | 否 | `/data/backups` | 备份目录 |
| `BACKUP_INTERVAL` | 否 | `1h` | 两次备份写入之间的最小间隔 |
| `BACKUP_RETENTION_DAYS` | 否 | `30` | 备份保留天数 |

`APP_BASE_PATH` 设置规则：
- 留空表示部署在根路径 `/`
- 如果要部署到子路径，必须以 `/` 开头，例如 `/cpa`
- 可以写成 `/cpa/`，程序会自动规范成 `/cpa`
- 像 `cpa` 这样不带前导 `/` 的写法是无效的

CPA v6.9.38+ 支持通过 management data stream 拉取 usage 数据。默认 `USAGE_SYNC_MODE=auto` 会优先持续 drain data stream，并在非鉴权类连接/协议错误时回退到旧兼容方式；`USAGE_SYNC_MODE=redis` 只持续 drain data stream；`USAGE_SYNC_MODE=legacy_export` 用于旧版 CPA 的临时兼容。data stream 队列是消费型且保留时间较短，约 1 分钟；同一个 CPA 实例通常只运行一个 keeper consumer，避免多消费者分流数据。

在 Redis 模式下，`POLL_INTERVAL` 不再控制 Redis pop 频率。非空 Redis 批次会先写入本地 SQLite inbox，再进行解码和 usage event 写入，然后立即继续下一次 pop；`REDIS_QUEUE_IDLE_INTERVAL` 控制空队列检查间隔；Redis/网络/协议临时错误后的重试退避固定为 10s。`REDIS_QUEUE_BATCH_SIZE=1000` 是每次 pop 的 count，高流量部署可在观察 DB 写入延迟和队列积压后调大。空 Redis 队列检查不会创建 snapshot 行，metadata 会每 30s 以及手动同步时刷新。

Redis/RESP 是原始 TCP 协议，不是 HTTP。CPA 目前提供的是消费型读取，尚无 processing/ack 协议；本地 inbox 可以保护应用收到消息之后的本地解码/写入失败，但无法消除 CPA 已移除消息与 SQLite 成功提交 inbox 行之间的故障窗口。反复写入 usage event 失败的 inbox 行最多重试 5 次，之后会标记为 discarded，并只记录元信息日志，不会把原始 usage payload 写入日志，避免旧异常数据阻塞新数据写入。请将 SQLite 放在可靠的持久化存储上。`REDIS_QUEUE_ADDR` 为空时会默认使用 `CPA_BASE_URL` 的主机名加 `8317` 端口；如果你用 nginx stream 把 CPA 原始端口映射到其它端口，应设置成对应的 `host:port`，例如 `REDIS_QUEUE_ADDR=cpa.example.com:6380`。普通 nginx HTTP `location` 反代不能代理 Redis/RESP。`CPA_MANAGEMENT_KEY` 鉴权失败是硬配置错误，不会回退到旧方式。

启用备份后，服务会按照 `BACKUP_INTERVAL` 控制原始数据备份的落盘频率；每次非空同步仍会正常记录 `SnapshotRun` 并持久化 usage 事件。

安全与数据说明：
- SQLite 数据库和原始备份会保存从 CPA 拉取到的原始 usage/source 数据，备份文件不做加密。
- 面向浏览器的 API 会对 key-like source/lookup 字段做脱敏或稳定公开标识映射，但这不改变本地数据库中的原始值。
- 公开部署时建议开启 `AUTH_ENABLED=true`，并在反向代理层配置 HTTPS。
- 登录 session 存在服务进程内存中，服务重启后已登录 session 会失效。

## 本地开发

### 前置依赖

- Go 1.22+
- Node.js 22+
- npm
- 已运行的 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI)

### 本地启动

1. 复制本地配置：

```bash
cp .env.example .env
```

2. 启动后端：

```bash
go run ./cmd/server/main.go
```

3. 在另一个终端安装前端依赖并启动开发服务器：

```bash
npm --prefix ./web ci
npm --prefix ./web run dev -- --host 127.0.0.1
```

4. 构建前端生产产物：

```bash
npm --prefix ./web run build
```

### 测试

运行完整的本地验证基线：

```bash
make verify
```

也可以单独运行各项检查：

```bash
go test ./cmd/... ./internal/...
npm --prefix ./web run test
npm --prefix ./web run lint
npm --prefix ./web run typecheck
npm --prefix ./web run build
```

## Docker

### 使用 GitHub Actions 自动发布到 GHCR

这个仓库可以把 Docker 镜像自动发布到 GitHub Container Registry（GHCR）：

- GitHub 仓库存放源码
- GitHub Actions 负责自动构建和发布镜像
- GHCR 存放构建后的镜像，地址为 `ghcr.io/willxup/cpa-usage-keeper`

当你把 `.github/workflows/docker-publish.yml` 提交到 GitHub 后，仓库就已经具备了自动发布能力，但你通常还需要在 GitHub 上做两件事：

1. 打开仓库的 `Actions` 页面，如果 GitHub 提示启用 Actions，就点启用。
2. 第一次发布成功后，打开 package 页面；如果你希望别人无需登录即可拉取镜像，需要把镜像设为 public。

workflow 会在你 push 版本 tag（例如 `v1.0.0`）时自动发布。

### 直接使用已发布镜像

Docker 部署时，`.env` 文件是可选的。你可以：
- 复制 `.env.example` 为 `.env`，然后通过 `--env-file .env` 传入
- 或者直接在命令行里使用 `-e` 传入所需环境变量

1. 可选：复制环境变量模板：

```bash
cp .env.example .env
```

2. 如果你使用 `.env`，至少填写：
- `CPA_BASE_URL`
- `CPA_MANAGEMENT_KEY`
- `SQLITE_PATH=/data/app.db`（可选；默认就是 `/data/app.db`）

3. 拉取镜像：

```bash
docker pull ghcr.io/willxup/cpa-usage-keeper:latest
```

4. 运行容器。

如果你使用 `.env`：

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --env-file .env \
  ghcr.io/willxup/cpa-usage-keeper:latest
```

如果你不使用 `.env`：

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  -e CPA_BASE_URL=http://127.0.0.1:8317 \
  -e CPA_MANAGEMENT_KEY=replace-with-your-management-key \
  ghcr.io/willxup/cpa-usage-keeper:latest
```

5. 验证服务是否正常：

```bash
curl -i http://127.0.0.1:8080/healthz
```

说明：
- `APP_BASE_PATH` 是运行时环境变量，不是 Docker 构建参数
- 同一个镜像既可以运行在根路径 `/`，也可以运行在 `/cpa` 这类子路径下
- `BACKUP_DIR` 通常应设置为 `/data/backups`
- `SQLITE_PATH` 在 Docker 部署下是可选的，默认就是 `/data/app.db`
- 镜像里不会包含你的运行时密钥，所有部署差异都通过 `.env` 或运行时环境变量提供
- 如果不挂载 `./data:/data`，SQLite 数据库和 backup 都会是临时的

### 本地构建镜像

如果你仍然想在仓库根目录本地构建：

```bash
docker build -t cpa-usage-keeper .
```

然后运行：

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --env-file .env \
  cpa-usage-keeper
```

## Docker Compose

Docker Compose 部署时，`.env` 文件同样是可选的。

- 如果仓库根目录存在 `.env`，Docker Compose 会自动加载
- 你也可以显式传 `--env-file .env`
- 如果你不想使用 `.env`，也可以先在 shell 中设置环境变量再启动 Compose

1. 可选：复制根目录环境变量模板：

```bash
cp .env.example .env
```

2. 如果你使用 `.env`，编辑它并填入 CPA 凭据和运行参数。

3. 拉取已发布镜像：

```bash
docker compose -f docker-compose.example.yml --env-file .env pull
```

4. 启动服务：

```bash
docker compose -f docker-compose.example.yml --env-file .env up -d
```

5. 停止服务：

```bash
docker compose -f docker-compose.example.yml --env-file .env down
```

如果你不想使用 `.env`，也可以这样运行：

```bash
CPA_BASE_URL=http://127.0.0.1:8317 \
CPA_MANAGEMENT_KEY=replace-with-your-management-key \
docker compose -f docker-compose.example.yml up -d
```

默认情况下，`docker-compose.example.yml` 会拉取 `ghcr.io/willxup/cpa-usage-keeper:latest`，而不是使用本地 `Dockerfile` 构建。

compose 会将仓库根目录的 `data` 以 bind mount 方式挂载到容器内的 `/data`，用于保存 SQLite 数据库和备份文件。

如果你只是本地开发，仍然可以把 `image:` 改回 `build:` 来走本地构建流程。

当设置 `APP_BASE_PATH=/cpa` 时，应用访问入口应为 `/cpa/`，Nginx 反代时也应保留这个前缀，而不是先重写掉再转发。

## Nginx 子路径反代示例

如果应用部署在 `/cpa` 这类子路径下，请设置 `APP_BASE_PATH=/cpa`，并在 Nginx 中保留相同前缀转发：

```nginx
location /cpa/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

不要在反代前把 `/cpa` 前缀重写掉。
