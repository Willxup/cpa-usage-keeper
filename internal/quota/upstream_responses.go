package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

// UpstreamResponse 是 CPA api-call 返回的上游响应快照；刻意不保存任何请求 Header。
type UpstreamResponse struct {
	Method     string              `json:"method"`
	URL        string              `json:"url"`
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header,omitempty"`
	Body       string              `json:"body"`
}

type upstreamResponseCollector struct {
	mu        sync.Mutex
	responses []UpstreamResponse
}

type upstreamResponseCollectorContextKey struct{}

func withUpstreamResponseCollector(ctx context.Context) (context.Context, *upstreamResponseCollector) {
	collector := &upstreamResponseCollector{}
	return context.WithValue(ctx, upstreamResponseCollectorContextKey{}, collector), collector
}

func upstreamResponseCollectorFromContext(ctx context.Context) *upstreamResponseCollector {
	if ctx == nil {
		return nil
	}
	collector, _ := ctx.Value(upstreamResponseCollectorContextKey{}).(*upstreamResponseCollector)
	return collector
}

func (c *upstreamResponseCollector) record(request apicall.Request, response *apicall.Response) {
	if c == nil || !hasUpstreamResponse(response) {
		return
	}
	snapshot := UpstreamResponse{
		Method:     strings.ToUpper(strings.TrimSpace(request.Method)),
		URL:        strings.TrimSpace(request.URL),
		StatusCode: response.StatusCode,
		Header:     cloneUpstreamHeader(response.Header),
		Body:       upstreamResponseBody(response),
	}
	c.mu.Lock()
	c.responses = append(c.responses, snapshot)
	c.mu.Unlock()
}

func (c *upstreamResponseCollector) snapshot() []UpstreamResponse {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneUpstreamResponses(c.responses)
}

type upstreamResponseRecordingCaller struct {
	caller ManagementAPICaller
}

func (c upstreamResponseRecordingCaller) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	response, err := c.caller.CallManagementAPI(ctx, request)
	// 在 provider 解析前记录，因此非 2xx 或无法解析的真实上游响应仍可从刷新任务排查。
	if collector := upstreamResponseCollectorFromContext(ctx); collector != nil {
		collector.record(request, response)
	}
	return response, err
}

func hasUpstreamResponse(response *apicall.Response) bool {
	return response != nil && (response.StatusCode != 0 || len(response.Header) > 0 || response.BodyText != "" || len(bytes.TrimSpace(response.Body)) > 0)
}

func upstreamResponseBody(response *apicall.Response) string {
	if response == nil {
		return ""
	}
	if raw := bytes.TrimSpace(response.Body); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		// CPA 当前把原始 body 放在 JSON string 中；旧兼容响应也可能直接放 object/array。
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			return text
		}
		return string(raw)
	}
	return response.BodyText
}

func cloneUpstreamResponses(values []UpstreamResponse) []UpstreamResponse {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]UpstreamResponse, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].Header = cloneUpstreamHeader(value.Header)
	}
	return cloned
}

func cloneUpstreamHeader(header map[string][]string) map[string][]string {
	if len(header) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}
