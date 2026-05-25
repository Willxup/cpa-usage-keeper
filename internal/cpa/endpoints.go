package cpa

const (
	cpaManagementAuthFilesEndpoint           = "/v0/management/auth-files"
	cpaManagementAPIKeysEndpoint             = "/v0/management/api-keys"
	cpaManagementVertexAPIKeyEndpoint        = "/v0/management/vertex-api-key"
	cpaManagementGeminiAPIKeyEndpoint        = "/v0/management/gemini-api-key"
	cpaManagementCodexAPIKeyEndpoint         = "/v0/management/codex-api-key"
	cpaManagementClaudeAPIKeyEndpoint        = "/v0/management/claude-api-key"
	cpaManagementAmpcodeEndpoint             = "/v0/management/ampcode"
	cpaManagementOpenAICompatibilityEndpoint = "/v0/management/openai-compatibility"
	cpaManagementUsageQueueEndpoint          = "/v0/management/usage-queue"
	cpaManagementAPICallEndpoint             = "/v0/management/api-call"
	cpaModelsEndpoint                        = "/v1/models"

	cpaManagementRedisNetwork        = "tcp"
	ManagementRedisDefaultPort       = "8317"
	ManagementRedisAuthCommand       = "AUTH"
	ManagementRedisPopCommand        = "LPOP"
	ManagementRedisSubscribeCommand  = "SUBSCRIBE"
	ManagementUsageQueueKey          = "queue"
	ManagementUsageSubscribeChannel  = "usage"
	ManagementUsageQueueMaxBatchSize = 10000
)
