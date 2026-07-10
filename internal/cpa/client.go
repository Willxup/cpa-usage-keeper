package cpa

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
)

type Client struct {
	baseURL           string
	managementKey     string
	httpClient        *http.Client
	streamHTTPClient  *http.Client
	streamIdleTimeout time.Duration
}

type RequestLogResult struct {
	StatusCode    int
	Body          []byte
	Filename      string
	ContentType   string
	ContentLength int64
	BodyTruncated bool
}

type RequestLogStream struct {
	StatusCode    int
	Body          io.ReadCloser
	Filename      string
	ContentType   string
	ContentLength int64
}

type authFileStatusRequest struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
}

type authFilesDeleteRequest struct {
	Names []string `json:"names"`
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
	if target == nil || isBlankJSONResponseBody(responseBody) {
		return resp.StatusCode, responseBody, nil
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return resp.StatusCode, responseBody, fmt.Errorf("decode %s json: %w", kind, err)
	}
	return resp.StatusCode, responseBody, nil
}

func isBlankJSONResponseBody(body []byte) bool {
	return len(bytes.TrimSpace(body)) == 0
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
	return c.doManagementJSONRequestWithBody(ctx, http.MethodPost, path, requestBody, target, kind)
}

func (c *Client) doManagementJSONRequestWithBody(ctx context.Context, method string, path string, requestBody any, target any, kind string) (int, []byte, error) {
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
	return c.doJSONRequestWithBody(ctx, method, path, body, target, "management "+kind, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+c.managementKey)
		req.Header.Set("Content-Type", "application/json")
	})
}

const defaultRequestLogStreamIdleTimeout = 30 * time.Second

func NewClient(baseURL, managementKey string, timeout time.Duration, tlsSkipVerify bool) *Client {
	transport := cloneDefaultHTTPTransport()
	if tlsSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	streamTransport := transport.Clone()
	if timeout > 0 {
		streamTransport.ResponseHeaderTimeout = timeout
	}
	return &Client{
		baseURL:           strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		managementKey:     strings.TrimSpace(managementKey),
		httpClient:        httpClient,
		streamHTTPClient:  &http.Client{Transport: streamTransport},
		streamIdleTimeout: requestLogStreamIdleTimeout(timeout),
	}
}

func cloneDefaultHTTPTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return (&http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}).Clone()
}

func requestLogStreamIdleTimeout(timeout time.Duration) time.Duration {
	if timeout > defaultRequestLogStreamIdleTimeout {
		return timeout
	}
	return defaultRequestLogStreamIdleTimeout
}

func (c *Client) FetchRequestLogByID(ctx context.Context, requestID string) (*RequestLogResult, error) {
	return c.fetchRequestLogByID(ctx, requestID, RequestLogPreviewMaxBytes)
}

const RequestLogPreviewMaxBytes int64 = 6 * 1024 * 1024

