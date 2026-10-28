package response

import (
	"encoding/json"

	"cpa-usage-keeper/internal/cpa/dto/cpaapikeys"
	"cpa-usage-keeper/internal/cpa/dto/credentials"
	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

// ManagementAPIKeysResult 是 FetchManagementAPIKeys 返回的 HTTP 包装，保留状态码、原始响应体和解析后的 DTO。
type ManagementAPIKeysResult struct {
	StatusCode int
	Body       []byte
	Payload    cpaapikeys.ManagementAPIKeysResponse
}

// ModelsResult 是 FetchModels 返回的 HTTP 包装，保留状态码、原始响应体和解析后的 DTO。
type ModelsResult struct {
	StatusCode int
	Body       []byte
	Payload    models.ModelsResponse
}

// CredentialsResult 是 FetchCredentials 返回的 HTTP 包装，保留状态码、原始响应体和解析后的 DTO。
type CredentialsResult struct {
	StatusCode int
	Body       []byte
	Payload    credentials.CredentialsResponse
}

// UsageQueueResult 是 FetchUsageQueue 返回的 HTTP 包装，payload 保留为 raw JSON 供 Redis usage 解码流程处理。
type UsageQueueResult struct {
	StatusCode int
	Body       []byte
	Payload    []json.RawMessage
}

// ProviderKeyConfigResult 保留同一次供应商读取的两种视图。
// Payload 用于同步与有效状态判断；Document 保留完整配置用于写回，不能用 Payload 代替。
type ProviderKeyConfigResult struct {
	StatusCode int
	ETag       string
	Body       []byte
	Payload    []providerconfig.ProviderKeyConfig
	Document   *providerconfig.Document
}

// OpenAICompatibilityResult 的 Payload 与 Document 保持相同组顺序。
// 编辑通过 Document 定位组，并用 Payload 中该组的成员标识更新本地优先级。
type OpenAICompatibilityResult struct {
	StatusCode int
	ETag       string
	Body       []byte
	Payload    []providerconfig.OpenAICompatibilityConfig
	Document   *providerconfig.Document
}
