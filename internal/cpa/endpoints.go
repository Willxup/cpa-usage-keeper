package cpa

const (
	cpaManagementCredentialsEndpoint         = "/v8/management/credentials"
	cpaManagementCredentialsDownloadEndpoint = "/v8/management/credentials/download"
	cpaManagementCredentialsStatusEndpoint   = "/v8/management/credentials/status"
	cpaManagementCredentialsFieldsEndpoint   = "/v8/management/credentials/fields"
	cpaManagementAPIKeysEndpoint             = "/v8/management/config/access/api-keys"
	cpaManagementVertexAPIKeyEndpoint        = "/v8/management/config/api-keys/vertex"
	cpaManagementGeminiAPIKeyEndpoint        = "/v8/management/config/api-keys/gemini"
	cpaManagementCodexAPIKeyEndpoint         = "/v8/management/config/api-keys/codex"
	cpaManagementClaudeAPIKeyEndpoint        = "/v8/management/config/api-keys/claude"
	cpaManagementMetaAPIKeyEndpoint          = "/v8/management/config/api-keys/meta"
	cpaManagementOpenAICompatibilityEndpoint = "/v8/management/config/api-keys/openai-compatibility"
	cpaManagementUsageQueueEndpoint          = "/v8/management/observability/usage/queue"
	cpaManagementAPICallEndpoint             = "/v8/management/requests/api-call"
	cpaManagementResetQuotaEndpoint          = "/v8/management/routing/cooldown/reset"
	cpaManagementRequestLogByIDEndpoint      = "/v8/management/observability/logs/requests"
	cpaModelsEndpoint                        = "/v1/models"

	cpaManagementRedisNetwork       = "tcp"
	ManagementRedisDefaultPort      = "8317"
	ManagementRedisAuthCommand      = "AUTH"
	ManagementRedisPopCommand       = "LPOP"
	ManagementRedisSubscribeCommand = "SUBSCRIBE"
	ManagementUsageQueueKey         = "usage"
	ManagementUsageLegacyQueueKey   = "queue"
	ManagementUsageSubscribeChannel = "usage"
	// ManagementErrorsSubscribeChannel 是 CPA 只广播、不提供 LPOP 补偿的凭证错误 channel。
	ManagementErrorsSubscribeChannel = "errors"
	ManagementUsageQueueMaxBatchSize = 10000
)

const (
	// cpaManagementInteractionsAPIKeyEndpoint 只读取 Gemini Interactions metadata，不参与 usage 拉取。
	cpaManagementInteractionsAPIKeyEndpoint = "/v8/management/config/api-keys/interactions"
	// cpaManagementXAIAPIKeyEndpoint 只读取 xAI API Key metadata，不改变现有 xAI OAuth 或 quota 路径。
	cpaManagementXAIAPIKeyEndpoint = "/v8/management/config/api-keys/xai"
)
