package test

import (
	. "cpa-usage-keeper/internal/service"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	"cpa-usage-keeper/internal/quota"
)

func TestDecodeRedisUsageMessageMapsPayloadToUsageEvent(t *testing.T) {
	fetchedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)

	event, raw, err := DecodeRedisUsageMessage(`{
		"timestamp":"2026-04-27T07:59:00Z",
		"latency_ms":1234,
		"ttft_ms":456,
		"service_tier":"standard",
		"source":"sk-test",
		"auth_index":"auth-1",
		"tokens":{"input_tokens":10,"output_tokens":20,"reasoning_tokens":3,"cached_tokens":4,"cache_read_tokens":5,"cache_creation_tokens":6,"total_tokens":0},
		"failed":true,
		"provider":"claude",
		"model":"claude-sonnet-4-6",
		"alias":"claude-sonnet-alias",
		"reasoning_effort":"medium",
		"executor_type":"responses",
		"endpoint":"/v1/messages",
		"auth_type":"api_key",
		"api_key":"raw-key",
		"request_id":"req-123",
		"session_id":"session-root",
		"parent_session_id":"session-parent",
		"unknown":"ignored"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.EventKey != "req-123" || event.APIGroupKey != "raw-key" || event.Model != "claude-sonnet-4-6" || event.Source != "sk-test" || event.AuthIndex != "auth-1" || !event.Failed || event.LatencyMS != 1234 {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.TTFTMS == nil || *event.TTFTMS != 456 {
		t.Fatalf("expected ttft_ms to decode, got %+v", event.TTFTMS)
	}
	if event.Provider != "claude" || event.Endpoint != "/v1/messages" || event.AuthType != "apikey" || event.RequestID != "req-123" {
		t.Fatalf("unexpected redis identity fields: %+v", event)
	}
	if event.SessionID != "session-root" || event.ParentSessionID == nil || *event.ParentSessionID != "session-parent" {
		t.Fatalf("unexpected session fields: session_id=%q parent_session_id=%v", event.SessionID, event.ParentSessionID)
	}
	if event.ModelAlias == nil || *event.ModelAlias != "claude-sonnet-alias" {
		t.Fatalf("expected model alias to decode, got %+v", event.ModelAlias)
	}
	if event.ReasoningEffort != "medium" || event.ExecutorType != "responses" || event.ServiceTier != "standard" {
		t.Fatalf("unexpected request metadata: %+v", event)
	}
	if event.InputTokens != 10 || event.OutputTokens != 20 || event.ReasoningTokens != 3 || event.CachedTokens != 4 || event.CacheReadTokens != 5 || event.CacheCreationTokens != 6 || event.TotalTokens != 0 {
		t.Fatalf("unexpected tokens: %+v", event)
	}
	if !event.Timestamp.Equal(time.Date(2026, 4, 27, 7, 59, 0, 0, time.UTC)) {
		t.Fatalf("unexpected timestamp: %s", event.Timestamp)
	}
	if !strings.Contains(string(raw), `"unknown":"ignored"`) {
		t.Fatalf("expected raw message to be preserved, got %s", string(raw))
	}
}

func TestDecodeRedisUsageMessageRequiresRequestID(t *testing.T) {
	_, _, err := DecodeRedisUsageMessage(`{"latency_ms":-5,"tokens":{"input_tokens":1,"output_tokens":2},"endpoint":"/fallback"}`, time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "request_id is required") {
		t.Fatalf("expected missing request_id error, got %v", err)
	}
}

func TestDecodeRedisUsageMessageNormalizesBlankParentSessionIDToNull(t *testing.T) {
	for _, payload := range []string{
		`{"request_id":"missing-parent"}`,
		`{"request_id":"empty-parent","parent_session_id":""}`,
		`{"request_id":"blank-parent","parent_session_id":"   "}`,
	} {
		event, _, err := DecodeRedisUsageMessage(payload, time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
		}
		if event.ParentSessionID != nil {
			t.Fatalf("expected blank parent_session_id to normalize to nil, got %q", *event.ParentSessionID)
		}
	}
}

func TestDecodeRedisUsageMessageWithHeadersExtractsQuotaSnapshot(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local)
	event, _, snapshot, err := DecodeRedisUsageMessageWithHeaders(`{
		"timestamp":"2026-06-22T11:10:43+08:00",
		"auth_type":"oauth",
		"auth_index":"codex-auth",
		"provider":"codex",
		"request_id":"req-header",
		"response_headers":{
			"X-Codex-Plan-Type":["pro"],
			"X-Codex-Primary-Used-Percent":["4"],
			"X-Codex-Primary-Window-Minutes":["300"],
			"X-Codex-Primary-Reset-After-Seconds":["60"]
		}
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessageWithHeaders returned error: %v", err)
	}
	if event.AuthType != "oauth" || event.AuthIndex != "codex-auth" {
		t.Fatalf("unexpected event identity: %+v", event)
	}
	if snapshot == nil {
		t.Fatal("expected quota header snapshot")
	}
	if snapshot.AuthType != "oauth" || snapshot.AuthIndex != "codex-auth" || snapshot.Provider != "codex" {
		t.Fatalf("unexpected snapshot identity: %+v", snapshot)
	}
	if codexSnapshotPlan(snapshot) != "pro" {
		t.Fatalf("expected decoded Codex plan, got %#v", snapshot.CacheOutput)
	}
	if snapshot.ObservedAt.IsZero() {
		t.Fatalf("expected observed timestamp")
	}
}

