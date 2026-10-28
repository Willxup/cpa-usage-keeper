package cpa

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
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

type credentialStatusRequest struct {
	Name string `json:"name"`
	// AuthIndex 只在单条凭证开关时携带；CPA 用它校验同名文件确实指向目标账号，批量接口保持原样。
	AuthIndex string `json:"auth_index,omitempty"`
	Disabled  bool   `json:"disabled"`
}

type credentialsDeleteRequest struct {
	Names []string `json:"names"`
}

// httpStatusError 只随本次失败响应携带 CPA 标识，不缓存到客户端或输出到错误文本。
type httpStatusError struct {
	kind       string
	statusCode int
	cpaVersion string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s request returned status %d", e.kind, e.statusCode)
}

func (c *Client) doJSONRequest(ctx context.Context, path string, target any, kind string, configure func(*http.Request)) (int, []byte, error) {
	statusCode, body, _, err := c.doJSONRequestWithBody(ctx, http.MethodGet, path, nil, target, kind, configure)
	return statusCode, body, err
}

func (c *Client) doJSONRequestWithBody(ctx context.Context, method string, path string, body []byte, target any, kind string, configure func(*http.Request)) (int, []byte, http.Header, error) {
	if c == nil {
		return 0, nil, nil, fmt.Errorf("cpa client is nil")
	}
	if c.baseURL == "" {
		return 0, nil, nil, fmt.Errorf("cpa base url is required")
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("build %s request: %w", kind, err)
	}
	if configure != nil {
		configure(req)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("request %s: %w", kind, err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, resp.Header, fmt.Errorf("read %s response: %w", kind, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, responseBody, resp.Header, &httpStatusError{
			kind: kind, statusCode: resp.StatusCode, cpaVersion: resp.Header.Get("X-CPA-VERSION"),
		}
	}
	if target == nil || isBlankJSONResponseBody(responseBody) {
		return resp.StatusCode, responseBody, resp.Header, nil
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return resp.StatusCode, responseBody, resp.Header, fmt.Errorf("decode %s json: %w", kind, err)
	}
	return resp.StatusCode, responseBody, resp.Header, nil
}

func isBlankJSONResponseBody(body []byte) bool {
	return len(bytes.TrimSpace(body)) == 0
}

func (c *Client) doManagementJSONRequest(ctx context.Context, path string, target any, kind string) (int, []byte, error) {
	statusCode, body, _, err := c.doManagementJSONRequestWithHeaders(ctx, http.MethodGet, path, nil, target, kind, nil)
	return statusCode, body, err
}

func (c *Client) doManagementJSONPostRequest(ctx context.Context, path string, requestBody any, target any, kind string) (int, []byte, error) {
	return c.doManagementJSONRequestWithBody(ctx, http.MethodPost, path, requestBody, target, kind)
}

func (c *Client) doManagementJSONRequestWithBody(ctx context.Context, method string, path string, requestBody any, target any, kind string) (int, []byte, error) {
	statusCode, body, _, err := c.doManagementJSONRequestWithHeaders(ctx, method, path, requestBody, target, kind, nil)
	return statusCode, body, err
}

// doManagementJSONRequestWithHeaders 将请求头和响应头限定在本次调用，避免并发请求串用 ETag。
func (c *Client) doManagementJSONRequestWithHeaders(ctx context.Context, method string, path string, requestBody any, target any, kind string, headers http.Header) (int, []byte, http.Header, error) {
	if c == nil {
		return 0, nil, nil, fmt.Errorf("cpa client is nil")
	}
	if c.managementKey == "" {
		return 0, nil, nil, fmt.Errorf("cpa management key is required")
	}
	var body []byte
	if method != http.MethodGet {
		var err error
		body, err = json.Marshal(requestBody)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("encode management %s json: %w", kind, err)
		}
	}
	return c.doJSONRequestWithBody(ctx, method, path, body, target, "management "+kind, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+c.managementKey)
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		for name, values := range headers {
			req.Header[name] = values
		}
	})
}

const (
	defaultRequestLogStreamIdleTimeout = 30 * time.Second
	// 同时覆盖八路 metadata 和默认十个 quota worker（部分每个发两次请求）的空闲连接复用。
	defaultMaxIdleConnsPerHost = 32
)

func NewClient(baseURL, managementKey string, timeout time.Duration, tlsSkipVerify bool) *Client {
	transport := cloneDefaultHTTPTransport()
	transport.MaxIdleConnsPerHost = max(transport.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost)
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

// FetchManagementAPIKeys 读取访问 CPA 的客户端 Key，供本地 Key 同步和模型列表请求使用。
// 请求失败（包括 404）保持失败，只有成功返回空数组才表示远端列表确实已清空。
func (c *Client) FetchManagementAPIKeys(ctx context.Context) (*response.ManagementAPIKeysResult, error) {
	result := &response.ManagementAPIKeysResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementAPIKeysEndpoint, nil, "api keys")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result.Payload); err != nil {
		return result, fmt.Errorf("decode management api keys json: %w", err)
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

func (c *Client) FetchCredentials(ctx context.Context) (*response.CredentialsResult, error) {
	result := &response.CredentialsResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementCredentialsEndpoint, &result.Payload, "credentials")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	return result, nil
}

