package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// Fetcher 汇总七个 provider metadata endpoint，service 通过这一接口注入真实 CPA client 或测试替身。
type Fetcher interface {
	// FetchCodexAPIKeys 读取 Codex API Key metadata。
	FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error)
	// FetchXAIAPIKeys 读取 xAI API Key metadata。
	FetchXAIAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error)
	// FetchGeminiAPIKeys 读取普通 Gemini API Key metadata。
	FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error)
	// FetchInteractionsAPIKeys 读取 Gemini Interactions API Key metadata。
	FetchInteractionsAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error)
	// FetchClaudeAPIKeys 读取 Claude API Key metadata。
	FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error)
	// FetchVertexAPIKeys 读取 Vertex API Key metadata。
	FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error)
	// FetchOpenAICompatibility 读取 OpenAI Compatibility provider 与多 key metadata。
	FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error)
}

// Credential 是 provider endpoint 归一化后的纯 metadata，不依赖数据库实体。
type Credential struct {
	// LookupKey 只接收 CPA api-key，供 Keeper 内部 usage lookup 使用。
	LookupKey string
	// Prefix 只接收 CPA 独立 prefix 字段。
	Prefix string
	// ProviderType 保留 CPA 原始 provider 类型。
	ProviderType string
	// DisplayName 保存 CPA name 或来源默认展示名。
	DisplayName string
	// AuthIndex 保存 CPA 返回的稳定 auth-index，也是 identity 的唯一来源。
	AuthIndex string
	// BaseURL 只接收 CPA 独立 base URL 字段。
	BaseURL string
	// Priority 保留 CPA 可选优先级的 nil 与数值语义。
	Priority *int
	// Disabled 保留 CPA 可选禁用状态的 nil 与布尔语义。
	Disabled *bool
	// Note 保留 CPA 可选备注的 nil 与字符串语义。
	Note *string
}

// Snapshot 汇总一轮所有成功来源的稳定结果。
type Snapshot struct {
	// Credentials 按 registry 与来源 entry 顺序保存有效凭证。
	Credentials []Credential
	// FetchedProviderTypes 只包含本轮成功返回的 provider 类型。
	FetchedProviderTypes []string
}
