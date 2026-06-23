# CPA Usage Keeper

[English README](./README.md)

`CPA Usage Keeper` 是一个独立的 CPA 用量持久化与可视化服务。

它依赖 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 作为后端 CPA 数据来源，目标是在 CPA 之上补充持久化存储与统计分析能力。服务会从 CPA Redis usage 队列消费事件并写入 SQLite，定时拉取 CPA metadata，暴露聚合 API，并提供内置 Web Dashboard 用于查看 usage、pricing、request health 和 model/API 维度的统计信息。

<p float="left">
  <img src="https://images.bitskyline.com/i/2026/06/xwjnop.png" width="49%" />
  <img src="https://images.bitskyline.com/i/2026/06/xwk25d.png" width="49%" />
</p>
<p float="left">
  <img src="https://images.bitskyline.com/i/2026/06/xw9jj4.png" width="49%" />
  <img src="https://images.bitskyline.com/i/2026/06/xybv3z.png" width="49%" />
</p>

## 功能特性

- 持久保存 CPA usage 数据到 SQLite
- Dashboard 查看请求量、Token、成本、缓存、成功率和请求性能
- 支持按时间范围、模型、API Key、来源和请求结果筛选用量明细
- Request Events 提供请求级明细查看、筛选、分页、导出和自定义显示
- 分析页面提供用量趋势、成本分析、模型/API Key/AI Provider 构成和时段热力图
- API Key 独立查询页，可按 CPA API Key 查看专属用量
- 凭证页面展示 Auth File 与 AI Provider 使用情况，支持限额查询、刷新、巡检和排序
- 支持多 Provider quota 窗口用量与限额展示
- 可维护模型价格，用于成本估算和统计展示
- 自动同步 CPA Auth Files、API Keys、AI Providers 等 metadata 变化
- 可选密码登录保护、SQLite 备份、Docker/Docker Compose 和 systemd 部署

## 赞助与特别感谢