// UpdateCredentialStatus 切换凭证启用状态；authIndex 为空时退化为 CPA 原有的按文件名定位。
func (c *Client) UpdateCredentialStatus(ctx context.Context, name string, authIndex string, disabled bool) (int, error) {
	statusCode, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodPatch, cpaManagementCredentialsStatusEndpoint, credentialStatusRequest{
		Name:      name,
		AuthIndex: authIndex,
		Disabled:  disabled,
	}, nil, "credential status")
	return statusCode, err
}

func (c *Client) DeleteCredentials(ctx context.Context, names []string) error {
	_, _, err := c.doManagementJSONRequestWithBody(ctx, http.MethodDelete, cpaManagementCredentialsEndpoint, credentialsDeleteRequest{Names: names}, nil, "credentials delete")
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

func (c *Client) ResetQuota(ctx context.Context, authIndex string) error {
	_, _, err := c.doManagementJSONPostRequest(ctx, cpaManagementResetQuotaEndpoint, struct {
		AuthIndex string `json:"auth_index"`
	}{AuthIndex: authIndex}, nil, "quota recovery")
	return err
}

func (c *Client) FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementGeminiAPIKeyEndpoint, "gemini api keys")
}

// FetchInteractionsAPIKeys 读取独立的 Gemini Interactions API Key metadata endpoint。
func (c *Client) FetchInteractionsAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 标准供应商统一读取 v8 分组配置。
	return c.fetchProviderKeyConfig(ctx, cpaManagementInteractionsAPIKeyEndpoint, "interactions api keys")
}

func (c *Client) FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementClaudeAPIKeyEndpoint, "claude api keys")
}

func (c *Client) FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementCodexAPIKeyEndpoint, "codex api keys")
}

// FetchMetaAPIKeys 读取独立的 Meta API Key metadata endpoint，不经过 Credential 或通用 quota 路径。
func (c *Client) FetchMetaAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// Meta 只在有效值投影中补默认地址。
	return c.fetchProviderKeyConfig(ctx, cpaManagementMetaAPIKeyEndpoint, "meta api keys")
}

// FetchXAIAPIKeys 读取 xAI API Key metadata endpoint，不复用 OAuth Credential 路径。
func (c *Client) FetchXAIAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 复用标准 provider key 解码器，忽略 websockets 等 Keeper 不消费的额外字段。
	return c.fetchProviderKeyConfig(ctx, cpaManagementXAIAPIKeyEndpoint, "xai api keys")
}

func (c *Client) FetchVertexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementVertexAPIKeyEndpoint, "vertex api keys")
}

// configurationNotFound 只接受同次 CPA 响应明确返回的配置缺省。
// 标识头只验证非空，允许 dev；缺头的网关 404 仍失败，由同步层保留本地身份。
func configurationNotFound(statusCode int, body []byte, requestErr error) bool {
	if statusCode != http.StatusNotFound {
		return false
	}
	var statusErr *httpStatusError
	if !errors.As(requestErr, &statusErr) || strings.TrimSpace(statusErr.cpaVersion) == "" {
		return false
	}
	var payload struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Error == "not_found"
}

// fetchProviderKeyConfig 从同一次响应生成写回原文和有效成员视图，保证二者顺序一致。
// 明确缺省是成功的空来源；请求或解码失败则交给同步层保留已有来源数据。
func (c *Client) fetchProviderKeyConfig(ctx context.Context, path string, kind string) (*response.ProviderKeyConfigResult, error) {
	result := &response.ProviderKeyConfigResult{}
	statusCode, body, headers, err := c.doManagementJSONRequestWithHeaders(ctx, http.MethodGet, path, nil, nil, kind, nil)
	result.StatusCode, result.Body, result.ETag = statusCode, body, headers.Get("ETag")
	if configurationNotFound(statusCode, body, err) {
		result.Document = &providerconfig.Document{}
		result.Payload = []providerconfig.ProviderKeyConfig{}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var document providerconfig.Document
	if err := json.Unmarshal(body, &document); err != nil {
		return result, fmt.Errorf("decode management %s json: %w", kind, err)
	}
	defaultBaseURL := ""
	if path == cpaManagementMetaAPIKeyEndpoint {
		defaultBaseURL = "https://api.meta.ai/v1"
	}
	payload, err := document.ProviderKeys(defaultBaseURL)
	if err != nil {
		return result, fmt.Errorf("decode management %s json: %w", kind, err)
	}
	result.Document, result.Payload = &document, payload
	return result, nil
}

// FetchOpenAICompatibility 保留 OpenAI 组边界，使组级编辑可复用本次读取的完整配置。
// 缺省与失败的处理和标准供应商一致，但有效值投影不套用标准供应商的成员继承规则。
func (c *Client) FetchOpenAICompatibility(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
	result := &response.OpenAICompatibilityResult{}
	statusCode, body, headers, err := c.doManagementJSONRequestWithHeaders(ctx, http.MethodGet, cpaManagementOpenAICompatibilityEndpoint, nil, nil, "openai compatibility", nil)
	result.StatusCode, result.Body, result.ETag = statusCode, body, headers.Get("ETag")
	if configurationNotFound(statusCode, body, err) {
		result.Document = &providerconfig.Document{}
		result.Payload = []providerconfig.OpenAICompatibilityConfig{}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var document providerconfig.Document
	if err := json.Unmarshal(body, &document); err != nil {
		return result, fmt.Errorf("decode management openai compatibility json: %w", err)
	}
	payload, err := document.OpenAIProviders()
	if err != nil {
		return result, fmt.Errorf("decode management openai compatibility json: %w", err)
	}
	result.Document, result.Payload = &document, payload
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
