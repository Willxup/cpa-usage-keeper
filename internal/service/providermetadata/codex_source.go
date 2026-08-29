package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// codexSource 声明 CPA codex-api-key endpoint 与 Keeper codex provider 的一一边界。
func codexSource() source {
	// Codex 是既有必选 endpoint，404 继续作为 warning。
	return newProviderKeySource("codex", "codex", "codex", "codex api keys", false, func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 Codex 专属 client 方法。
		return fetcher.FetchCodexAPIKeys(ctx)
	})
}
