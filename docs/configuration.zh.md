# 高级配置

[返回 README](../README.zh.md) · [English](./configuration.md)

常用参数见 README 的[配置](../README.zh.md#配置)，完整变量模板见 [`.env.example`](../.env.example)。

## Web 访问与反代

### 监听与子路径

`--host` 优先于 `APP_HOST`；均未设置时监听所有可用接口。Docker/Compose 应保持 `APP_HOST` 为空，仅限宿主机访问时使用端口映射 `127.0.0.1:8080:8080`。

`APP_BASE_PATH` 必须为空或以 `/` 开头，尾部 `/` 会被移除。反向代理必须保留该前缀，参见 [Nginx 示例](../README.zh.md#nginx-反向代理)。

### CPA 地址

- `CPA_BASE_URL`：Keeper 服务端使用，可为内网地址或 Docker 服务名。
- `CPA_PUBLIC_URL`：浏览器使用，控制“返回 CPA”和跨域嵌入。CPA 与 Keeper 同源且位于 `/management.html` 时可留空；域名、端口或路径不同时应设置。
- 跨域嵌入要求 `CPA_PUBLIC_URL` 为带主机名的完整 HTTP/HTTPS URL；相对路径只影响跳转。

### 代理信任

仅本机 loopback 和 `TRUSTED_PROXY_CIDRS` 指定的代理可以提供可信的 `X-Forwarded-For`。本机 Nginx 无需额外设置；容器、远程代理或 CDN 场景应配置实际代理网段。不要照搬示例网段或信任所有来源，全网 CIDR 会被拒绝。

## Compose 可选环境文件

CPA + Keeper 联合部署模板也可从 `./keeper/.env` 读取配置；文件不存在时不影响启动。`environment` 优先于 `env_file`：改用文件中的值时，须删除 Compose `environment` 中对应项，包括空值占位。

## 内置 HTTPS

通常由反向代理提供 HTTPS；Keeper 直接提供 HTTPS 时使用以下配置。

原生部署的证书相对路径按 Keeper 读取的环境文件目录解析；Compose 须挂载证书和私钥，并填写容器内路径，`env_file` 不会挂载文件。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `TLS_ENABLED` | `false` | 设为 `true` 启用内置 HTTPS |
| `TLS_CERT_FILE` | 空 | 启用时必填，证书文件路径 |
| `TLS_KEY_FILE` | 空 | 启用时必填，私钥文件路径 |

## Credentials 限额刷新

常规定时刷新在 Credentials 巡检弹窗中设置；以下参数仅用于调节并发或排障，均可选。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `QUOTA_REFRESH_WORKER_LIMIT` | `10` | 手动及定时刷新共用的最大并发数，上限 `100` |
| `QUOTA_UPSTREAM_RESPONSES_ENABLED` | `false` | 缓存各凭证最近的原始限额响应，供浏览器 Network 面板排障；可能包含账号数据 |

## Redis 队列高级配置

以下变量均可选。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `REDIS_QUEUE_ADDR` | 从 `CPA_BASE_URL` 推导 | 显式设置时优先使用该地址；留空时沿用 `CPA_BASE_URL` 的主机和显式端口，未指定端口时使用 `8317`。独立地址填写 `host:port` |
| `REDIS_QUEUE_TLS` | `false` | `true` 强制启用 TLS；未指定队列地址时，TLS 默认跟随 `CPA_BASE_URL` 的协议。`false` 不会覆盖自动推导的 TLS 设置 |
| `REDIS_QUEUE_BATCH_SIZE` | `10000` | 每次最多拉取的记录数 |
| `REDIS_QUEUE_IDLE_INTERVAL` | `1s` | 队列为空时的检查间隔 |

## 会话与消息保留

登录会话有效期由 `AUTH_SESSION_TTL` 控制。OIDC 的退出及撤销规则见 [OIDC 会话说明](./oidc.zh.md#会话与退出)。Redis inbox 消息成功后保留到当天结束，失败后保留 7 天。

## 存储与日志细节

常用变量见 README 的[存储、日志与备份](../README.zh.md#存储日志与备份)。

- **归档：**每天按配置时区在 04:30 归档超过 90 个本地自然日的原始请求。常规页面不直接查询归档明细。
- **删除：**归档默认永久保留。`USAGE_RAW_RETENTION_DAYS>=90` 时，按请求日期清理超期归档，不改变最近 90 天数据的保留窗口；历史汇总和备份使用各自的保留策略。
- **影响：**删除的原始记录不能用于历史重算，SQLite 文件也不一定立即缩小。

文件日志使用 `cpa-usage-keeper-YYYY-MM-DD.log`；error、fatal、panic 还写入 `cpa-usage-keeper-error-YYYY-MM-DD.log`，错误日志固定保留历史 30 个本地自然日及当天。
