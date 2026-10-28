# OIDC 管理员登录

[返回 README](../README.zh.md) · [English](./oidc.md)

OIDC 是可选的管理员登录方式，默认关闭，仅用于独立页面的管理员登录，不用于 API Key 登录或 CPAMC 嵌入模式。密码登录仍可使用。

## 接入步骤

1. 注册带密钥的服务器端客户端，启用 Authorization Code Flow、PKCE S256，并允许 Keeper 请求的 `openid profile email` scopes。
2. 注册 Keeper 的公开回调地址：根路径部署为 `https://keeper.example.com/api/v1/auth/oidc/callback`；子路径为 `/keeper` 时，使用 `https://keeper.example.com/keeper/api/v1/auth/oidc/callback`。
3. 填写下表配置，设置管理员准入策略，然后重启 Keeper。从与回调相同的公开 origin 打开 Keeper，选择 OIDC 登录。

## 配置项

保留 `AUTH_ENABLED=true` 和私有的 `LOGIN_PASSWORD`，以便身份平台不可用时使用密码登录。

| 变量 | 启用时要求 | 说明 |
| --- | --- | --- |
| `AUTH_OIDC_ENABLED` | 设为 `true` | 默认 `false` |
| `AUTH_OIDC_ISSUER` | 必填 | 平台公布的完整 Issuer，包含租户路径 |
| `AUTH_OIDC_CLIENT_ID` | 必填 | 已注册的客户端 ID |
| `AUTH_OIDC_CLIENT_SECRET` | 必填 | 客户端密钥 |
| `AUTH_OIDC_REDIRECT_URL` | 必填 | 与平台注册值完全一致的公开回调地址 |
| `AUTH_OIDC_ALLOWED_SUBJECTS` | 可选 | 精确匹配、区分大小写的 `sub` JSON 数组，默认 `[]` |
| `AUTH_OIDC_PROMPT_LOGIN` | 可选 | 默认 `true`，发送 `prompt=login` 要求重新认证；设为 `false` 允许复用身份平台登录状态 |

## 限制管理员访问

**未设置 `AUTH_OIDC_ALLOWED_SUBJECTS` 或设置为空数组时，通过该客户端认证的所有用户都会成为 Keeper 管理员。** 请在身份平台限制应用访问权限，或配置 subject 白名单：

```env
AUTH_OIDC_ALLOWED_SUBJECTS='["user-123","user-456"]'
```

使用平台实际签发给此客户端的 `sub`，不能用邮箱、显示名或猜测的用户 ID 替代。

旧配置 `AUTH_OIDC_ALLOWED_USERS` 非空会阻止启动。迁移时，应先改为实际 subject，或在身份平台落实相应访问限制，再删除旧配置。

## 会话与退出

- 会话有效期由 `AUTH_SESSION_TTL` 控制，可在会话管理中撤销。
- 退出仅撤销 Keeper 会话，不会退出身份平台；允许复用平台登录状态时，再次登录可能自动完成。
- 关闭 OIDC、切换 Issuer/Client ID 或收紧 subject 白名单，会在重启后撤销受影响的会话；仅更换客户端密钥或调整 `AUTH_OIDC_PROMPT_LOGIN` 不会撤销已有会话。
- 平台侧删号或收紧权限不会自动撤销 Keeper 会话，需手动撤销或等待过期。
- 回退到不支持 OIDC 的 Keeper 版本前，先撤销所有 OIDC 会话，再停止服务并替换程序。

## 网络与排障

- **地址与连通性：**Issuer 必须与 Discovery 和 ID Token 中的 `iss` 完全一致，不能随意替换为容器内网地址。Keeper 服务端须能访问 Discovery、Token 和 JWKS 端点，浏览器须能访问授权端点。
- **HTTPS：**推荐所有相关地址使用 HTTPS。HTTP 虽可使用，但无法防止窃听或 Discovery/JWKS 被篡改。内部 CA 须受服务信任；HTTPS 失败不会降级或跳过校验。
- **反向代理：**保留公开 `Host` 和 `X-Forwarded-Proto`（或提供 `X-Forwarded-Host`），并配置实际的 `TRUSTED_PROXY_CIDRS`。访问日志应过滤回调查询参数，避免记录授权码和 state。
- **登录中断：**登录尝试五分钟过期；服务重启或超时后，重新点击登录按钮。

## 兼容范围

支持单个身份平台、Discovery/JWKS、签名 ID Token，以及 `client_secret_basic` / `client_secret_post` 客户端认证。不支持公共客户端、`form_post`、JWE、JWT Client Authentication、多 Issuer 选择或全局单点注销。
