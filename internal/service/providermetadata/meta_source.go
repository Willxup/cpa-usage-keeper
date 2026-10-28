package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// metaSource 声明 CPA meta-api-key endpoint 与 Keeper meta provider 的独立边界。
func metaSource() source {
	return newProviderKeySource("meta", "meta", "Meta", "meta api keys", func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 Meta API Key 专属 client 方法，不经过 Credential 或 quota 路径。
		return fetcher.FetchMetaAPIKeys(ctx)
	})
}
