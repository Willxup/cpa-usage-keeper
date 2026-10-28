package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// xaiSource 声明 CPA xai-api-key endpoint 与 Keeper xai API Key provider 的边界。
func xaiSource() source {
	return newProviderKeySource("xai", "xai", "xAI", "xai api keys", func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 xAI API Key 专属 client 方法，不经过 OAuth Credentials。
		return fetcher.FetchXAIAPIKeys(ctx)
	})
}
