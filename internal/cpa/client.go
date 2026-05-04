package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL       string
	managementKey string
	httpClient    *http.Client
}

func (c *Client) doJSONRequest(ctx context.Context, path string, target any, kind string, configure func(*http.Request)) (int, []byte, error) {
	if c == nil {
		return 0, nil, fmt.Errorf("cpa client is nil")
	}
	if c.baseURL == "" {
		return 0, nil, fmt.Errorf("cpa base url is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build %s request: %w", kind, err)
	}
	if configure != nil {
		configure(req)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request %s: %w", kind, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s response: %w", kind, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, body, fmt.Errorf("%s request returned status %d", kind, resp.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return resp.StatusCode, body, fmt.Errorf("decode %s json: %w", kind, err)
	}
	return resp.StatusCode, body, nil
}

func (c *Client) doManagementJSONRequest(ctx context.Context, path string, target any, kind string) (int, []byte, error) {
	if c == nil {
		return 0, nil, fmt.Errorf("cpa client is nil")
	}
	if c.managementKey == "" {
		return 0, nil, fmt.Errorf("cpa management key is required")
	}
	return c.doJSONRequest(ctx, path, target, "management "+kind, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+c.managementKey)
	})
}

func NewClient(baseURL, managementKey string, timeout time.Duration) *Client {
	return &Client{
		baseURL:       strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		managementKey: strings.TrimSpace(managementKey),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) FetchExternalAPIKeys(ctx context.Context) (*ExternalAPIKeysResult, error) {
	result := &ExternalAPIKeysResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementExternalAPIKeysEndpoint, &result.Payload, "external api keys")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchModels(ctx context.Context) (*ModelsResult, error) {
	externalAPIKeys, err := c.FetchExternalAPIKeys(ctx)
	if err != nil {
		return &ModelsResult{}, err
	}
	externalAPIKey := firstNonEmptyString(externalAPIKeys.Payload.ExternalAPIKeys)
	if externalAPIKey == "" {
		return &ModelsResult{}, fmt.Errorf("cpa external api keys are required")
	}

	result := &ModelsResult{}
	statusCode, body, err := c.doJSONRequest(ctx, cpaModelsEndpoint, &result.Payload, "models", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+externalAPIKey)
	})
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchAuthFiles(ctx context.Context) (*AuthFilesResult, error) {
	result := &AuthFilesResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementAuthFilesEndpoint, &result.Payload, "auth files")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchGeminiAPIKeys(ctx context.Context) (*ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementGeminiAPIKeyEndpoint, "gemini-api-key", "gemini api keys")
}

func (c *Client) FetchClaudeAPIKeys(ctx context.Context) (*ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementClaudeAPIKeyEndpoint, "claude-api-key", "claude api keys")
}

func (c *Client) FetchCodexAPIKeys(ctx context.Context) (*ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementCodexAPIKeyEndpoint, "codex-api-key", "codex api keys")
}

func (c *Client) FetchVertexAPIKeys(ctx context.Context) (*ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementVertexAPIKeyEndpoint, "vertex-api-key", "vertex api keys")
}

func (c *Client) fetchProviderKeyConfig(ctx context.Context, path string, payloadKey string, kind string) (*ProviderKeyConfigResult, error) {
	result := &ProviderKeyConfigResult{}
	var raw json.RawMessage
	statusCode, body, err := c.doManagementJSONRequest(ctx, path, &raw, kind)
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	payload, err := decodeProviderKeyConfigPayload(raw, payloadKey)
	if err != nil {
		return result, fmt.Errorf("decode management %s json: %w", kind, err)
	}
	result.Payload = payload
	return result, nil
}

func (c *Client) FetchOpenAICompatibility(ctx context.Context) (*OpenAICompatibilityResult, error) {
	result := &OpenAICompatibilityResult{}
	var raw json.RawMessage
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementOpenAICompatibilityEndpoint, &raw, "openai compatibility")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	payload, err := decodeOpenAICompatibilityPayload(raw, "openai-compatibility")
	if err != nil {
		return result, fmt.Errorf("decode management openai compatibility json: %w", err)
	}
	result.Payload = payload
	return result, nil
}

func decodeProviderKeyConfigPayload(raw json.RawMessage, payloadKey string) ([]ProviderKeyConfig, error) {
	var direct []ProviderKeyConfig
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	payloadRaw, ok := wrapped[payloadKey]
	if !ok {
		return nil, fmt.Errorf("missing %s payload", payloadKey)
	}
	if err := json.Unmarshal(payloadRaw, &direct); err != nil {
		return nil, err
	}
	return direct, nil
}

func decodeOpenAICompatibilityPayload(raw json.RawMessage, payloadKey string) ([]OpenAICompatibilityConfig, error) {
	var direct []OpenAICompatibilityConfig
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	payloadRaw, ok := wrapped[payloadKey]
	if !ok {
		return nil, fmt.Errorf("missing %s payload", payloadKey)
	}
	if err := json.Unmarshal(payloadRaw, &direct); err != nil {
		return nil, err
	}
	return direct, nil
}

func firstNonEmptyString(values []string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
