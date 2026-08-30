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
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
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

type codexAuthFileClassification uint8

const (
	codexAuthFileUnknown codexAuthFileClassification = iota
	codexAuthFileNative
	codexAuthFileSidecar
)

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
	return c.fetchAuthFiles(ctx, true)
}

func (c *Client) fetchAuthFiles(ctx context.Context, enrichCodexMetadata bool) (*response.AuthFilesResult, error) {
	result := &response.AuthFilesResult{}
	statusCode, body, err := c.doManagementJSONRequest(ctx, cpaManagementAuthFilesEndpoint, &result.Payload, "auth files")
	result.StatusCode = statusCode
	result.Body = body
	if err != nil {
		return result, err
	}
	if enrichCodexMetadata {
		// CPA versions before plugin metadata projection only expose the runtime
		// provider and email in this list. Fill the Codex-specific account/team
		// fields from clearly sidecar-managed files without returning their raw
		// credential JSON to callers.
		c.enrichCodexAuthFileMetadata(ctx, &result.Payload)
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
	if !isCodexQuotaRequest(request) {
		result, _, err := c.callManagementAPIAt(ctx, cpaManagementAPICallEndpoint, request, "api call")
		return result, err
	}

	// CPA reserves /v0/management/api-call before plugin routes are mounted.
	// Send Codex quota/reset calls through the plugin-owned route so a
	// sidecar-managed auth file is converted to the correct AgentAssertion or
	// PAT bearer token instead of leaking its opaque cais_* client key upstream.
	result, statusCode, err := c.callManagementAPIAt(ctx, cpaManagementCodexAgentIdentityAPICallEndpoint, request, "codex api call")
	if err == nil {
		return result, nil
	}
	if !isManagementRouteUnavailable(statusCode) {
		return result, err
	}

	// Keep native OAuth working on CPA deployments where the plugin is not
	// installed yet. Only fall back after positively identifying the auth file
	// as a native credential; an unknown or sidecar-managed file must never be
	// sent through CPA's native route with cais_* as a Bearer token.
	classification, classifyErr := c.classifyCodexAuthIndex(ctx, request.AuthIndex)
	if classifyErr != nil {
		return result, fmt.Errorf("codex agent identity bridge unavailable; refusing unsafe native fallback: %w", classifyErr)
	}
	if classification != codexAuthFileNative {
		return result, fmt.Errorf("codex agent identity bridge unavailable for this auth file; install or enable the Codex Agent Identity plugin")
	}

	nativeResult, _, nativeErr := c.callManagementAPIAt(ctx, cpaManagementAPICallEndpoint, request, "api call")
	return nativeResult, nativeErr
}

func (c *Client) callManagementAPIAt(ctx context.Context, path string, request apicall.Request, kind string) (*apicall.Response, int, error) {
	result := &apicall.Response{}
	statusCode, _, err := c.doManagementJSONPostRequest(ctx, path, request, result, kind)
	return result, statusCode, err
}

func isManagementRouteUnavailable(statusCode int) bool {
	return statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed
}

func isCodexQuotaRequest(request apicall.Request) bool {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	parsed, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "chatgpt.com") || parsed.Port() != "" {
		return false
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" {
		return false
	}

	switch parsed.Path {
	case "/backend-api/wham/usage", "/backend-api/wham/rate-limit-reset-credits":
		return method == http.MethodGet
	case "/backend-api/wham/rate-limit-reset-credits/consume":
		return method == http.MethodPost
	default:
		return false
	}
}

