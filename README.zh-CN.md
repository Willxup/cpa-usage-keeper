# CPA Usage Keeper

[English README](./README.md)

`CPA Usage Keeper` 是一个独立的 CPA 用量持久化与可视化服务。

它依赖 `cli-proxy-api` 作为后端 CPA 数据来源，目标是在 CPA 之上补充持久化存储与统计分析能力。服务会定时拉取 CPA 的 `usage/export` 数据，将规范化后的事件写入 SQLite，暴露聚合 API，并提供内置 Web Dashboard 用于查看 usage、pricing、request health 和 model/API 维度的统计信息。

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
| `CPA_BASE_URL` | 是 | - | CPA 服务地址 |
| `CPA_MANAGEMENT_KEY` | 是 | - | CPA management key |
| `POLL_INTERVAL` | 否 | `5m` | usage 同步周期 |
| `SQLITE_PATH` | 是 | - | SQLite 数据库路径 |
| `BACKUP_ENABLED` | 否 | `true` | 是否启用原始备份 |
| `BACKUP_DIR` | 否 | `/data/backups` | 备份目录 |
| `BACKUP_RETENTION_DAYS` | 否 | `30` | 备份保留天数 |
| `REQUEST_TIMEOUT` | 否 | `30s` | CPA 请求超时 |
| `LOG_LEVEL` | 否 | `info` | 日志级别 |
| `AUTH_ENABLED` | 否 | `false` | 是否启用登录保护 |
| `LOGIN_PASSWORD` | 鉴权启用时必填 | - | 登录密码 |
| `AUTH_SESSION_TTL` | 否 | `168h` | Session 生命周期 |

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

在仓库根目录构建镜像：

```bash
docker build -t cpa-usage-keeper .
```

运行容器：

```bash
docker run --rm \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  --env-file .env \
  cpa-usage-keeper
```

说明：
- 设置 `SQLITE_PATH=/data/app.db`
- 设置 `BACKUP_DIR=/data/backups`
- Go 服务会直接托管构建后的 `web/dist`

## Docker Compose

1. 复制根目录环境变量模板：

```bash
cp .env.example .env
```

2. 编辑 `.env`，填入 CPA 凭据和运行参数。

3. 启动服务：

```bash
docker compose -f docker-compose.example.yml --env-file .env up -d --build
```

4. 停止服务：

```bash
docker compose -f docker-compose.example.yml --env-file .env down
```

compose 会将仓库根目录的 `data` 以 bind mount 方式挂载到容器内的 `/data`，用于保存 SQLite 数据库和备份文件。
