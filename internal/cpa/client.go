package cpa

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
)

type Client struct {
	baseURL       string
	managementKey string
	httpClient    *http.Client
}

func (c *Client) doJSONRequest(ctx context.Context, path string, target any, kind string, configure func(*http.Request)) (int, []byte, error) {
	return c.doJSONRequestWithBody(ctx, http.MethodGet, path, nil, target, kind, configure)
}

func (c *Client) doJSONRequestWithBody(ctx context.Context, method string, path string, body []byte, target any, kind string, configure func(*http.Request)) (int, []byte, error) {
	if c == nil {
		return 0, nil, fmt.Errorf("cpa client is nil")
	}
	if c.baseURL == "" {
		return 0, nil, fmt.Errorf("cpa base url is required")
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
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

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s response: %w", kind, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, responseBody, fmt.Errorf("%s request returned status %d", kind, resp.StatusCode)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return resp.StatusCode, responseBody, fmt.Errorf("decode %s json: %w", kind, err)
	}
	return resp.StatusCode, responseBody, nil
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

func (c *Client) doManagementJSONPostRequest(ctx context.Context, path string, requestBody any, target any, kind string) (int, []byte, error) {
	if c == nil {
		return 0, nil, fmt.Errorf("cpa client is nil")
	}
	if c.managementKey == "" {
		return 0, nil, fmt.Errorf("cpa management key is required")
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return 0, nil, fmt.Errorf("encode management %s json: %w", kind, err)
	}
	return c.doJSONRequestWithBody(ctx, http.MethodPost, path, body, target, "management "+kind, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+c.managementKey)
		req.Header.Set("Content-Type", "application/json")
	})
}

func NewClient(baseURL, managementKey string, timeout time.Duration, tlsSkipVerify bool) *Client {
	httpClient := &http.Client{
		Timeout: timeout,
	}
	if tlsSkipVerify {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		httpClient.Transport = transport
	}
	return &Client{
		baseURL:       strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		managementKey: strings.TrimSpace(managementKey),
		httpClient:    httpClient,
	}
}

func (c *Client) FetchExternalAPIKeys(ctx context.Context) (*response.ExternalAPIKeysResult, error) {
	result := &response.ExternalAPIKeysResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementExternalAPIKeysEndpoint, &result.Payload, "external api keys")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchUsageQueue(ctx context.Context, count int) (*response.UsageQueueResult, error) {
	result := &response.UsageQueueResult{}
	if count <= 0 {
		return result, fmt.Errorf("usage queue count must be positive")
	}
	queryPath := cpaManagementUsageQueueEndpoint + "?count=" + url.QueryEscape(strconv.Itoa(count))
	statusCode, body, err := c.doManagementJSONRequest(ctx, queryPath, &result.Payload, "usage queue")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchModels(ctx context.Context) (*response.ModelsResult, error) {
	externalAPIKeys, err := c.FetchExternalAPIKeys(ctx)
	if err != nil {
		return &response.ModelsResult{}, err
	}
	externalAPIKey := firstNonEmptyString(externalAPIKeys.Payload.ExternalAPIKeys)
	if externalAPIKey == "" {
		return &response.ModelsResult{}, fmt.Errorf("cpa external api keys are required")
	}

	result := &response.ModelsResult{}
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

func (c *Client) FetchAuthFiles(ctx context.Context) (*response.AuthFilesResult, error) {
	result := &response.AuthFilesResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementAuthFilesEndpoint, &result.Payload, "auth files")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	result := &apicall.Response{}
	_, _, err := c.doManagementJSONPostRequest(ctx, cpaManagementAPICallEndpoint, request, result, "api call")
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementGeminiAPIKeyEndpoint, "gemini-api-key", "gemini api keys")
}

func (c *Client) FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementClaudeAPIKeyEndpoint, "claude-api-key", "claude api keys")
}

func (c *Client) FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementCodexAPIKeyEndpoint, "codex-api-key", "codex api keys")
}

func (c *Client) FetchVertexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementVertexAPIKeyEndpoint, "vertex-api-key", "vertex api keys")
}

func (c *Client) fetchProviderKeyConfig(ctx context.Context, path string, payloadKey string, kind string) (*response.ProviderKeyConfigResult, error) {
	result := &response.ProviderKeyConfigResult{}
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

func (c *Client) FetchOpenAICompatibility(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
	result := &response.OpenAICompatibilityResult{}
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

func decodeProviderKeyConfigPayload(raw json.RawMessage, payloadKey string) ([]providerconfig.ProviderKeyConfig, error) {
	var direct []providerconfig.ProviderKeyConfig
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

func decodeOpenAICompatibilityPayload(raw json.RawMessage, payloadKey string) ([]providerconfig.OpenAICompatibilityConfig, error) {
	var direct []providerconfig.OpenAICompatibilityConfig
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
