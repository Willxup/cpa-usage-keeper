package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestRedisUsageFetcherMapsPayloadToUsageEvent(t *testing.T) {
	fetchedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	fetcher := redisUsageFetcher{queue: staticRedisQueue{messages: []string{`{
		"timestamp":"2026-04-27T07:59:00Z",
		"latency_ms":1234,
		"source":"sk-test",
		"auth_index":"auth-1",
		"tokens":{"input_tokens":10,"output_tokens":20,"reasoning_tokens":3,"cached_tokens":4,"total_tokens":0},
		"failed":true,
		"provider":"claude",
		"model":"claude-sonnet-4-6",
		"endpoint":"/v1/messages",
		"auth_type":"api_key",
		"api_key":"raw-key",
		"request_id":"req-123",
		"unknown":"ignored"
	}`}}}

	result, err := fetcher.FetchUsage(context.Background(), fetchedAt)
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %d", len(result.Events))
	}
	event := result.Events[0]
	if event.EventKey != "req-123" || event.APIGroupKey != "raw-key" || event.Model != "claude-sonnet-4-6" || event.Source != "sk-test" || event.AuthIndex != "auth-1" || !event.Failed || event.LatencyMS != 1234 {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.InputTokens != 10 || event.OutputTokens != 20 || event.ReasoningTokens != 3 || event.CachedTokens != 4 || event.TotalTokens != 33 {
		t.Fatalf("unexpected tokens: %+v", event)
	}
	if !event.Timestamp.Equal(time.Date(2026, 4, 27, 7, 59, 0, 0, time.UTC)) {
		t.Fatalf("unexpected timestamp: %s", event.Timestamp)
	}
	var rawBatch []map[string]any
	if err := json.Unmarshal(result.RawPayload, &rawBatch); err != nil {
		t.Fatalf("raw payload is not a JSON array: %v", err)
	}
	if len(rawBatch) != 1 || rawBatch[0]["request_id"] != "req-123" || rawBatch[0]["unknown"] != "ignored" {
		t.Fatalf("unexpected raw payload: %s", string(result.RawPayload))
	}
}

func TestRedisUsageFetcherFallsBackFieldsAndEventKey(t *testing.T) {
	fetchedAt := time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC)
	fetcher := redisUsageFetcher{queue: staticRedisQueue{messages: []string{`{"latency_ms":-5,"tokens":{"input_tokens":1,"output_tokens":2},"endpoint":"/fallback"}`}}}

	result, err := fetcher.FetchUsage(context.Background(), fetchedAt)
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	event := result.Events[0]
	if event.APIGroupKey != "/fallback" || event.Model != "unknown" || event.LatencyMS != 0 {
		t.Fatalf("unexpected fallback event: %+v", event)
	}
	if !event.Timestamp.Equal(fetchedAt) {
		t.Fatalf("expected fetchedAt timestamp, got %s", event.Timestamp)
	}
	expectedKey := BuildEventKey("/fallback", "unknown", fetchedAt, "", "", false, cpa.TokenStats{InputTokens: 1, OutputTokens: 2})
	if event.EventKey != expectedKey {
		t.Fatalf("expected fallback event key %s, got %s", expectedKey, event.EventKey)
	}
}

func TestRedisUsageFetcherFallsBackToProviderWhenAPIKeyIsBlank(t *testing.T) {
	fetcher := redisUsageFetcher{queue: staticRedisQueue{messages: []string{`{"api_key":"   ","provider":"claude","endpoint":"/v1/messages","request_id":"req-blank-key"}`}}}

	result, err := fetcher.FetchUsage(context.Background(), time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	event := result.Events[0]
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

func TestRedisUsageFetcherReportsMalformedJSONIndex(t *testing.T) {
	fetcher := redisUsageFetcher{queue: staticRedisQueue{messages: []string{`{"request_id":"ok"}`, `{bad-json}`}}}

	_, err := fetcher.FetchUsage(context.Background(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "decode redis usage message 1") {
		t.Fatalf("expected message index decode error, got %v", err)
	}
}

func TestRedisUsageFetcherHandlesEmptyBatch(t *testing.T) {
	fetcher := redisUsageFetcher{queue: staticRedisQueue{messages: nil}}

	result, err := fetcher.FetchUsage(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("expected no events, got %d", len(result.Events))
	}
	if string(result.RawPayload) != "[]" {
		t.Fatalf("expected empty raw payload array, got %s", string(result.RawPayload))
	}
}

type staticRedisQueue struct {
	messages []string
	err      error
}

func (q staticRedisQueue) PopUsage(context.Context) ([]string, error) {
	return q.messages, q.err
}
