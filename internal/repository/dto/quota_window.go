package dto

import "strings"

// QuotaWindowSpec 声明单个可持久化限额历史窗口的契约与展示元数据。
type QuotaWindowSpec struct {
	WindowRole string // API 查询参数与状态机槽位键（如 "primary"、"gemini_5h"、"weekly"）
	QuotaKey   string // quota_cycles.quota_key 稳定存储键
	GroupKey   string // 可选模型池键（单池渠道为空）
	GroupLabel string // 可选详情页短池名（如 "Gemini"、"Claude & GPT"，单池渠道为空）
	Order      int    // 同一凭证内窗口切换器的确定性排序序号
}

// QuotaHistoryProviderSpec 封装一种凭证类型在存储与汇总层的全部差异。
type QuotaHistoryProviderSpec struct {
	Provider    string
	Windows     []QuotaWindowSpec
	ModelScoped bool                                     // false=整号归属（无需按 model 过滤）；true=按模型前缀分池归属
	MatchModel  func(quotaKey string, model string) bool // 分池渠道判断事件 model 是否归属当前 quotaKey
}

type quotaWindowLookupKey struct {
	provider string
	value    string
}

// quotaHistoryProviderSpecs 固定有序声明所有已接入限额历史的渠道，避免无序 map 造成 SQL 参数抖动。
var quotaHistoryProviderSpecs = []QuotaHistoryProviderSpec{
	codexQuotaHistoryProviderSpec,
	claudeQuotaHistoryProviderSpec,
}

var (
	quotaHistoryProviderByName map[string]QuotaHistoryProviderSpec
	quotaWindowSpecByRoleMap   map[quotaWindowLookupKey]QuotaWindowSpec
	quotaWindowSpecByKeyMap    map[quotaWindowLookupKey]QuotaWindowSpec
	supportedHistoryProviders  []string
)

func init() {
	quotaHistoryProviderByName = make(map[string]QuotaHistoryProviderSpec, len(quotaHistoryProviderSpecs))
	quotaWindowSpecByRoleMap = make(map[quotaWindowLookupKey]QuotaWindowSpec)
	quotaWindowSpecByKeyMap = make(map[quotaWindowLookupKey]QuotaWindowSpec)
	supportedHistoryProviders = make([]string, 0, len(quotaHistoryProviderSpecs))
	for _, spec := range quotaHistoryProviderSpecs {
		provider := strings.ToLower(strings.TrimSpace(spec.Provider))
		spec.Provider = provider
		quotaHistoryProviderByName[provider] = spec
		supportedHistoryProviders = append(supportedHistoryProviders, provider)
		for _, window := range spec.Windows {
			role := strings.ToLower(strings.TrimSpace(window.WindowRole))
			window.WindowRole = role
			quotaWindowSpecByRoleMap[quotaWindowLookupKey{provider: provider, value: role}] = window
			quotaWindowSpecByKeyMap[quotaWindowLookupKey{provider: provider, value: window.QuotaKey}] = window
		}
	}
}

// LookupQuotaHistoryProvider 按归一化凭证类型查询限额历史规格。
func LookupQuotaHistoryProvider(provider string) (QuotaHistoryProviderSpec, bool) {
	spec, ok := quotaHistoryProviderByName[strings.ToLower(strings.TrimSpace(provider))]
	return spec, ok
}

// SupportedQuotaHistoryProviders 返回固定有序的已支持限额历史渠道列表副本。
func SupportedQuotaHistoryProviders() []string {
	result := make([]string, len(supportedHistoryProviders))
	copy(result, supportedHistoryProviders)
	return result
}

// QuotaWindowSpecByRole 按 provider 与 window_role 查询窗口规格。
func QuotaWindowSpecByRole(provider, role string) (QuotaWindowSpec, bool) {
	spec, ok := quotaWindowSpecByRoleMap[quotaWindowLookupKey{
		provider: strings.ToLower(strings.TrimSpace(provider)),
		value:    strings.ToLower(strings.TrimSpace(role)),
	}]
	return spec, ok
}

// QuotaWindowSpecByKey 按 provider 与持久化 quota_key 查询窗口规格。
func QuotaWindowSpecByKey(provider, quotaKey string) (QuotaWindowSpec, bool) {
	spec, ok := quotaWindowSpecByKeyMap[quotaWindowLookupKey{
		provider: strings.ToLower(strings.TrimSpace(provider)),
		value:    strings.TrimSpace(quotaKey),
	}]
	return spec, ok
}

// AllowedQuotaKeys 返回指定 provider 允许持久化与删除的全部 quota_key 列表。
func AllowedQuotaKeys(provider string) []string {
	spec, ok := LookupQuotaHistoryProvider(provider)
	if !ok || len(spec.Windows) == 0 {
		return nil
	}
	keys := make([]string, 0, len(spec.Windows))
	for _, window := range spec.Windows {
		keys = append(keys, window.QuotaKey)
	}
	return keys
}

// QuotaWindowKey 兼容薄封装：将 provider 与 API 角色映射为持久化 quota_key。
func QuotaWindowKey(provider, role string) (string, bool) {
	spec, ok := QuotaWindowSpecByRole(provider, role)
	if !ok {
		return "", false
	}
	return spec.QuotaKey, true
}

// QuotaWindowRole 兼容薄封装：将 provider 与持久化 quota_key 反向映射为 API 角色。
func QuotaWindowRole(provider, key string) (string, bool) {
	spec, ok := QuotaWindowSpecByKey(provider, key)
	if !ok {
		return "", false
	}
	return spec.WindowRole, true
}
