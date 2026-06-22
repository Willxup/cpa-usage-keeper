package service

import (
	"strings"
	"testing"
	"time"
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
	if event.ModelAlias == nil || *event.ModelAlias != "claude-sonnet-alias" {
		t.Fatalf("expected model alias to decode, got %+v", event.ModelAlias)
	}
	if event.ReasoningEffort != "medium" {
		t.Fatalf("expected reasoning effort to decode, got %q", event.ReasoningEffort)
	}
	if event.ExecutorType != "responses" {
		t.Fatalf("expected executor type to decode, got %q", event.ExecutorType)
	}
	if event.ServiceTier != "standard" {
		t.Fatalf("expected service tier to decode, got %q", event.ServiceTier)
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
	if snapshot.Headers.Get("X-Codex-Plan-Type") != "pro" {
		t.Fatalf("expected codex plan header, got %#v", snapshot.Headers)
	}
	if snapshot.ObservedAt.IsZero() {
		t.Fatalf("expected observed timestamp")
	}
}

func TestDecodeRedisUsageMessageWithHeadersSkipsMalformedHeadersWithoutBlockingEvent(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local)
	event, _, snapshot, err := DecodeRedisUsageMessageWithHeaders(`{
		"timestamp":"2026-06-22T11:10:43+08:00",
		"auth_type":"oauth",
		"auth_index":"codex-auth",
		"provider":"codex",
		"request_id":"req-header-malformed",
		"response_headers":"not-a-header-map"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessageWithHeaders returned error: %v", err)
	}
	if event.RequestID != "req-header-malformed" || event.AuthIndex != "codex-auth" {
		t.Fatalf("expected usage event to decode despite malformed headers, got %+v", event)
	}
	if snapshot != nil {
		t.Fatalf("expected malformed headers to skip quota snapshot, got %+v", snapshot)
	}
}

func TestDecodeRedisUsageMessageWithHeadersSkipsHeadersWithoutCompleteCodexQuota(t *testing.T) {
	tests := []struct {
		name    string
		headers string
	}{
		{
			name:    "ordinary response headers",
			headers: `"Date":["Mon, 22 Jun 2026 03:10:44 GMT"]`,
		},
		{
			name:    "codex quota without reset boundary",
			headers: `"X-Codex-Primary-Used-Percent":["4"],"X-Codex-Primary-Window-Minutes":["300"]`,
		},
		{
			name:    "codex quota without window minutes",
			headers: `"X-Codex-Primary-Used-Percent":["4"],"X-Codex-Primary-Reset-After-Seconds":["60"]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, snapshot, err := DecodeRedisUsageMessageWithHeaders(`{
				"timestamp":"2026-06-22T11:10:43+08:00",
				"auth_type":"oauth",
				"auth_index":"codex-auth",
				"provider":"codex",
				"request_id":"req-header-ignored",
				"response_headers":{`+tt.headers+`}
			}`, time.Date(2026, 6, 22, 11, 10, 43, 0, time.Local))
			if err != nil {
				t.Fatalf("DecodeRedisUsageMessageWithHeaders returned error: %v", err)
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
