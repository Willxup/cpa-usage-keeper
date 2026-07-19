package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// geminiInteractionsSource 声明 CPA interactions-api-key endpoint 与独立 provider 类型的边界。
func geminiInteractionsSource() source {
	// Interactions 是新增 optional endpoint，不能改写成普通 gemini 类型。
	return newProviderKeySource("gemini-interactions", "gemini-interactions", "Gemini Interactions", "interactions api keys", true, func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 Interactions 专属 client 方法。
		return fetcher.FetchInteractionsAPIKeys(ctx)
	})
}