- 感谢 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI) 提供本项目所依赖的上游 CPA 基础与数据来源。
- 感谢 [@YouShouldBetOnMe](https://github.com/YouShouldBetOnMe) 对 CPA Usage Keeper 的支持。

## 快速开始

> 使用前请确认 CPA 配置已开启 usage 统计：`usage-statistics-enabled: true`。

推荐部署路径：

- 第一次部署 CPA + Keeper：优先使用 [Docker Compose](#docker-compose推荐)。
- CPA 已在宿主机运行：使用 [Docker](#dockercpa-已在宿主机运行)。
- macOS：使用 [Homebrew](#macos-homebrew)。
- Linux 不使用容器：使用 [Linux 二进制](#linux-二进制)。

公网部署建议启用 `AUTH_ENABLED=true`，并配置 `LOGIN_PASSWORD` 保护数据。

## 部署方式

### Docker Compose（推荐）

下面是一个同时部署 CPA 和 CPA Usage Keeper 的最简参考示例。

仓库中的 `docker-compose.example.yml` 是 Keeper 专用模板。CPA 已经部署好时可以直接使用，也可以把其中的 `cpa-usage-keeper` 服务合并到你自己的 Compose 项目里。

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
      TZ: Asia/Shanghai # 设置容器时区，日志时间会按该时区显示。
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

如果想用 `.env` 文件管理 Keeper 配置，可以删除 `cpa-usage-keeper` 服务里的 `environment` 块，并添加 `env_file`：

```yaml
    env_file:
      - .env
    volumes:
      - ./keeper:/data
```

然后在宿主机的 `docker-compose.yml` 同一目录创建 `.env` 文件，例如：

```env
TZ=Asia/Shanghai
CPA_BASE_URL=http://cli-proxy-api:8317
CPA_MANAGEMENT_KEY=replace-with-your-management-key
AUTH_ENABLED=true
LOGIN_PASSWORD=replace-with-your-login-password
```

Docker Compose 会把 `.env` 中的配置注入 Keeper 容器环境变量。如果 CPA 使用非默认 Redis/RESP 地址，再在 `.env` 中设置 `REDIS_QUEUE_ADDR`。

启动：

```bash
docker compose up -d
```

停止：

```bash
docker compose down
```

CPA 文件放在 `./cpa`，CPA Usage Keeper 数据放在 `./keeper`。

### Docker（CPA 已在宿主机运行）

复制配置模板并编辑，至少设置 `CPA_BASE_URL` 和 `CPA_MANAGEMENT_KEY`。公网部署建议同时设置 `AUTH_ENABLED=true` 和 `LOGIN_PASSWORD`。如果 CPA 使用非默认 Redis/RESP 地址，再设置 `REDIS_QUEUE_ADDR`：

```bash
cp .env.example .env
vim .env
```

宿主机运行 CPA 时，`.env` 中通常需要这样设置：

```env
CPA_BASE_URL=http://host.docker.internal:8317
CPA_MANAGEMENT_KEY=replace-with-your-management-key
AUTH_ENABLED=true
LOGIN_PASSWORD=replace-with-your-login-password
```

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

Homebrew 是 macOS 推荐的二进制安装方式。它会从 CPA Usage Keeper 的 tap 安装 macOS 包，后续新版本也可以用标准 Homebrew 命令升级。

安装：

```bash
brew tap Willxup/cpa-usage-keeper
brew install cpa-usage-keeper
```

编辑自动生成的配置文件，至少设置 `CPA_BASE_URL` 和 `CPA_MANAGEMENT_KEY`。公网部署建议同时设置 `AUTH_ENABLED=true` 和 `LOGIN_PASSWORD`：

```bash
vim "$(brew --prefix)/etc/cpa-usage-keeper.env"
```

启动后台服务：

```bash
brew services start cpa-usage-keeper
```

Homebrew 会把 Keeper 数据放在 `$(brew --prefix)/var/cpa-usage-keeper`，stdout 日志放在 `$(brew --prefix)/var/log/cpa-usage-keeper.log`，stderr 日志放在 `$(brew --prefix)/var/log/cpa-usage-keeper.err.log`。

常用命令：

```bash
brew services list
brew services restart cpa-usage-keeper
brew update
brew upgrade cpa-usage-keeper
```

### Linux 二进制

#### 下载

在 [Releases](https://github.com/Willxup/cpa-usage-keeper/releases/latest) 下载对应架构的 Linux 二进制包，或使用命令行下载：

```bash
curl -L -o cpa-usage-keeper.tar.gz "<替换为 Linux 二进制包下载地址>"
mkdir -p cpa-usage-keeper
tar -xzf cpa-usage-keeper.tar.gz -C cpa-usage-keeper --strip-components=1
cd cpa-usage-keeper
```

请在 Releases 页面复制 `linux_amd64` 或 `linux_arm64` 包的下载地址，并替换上面命令中的占位符。

#### 配置和运行

复制配置模板并编辑，具体配置项参考 [配置](#配置)：

```bash
cp .env.example .env
vim .env
./cpa-usage-keeper
```

#### systemd 常驻运行

Linux 二进制包内置 `cpa-usage-keeper.service`，可直接注册为 `systemd` 服务。启动后进程由 systemd 托管，关闭 SSH 或终端不会结束进程。

`systemd` 的 `WorkingDirectory` 需要绝对路径。下面的 `sed` 命令会把当前目录自动写入 service 文件：

```bash
sudo cp cpa-usage-keeper.service /etc/systemd/system/cpa-usage-keeper.service # 复制 service 文件到 systemd 目录
sudo sed -i "s|__CPA_USAGE_KEEPER_DIR__|$(pwd)|g" /etc/systemd/system/cpa-usage-keeper.service # 写入当前目录作为 WorkingDirectory
sudo systemctl daemon-reload # 重新加载 systemd 配置
sudo systemctl enable --now cpa-usage-keeper # 设置开机自启并立即启动服务
```

常用命令：

```bash
sudo systemctl status cpa-usage-keeper # 查看服务状态
sudo journalctl -u cpa-usage-keeper -f # 实时查看服务日志
sudo systemctl restart cpa-usage-keeper # 重启服务
```

## 配置

复制配置模板：

```bash
cp .env.example .env
```

新手部署时优先看“最小必填”和“Web 访问与反代”两组，其它配置保持默认即可。

### 最小必填

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `CPA_BASE_URL` | 是 | - | Keeper 服务端访问 CPA 的地址。Docker Compose 内通常是 `http://cli-proxy-api:8317`，可以是内网地址或容器服务名 |
| `CPA_MANAGEMENT_KEY` | 是 | - | CPA management key，用于读取 CPA 管理接口数据 |

### Web 访问与反代

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `APP_PORT` | 否 | `8080` | Keeper HTTP 监听端口 |
| `APP_BASE_PATH` | 否 | 根路径 | Keeper 子路径部署前缀，例如 `/keeper`；留空表示部署在 `/` |
| `CPA_PUBLIC_URL` | 否 | 当前浏览器同源根路径 | 浏览器访问 CPA 的公开地址，用于“返回 CPA”跳转 |

`APP_BASE_PATH` 必须为空或以 `/` 开头；例如 `/cpa`，`/cpa/` 会规范为 `/cpa`。

`CPA_PUBLIC_URL` 可填写域名、带协议的完整地址或相对路径，例如 `https://cpa.example.com`、`https://cpa.example.com/cpa/` 或 `/cpa/`。前端会自动追加 `management.html`，并兼容末尾已有 `/` 或已经填写到 `management.html` 的情况。未配置时，“返回 CPA”默认跳转到当前浏览器同源根路径下的 `/management.html`；如果 CPA 和 Keeper 的外部域名、端口或路径不一致，请显式设置 `CPA_PUBLIC_URL`。

`CPA_BASE_URL` 只用于服务端访问 CPA，可以是 Docker 内部服务名或内网地址；不要把它当作浏览器跳转地址使用。

### 登录保护

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `AUTH_ENABLED` | 否 | `false` | 是否启用登录保护 |
| `LOGIN_PASSWORD` | 鉴权启用时必填 | - | 登录密码 |
| `AUTH_SESSION_TTL` | 否 | `168h` | 登录 session 有效时长 |

### 时区与请求行为

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `TZ` | 否 | `Asia/Shanghai` | 统计和展示使用的时区；Today、按天统计、页面时间、日志时间和每日清理时间都会按这个时区计算 |
| `REQUEST_TIMEOUT` | 否 | `30s` | 请求 CPA HTTP 接口和 Redis 队列的超时时间 |
| `TLS_SKIP_VERIFY` | 否 | `false` | 跳过 CPA HTTPS 和 Redis 队列 TLS 的证书验证；仅在使用自签名证书时启用 |

### Auth Files 限额刷新

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `QUOTA_AUTO_REFRESH_ENABLED` | 否 | `false` | 是否启用 Auth Files 限额自动刷新；仅在后台页面可见并持续心跳时执行 |
| `QUOTA_AUTO_REFRESH_INTERVAL` | 否 | `5m` | Auth Files 限额自动刷新间隔，最低 `60s`，仅在后台页面活跃时生效 |
| `QUOTA_REFRESH_WORKER_LIMIT` | 否 | `10` | Auth Files 限额刷新队列最大并发数，最大 `100` |

### Redis 队列高级配置

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `REDIS_QUEUE_ADDR` | 否 | `CPA_BASE_URL` 主机名 + `8317` | CPA Redis/RESP TCP 地址；一般保持空即可。非默认端口或单独暴露 Redis stream 时填写 `host:port` |
| `REDIS_QUEUE_TLS` | 否 | `false` | 是否使用 TLS 连接 Redis 队列；显式设置 `REDIS_QUEUE_ADDR` 且需要 TLS 时设为 `true` |
| `REDIS_QUEUE_BATCH_SIZE` | 否 | `10000` | 每次最多拉取的队列记录数 |
| `REDIS_QUEUE_IDLE_INTERVAL` | 否 | `1s` | 队列为空时的检查间隔 |

### 存储、日志与备份

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `WORK_DIR` | 否 | `./data` | 应用工作目录；数据库、日志和备份默认分别写入 `app.db`、`logs/`、`backups/` |
| `LOG_LEVEL` | 否 | `info` | 日志级别 |
| `LOG_FILE_ENABLED` | 否 | `true` | 是否写入持久化日志文件 |
| `LOG_RETENTION_DAYS` | 否 | `7` | 日志保留天数；`0` 表示不自动清理 |
| `BACKUP_ENABLED` | 否 | `true` | 是否启用 SQLite 数据库备份 |
| `BACKUP_INTERVAL` | 否 | `24h` | 数据库备份间隔 |
| `BACKUP_RETENTION_DAYS` | 否 | `7` | 备份保留天数 |

### 内置 HTTPS

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `TLS_ENABLED` | 否 | `false` | 是否让 Keeper 自己启用 HTTPS/TLS |
| `TLS_CERT_FILE` | 启用 TLS 时必填 | - | HTTPS 证书文件路径 |
| `TLS_KEY_FILE` | 启用 TLS 时必填 | - | HTTPS 私钥文件路径 |

通常建议在 nginx、Caddy 等反向代理层处理 HTTPS。只有需要 Keeper 进程直接提供 HTTPS 时，才设置 `TLS_ENABLED=true`，并填写 `TLS_CERT_FILE` 和 `TLS_KEY_FILE`；相对路径会按 `.env` 所在目录解析。

安全与数据说明：

- SQLite 数据库备份会保存应用数据库中的原始数据，备份文件不做加密。
- 面向浏览器的 API 会对 key-like source/lookup 字段做脱敏或稳定公开标识映射，但不会修改数据库原始值。
- 公开部署建议开启 `AUTH_ENABLED=true`，并在反向代理层配置 HTTPS。
- 登录 session hash 存在 SQLite 中，服务重启后仍会保持有效，直到用户退出登录或超过 `AUTH_SESSION_TTL`。
- Redis inbox 原始消息会自动清理：成功数据保留到当天结束后清理，失败数据保留 7 天。

## Nginx反代

部署到 `/cpa` 时设置 `APP_BASE_PATH=/cpa`，并在反向代理中保留该前缀：

```nginx
location /cpa/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

如果 CPA 管理页和 Keeper 使用同一个浏览器域名，且 CPA 管理页在该域名根路径的 `/management.html`，可以不配置 `CPA_PUBLIC_URL`。例如 Keeper 在 `https://cpa.example.com/keeper/` 时，“返回 CPA”会默认跳转到 `https://cpa.example.com/management.html`。

如果 CPA 管理页在其它域名、端口或路径下，请配置 `CPA_PUBLIC_URL`，例如：

```env
CPA_PUBLIC_URL=https://cpa.example.com
```

## 项目结构

```text
cmd/server/              应用入口
internal/api/            HTTP 路由与处理器
internal/app/            应用装配与启动
internal/auth/           session 鉴权与持久化
internal/backup/         SQLite 数据库备份管理
internal/benchmark/      聚合性能基准测试辅助
internal/config/         环境配置加载
internal/cpa/            CPA 客户端与类型定义
internal/entities/       GORM 数据模型
internal/helper/         后端通用辅助方法与前端展示字段脱敏
internal/logging/        日志初始化与保留策略
internal/poller/         后台队列消费与 metadata 同步
internal/quota/          quota 缓存、刷新与查询服务
internal/repository/     SQLite 访问与聚合逻辑
internal/service/        usage、pricing 与身份数据服务
internal/timeutil/       项目时区与时间工具
internal/updatecheck/    GitHub Release 更新检查
internal/version/        构建版本信息
deploy/linux/            Linux systemd 服务文件
web/                     React + TypeScript 前端
```

## 本地开发

### 前置依赖

- Go 1.22+
- Node.js 22+
- npm
- 已运行的 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI)

### 本地启动

1. 复制并编辑本地配置，至少设置 `CPA_BASE_URL` 和 `CPA_MANAGEMENT_KEY`。如果 CPA 的 Redis/RESP 端口不是默认 `8317`，同时设置 `REDIS_QUEUE_ADDR`：

```bash
cp .env.example .env
vim .env
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

前端开发服务器默认把 `/api` 代理到 `http://127.0.0.1:8080`，访问 `http://127.0.0.1:5173` 即可联调。如果后端使用了其他端口：

```bash
VITE_API_PROXY_TARGET=http://127.0.0.1:9090 npm --prefix ./web run dev -- --host 127.0.0.1
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

## Star History

<p>
  <img src="https://api.star-history.com/chart?repos=willxup/cpa-usage-keeper&type=date&legend=top-left" />
</p>


## License

本项目基于 [MIT License](./LICENSE) 开源。
