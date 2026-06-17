package service

import (
	"context"
	"encoding/json"
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

func TestDecodeRedisUsageMessageExtractsFailureFields(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-fail-1",
		"failed": true,
		"status_code": 503,
		"error": {
			"type": "auth_unavailable",
			"message": "no auth available",
			"response_body": "{\"error\":{\"code\":\"auth_unavailable\",\"message\":\"no auth available\"}}"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil {
		t.Fatal("expected FailureStatusCode != nil")
	}
	if *event.FailureStatusCode != 503 {
		t.Fatalf("expected FailureStatusCode==503, got %d", *event.FailureStatusCode)
	}
	if event.FailureCode != "auth_unavailable" {
		t.Fatalf("expected FailureCode==auth_unavailable, got %q", event.FailureCode)
	}
	if event.FailureMessage != "no auth available" {
		t.Fatalf("expected FailureMessage==\"no auth available\", got %q", event.FailureMessage)
	}
	if event.FailureBody == "" {
		t.Fatal("expected FailureBody non-empty")
	}
}

func TestDecodeRedisUsageMessageFailureFieldsSanitized(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	sensitiveBody := `Authorization: Bearer test-token-secret sk-test123456789abc https://example.com/v1/chat/completions refresh_token=my_refresh_tok`
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-fail-2",
		"failed": true,
		"status_code": 401,
		"error": {
			"type": "unauthorized",
			"message": "invalid key",
			"response_body": "`+strings.ReplaceAll(sensitiveBody, `"`, `\"`)+`"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	body := event.FailureBody
	if strings.Contains(body, "Bearer test-token") {
		t.Fatal("body should not contain Authorization Bearer token")
	}
	if strings.Contains(body, "sk-test123456789") {
		t.Fatal("body should not contain sk- key")
	}
	if strings.Contains(body, "https://example.com") {
		t.Fatal("body should not contain URL")
	}
	if strings.Contains(body, "my_refresh_tok") {
		t.Fatal("body should not contain refresh_token value")
	}
	if !strings.Contains(body, "[redacted_authorization]") {
		t.Fatal("body should contain [redacted_authorization]")
	}
	if !strings.Contains(body, "[redacted_key]") {
		t.Fatal("body should contain [redacted_key]")
	}
	if !strings.Contains(body, "[redacted_url]") {
		t.Fatal("body should contain [redacted_url]")
	}
}

func TestDecodeRedisUsageMessageNotFailedNoFailureFields(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-success-1",
		"failed": false,
		"status_code": 429,
		"error": {
			"type": "usage_limit_reached",
			"message": "should not appear"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode != nil {
		t.Fatal("expected FailureStatusCode nil for non-failed event")
	}
	if event.FailureCode != "" {
		t.Fatalf("expected empty FailureCode, got %q", event.FailureCode)
	}
	if event.FailureMessage != "" {
		t.Fatalf("expected empty FailureMessage, got %q", event.FailureMessage)
	}
	if event.FailureBody != "" {
		t.Fatalf("expected empty FailureBody, got %q", event.FailureBody)
	}
}

// --- OpenAI / Codex style ---

func TestDecodeRedisUsageMessageOpenAICodexError(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-openai-1",
		"failed": true,
		"status_code": 429,
		"error": {
			"message": "The usage limit has been reached",
			"type": "usage_limit_reached",
			"code": "usage_limit_reached",
			"response_body": "{\"error\":{\"message\":\"The usage limit has been reached\",\"type\":\"usage_limit_reached\",\"code\":\"usage_limit_reached\"}}"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 429 {
		t.Fatalf("expected FailureStatusCode==429, got %+v", event.FailureStatusCode)
	}
	if event.FailureCode != "usage_limit_reached" {
		t.Fatalf("expected FailureCode==usage_limit_reached, got %q", event.FailureCode)
	}
	if event.FailureMessage != "The usage limit has been reached" {
		t.Fatalf("expected FailureMessage, got %q", event.FailureMessage)
	}
	if event.FailureBody == "" {
		t.Fatal("expected FailureBody non-empty")
	}
}

// --- Anthropic / Claude style ---

func TestDecodeRedisUsageMessageAnthropicWrapperError(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-anthropic-1",
		"failed": true,
		"status_code": 401,
		"error": {
			"type": "error",
			"error": {"type": "authentication_error", "message": "invalid x-api-key"}
		},
		"provider": "claude",
		"endpoint": "/v1/messages"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 401 {
		t.Fatalf("expected FailureStatusCode==401, got %+v", event.FailureStatusCode)
	}
	if event.FailureCode != "authentication_error" {
		t.Fatalf("expected FailureCode==authentication_error, got %q", event.FailureCode)
	}
	if event.FailureMessage != "invalid x-api-key" {
		t.Fatalf("expected FailureMessage, got %q", event.FailureMessage)
	}
}

// --- Gemini / Google style ---

func TestDecodeRedisUsageMessageGeminiNumericCode(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-gemini-1",
		"failed": true,
		"status_code": 0,
		"error": {
			"code": 429,
			"message": "Resource exhausted",
			"status": "RESOURCE_EXHAUSTED",
			"response_body": "{\"error\":{\"code\":429,\"message\":\"Resource exhausted\",\"status\":\"RESOURCE_EXHAUSTED\"}}"
		},
		"provider": "gemini",
		"endpoint": "/v1beta/models/generateContent"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 429 {
		t.Fatalf("expected FailureStatusCode==429, got %+v", event.FailureStatusCode)
	}
	if event.FailureCode != "resource_exhausted" {
		t.Fatalf("expected FailureCode==resource_exhausted, got %q", event.FailureCode)
	}
	if event.FailureMessage != "Resource exhausted" {
		t.Fatalf("expected FailureMessage, got %q", event.FailureMessage)
	}
}

// --- OpenRouter / gateway style ---

func TestDecodeRedisUsageMessageOpenRouterNumericCode(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-or-1",
		"failed": true,
		"status_code": 429,
		"error": {
			"message": "Provider returned error",
			"code": 429,
			"response_body": "{\"error\":{\"message\":\"Provider returned error\",\"code\":429}}"
		},
		"provider": "openrouter",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 429 {
		t.Fatalf("expected FailureStatusCode==429, got %+v", event.FailureStatusCode)
	}
	// code=429 is numeric, should not appear as FailureCode
	if event.FailureCode != "" {
		t.Fatalf("expected empty FailureCode for numeric code, got %q", event.FailureCode)
	}
	if event.FailureMessage != "Provider returned error" {
		t.Fatalf("expected FailureMessage, got %q", event.FailureMessage)
	}
}

// --- CPA body / body_text / bodyText variants ---

func TestDecodeRedisUsageMessageCPABodyVariants(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	// Test body field
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-cpa-body-1",
		"failed": true,
		"status_code": 502,
		"error": {
			"type": "bad_gateway",
			"message": "upstream error",
			"body": "{\"error\":{\"message\":\"upstream error\"}}"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureBody == "" {
		t.Fatal("expected FailureBody non-empty from body field")
	}
	// Test body_text field
	event, _, err = DecodeRedisUsageMessage(`{
		"request_id": "test-cpa-body-2",
		"failed": true,
		"status_code": 503,
		"error": {
			"type": "service_unavailable",
			"body_text": "Service Unavailable"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureBody == "" {
		t.Fatal("expected FailureBody non-empty from body_text field")
	}
	if event.FailureMessage != "Service Unavailable" {
		t.Fatalf("expected FailureMessage from body_text fallback, got %q", event.FailureMessage)
	}
}

// --- Status code only, no body ---

func TestDecodeRedisUsageMessageStatusCodeOnly(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-status-only",
		"failed": true,
		"status_code": 500,
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 500 {
		t.Fatalf("expected FailureStatusCode==500, got %+v", event.FailureStatusCode)
	}
	if event.FailureCode != "" {
		t.Fatalf("expected empty FailureCode, got %q", event.FailureCode)
	}
	if event.FailureMessage != "" {
		t.Fatalf("expected empty FailureMessage, got %q", event.FailureMessage)
	}
}

// --- Sanitization: JSON token fields and Cookie ---

func TestDecodeRedisUsageMessageSanitizeJSONTokenFields(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	sensitiveBody := `{"access_token": "real_access_token_123", "refresh_token": "real_refresh_456", "api_key": "sk-proj-realkey", "token": "bearer_tok_789", "Cookie": "session=abc123", "error": "bad"}`
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-sanitize-json",
		"failed": true,
		"status_code": 401,
		"error": {
			"type": "unauthorized",
			"message": "bad token",
			"response_body": "` + strings.ReplaceAll(sensitiveBody, `"`, `\"`) + `"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	body := event.FailureBody
	if strings.Contains(body, "real_access_token_123") {
		t.Fatal("body should not contain access_token value")
	}
	if strings.Contains(body, "real_refresh_456") {
		t.Fatal("body should not contain refresh_token value")
	}
	if strings.Contains(body, "sk-proj-realkey") {
		t.Fatal("body should not contain api_key value")
	}
	if strings.Contains(body, "bearer_tok_789") {
		t.Fatal("body should not contain token value")
	}
	if strings.Contains(body, "session=abc123") {
		t.Fatal("body should not contain Cookie value")
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatal("body should contain [redacted] placeholder")
	}
}

// --- Anthropic wrapper: ensure type=="error" is handled ---

func TestDecodeRedisUsageMessageAnthropicTypeEwrapper(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-anthropic-2",
		"failed": true,
		"status_code": 403,
		"error": {
			"type": "error",
			"error": {"type": "permission_denied", "message": "access blocked"}
		},
		"provider": "claude",
		"endpoint": "/v1/messages"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureCode != "permission_denied" {
		t.Fatalf("expected FailureCode==permission_denied, got %q", event.FailureCode)
	}
	if event.FailureMessage != "access blocked" {
		t.Fatalf("expected FailureMessage, got %q", event.FailureMessage)
	}
}

// --- Gemini status code from error.code when top-level status_code is 0 ---

func TestDecodeRedisUsageMessageGeminiStatusCodeFromBody(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-gemini-2",
		"failed": true,
		"status_code": 0,
		"error": {
			"code": 503,
			"message": "Service unavailable",
			"status": "UNAVAILABLE"
		},
		"provider": "gemini",
		"endpoint": "/v1beta/models/generateContent"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 503 {
		t.Fatalf("expected FailureStatusCode==503, got %+v", event.FailureStatusCode)
	}
}

// --- Failed with error but no response_body: body falls back to marshal ---

func TestDecodeRedisUsageMessageErrorBodyFallbackMarshal(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "test-fallback-marshal",
		"failed": true,
		"status_code": 400,
		"error": {
			"type": "invalid_request",
			"message": "bad request"
		},
		"provider": "openai",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureBody == "" {
		t.Fatal("expected FailureBody non-empty from marshal fallback")
	}
	if event.FailureCode != "invalid_request" {
		t.Fatalf("expected FailureCode==invalid_request, got %q", event.FailureCode)
	}
}

// --- CPA fail 字段（CPA 实际发送的结构） ---

func TestDecodeRedisUsageMessageCPAFailField(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "req-cpa-fail-1",
		"failed": true,
		"fail": {
			"status_code": 502,
			"body": "{\"error\":{\"message\":\"unknown provider for model gpt-5.4\",\"type\":\"bad_gateway\"}}"
		},
		"provider": "ai中转流量联盟",
		"model": "gpt-5.4",
		"endpoint": "/v1/responses"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 502 {
		t.Fatalf("expected FailureStatusCode==502, got %+v", event.FailureStatusCode)
	}
	if event.FailureBody == "" {
		t.Fatal("expected FailureBody non-empty from fail.body")
	}
	if !strings.Contains(event.FailureBody, "unknown provider") {
		t.Fatalf("expected FailureBody to contain upstream error message, got %q", event.FailureBody)
	}
	if event.FailureMessage == "" {
		t.Fatal("expected FailureMessage extracted from fail.body")
	}
}

func TestDecodeRedisUsageMessageCPAFailStatusCodeOnly(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"request_id": "req-cpa-fail-2",
		"failed": true,
		"fail": {
			"status_code": 429
		},
		"provider": "openai",
		"model": "gpt-4o",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 429 {
		t.Fatalf("expected FailureStatusCode==429, got %+v", event.FailureStatusCode)
	}
}

// --- F 类：request_id 缺失 fallback ---

func TestDecodeRedisUsageMessageFailedWithoutRequestID(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	event, _, err := DecodeRedisUsageMessage(`{
		"failed": true,
		"fail": {
			"status_code": 502,
			"body": "bad gateway"
		},
		"provider": "some-provider",
		"model": "some-model",
		"endpoint": "/v1/chat/completions"
	}`, fetchedAt)
	if err != nil {
		t.Fatalf("expected failed event to be decoded with fallback key, got error: %v", err)
	}
	if !strings.HasPrefix(event.EventKey, "failed:") {
		t.Fatalf("expected EventKey with 'failed:' prefix, got %q", event.EventKey)
	}
	if event.FailureStatusCode == nil || *event.FailureStatusCode != 502 {
		t.Fatalf("expected FailureStatusCode==502, got %+v", event.FailureStatusCode)
	}
	if !event.Failed {
		t.Fatal("expected Failed=true")
	}
}

func TestDecodeRedisUsageMessageSuccessWithoutRequestIDRejected(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	_, _, err := DecodeRedisUsageMessage(`{
		"failed": false,
		"provider": "openai",
		"model": "gpt-4o"
	}`, fetchedAt)
	if err == nil {
		t.Fatal("expected error for success event without request_id")
	}
	if !strings.Contains(err.Error(), "request_id is required") {
		t.Fatalf("expected 'request_id is required' error, got: %v", err)
	}
}

func TestBuildFallbackUsageEventKeyStable(t *testing.T) {
	raw := json.RawMessage(`{"failed":true,"provider":"test"}`)
	key1 := buildFallbackUsageEventKey(raw)
	key2 := buildFallbackUsageEventKey(raw)
	if key1 != key2 {
		t.Fatalf("expected stable fallback key, got %q and %q", key1, key2)
	}
	if !strings.HasPrefix(key1, "failed:") {
		t.Fatalf("expected 'failed:' prefix, got %q", key1)
	}
}
