package service

import (
	"context"
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

type staticRedisQueue struct {
	messages []string
	err      error
}

func (q staticRedisQueue) PopUsage(context.Context) ([]string, error) {
	return q.messages, q.err
}

func TestExtractCodex429TelemetryParsesStatusCode429(t *testing.T) {
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":true,
		"error":{"type":"usage_limit_reached","resets_in_seconds":300},
		"request_id":"req-1",
		"auth_index":"auth-codex-1"
	}`))
	if tel == nil {
		t.Fatal("expected telemetry, got nil")
	}
	if tel.StatusCode != 429 {
		t.Fatalf("expected status 429, got %d", tel.StatusCode)
	}
	if tel.ErrorType != "usage_limit_reached" {
		t.Fatalf("expected usage_limit_reached, got %q", tel.ErrorType)
	}
	if tel.ResetsInSec == nil || *tel.ResetsInSec != 300 {
		t.Fatalf("expected resets_in_seconds=300, got %+v", tel.ResetsInSec)
	}
}

func TestExtractCodex429TelemetryParsesResetsAt(t *testing.T) {
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":true,
		"error":{"type":"usage_limit_reached","resets_at":"2026-06-01T12:00:00Z"},
		"request_id":"req-2",
		"auth_index":"auth-codex-2"
	}`))
	if tel == nil {
		t.Fatal("expected telemetry, got nil")
	}
	if tel.ResetsAt == nil {
		t.Fatal("expected resets_at, got nil")
	}
	expected := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if !tel.ResetsAt.Equal(expected) {
		t.Fatalf("expected resets_at %s, got %s", expected, *tel.ResetsAt)
	}
}

func TestExtractCodex429TelemetryParsesNestedErrorType(t *testing.T) {
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":true,
		"error":{"error":{"type":"usage_limit_reached"},"resets_in_seconds":120},
		"request_id":"req-3",
		"auth_index":"auth-codex-3"
	}`))
	if tel == nil {
		t.Fatal("expected telemetry, got nil")
	}
	if tel.ErrorType != "usage_limit_reached" {
		t.Fatalf("expected usage_limit_reached from nested path, got %q", tel.ErrorType)
	}
}

func TestExtractCodex429TelemetryMissingFieldsNoCooldown(t *testing.T) {
	// 没有 status_code
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"failed":true,
		"request_id":"req-4",
		"auth_index":"auth"
	}`))
	if tel != nil {
		t.Fatal("expected nil for missing status_code")
	}
	// 非 codex provider
	tel = ExtractCodex429Telemetry([]byte(`{
		"provider":"openai",
		"status_code":429,
		"failed":true,
		"error":{"type":"usage_limit_reached"},
		"request_id":"req-5"
	}`))
	if tel != nil {
		t.Fatal("expected nil for non-codex provider")
	}
	// 非 429
	tel = ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":500,
		"failed":true,
		"error":{"type":"usage_limit_reached"},
		"request_id":"req-6"
	}`))
	if tel != nil {
		t.Fatal("expected nil for non-429 status")
	}
	// 429 但 error.type 不是 usage_limit_reached
	tel = ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":true,
		"error":{"type":"rate_limit_exceeded"},
		"request_id":"req-7"
	}`))
	if tel != nil {
		t.Fatal("expected nil for non-usage_limit_reached error type")
	}
}

func TestExtractCodex429TelemetryNonFailedNotTriggered(t *testing.T) {
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":false,
		"error":{"type":"usage_limit_reached"},
		"request_id":"req-8"
	}`))
	if tel != nil {
		t.Fatal("expected nil for non-failed event")
	}
}

func TestExtractCodex429TelemetryPreservesRawErrorBody(t *testing.T) {
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":true,
		"error":{"type":"usage_limit_reached","resets_in_seconds":60,"response_body":"rate limit hit"},
		"request_id":"req-9",
		"auth_index":"auth-codex-9"
	}`))
	if tel == nil {
		t.Fatal("expected telemetry, got nil")
	}
	if !strings.Contains(tel.RawErrorBody, "rate limit hit") {
		t.Fatalf("expected raw error body, got %q", tel.RawErrorBody)
	}
}

func TestExtractCodex429TelemetryTruncatedBody(t *testing.T) {
	longBody := ""
	for i := 0; i < 2000; i++ {
		longBody += "x"
	}
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":true,
		"error":{"type":"usage_limit_reached","resets_in_seconds":60,"response_body":"` + longBody + `"},
		"request_id":"req-10",
		"auth_index":"auth-codex-10"
	}`))
	if tel == nil {
		t.Fatal("expected telemetry, got nil")
	}
	truncated := tel.RawErrorBodyTruncated()
	if len(truncated) > 1024 {
		t.Fatalf("expected truncated body <= 1024, got %d", len(truncated))
	}
}

func TestExtractCodex429TelemetryEmptyOrInvalidJSON(t *testing.T) {
	if tel := ExtractCodex429Telemetry(nil); tel != nil {
		t.Fatal("expected nil for nil input")
	}
	if tel := ExtractCodex429Telemetry([]byte("")); tel != nil {
		t.Fatal("expected nil for empty input")
	}
	if tel := ExtractCodex429Telemetry([]byte("{bad}")); tel != nil {
		t.Fatal("expected nil for invalid JSON")
	}
}

func TestExtractCodex429TelemetryNoErrorBlock(t *testing.T) {
	tel := ExtractCodex429Telemetry([]byte(`{
		"provider":"codex",
		"status_code":429,
		"failed":true,
		"request_id":"req-11",
		"auth_index":"auth-11"
	}`))
	if tel != nil {
		t.Fatal("expected nil when no error block present")
	}
}
