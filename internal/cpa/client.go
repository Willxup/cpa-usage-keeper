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

func (c *Client) doManagementJSONRequest(ctx context.Context, path string, target any, kind string) (int, []byte, error) {
	if c == nil {
		return 0, nil, fmt.Errorf("cpa client is nil")
	}
	if c.baseURL == "" {
		return 0, nil, fmt.Errorf("cpa base url is required")
	}
	if c.managementKey == "" {
		return 0, nil, fmt.Errorf("cpa management key is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build %s request: %w", kind, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.managementKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request %s: %w", kind, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s response: %w", kind, err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, body, fmt.Errorf("management %s request failed with status %d", kind, resp.StatusCode)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return resp.StatusCode, body, fmt.Errorf("management %s request returned status %d", kind, resp.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return resp.StatusCode, body, fmt.Errorf("decode %s json: %w", kind, err)
	}
	return resp.StatusCode, body, nil
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