func (c *Client) classifyCodexAuthIndex(ctx context.Context, authIndex string) (codexAuthFileClassification, error) {
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		// No auth index means there is no opaque sidecar key that this client
		// could accidentally forward. Native CPA resolution remains safe.
		return codexAuthFileNative, nil
	}

	files, err := c.fetchAuthFiles(ctx, false)
	if err != nil {
		return codexAuthFileUnknown, fmt.Errorf("inspect CPA auth files: %w", err)
	}
	found := false
	for _, file := range files.Payload.Files {
		if strings.TrimSpace(file.AuthIndex) != authIndex {
			continue
		}
		found = true
		if isSidecarCodexAuthFile(file) {
			return codexAuthFileSidecar, nil
		}
		if len(authFileDownloadNames(file.Name, file.Path)) == 0 {
			return codexAuthFileUnknown, fmt.Errorf("CPA auth file name is unavailable")
		}
		raw, exists, err := c.downloadAuthFileRaw(ctx, file.Name, file.Path)
		if err != nil {
			return codexAuthFileUnknown, fmt.Errorf("inspect CPA auth file: %w", err)
		}
		if !exists {
			continue
		}
		switch classifyCodexAuthFile(raw) {
		case codexAuthFileSidecar:
			return codexAuthFileSidecar, nil
		case codexAuthFileNative:
			return codexAuthFileNative, nil
		}
	}
	if !found {
		return codexAuthFileUnknown, nil
	}
	return codexAuthFileUnknown, nil
}

func authFileDownloadName(name, filePath string) string {
	names := authFileDownloadNames(name, filePath)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func authFileDownloadNames(name, filePath string) []string {
	candidates := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, raw := range []string{name, filePath} {
		candidate := authFileDownloadCandidate(raw)
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func authFileDownloadCandidate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return ""
		}
	}
	candidate := path.Base(normalized)
	if candidate == "" || candidate == "." || candidate == "/" || strings.ContainsAny(candidate, "/\\\r\n\x00") {
		return ""
	}
	if len(candidate) <= len(".json") || !strings.EqualFold(path.Ext(candidate), ".json") {
		return ""
	}
	return candidate
}

// enrichCodexAuthFileMetadata performs a best-effort, in-memory metadata
// projection for the sidecar files emitted by cpa-codex-agent-identity. The
// CPA list endpoint is intentionally not assumed to expose plugin Metadata or
// Attributes, so Keeper reads only the small set of non-secret identity fields
// needed for display and ChatGPT-Account-Id routing.
func (c *Client) enrichCodexAuthFileMetadata(ctx context.Context, payload *authfiles.AuthFilesResponse) {
	if c == nil || payload == nil {
		return
	}
	for index := range payload.Files {
		file := &payload.Files[index]
		if !shouldInspectCodexAuthFile(*file) {
			continue
		}
		if len(authFileDownloadNames(file.Name, file.Path)) == 0 {
			continue
		}
		raw, exists, err := c.downloadAuthFileRaw(ctx, file.Name, file.Path)
		if err != nil || !exists {
			// Metadata enrichment must not turn a successful auth-files list into
			// an all-or-nothing failure. The normal bridge path still refuses an
			// unsafe fallback if it cannot classify the credential later.
			continue
		}
		metadata, classification, ok := parseCodexAuthFileMetadata(raw)
		if !ok || classification != codexAuthFileSidecar {
			continue
		}
		mergeCodexAuthFileMetadata(file, metadata)
	}
}

func shouldInspectCodexAuthFile(file authfiles.AuthFile) bool {
	typeName := strings.ToLower(strings.TrimSpace(file.Type))
	providerName := strings.ToLower(strings.TrimSpace(file.Provider))
	if typeName != "codex" && typeName != "codex-agent-identity" && providerName != "codex" && providerName != "codex-agent-identity" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(file.AuthMode), "agent_identity_sidecar") || strings.TrimSpace(file.AgentIdentityID) != "" {
		return true
	}
	for _, name := range authFileDownloadNames(file.Name, file.Path) {
		name = strings.ToLower(name)
		if strings.HasSuffix(name, "-agent-identity.json") || strings.HasPrefix(name, "codex-agent-identity-") {
			return true
		}
	}
	return false
}

func isSidecarCodexAuthFile(file authfiles.AuthFile) bool {
	return strings.EqualFold(strings.TrimSpace(file.AuthMode), "agent_identity_sidecar") || strings.TrimSpace(file.AgentIdentityID) != ""
}

