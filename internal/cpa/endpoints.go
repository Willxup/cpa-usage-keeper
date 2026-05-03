package cpa

const (
	cpaManagementAuthFilesEndpoint           = "/v0/management/auth-files"
	cpaManagementExternalAPIKeysEndpoint     = "/v0/management/api-keys"
	cpaManagementVertexAPIKeyEndpoint        = "/v0/management/vertex-api-key"
	cpaManagementGeminiAPIKeyEndpoint        = "/v0/management/gemini-api-key"
	cpaManagementCodexAPIKeyEndpoint         = "/v0/management/codex-api-key"
	cpaManagementClaudeAPIKeyEndpoint        = "/v0/management/claude-api-key"
	cpaManagementAmpcodeEndpoint             = "/v0/management/ampcode"
	cpaManagementOpenAICompatibilityEndpoint = "/v0/management/openai-compatibility"
	cpaModelsEndpoint                        = "/v1/models"

	cpaManagementRedisNetwork     = "tcp"
	ManagementRedisDefaultPort    = "8317"
	cpaManagementRedisAuthCommand = "AUTH"
	cpaManagementRedisPopCommand  = "LPOP"
	ManagementUsageQueueKey       = "queue"
)
