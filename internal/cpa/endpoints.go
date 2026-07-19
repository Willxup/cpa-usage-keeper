package cpa

const (
	cpaManagementAuthFilesEndpoint           = "/v0/management/auth-files"
	cpaManagementAuthFilesStatusEndpoint     = "/v0/management/auth-files/status"
	cpaManagementAPIKeysEndpoint             = "/v0/management/api-keys"
	cpaManagementVertexAPIKeyEndpoint        = "/v0/management/vertex-api-key"
	cpaManagementGeminiAPIKeyEndpoint        = "/v0/management/gemini-api-key"
	cpaManagementCodexAPIKeyEndpoint         = "/v0/management/codex-api-key"
	cpaManagementClaudeAPIKeyEndpoint        = "/v0/management/claude-api-key"
	cpaManagementAmpcodeEndpoint             = "/v0/management/ampcode"
	cpaManagementOpenAICompatibilityEndpoint = "/v0/management/openai-compatibility"
	cpaManagementUsageQueueEndpoint          = "/v0/management/usage-queue"
	cpaManagementAPICallEndpoint             = "/v0/management/api-call"
	cpaManagementRequestLogByIDEndpoint      = "/v0/management/request-log-by-id"
	cpaModelsEndpoint                        = "/v1/models"

	cpaManagementRedisNetwork        = "tcp"
	ManagementRedisDefaultPort       = "8317"
	ManagementRedisAuthCommand       = "AUTH"
	ManagementRedisPopCommand        = "LPOP"
	ManagementRedisSubscribeCommand  = "SUBSCRIBE"
	ManagementUsageQueueKey          = "usage"
	ManagementUsageLegacyQueueKey    = "queue"
	ManagementUsageSubscribeChannel  = "usage"
	ManagementUsageQueueMaxBatchSize = 10000
)

const (
	// cpaManagementInteractionsAPIKeyEndpoint 只读取 Gemini Interactions metadata，不参与 usage 拉取。
	cpaManagementInteractionsAPIKeyEndpoint = "/v0/management/interactions-api-key"
	// cpaManagementXAIAPIKeyEndpoint 只读取 xAI API Key metadata，不改变现有 xAI OAuth 或 quota 路径。
	cpaManagementXAIAPIKeyEndpoint = "/v0/management/xai-api-key"
)
