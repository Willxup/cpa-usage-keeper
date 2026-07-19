package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// geminiSource 声明 CPA gemini-api-key endpoint 与 Keeper gemini provider 的一一边界。
func geminiSource() source {
	// 普通 Gemini 是既有必选 endpoint，默认展示名保持原小写值。
	return newProviderKeySource("gemini", "gemini", "gemini", "gemini api keys", false, func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用普通 Gemini 专属 client 方法。
		return fetcher.FetchGeminiAPIKeys(ctx)
	})
}
