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

type ExportResult struct {
	StatusCode int
	Body       []byte
	Payload    UsageExport
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

func (c *Client) FetchUsageExport(ctx context.Context) (*ExportResult, error) {
	result := &ExportResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, "/v0/management/usage/export", &result.Payload, "export")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchAPIKeys(ctx context.Context) (*APIKeysResult, error) {
	result := &APIKeysResult{}
	statusCode, _, err := c.doManagementJSONRequest(ctx, "/v0/management/api-keys", &result.Payload, "api keys")
	result.StatusCode = statusCode
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchModels(ctx context.Context) (*ModelsResult, error) {
	apiKeys, err := c.FetchAPIKeys(ctx)
	if err != nil {
		return &ModelsResult{}, err
	}
	apiKey := firstNonEmptyString(apiKeys.Payload.APIKeys)
	if apiKey == "" {
		return &ModelsResult{}, fmt.Errorf("cpa api keys are required")
	}

	result := &ModelsResult{}
	statusCode, body, err := c.doJSONRequest(ctx, "/v1/models", &result.Payload, "models", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
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
	statusCode, body, err := c.doManagementJSONRequest(ctx, "/v0/management/auth-files", &result.Payload, "auth files")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) FetchManagementConfig(ctx context.Context) (*ManagementConfigResult, error) {
	result := &ManagementConfigResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, "/v0/management/config", &result.Payload, "config")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
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
