package response

import (
	"encoding/json"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/cpaapikeys"
	"cpa-usage-keeper/internal/cpa/dto/keypolicy"
	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

// ManagementAPIKeysResult 是 FetchManagementAPIKeys 返回的 HTTP 包装，保留状态码、原始响应体和解析后的 DTO。
type ManagementAPIKeysResult struct {
	StatusCode int
	Body       []byte
	Payload    cpaapikeys.ManagementAPIKeysResponse
}

// KeyPolicyPluginKeysResult 是 FetchKeyPolicyPluginKeys 返回的 HTTP 包装，payload 只包含启用的插件 key。
type KeyPolicyPluginKeysResult struct {
	StatusCode int
	Body       []byte
	Payload    keypolicy.PluginKeysResponse
}

// ModelsResult 是 FetchModels 返回的 HTTP 包装，保留状态码、原始响应体和解析后的 DTO。
type ModelsResult struct {
	StatusCode int
	Body       []byte
	Payload    models.ModelsResponse
}

// AuthFilesResult 是 FetchAuthFiles 返回的 HTTP 包装，保留状态码、原始响应体和解析后的 DTO。
type AuthFilesResult struct {
	StatusCode int
	Body       []byte
	Payload    authfiles.AuthFilesResponse
}

// UsageQueueResult 是 FetchUsageQueue 返回的 HTTP 包装，payload 保留为 raw JSON 供 Redis usage 解码流程处理。
type UsageQueueResult struct {
	StatusCode int
	Body       []byte
	Payload    []json.RawMessage
}

// ProviderKeyConfigResult 是 provider API key 管理接口返回的 HTTP 包装，payload 是兼容归一化后的 provider 配置。
type ProviderKeyConfigResult struct {
	StatusCode int
	Body       []byte
	Payload    []providerconfig.ProviderKeyConfig
}

// OpenAICompatibilityResult 是 openai-compatibility 管理接口返回的 HTTP 包装，payload 是兼容归一化后的 provider 配置。
type OpenAICompatibilityResult struct {
	StatusCode int
	Body       []byte
	Payload    []providerconfig.OpenAICompatibilityConfig
}