type codexAuthFileMetadata struct {
	Type                  string
	AuthMode              string
	CredentialKind        string
	AgentIdentityID       string
	Email                 string
	AccountID             string
	ChatGPTAccountID      string
	ChatGPTAccountIDCamel string
	ChatGPTUserID         string
	ChatGPTUserIDCamel    string
	PlanType              string
	PlanTypeCamel         string
}

func parseCodexAuthFileMetadata(raw []byte) (codexAuthFileMetadata, codexAuthFileClassification, bool) {
	var metadata codexAuthFileMetadata
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return metadata, codexAuthFileUnknown, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return metadata, codexAuthFileUnknown, false
	}
	populateCodexAuthFileMetadata(&metadata, object)
	metadata.Type = strings.ToLower(strings.TrimSpace(metadata.Type))
	metadata.AuthMode = strings.ToLower(strings.TrimSpace(metadata.AuthMode))
	if metadata.Type != "codex" && metadata.Type != "codex-agent-identity" {
		return metadata, codexAuthFileUnknown, false
	}

	credentialObjects := codexAuthCredentialObjects(object)
	accessToken := firstCodexAuthString(credentialObjects, "access_token")
	if metadata.AuthMode == "agent_identity_sidecar" ||
		strings.HasPrefix(accessToken, "cais_") ||
		metadata.AgentIdentityID != "" {
		return metadata, codexAuthFileSidecar, true
	}

	// A native Codex OAuth file has an id_token plus at least one OAuth token.
	// Do not classify an arbitrary type=codex JSON file as native: doing so
	// could make the Keeper fallback send an opaque plugin credential through
	// CPA's native route.
	idToken := strings.TrimSpace(firstCodexAuthString(credentialObjects, "id_token"))
	refreshToken := strings.TrimSpace(firstCodexAuthString(credentialObjects, "refresh_token"))
	if metadata.Type == "codex" && idToken != "" && (refreshToken != "" || accessToken != "") {
		return metadata, codexAuthFileNative, true
	}
	return metadata, codexAuthFileUnknown, true
}

func populateCodexAuthFileMetadata(metadata *codexAuthFileMetadata, object map[string]any) {
	if metadata == nil {
		return
	}
	setCodexAuthString(&metadata.Type, object, "type")
	setCodexAuthString(&metadata.AuthMode, object, "auth_mode")
	setCodexAuthString(&metadata.CredentialKind, object, "credential_kind")
	setCodexAuthString(&metadata.AgentIdentityID, object, "agent_identity_id")
	setCodexAuthString(&metadata.Email, object, "email")
	setCodexAuthString(&metadata.AccountID, object, "account_id")
	setCodexAuthString(&metadata.ChatGPTAccountID, object, "chatgpt_account_id")
	setCodexAuthString(&metadata.ChatGPTAccountIDCamel, object, "chatgptAccountId")
	setCodexAuthString(&metadata.ChatGPTUserID, object, "chatgpt_user_id")
	setCodexAuthString(&metadata.ChatGPTUserIDCamel, object, "chatgptUserId")
	setCodexAuthString(&metadata.PlanType, object, "plan_type")
	setCodexAuthString(&metadata.PlanTypeCamel, object, "planType")
	populateCodexAuthIDTokenMetadataMissing(metadata, object)

	// A few CPA/plugin revisions wrap provider metadata under metadata or
	// attributes. Read only the same allowlisted fields and never retain the
	// wrapper or any access/refresh token value.
	for _, key := range []string{"metadata", "attributes"} {
		nested, ok := object[key].(map[string]any)
		if !ok {
			continue
		}
		populateCodexAuthFileMetadataMissing(metadata, nested)
		populateCodexAuthIDTokenMetadataMissing(metadata, nested)
	}
}

