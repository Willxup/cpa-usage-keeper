package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// xaiSource 声明 CPA xai-api-key endpoint 与 Keeper xai API Key provider 的边界。
func xaiSource() source {
	// xAI 是新增 optional endpoint，旧 CPA typed 404 时静默跳过。
	return newProviderKeySource("xai", "xai", "xAI", "xai api keys", true, func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 xAI API Key 专属 client 方法，不经过 OAuth Auth Files。
		return fetcher.FetchXAIAPIKeys(ctx)
	})
}
