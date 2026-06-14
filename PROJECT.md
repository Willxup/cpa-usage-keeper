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
- 分析页面：用量趋势、成本分析、模型/API Key/AI Provider 构成、时段热力图
- API Key 独立查询页
- 凭证页面：Auth File 与 AI Provider 使用情况，支持限额查询、刷新、巡检和排序
- 多 Provider quota 窗口用量与限额展示
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

## 进度记录

| 日期 | 说明 |
|------|------|
| 2026-06-11 | 从外部项目迁移至 `D:\AI_Workspace\Project\CPAstats` 并加入项目索引管理 |
