# CPA Usage Keeper — PROJECT.md

## 项目概述

CPA Usage Keeper 是一个独立的 CPA 用量持久化与可视化服务。它依赖 CLIProxyAPI（CPA）作为后端数据来源，从 CPA Redis usage 队列消费事件并写入 SQLite，定时拉取 CPA metadata，暴露聚合 API，并提供内置 Web Dashboard 用于查看用量、定价、请求健康度和模型/API 维度的统计分析。

**创建日期**：2026-06-11（从外部项目迁移至本工作区管理）
**状态**：进行中

---

## 文件结构

```
CPAstats/
├── PROJECT.md                   # 项目文档（本文件）
├── cmd/server/main.go           # 入口文件
├── internal/
│   ├── api/                     # HTTP API 路由与处理器
│   ├── app/                     # 应用层：同步、备份、维护
│   ├── auth/                    # 会话认证
│   ├── backup/                  # SQLite 备份
│   ├── config/                  # 配置管理
│   ├── cpa/                     # CPA 客户端、Redis 队列消费
│   ├── entities/                # 数据实体定义
│   ├── helper/                  # 工具函数
│   ├── logging/                 # 日志系统
│   ├── poller/                  # 轮询器（HTTP/Redis/Subscription）
│   ├── quota/                   # 多 Provider 配额管理（配额刷新、巡检、归一化）
│   ├── repository/              # 数据仓库（SQLite 操作、迁移）
│   ├── service/                 # 业务服务层
│   ├── timeutil/                # 时区工具
│   ├── updatecheck/             # 更新检查
│   └── version/                 # 版本信息
├── web/                         # React/TypeScript 前端
│   ├── src/
│   │   ├── components/          # UI 组件
│   │   ├── pages/               # 页面（UsagePage, KeyOverviewPage, LoginPage）
│   │   ├── hooks/               # 自定义 Hook
│   │   ├── stores/              # 状态管理
│   │   ├── i18n/                # 国际化
│   │   └── lib/                 # API 客户端与类型定义
│   └── vite.config.js
├── deploy/linux/                # Linux systemd 部署
├── Dockerfile                   # Docker 构建
├── docker-compose.example.yml   # Docker Compose 示例
└── docker-entrypoint.sh         # Docker 入口脚本
```

---

## 关联资源

- **上游项目**：[CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) — 后端 CPA 数据来源
- **官方镜像**：`ghcr.io/willxup/cpa-usage-keeper:latest`
- **Docker Hub 镜像**：可参考 `docker-compose.example.yml` 中的镜像配置
- **CI/CD**：GitHub Actions（`.github/workflows/`）— 含 binary release、docker publish、CI

---

## 核心内容

### 技术栈

| 层 | 技术 |
|----|------|
| 后端语言 | Go（Go Modules） |
| 前端框架 | React 18 + TypeScript |
| 构建工具 | Vite |
| 数据库 | SQLite |
| 缓存/队列 | Redis（消费 CPA usage 队列） |
| 容器化 | Docker / Docker Compose |
| 部署 | systemd（Linux） |

### 功能特性

- 持久保存 CPA usage 数据到 SQLite
- Dashboard 查看请求量、Token、成本、缓存、成功率和请求性能
- 按时间范围、模型、API Key、来源和请求结果筛选用量明细
- Request Events 请求级明细查看、筛选、分页、导出和自定义显示
- 失败事件详情归一化解析：支持 CPA `fail` 字段、OpenAI/Codex、Anthropic、Gemini、OpenRouter 多种上游错误结构
- 失败事件 `request_id` 缺失时自动生成 fallback EventKey，避免事件丢失
- 失败详情脱敏：自动清除 Authorization、Cookie、sk-key、token、URL 等敏感信息
- 分析页面：用量趋势、成本分析、模型/API Key/AI Provider 构成、时段热力图
- API Key 独立查询页
- 凭证页面：Auth File 与 AI Provider 使用情况，支持限额查询、刷新、巡检和排序
- 多 Provider quota 窗口用量与限额展示
- Codex 429 cooldown 自动处理（支持 `fail.status_code` 回退）
- 模型价格维护（成本估算和统计展示）
- 自动同步 CPA Auth Files、API Keys、AI Providers 等 metadata
- 可选密码登录保护、SQLite 备份

### 配置方式

通过 `.env` 或环境变量配置，关键配置项：

- `CPA_BASE_URL` — CPA 服务地址
- `CPA_MANAGEMENT_KEY` — CPA 管理密钥
- `REDIS_QUEUE_ADDR` — Redis 队列地址
- `LISTEN_ADDR` — 监听地址（默认 `:8080`）
- `AUTH_ENABLED` / `LOGIN_PASSWORD` — 登录保护
- `BACKUP_ENABLED` / `BACKUP_DIR` / `BACKUP_INTERVAL` — SQLite 备份策略