func populateCodexAuthFileMetadataMissing(metadata *codexAuthFileMetadata, object map[string]any) {
	if metadata == nil {
		return
	}
	setCodexAuthStringMissing(&metadata.Type, object, "type")
	setCodexAuthStringMissing(&metadata.AuthMode, object, "auth_mode")
	setCodexAuthStringMissing(&metadata.CredentialKind, object, "credential_kind")
	setCodexAuthStringMissing(&metadata.AgentIdentityID, object, "agent_identity_id")
	setCodexAuthStringMissing(&metadata.Email, object, "email")
	setCodexAuthStringMissing(&metadata.AccountID, object, "account_id")
	setCodexAuthStringMissing(&metadata.ChatGPTAccountID, object, "chatgpt_account_id")
	setCodexAuthStringMissing(&metadata.ChatGPTAccountIDCamel, object, "chatgptAccountId")
	setCodexAuthStringMissing(&metadata.ChatGPTUserID, object, "chatgpt_user_id")
	setCodexAuthStringMissing(&metadata.ChatGPTUserIDCamel, object, "chatgptUserId")
	setCodexAuthStringMissing(&metadata.PlanType, object, "plan_type")
	setCodexAuthStringMissing(&metadata.PlanTypeCamel, object, "planType")
	setCodexAuthStringMissing(&metadata.PlanType, object, "chatgpt_plan_type")
	setCodexAuthStringMissing(&metadata.PlanTypeCamel, object, "chatgptPlanType")
}

func populateCodexAuthIDTokenMetadataMissing(metadata *codexAuthFileMetadata, object map[string]any) {
	if metadata == nil {
		return
	}
	idToken, ok := object["id_token"].(map[string]any)
	if !ok {
		return
	}
	populateCodexAuthFileMetadataMissing(metadata, idToken)
	for _, key := range []string{"auth", "https://api.openai.com/auth", "profile", "https://api.openai.com/profile"} {
		nested, ok := idToken[key].(map[string]any)
		if !ok {
			continue
		}
		populateCodexAuthFileMetadataMissing(metadata, nested)
	}
}

func codexAuthCredentialObjects(object map[string]any) []map[string]any {
	objects := make([]map[string]any, 0, 3)
	if object == nil {
		return objects
	}
	objects = append(objects, object)
	for _, key := range []string{"metadata", "attributes"} {
		if nested, ok := object[key].(map[string]any); ok {
			objects = append(objects, nested)
		}
	}
	return objects
}

func firstCodexAuthString(objects []map[string]any, key string) string {
	for _, object := range objects {
		if value := strings.TrimSpace(codexAuthString(object[key])); value != "" {
			return value
		}
	}
	return ""
}

func setCodexAuthString(target *string, object map[string]any, key string) {
	if target == nil {
		return
	}
	*target = strings.TrimSpace(codexAuthString(object[key]))
}

func setCodexAuthStringMissing(target *string, object map[string]any, key string) {
	if target == nil || strings.TrimSpace(*target) != "" {
		return
	}
	setCodexAuthString(target, object, key)
}

