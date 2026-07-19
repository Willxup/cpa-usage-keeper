package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// vertexSource 声明 CPA vertex-api-key endpoint 与 Keeper vertex provider 的一一边界。
func vertexSource() source {
	// Vertex 是既有必选 endpoint，provider type 使用 CPA 正确拼写。
	return newProviderKeySource("vertex", "vertex", "vertex", "vertex api keys", false, func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 Vertex 专属 client 方法。
		return fetcher.FetchVertexAPIKeys(ctx)
	})
}
