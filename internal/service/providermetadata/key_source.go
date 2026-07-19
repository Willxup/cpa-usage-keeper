package providermetadata

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
)

// providerKeyFetch 描述一个标准 API Key endpoint 的公开 Fetcher 方法。
type providerKeyFetch func(context.Context, Fetcher) (*response.ProviderKeyConfigResult, error)

// newProviderKeySource 组装六类标准 API Key source 的共同状态转换。
func newProviderKeySource(id string, providerType string, defaultDisplayName string, warningName string, optionalNotFound bool, endpointFetch providerKeyFetch) source {
	// item 先保存不随请求变化的来源合同。
	item := source{id: id, providerType: providerType, defaultDisplayName: defaultDisplayName, warningName: warningName, optionalNotFound: optionalNotFound}
	// fetch 只调用当前 endpoint，再进入共享标准 key 归一化。
	item.fetch = func(ctx context.Context, fetcher Fetcher) sourceResult {
		// endpointFetch 由各 source 文件显式绑定，避免运行时 switch 或动态注册。
		return fetchProviderKeySource(ctx, fetcher, item, endpointFetch)
	}
	// 返回完整 source 供固定 registry 使用。
	return item
}

// fetchProviderKeySource 把单个标准 API Key endpoint 的 result/error 转成统一来源状态。
func fetchProviderKeySource(ctx context.Context, fetcher Fetcher, item source, endpointFetch providerKeyFetch) sourceResult {
	// 调用当前 source 文件绑定的 CPA client 方法。
	result, err := endpointFetch(ctx, fetcher)
	// endpoint error 必须先判断两个新来源的 typed 404。
	if err != nil {
		// optional 只依赖非 nil result 的真实状态码，禁止解析错误字符串。
		if item.optionalNotFound && result != nil && result.StatusCode == http.StatusNotFound {
			return sourceResult{}
		}
		// 其它错误保留来源名称并交给 registry 稳定归并。
		return sourceResult{warning: fmt.Errorf("fetch %s: %w", item.warningName, err)}
	}
	// nil response 即使没有 error 也不能标记来源成功，避免误 stale 旧行。
	if result == nil {
		return sourceResult{warning: fmt.Errorf("%s response is nil", item.warningName)}
	}
	// credentials 预分配 endpoint entry 容量，但只追加通过必填校验的条目。
	credentials := make([]Credential, 0, len(result.Payload))
	// 保持 CPA payload entry 顺序，不按展示字段重新排序。
	for _, config := range result.Payload {
		// credential 只从当前 source 常量和当前 entry 的单向字段映射生成。
		credential, ok := providerKeyCredential(item, config)
		// 缺 API Key 或 auth-index 的 entry 被过滤，但来源仍保持 fetched。
		if !ok {
			continue
		}
		// 有效 entry 按原顺序加入来源结果。
		credentials = append(credentials, credential)
	}
	// 成功 endpoint 无论 payload 是否为空都必须标记 fetched。
	return sourceResult{credentials: credentials, fetched: true}
}

// providerKeyCredential 执行标准 API Key entry 的字段单向映射和必填校验。
func providerKeyCredential(item source, config providerconfig.ProviderKeyConfig) (Credential, bool) {
	// lookupKey 只来自 CPA api-key 兼容字段。
	lookupKey := strings.TrimSpace(config.APIKey)
	// prefix 只来自 CPA 独立 prefix 字段。
	prefix := strings.TrimSpace(config.Prefix)
	// providerType 只来自 source 常量，不能由 payload 文案推断。
	providerType := strings.TrimSpace(item.providerType)
	// displayName 优先使用 CPA name，缺失时使用 source 默认值。
	displayName := firstNonEmpty(config.Name, item.defaultDisplayName)
	// authIndex 只来自 CPA auth-index 兼容字段。
	authIndex := strings.TrimSpace(config.AuthIndex)
	// baseURL 只来自 CPA 独立 base URL 兼容字段。
	baseURL := strings.TrimSpace(config.BaseURL)
	// API Key、provider type、display name 和 auth-index 缺一不可。
	if lookupKey == "" || providerType == "" || displayName == "" || authIndex == "" {
		return Credential{}, false
	}
	// 每个目标字段都只接收上面声明的唯一来源。
	credential := Credential{
		// LookupKey 保存内部 API Key lookup 值。
		LookupKey: lookupKey,
		// Prefix 保存独立 metadata，不额外创建 identity。
		Prefix: prefix,
		// ProviderType 保留 source 定义的 CPA 原始类型。
		ProviderType: providerType,
		// DisplayName 保存 CPA name 或固定默认名。
		DisplayName: displayName,
		// AuthIndex 保存稳定 identity 来源。
		AuthIndex: authIndex,
		// BaseURL 保存独立连接 metadata。
		BaseURL: baseURL,
		// Priority 原样保留 nil 与数值。
		Priority: config.Priority,
		// Disabled 原样保留 nil、false 与 true。
		Disabled: config.Disabled,
		// Note 原样保留 nil 与字符串。
		Note: config.Note,
	}
	// 返回通过必填校验的 Credential。
	return credential, true
}

// firstNonEmpty 返回第一个去空格后非空的展示字段。
func firstNonEmpty(values ...string) string {
	// 按调用方给定的业务优先级检查候选值。
	for _, value := range values {
		// 展示字段沿用旧逻辑去除首尾空格。
		trimmed := strings.TrimSpace(value)
		// 第一个非空值立即成为最终结果。
		if trimmed != "" {
			return trimmed
		}
	}
	// 所有候选为空时返回空字符串，让必填校验拒绝条目。
	return ""
}
