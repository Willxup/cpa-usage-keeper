package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// claudeSource 声明 CPA claude-api-key endpoint 与 Keeper claude provider 的一一边界。
func claudeSource() source {
	// Claude 是既有必选 endpoint，默认展示名保持原小写值。
	return newProviderKeySource("claude", "claude", "claude", "claude api keys", false, func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 Claude 专属 client 方法。
		return fetcher.FetchClaudeAPIKeys(ctx)
	})
}