---

## 备注

- 本项目的目录名为 `CPAstats`，项目全称为 "CPA Usage Keeper"
- 本项目最初不在本工作区管理，现迁移至工作区进行统一跟踪
- 使用前需确认 CPA 已开启 `usage-statistics-enabled: true`

---

## 生产部署信息

### 服务器

- **SSH 别名**：`sivan-api`（在 `~/.ssh/config` 中定义）
- **主机**：`168.144.38.43:22022`，用户 `root`
- **系统**：Ubuntu 24.04.3 LTS，内存 1.9GB + 1GB swap
- **登录方式**：SSH 密钥免密（本机密钥 `do_newapi_ed25519`）
- **同服务器共存服务**：CLIProxyAPI、New API、MySQL、Redis、Caddy、mail-fetch-proxy 等

### 部署架构

- **部署方式**：Docker Compose，**本地源码构建镜像**
- **部署目录**：`/opt/cpa-usage-keeper/`
- **compose 文件**：`/opt/cpa-usage-keeper/docker-compose.yml`
- **镜像**：`cpa-usage-keeper:latest`（多阶段构建，最终镜像约 36.7MB）
- **容器**：`cpa-usage-keeper`，端口 `127.0.0.1:18080 → 8080`
- **数据卷**：`./data:/data`（bind mount，SQLite `app.db` + 日志 + 备份）
- **网络**：`newapi-net`（外部网络，与 CPA/Redis 等互通）
- **反代**：经 Caddy 反向代理对外暴露
- **配置**：`/opt/cpa-usage-keeper/.env`（生产密钥，不在仓库中）

### 自动更新服务

- **已于 2026-06-15 移除**：原 `cpa-usage-keeper-auto-update.timer` + `.service` + `/usr/local/bin/cpa-usage-keeper-auto-update.sh` 脚本已全部删除。原因是该脚本逻辑是拉官方 `ghcr.io/willxup/cpa-usage-keeper:latest` 镜像，与本地源码构建的部署方式不匹配，启用后会引入官方镜像替换本地构建版本，且之前一直因容器名冲突报错 failed。
- **后续更新方式**：手动源码构建部署（流程见下方"更新部署流程"），不再自动更新。
- 说明：共用的 `/usr/local/bin/apply-dynamic-resource-limits.sh` 保留（CPA 自动更新脚本也在引用）。

### 更新部署流程（手动，源码构建）

1. 本地切到 `main` 分支并 `git pull` 确认最新
2. 打包源码（排除 `.git`、`data`、`node_modules`、`web/dist`）
3. scp 上传到服务器，解压到 `/opt/cpa-usage-keeper-src/`
4. `docker build -t cpa-usage-keeper:new .`（注意内存，1.9GB 服务器前端+Go 构建临界）
5. `docker tag cpa-usage-keeper:latest cpa-usage-keeper:rollback-<时间戳>` 备份回滚点
6. `docker tag cpa-usage-keeper:new cpa-usage-keeper:latest`
7. 若容器是手动 docker run 创建的（非 compose）：先 `docker stop && docker rm`，再 `docker compose up -d`
8. 健康检查：轮询 `http://127.0.0.1:18080/healthz` 返回 `{"status":"ok"}`
9. 清理：删除临时源码目录、tar 包、rollback 镜像、`docker builder prune -af`

---

## 进度记录

| 日期 | 说明 |
|------|------|
| 2026-06-11 | 从外部项目迁移至 `D:\AI_Workspace\Project\CPAstats` 并加入项目索引管理 |
| 2026-06-15 | 部署 main 分支最新代码（`ad5c6a7`）到 sivan-api 生产环境；修正 compose 镜像名（ghcr → 本地构建）；清理临时文件、回滚镜像和 1.6GB 构建缓存；移除不兼容的 keeper 自动更新服务（timer+service+脚本），后续改用手动源码构建部署 |
| 2026-06-16 | 部署 `fix-failure-details-and-ci` 分支到生产环境；归一化失败详情解析 + CI embed 修复 + i18n 对齐；修复 Gemini status 回退 + Cookie JSON 脱敏 + error code 小写化；回滚镜像 `rollback-20260616b` |
| 2026-06-17 | 合并 PR #16（CPA `fail` 字段解析 + `request_id` 缺失兜底 + TS 类型对齐）和 PR #17（`ExtractCodex429Telemetry` cooldown 回退修复 + 优先级测试 + gitignore 临时文件模式）；部署最新 main 到生产环境 |