func codexSnapshotPlan(snapshot *quota.UsageHeaderSnapshot) string {
	// 快照已不持有 Header；测试从只读 cache 投影验证同一字段语义。
	if snapshot == nil {
		return ""
	}
	result, ok := snapshot.CacheOutput.Result.(quota.CodexResult)
	if !ok || result.Usage == nil {
		return ""
	}
	return result.Usage.PlanType
}

func codexSnapshotPrimaryUsedPercent(snapshot *quota.UsageHeaderSnapshot) (float64, bool) {
	// 主额度百分比从共享单次解码生成的 cache 投影读取，不依赖已释放的原始 Header。
	if snapshot == nil {
		return 0, false
	}
	result, ok := snapshot.CacheOutput.Result.(quota.CodexResult)
	if !ok || result.Usage == nil || result.Usage.RateLimit == nil || result.Usage.RateLimit.PrimaryWindow == nil {
		return 0, false
	}
	return result.Usage.RateLimit.PrimaryWindow.UsedPercent, true
}

func TestDecodeRedisUsageResponseHeadersSkipsNullWithoutAllocating(t *testing.T) {
	raw := json.RawMessage(" \nnull\t")
	allocs := testing.AllocsPerRun(1000, func() {
		headers, ok := decodeRedisUsageResponseHeaders(raw)
		if ok || headers != nil {
			t.Fatalf("expected null response_headers to be skipped, got ok=%v headers=%+v", ok, headers)
		}
	})
	if allocs != 0 {
		t.Fatalf("expected null response_headers check to avoid allocations, got %.2f", allocs)
	}
}

func TestDecodeRedisUsageMessageWithHeadersSkipsInvalidOrIncompleteHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers string
	}{
		{name: "malformed headers", headers: `"not-a-header-map"`},
		{
			name:    "ordinary response headers",
			headers: `{"Date":["Mon, 22 Jun 2026 03:10:44 GMT"]}`,
		},
		{
			name:    "codex quota without reset boundary",
			headers: `{"X-Codex-Primary-Used-Percent":["4"],"X-Codex-Primary-Window-Minutes":["300"]}`,
		},
		{
			name:    "codex quota without window minutes",
			headers: `{"X-Codex-Primary-Used-Percent":["4"],"X-Codex-Primary-Reset-After-Seconds":["60"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, _, snapshot, err := DecodeRedisUsageMessageWithHeaders(`{
				"timestamp":"2026-06-22T11:10:43+08:00",
				"auth_type":"oauth",
				"auth_index":"codex-auth",
				"provider":"codex",
				"request_id":"req-header-ignored",
				"response_headers":`+tt.headers+`
			}`, time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local))
			if err != nil {
				t.Fatalf("DecodeRedisUsageMessageWithHeaders returned error: %v", err)
			}
			if event.RequestID != "req-header-ignored" || event.AuthIndex != "codex-auth" {
				t.Fatalf("expected event to decode despite unusable headers: %+v", event)
			}
			if snapshot != nil {
				t.Fatalf("expected incomplete/non-codex headers to skip quota snapshot, got %+v", snapshot)
			}
		})
	}
}

func TestDecodeRedisUsageMessageFallsBackToProviderWhenAPIKeyIsBlank(t *testing.T) {
	event, _, err := DecodeRedisUsageMessage(`{"api_key":"   ","provider":"claude","endpoint":"/v1/messages","request_id":"req-blank-key"}`, time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.EventKey != "req-blank-key" || event.APIGroupKey != "claude" {
		t.Fatalf("unexpected fallback event: %+v", event)
	}
}

func TestDecodeRedisUsageMessageReportsOnlyMessageError(t *testing.T) {
	_, _, err := DecodeRedisUsageMessage(`{bad-json}`, time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

// 直接保留空 Header 的零分配检查，完整解码入口还包含 JSON/事件自身分配。
//
//go:linkname decodeRedisUsageResponseHeaders cpa-usage-keeper/internal/service.decodeRedisUsageResponseHeaders
func decodeRedisUsageResponseHeaders(raw json.RawMessage) (http.Header, bool)
