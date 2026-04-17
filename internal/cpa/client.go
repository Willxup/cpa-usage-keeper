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
	if c == nil {
		return nil, fmt.Errorf("cpa client is nil")
	}
	if c.baseURL == "" {
		return nil, fmt.Errorf("cpa base url is required")
	}
	if c.managementKey == "" {
		return nil, fmt.Errorf("cpa management key is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v0/management/usage/export", nil)
	if err != nil {
		return nil, fmt.Errorf("build export request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.managementKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request usage export: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read usage export response: %w", err)
	}

	result := &ExportResult{
		StatusCode: resp.StatusCode,
		Body:       body,
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return result, fmt.Errorf("management export request failed with status %d", resp.StatusCode)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return result, fmt.Errorf("management export request returned status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &result.Payload); err != nil {
		return result, fmt.Errorf("decode usage export json: %w", err)
	}

	return result, nil
}
