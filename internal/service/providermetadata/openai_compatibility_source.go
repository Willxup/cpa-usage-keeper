package providermetadata

import (
	"context"
	"fmt"
	"strings"
)

// openAICompatibilitySource 声明 CPA openai-compatibility endpoint 的专属多 key 归一化边界。
func openAICompatibilitySource() source {
	// item 保存 OpenAI 专属 endpoint 的固定来源合同。
	item := source{id: "openai", providerType: "openai", defaultDisplayName: "openai", warningName: "openai compatibility"}
	// fetch 保留 OpenAI provider 层与多 key entry 的专属归一化路径。
	item.fetch = func(ctx context.Context, fetcher Fetcher) sourceResult {
		// 调用 OpenAI Compatibility 专属 client 方法。
		result, err := fetcher.FetchOpenAICompatibility(ctx)
		// 既有 endpoint 的任何错误都必须作为 warning 返回。
		if err != nil {
			return sourceResult{warning: fmt.Errorf("fetch %s: %w", item.warningName, err)}
		}
		// nil response 不能误标成功，避免 stale 既有 OpenAI 行。
		if result == nil {
			return sourceResult{warning: fmt.Errorf("%s response is nil", item.warningName)}
		}
		// credentials 初始容量按 provider 数估算，多 key provider 会按需扩容。
		credentials := make([]Credential, 0, len(result.Payload))
		// 保持 CPA provider 和 entry 顺序，不做显示字段排序。
		for _, provider := range result.Payload {
			// displayName 优先使用 provider name，缺失时使用 openai。
			displayName := firstNonEmpty(provider.Name, item.defaultDisplayName)
			// prefix 只来自 provider 层独立字段。
			prefix := strings.TrimSpace(provider.Prefix)
			// baseURL 只来自 provider 层独立字段。
			baseURL := strings.TrimSpace(provider.BaseURL)
			// 每个 API key entry 独立生成一条 Credential。
			for _, entry := range provider.APIKeyEntries {
				// lookupKey 只来自当前 entry 的 API Key。
				lookupKey := strings.TrimSpace(entry.APIKey)
				// authIndex 只来自当前 entry 的 auth-index。
				authIndex := strings.TrimSpace(entry.AuthIndex)
				// OpenAI entry 缺 API Key 或 auth-index 时只过滤该 entry。
				if lookupKey == "" || authIndex == "" || displayName == "" {
					continue
				}
				// provider 层字段传播给当前有效 entry。
				credential := Credential{
					// LookupKey 保存当前 entry 的内部 API Key lookup 值。
					LookupKey: lookupKey,
					// Prefix 保存 provider 层独立 metadata。
					Prefix: prefix,
					// ProviderType 固定为 openai。
					ProviderType: item.providerType,
					// DisplayName 保存 provider name 或 openai 默认名。
					DisplayName: displayName,
					// AuthIndex 保存当前 entry 的稳定 identity。
					AuthIndex: authIndex,
					// BaseURL 保存 provider 层连接 metadata。
					BaseURL: baseURL,
					// Priority 传播 provider 层 nil 与数值。
					Priority: provider.Priority,
					// Disabled 传播 provider 层 nil 与布尔值。
					Disabled: provider.Disabled,
					// Note 传播 provider 层 nil 与字符串。
					Note: provider.Note,
				}
				// 有效 entry 按原顺序加入来源结果。
				credentials = append(credentials, credential)
			}
		}
		// 成功 endpoint 无论 credentials 是否为空都标记 fetched。
		return sourceResult{credentials: credentials, fetched: true}
	}
	// 返回完整 OpenAI 专属 source。
	return item
}
