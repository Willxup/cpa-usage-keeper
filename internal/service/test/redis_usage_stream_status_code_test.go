package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestDecodeRedisUsageMessagePreservesCPAStreamAndStatusCode(t *testing.T) {
	stream := true
	event, _, err := service.DecodeRedisUsageMessage(`{
		"request_id":"stream-status",
		"failed":false,
		"stream":true,
		"fail":{"status_code":200,"body":""},
		"tokens":{}
	}`, time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.Stream == nil || *event.Stream != stream {
		t.Fatalf("stream=%v, want %v", event.Stream, stream)
	}
	if event.StatusCode == nil || *event.StatusCode != 200 {
		t.Fatalf("status_code=%v, want 200", event.StatusCode)
	}
}

func TestDecodeRedisUsageMessagePreservesFailedCPAStatusCode(t *testing.T) {
	event, _, err := service.DecodeRedisUsageMessage(`{
		"request_id":"failed-status",
		"failed":true,
		"stream":false,
		"fail":{"status_code":429,"body":"upstream failure"},
		"tokens":{}
	}`, time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.Stream == nil || *event.Stream {
		t.Fatalf("stream=%v, want false", event.Stream)
	}
	if event.StatusCode == nil || *event.StatusCode != 429 {
		t.Fatalf("status_code=%v, want 429", event.StatusCode)
	}
}

func TestDecodeRedisUsageMessageLeavesNewFieldsUnknownForLegacyPayload(t *testing.T) {
	event, _, err := service.DecodeRedisUsageMessage(`{"request_id":"legacy-status","tokens":{}}`, time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
	}
	if event.Stream != nil || event.StatusCode != nil {
		t.Fatalf("legacy fields should remain unknown, stream=%v status_code=%v", event.Stream, event.StatusCode)
	}
}