func codexAuthString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func mergeCodexAuthFileMetadata(file *authfiles.AuthFile, metadata codexAuthFileMetadata) {
	if file == nil {
		return
	}
	if strings.TrimSpace(file.Email) == "" {
		file.Email = strings.TrimSpace(metadata.Email)
	}
	if strings.TrimSpace(file.AuthMode) == "" {
		file.AuthMode = strings.TrimSpace(metadata.AuthMode)
	}
	if strings.TrimSpace(file.CredentialKind) == "" {
		file.CredentialKind = strings.TrimSpace(metadata.CredentialKind)
	}
	if strings.TrimSpace(file.AgentIdentityID) == "" {
		file.AgentIdentityID = strings.TrimSpace(metadata.AgentIdentityID)
	}
	file.AccountID = firstNonEmpty(metadata.AccountID, metadata.ChatGPTAccountID, metadata.ChatGPTAccountIDCamel, file.AccountID)
	file.ChatGPTAccountID = firstNonEmpty(metadata.ChatGPTAccountID, metadata.AccountID, file.ChatGPTAccountID)
	file.ChatGPTAccountIDCamel = firstNonEmpty(metadata.ChatGPTAccountIDCamel, file.ChatGPTAccountIDCamel)
	file.ChatGPTUserID = firstNonEmpty(metadata.ChatGPTUserID, file.ChatGPTUserID)
	file.ChatGPTUserIDCamel = firstNonEmpty(metadata.ChatGPTUserIDCamel, file.ChatGPTUserIDCamel)
	file.PlanType = firstNonEmpty(metadata.PlanType, metadata.PlanTypeCamel, file.PlanType)
	file.PlanTypeCamel = firstNonEmpty(metadata.PlanTypeCamel, file.PlanTypeCamel)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

const authFileDownloadMaxBytes int64 = 2 * 1024 * 1024

func (c *Client) downloadAuthFileRaw(ctx context.Context, name, filePath string) ([]byte, bool, error) {
	candidates := authFileDownloadNames(name, filePath)
	if len(candidates) == 0 {
		return nil, false, fmt.Errorf("auth file name is invalid")
	}
	for _, candidate := range candidates {
		body, statusCode, err := c.downloadAuthFileCandidate(ctx, candidate)
		if statusCode == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		return body, true, nil
	}
	return nil, false, nil
}

func (c *Client) downloadAuthFileCandidate(ctx context.Context, name string) ([]byte, int, error) {
	if c == nil {
		return nil, 0, fmt.Errorf("cpa client is nil")
	}
	if c.baseURL == "" {
		return nil, 0, fmt.Errorf("cpa base url is required")
	}
	if c.managementKey == "" {
		return nil, 0, fmt.Errorf("cpa management key is required")
	}
	name = authFileDownloadCandidate(name)
	if name == "" {
		return nil, 0, fmt.Errorf("auth file name is invalid")
	}
	requestPath := cpaManagementAuthFilesDownloadEndpoint + "?name=" + url.QueryEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+requestPath, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build management auth file download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.managementKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request management auth file download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, resp.StatusCode, nil
	}
	if resp.ContentLength > authFileDownloadMaxBytes {
		return nil, resp.StatusCode, fmt.Errorf("auth file download response exceeds %d bytes", authFileDownloadMaxBytes)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, authFileDownloadMaxBytes+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read management auth file download response: %w", err)
	}
	if int64(len(body)) > authFileDownloadMaxBytes {
		return nil, resp.StatusCode, fmt.Errorf("auth file download response exceeds %d bytes", authFileDownloadMaxBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, resp.StatusCode, fmt.Errorf("management auth file download request returned status %d", resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}

func classifyCodexAuthFile(raw []byte) codexAuthFileClassification {
	_, classification, ok := parseCodexAuthFileMetadata(raw)
	if !ok {
		return codexAuthFileUnknown
	}
	return classification
}

func (c *Client) FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementGeminiAPIKeyEndpoint, "gemini-api-key", "gemini api keys")
}

// FetchInteractionsAPIKeys 读取独立的 Gemini Interactions API Key metadata endpoint。
func (c *Client) FetchInteractionsAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 复用标准 provider key 解码器，保持 direct/wrapped、状态码和错误包装与现有来源一致。
	return c.fetchProviderKeyConfig(ctx, cpaManagementInteractionsAPIKeyEndpoint, "interactions-api-key", "interactions api keys")
}

func (c *Client) FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementClaudeAPIKeyEndpoint, "claude-api-key", "claude api keys")
}

func (c *Client) FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return c.fetchProviderKeyConfig(ctx, cpaManagementCodexAPIKeyEndpoint, "codex-api-key", "codex api keys")
}

// FetchXAIAPIKeys 读取 xAI API Key metadata endpoint，不复用 OAuth Auth File 路径。
func (c *Client) FetchXAIAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 复用标准 provider key 解码器，忽略 websockets 等 Keeper 不消费的额外字段。
	return c.fetchProviderKeyConfig(ctx, cpaManagementXAIAPIKeyEndpoint, "xai-api-key", "xai api keys")
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
