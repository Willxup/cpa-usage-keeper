package cpa

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// ProviderKeyDisabledExcludedModel 是 CPA 六类 API Key 的整条停用标记：excluded-models 中精确的 "*"。
const ProviderKeyDisabledExcludedModel = "*"

// providerKeyPatchRequest 是六类 API Key 管理接口共用的 PATCH 结构，按配置数组下标精确定位目标字段。
type providerKeyPatchRequest struct {
	// Index 是 CPA provider 配置数组下标；CPA 优先使用它，避免重复 API Key 命中第一条。
	Index int                   `json:"index"`
	Value providerKeyPatchValue `json:"value"`
}

type providerKeyPatchValue struct {
	ExcludedModels []string `json:"excluded-models"`
}

// providerKeyEndpoint 把 Keeper 的 provider type 映射到 CPA 管理接口，OpenAI Compatibility 不在其中。
func providerKeyEndpoint(providerType string) (path string, payloadKey string, kind string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "codex":
		return cpaManagementCodexAPIKeyEndpoint, "codex-api-key", "codex api keys", true
	case "xai":
		return cpaManagementXAIAPIKeyEndpoint, "xai-api-key", "xai api keys", true
	case "gemini":
		return cpaManagementGeminiAPIKeyEndpoint, "gemini-api-key", "gemini api keys", true
	case "gemini-interactions":
		return cpaManagementInteractionsAPIKeyEndpoint, "interactions-api-key", "interactions api keys", true
	case "claude":
		return cpaManagementClaudeAPIKeyEndpoint, "claude-api-key", "claude api keys", true
	case "vertex":
		return cpaManagementVertexAPIKeyEndpoint, "vertex-api-key", "vertex api keys", true
	default:
		return "", "", "", false
	}
}

// FetchProviderKeyConfig 按 Keeper provider type 读取对应 API Key 列表，用于读取现有 excluded-models。
func (c *Client) FetchProviderKeyConfig(ctx context.Context, providerType string) (*response.ProviderKeyConfigResult, error) {
	path, payloadKey, kind, ok := providerKeyEndpoint(providerType)
	if !ok {
		return nil, fmt.Errorf("unsupported provider type %q", strings.TrimSpace(providerType))
	}
	return c.fetchProviderKeyConfig(ctx, path, payloadKey, kind)
}

// UpdateProviderKeyExcludedModels 按配置数组下标写回 excluded-models，空列表表示清除全部模型排除。
// 下标来自同一次 GET 原始 payload 中的位置，因此重复 API Key 也能精确命中用户选中的那一条。
func (c *Client) UpdateProviderKeyExcludedModels(ctx context.Context, providerType string, index int, excludedModels []string) (int, error) {
	path, _, kind, ok := providerKeyEndpoint(providerType)
	if !ok {
		return 0, fmt.Errorf("unsupported provider type %q", strings.TrimSpace(providerType))
	}
	if index < 0 {
		return 0, fmt.Errorf("provider key index must not be negative")
	}
	// nil 会被编码成 JSON null，CPA 会当成“未提供该字段”，因此空列表必须显式传空数组。
	if excludedModels == nil {
		excludedModels = []string{}
	}
	statusCode, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodPatch, path, providerKeyPatchRequest{
		Index: index,
		Value: providerKeyPatchValue{ExcludedModels: excludedModels},
	}, nil, kind)
	return statusCode, err
}

// ProviderKeyStatusSupported 报告 provider type 是否支持用 excluded-models 精确 "*" 整条停用。
func ProviderKeyStatusSupported(providerType string) bool {
	_, _, _, ok := providerKeyEndpoint(providerType)
	return ok
}
