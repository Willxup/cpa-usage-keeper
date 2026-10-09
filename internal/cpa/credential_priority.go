package cpa

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// 优先级 PATCH 使用独立 DTO，确保不会把 excluded-models 或任何密钥带入请求。
type providerPriorityPatchRequest struct {
	Index int                        `json:"index"`
	Value providerPriorityPatchValue `json:"value"`
}

type providerPriorityPatchValue struct {
	Priority int `json:"priority"`
}

type authFilePriorityPatchRequest struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

func (c *Client) UpdateAuthFilePriority(ctx context.Context, name string, priority int) (int, error) {
	statusCode, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodPatch, cpaManagementAuthFilesFieldsEndpoint,
		authFilePriorityPatchRequest{Name: name, Priority: priority}, nil, "auth file priority")
	return statusCode, err
}

// priorityProviderKeyEndpoint 与六类停用映射分开，支持新增的 Meta，但不扩大状态开关能力。
func priorityProviderKeyEndpoint(providerType string) (path, payloadKey, kind string, ok bool) {
	if strings.EqualFold(strings.TrimSpace(providerType), "meta") {
		return cpaManagementMetaAPIKeyEndpoint, "meta-api-key", "meta api keys", true
	}
	return providerKeyEndpoint(providerType)
}

func (c *Client) FetchPriorityProviderConfig(ctx context.Context, providerType string) (*response.ProviderKeyConfigResult, error) {
	path, payloadKey, kind, ok := priorityProviderKeyEndpoint(providerType)
	if !ok {
		return nil, fmt.Errorf("unsupported priority provider type %q", providerType)
	}
	return c.fetchProviderKeyConfig(ctx, path, payloadKey, kind)
}

func (c *Client) UpdateProviderPriority(ctx context.Context, providerType string, index int, priority int) (int, error) {
	path, _, kind, ok := priorityProviderKeyEndpoint(providerType)
	if !ok {
		return 0, fmt.Errorf("unsupported priority provider type %q", providerType)
	}
	return c.patchProviderPriority(ctx, path, kind, index, priority)
}

func (c *Client) UpdateOpenAICompatibilityPriority(ctx context.Context, index int, priority int) (int, error) {
	return c.patchProviderPriority(ctx, cpaManagementOpenAICompatibilityEndpoint, "openai compatibility", index, priority)
}

func (c *Client) patchProviderPriority(ctx context.Context, path, kind string, index int, priority int) (int, error) {
	if index < 0 {
		return 0, fmt.Errorf("provider index must not be negative")
	}
	statusCode, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodPatch, path,
		providerPriorityPatchRequest{Index: index, Value: providerPriorityPatchValue{Priority: priority}}, nil, kind)
	return statusCode, err
}

func ProviderPrioritySupported(providerType string) bool {
	if strings.EqualFold(strings.TrimSpace(providerType), "openai") {
		return true
	}
	_, _, _, ok := priorityProviderKeyEndpoint(providerType)
	return ok
}
