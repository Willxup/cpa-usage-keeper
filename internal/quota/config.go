package quota

import "time"

const (
	// RefreshWorkerLimit 控制限额刷新队列的最大并发 worker 数，手动刷新和自动刷新共用同一套队列。
	RefreshWorkerLimit = 10

	// RefreshTaskTimeout 限制单次 provider 限额查询的最长执行时间，避免上游长时间无响应时占住 worker。
	RefreshTaskTimeout = 20 * time.Second

	// RefreshTaskCooldown 是每个 worker 完成一次刷新后的冷却时间，冷却结束后才允许处理下一条任务。
	RefreshTaskCooldown = 1 * time.Second

	// RefreshTransientTaskTTL 是普通失败在内存中的短期保留时间，只用于当前页面轮询读取失败结果。
	RefreshTransientTaskTTL = 20 * time.Minute

	// RefreshErrorCacheTTL 是可恢复展示的 HTTP 错误缓存时间，过期后自动刷新可以重新尝试。
	RefreshErrorCacheTTL = 4 * time.Hour

	// CodexRateLimitResetCreditsConsumeURL 是 Codex 官方 reset credit 消费端点，reset 操作固定调用它。
	CodexRateLimitResetCreditsConsumeURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"

	// CodexRateLimitResetCreditsURL 返回当前账号每次可用 reset credit 及其过期时间。
	CodexRateLimitResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"

	// xaiGrokClientVersion 与 CPA 当前 Grok CLI chat-proxy 请求保持一致。
	xaiGrokClientVersion = "0.2.93"
)

// RefreshCacheableHTTPStatusCodes 定义会写入页面恢复缓存并被自动刷新跳过的 provider HTTP 状态码。
var RefreshCacheableHTTPStatusCodes = map[int]struct{}{
	401: {},
	402: {},
}

type APICallConfig struct {
	Method  string
	URL     string
	Headers map[string]string
}

type ProviderConfigs struct {
	Antigravity         []APICallConfig
	Codex               APICallConfig
	GeminiCLI           APICallConfig
	GeminiCLICodeAssist APICallConfig
	ClaudeUsage         APICallConfig
	ClaudeProfile       APICallConfig
	Kimi                APICallConfig
	XAIWeekly           APICallConfig
	XAIMonthly          APICallConfig
}

func DefaultProviderConfigs() ProviderConfigs {
	return ProviderConfigs{
		Antigravity: []APICallConfig{
			{
				Method: "POST",
				URL:    "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
				Headers: map[string]string{
					"Authorization": "Bearer $TOKEN$",
					"Content-Type":  "application/json",
					"User-Agent":    "antigravity/1.11.5 windows/amd64",
				},
			},
			{
				Method: "POST",
				URL:    "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels",
				Headers: map[string]string{
					"Authorization": "Bearer $TOKEN$",
					"Content-Type":  "application/json",
					"User-Agent":    "antigravity/1.11.5 windows/amd64",
				},
			},
			{
				Method: "POST",
				URL:    "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
				Headers: map[string]string{
					"Authorization": "Bearer $TOKEN$",
					"Content-Type":  "application/json",
					"User-Agent":    "antigravity/1.11.5 windows/amd64",
				},
			},
		},
		Codex: APICallConfig{
			Method: "GET",
			URL:    "https://chatgpt.com/backend-api/wham/usage",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
				"User-Agent":    "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal",
			},
		},
		GeminiCLI: APICallConfig{
			Method: "POST",
			URL:    "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
			},
		},
		GeminiCLICodeAssist: APICallConfig{
			Method: "POST",
			URL:    "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
				"Content-Type":  "application/json",
			},
		},
		ClaudeUsage: APICallConfig{
			Method: "GET",
			URL:    "https://api.anthropic.com/api/oauth/usage",
			Headers: map[string]string{
				"Authorization":  "Bearer $TOKEN$",
				"Content-Type":   "application/json",
				"anthropic-beta": "oauth-2025-04-20",
			},
		},
		ClaudeProfile: APICallConfig{
			Method: "GET",
			URL:    "https://api.anthropic.com/api/oauth/profile",
			Headers: map[string]string{
				"Authorization":  "Bearer $TOKEN$",
				"Content-Type":   "application/json",
				"anthropic-beta": "oauth-2025-04-20",
			},
		},
		Kimi: APICallConfig{
			Method: "GET",
			URL:    "https://api.kimi.com/coding/v1/usages",
			Headers: map[string]string{
				"Authorization": "Bearer $TOKEN$",
			},
		},
		XAIWeekly: APICallConfig{
			Method:  "GET",
			URL:     "https://cli-chat-proxy.grok.com/v1/billing?format=credits",
			Headers: xaiRequestHeaders(),
		},
		XAIMonthly: APICallConfig{
			Method:  "GET",
			URL:     "https://cli-chat-proxy.grok.com/v1/billing",
			Headers: xaiRequestHeaders(),
		},
	}
}

func xaiRequestHeaders() map[string]string {
	userAgent := "grok-pager/" + xaiGrokClientVersion + " grok-shell/" + xaiGrokClientVersion + " (macos; aarch64)"
	return map[string]string{
		"Authorization":         "Bearer $TOKEN$",
		"x-xai-token-auth":      "xai-grok-cli",
		"x-grok-client-version": xaiGrokClientVersion,
		"Accept":                "*/*",
		"User-Agent":            userAgent,
	}
}

func (c ProviderConfigs) APICallTemplates() []APICallConfig {
	templates := make([]APICallConfig, 0, len(c.Antigravity)+8)
	templates = append(templates, c.Antigravity...)
	templates = append(templates,
		c.Codex,
		c.GeminiCLI,
		c.GeminiCLICodeAssist,
		c.ClaudeUsage,
		c.ClaudeProfile,
		c.Kimi,
		c.XAIWeekly,
		c.XAIMonthly,
	)
	return templates
}
