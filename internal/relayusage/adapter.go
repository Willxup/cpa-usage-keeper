package relayusage

import (
	"context"
)

// Adapter 查询单个中转商平台的用量。Fetch 接收明文 API key（来自 UsageIdentity.LookupKey）
// 与 base-url；多数平台的用量接口 URL 是硬编码的，base-url 仅用于个别平台按区域选择入口。
type Adapter interface {
	Platform() string
	Fetch(ctx context.Context, apiKey, baseURL string) (RelayUsageResult, error)
}

// NewDefaultAdapterRegistry 构造内置四家中转商的 adapter 集合，key 为平台标识。
func NewDefaultAdapterRegistry(client HTTPDoer) map[string]Adapter {
	return map[string]Adapter{
		"glm":      newGLMAdapter(client),
		"minimax":  newMiniMaxAdapter(client),
		"kimi":     newKimiAdapter(client),
		"deepseek": newDeepSeekAdapter(client),
	}
}
