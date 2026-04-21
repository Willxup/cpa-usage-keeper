# CPA Usage Keeper

[English README](./README.md)

`CPA Usage Keeper` 是一个独立的 CPA 用量持久化与可视化服务。

它依赖 `cli-proxy-api` 作为后端 CPA 数据来源，目标是在 CPA 之上补充持久化存储与统计分析能力。服务会定时拉取 CPA 的 `usage/export` 数据，将规范化后的事件写入 SQLite，暴露聚合 API，并提供内置 Web Dashboard 用于查看 usage、pricing、request health 和 model/API 维度的统计信息。

![cpa-usage-keeper](https://images.bitskyline.com/i/2026/04/u903kd.png)

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
| `APP_PORT` | 否 | `8080` | HTTP 监听端口 |
| `APP_BASE_PATH` | 否 | 根路径 | 应用子路径前缀，例如 `/cpa`；留空表示部署在根路径 |
| `CPA_BASE_URL` | 是 | - | CPA 服务地址 |
| `CPA_MANAGEMENT_KEY` | 是 | - | CPA management key |
| `POLL_INTERVAL` | 否 | `5m` | usage 同步周期 |
| `SQLITE_PATH` | 是 | - | SQLite 数据库路径 |
| `BACKUP_ENABLED` | 否 | `true` | 是否启用原始备份 |
| `BACKUP_DIR` | 否 | `/data/backups` | 备份目录 |
| `BACKUP_INTERVAL` | 否 | `1h` | 两次备份写入之间的最小间隔 |
| `BACKUP_RETENTION_DAYS` | 否 | `30` | 备份保留天数 |
| `REQUEST_TIMEOUT` | 否 | `30s` | CPA 请求超时 |
| `LOG_LEVEL` | 否 | `info` | 日志级别 |
| `AUTH_ENABLED` | 否 | `false` | 是否启用登录保护 |
| `LOGIN_PASSWORD` | 鉴权启用时必填 | - | 登录密码 |
| `AUTH_SESSION_TTL` | 否 | `168h` | Session 生命周期 |

`APP_BASE_PATH` 设置规则：
- 留空表示部署在根路径 `/`
- 如果要部署到子路径，必须以 `/` 开头，例如 `/cpa`
- 可以写成 `/cpa/`，程序会自动规范成 `/cpa`
- 像 `cpa` 这样不带前导 `/` 的写法是无效的

启用备份后，服务会按照 `BACKUP_INTERVAL` 控制原始 export JSON 的落盘频率；即使本次未写入新的 backup，仍会正常记录 `SnapshotRun` 并持久化 usage 事件。

## 本地开发

### 前置依赖

- Go 1.22+
- Node.js 22+
- npm
- 已运行的 `cli-proxy-api`

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

```bash
go test ./...
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

workflow 会在以下情况下自动发布：
- push 到 `main`
- push 版本 tag，例如 `v1.0.0`

如果是 pull request，则只验证镜像能否正常构建，不会推送镜像。

### 直接使用已发布镜像

1. 复制环境变量模板：

```bash
cp .env.example .env
```

2. 编辑 `.env`，至少填写：
- `CPA_BASE_URL`
- `CPA_MANAGEMENT_KEY`
- `SQLITE_PATH=/data/app.db`

3. 拉取镜像：

```bash
docker pull ghcr.io/willxup/cpa-usage-keeper:latest
```

4. 运行容器：

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --env-file .env \
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
- 镜像里不会包含你的运行时密钥，所有部署差异都通过 `.env` 提供
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

1. 复制根目录环境变量模板：

```bash
cp .env.example .env
```

2. 编辑 `.env`，填入 CPA 凭据和运行参数。

3. 启动服务：

```bash
docker compose -f docker-compose.example.yml --env-file .env up -d
```

4. 停止服务：

```bash
docker compose -f docker-compose.example.yml --env-file .env down
```

默认情况下，`docker-compose.example.yml` 会拉取 `ghcr.io/willxup/cpa-usage-keeper:latest`。

compose 会将仓库根目录的 `data` 以 bind mount 方式挂载到容器内的 `/data`，用于保存 SQLite 数据库和备份文件。

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