func (c *Client) fetchRequestLogByID(ctx context.Context, requestID string, maxBodyBytes int64) (*RequestLogResult, error) {
	result := &RequestLogResult{}
	req, err := c.newRequestLogRequest(ctx, requestID)
	if err != nil {
		return result, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("request management request log: %w", err)
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.ContentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	result.Filename = filenameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	result.ContentLength = resp.ContentLength
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices && maxBodyBytes > 0 && resp.ContentLength > maxBodyBytes {
		result.BodyTruncated = true
		return result, nil
	}

	reader := io.Reader(resp.Body)
	if maxBodyBytes > 0 {
		reader = io.LimitReader(resp.Body, maxBodyBytes+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return result, fmt.Errorf("read management request log response: %w", err)
	}
	result.Body = body
	if maxBodyBytes > 0 && int64(len(body)) > maxBodyBytes {
		result.BodyTruncated = true
	} else if result.ContentLength < 0 {
		result.ContentLength = int64(len(body))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return result, fmt.Errorf("management request log request returned status %d", resp.StatusCode)
	}
	return result, nil
}

func (c *Client) OpenRequestLogByID(ctx context.Context, requestID string) (*RequestLogStream, error) {
	result := &RequestLogStream{}
	req, err := c.newRequestLogRequest(ctx, requestID)
	if err != nil {
		return result, err
	}

	httpClient := c.streamHTTPClient
	if httpClient == nil {
		httpClient = c.httpClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("request management request log: %w", err)
	}
	result.StatusCode = resp.StatusCode
	result.ContentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	result.Filename = filenameFromContentDisposition(resp.Header.Get("Content-Disposition"))
	result.ContentLength = resp.ContentLength
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return result, fmt.Errorf("management request log request returned status %d", resp.StatusCode)
	}
	result.Body = newIdleTimeoutReadCloser(resp.Body, c.streamIdleTimeout)
	return result, nil
}

type idleTimeoutReadCloser struct {
	body    io.ReadCloser
	timeout time.Duration

	mu       sync.Mutex
	timedOut bool
}

type idleTimeoutReadResult struct {
	n   int
	err error
}

func newIdleTimeoutReadCloser(body io.ReadCloser, timeout time.Duration) io.ReadCloser {
	if body == nil || timeout <= 0 {
		return body
	}
	return &idleTimeoutReadCloser{body: body, timeout: timeout}
}

func (r *idleTimeoutReadCloser) Read(p []byte) (int, error) {
	if r == nil {
		return 0, io.EOF
	}
	r.mu.Lock()
	if r.timedOut {
		r.mu.Unlock()
		return 0, fmt.Errorf("read management request log stream body: %w", context.DeadlineExceeded)
	}
	body := r.body
	timeout := r.timeout
	r.mu.Unlock()

	if body == nil {
		return 0, io.ErrClosedPipe
	}
	if timeout <= 0 {
		return body.Read(p)
	}

	// 让读取结果和 idle timer 竞速，只有 timer 先赢时才关闭底层 body。
	readResult := make(chan idleTimeoutReadResult, 1)
	go func() {
		n, err := body.Read(p)
		readResult <- idleTimeoutReadResult{n: n, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-readResult:
		return result.n, result.err
	case <-timer.C:
		select {
		case result := <-readResult:
			return result.n, result.err
		default:
		}
		r.markTimedOut()
		_ = body.Close()
		result := <-readResult
		if result.err != nil {
			return result.n, fmt.Errorf("read management request log stream body: %w", context.DeadlineExceeded)
		}
		return result.n, result.err
	}
}

func (r *idleTimeoutReadCloser) Close() error {
	if r == nil {
		return nil
	}
	return r.body.Close()
}

func (r *idleTimeoutReadCloser) markTimedOut() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timedOut = true
}

func (c *Client) newRequestLogRequest(ctx context.Context, requestID string) (*http.Request, error) {
	if c == nil {
		return nil, fmt.Errorf("cpa client is nil")
	}
	if c.baseURL == "" {
		return nil, fmt.Errorf("cpa base url is required")
	}
	if c.managementKey == "" {
		return nil, fmt.Errorf("cpa management key is required")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("request id is required")
	}
	if strings.ContainsAny(requestID, "/\\") {
		return nil, fmt.Errorf("request id is invalid")
	}

	path := cpaManagementRequestLogByIDEndpoint + "/" + url.PathEscape(requestID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build management request log request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.managementKey)
	return req, nil
}

func filenameFromContentDisposition(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(params["filename"])
}

func (c *Client) FetchManagementAPIKeys(ctx context.Context) (*response.ManagementAPIKeysResult, error) {
	result := &response.ManagementAPIKeysResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementAPIKeysEndpoint, &result.Payload, "api keys")
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
	apiKeys, err := c.FetchManagementAPIKeys(ctx)
	if err != nil {
		return &response.ModelsResult{}, err
	}
	apiKey := firstNonEmptyString(apiKeys.Payload.APIKeys)
	if apiKey == "" {
		return &response.ModelsResult{}, fmt.Errorf("cpa api keys are required")
	}

	result := &response.ModelsResult{}
	statusCode, body, err := c.doJSONRequest(ctx, cpaModelsEndpoint, &result.Payload, "models", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
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

func (c *Client) UpdateAuthFileStatus(ctx context.Context, name string, disabled bool) error {
	_, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodPatch, cpaManagementAuthFilesStatusEndpoint, authFileStatusRequest{
		Name:     name,
		Disabled: disabled,
	}, nil, "auth file status")
	return err
}

func (c *Client) DeleteAuthFiles(ctx context.Context, names []string) error {
	_, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodDelete, cpaManagementAuthFilesEndpoint, authFilesDeleteRequest{Names: names}, nil, "auth files delete")
	return err
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
