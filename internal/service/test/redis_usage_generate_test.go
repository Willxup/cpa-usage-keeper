package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestDecodeRedisUsageMessageNormalizesGenerate(t *testing.T) {
	testCases := []struct {
		name         string
		message      string
		wantGenerate bool
	}{
		{
			name:         "explicit false wins over token values",
			message:      `{"request_id":"explicit-false","generate":false,"tokens":{"input_tokens":1,"total_tokens":1}}`,
			wantGenerate: false,
		},
		{
			name:         "explicit true wins over legacy signature",
			message:      `{"request_id":"explicit-true","generate":true,"failed":false,"executor_type":"CodexWebsocketsExecutor","tokens":{}}`,
			wantGenerate: true,
		},
		{
			name:         "missing field recognizes legacy prewarm",
			message:      `{"request_id":"legacy-prewarm","failed":false,"executor_type":"CodexWebsocketsExecutor","tokens":{}}`,
			wantGenerate: false,
		},
		{
			name:         "missing field keeps websocket event with token detail",
			message:      `{"request_id":"legacy-cache-write","failed":false,"executor_type":"CodexWebsocketsExecutor","tokens":{"cache_creation_tokens":1}}`,
			wantGenerate: true,
		},
		{
			name:         "missing field keeps non-websocket zero token event",
			message:      `{"request_id":"legacy-http","failed":false,"executor_type":"CodexExecutor","tokens":{}}`,
			wantGenerate: true,
		},
		{
			name:         "missing field keeps failed websocket event",
			message:      `{"request_id":"legacy-failed","failed":true,"executor_type":"CodexWebsocketsExecutor","tokens":{}}`,
			wantGenerate: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event, _, err := service.DecodeRedisUsageMessage(testCase.message, time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("DecodeRedisUsageMessage returned error: %v", err)
			}
			if event.Generate == nil {
				t.Fatal("expected generate to be normalized to a non-nil value")
			}
			if *event.Generate != testCase.wantGenerate {
				t.Fatalf("generate=%v, want %v", *event.Generate, testCase.wantGenerate)
			}
		})
	}
}

func TestDecodeRedisUsageMessageReusesExplicitGenerateAllocation(t *testing.T) {
	fetchedAt := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	explicitMessage := `{"request_id":"allocation-check","generate":true,"executor_type":"CodexExecutor","tokens":{"input_tokens":1,"total_tokens":1}}`
	missingMessage := `{"request_id":"allocation-check","executor_type":"CodexExecutor","tokens":{"input_tokens":1,"total_tokens":1}}`

	decodeAllocs := func(message string) (float64, error) {
		var decodeErr error
		allocs := testing.AllocsPerRun(1000, func() {
			_, _, decodeErr = service.DecodeRedisUsageMessage(message, fetchedAt)
		})
		return allocs, decodeErr
	}

	explicitAllocs, err := decodeAllocs(explicitMessage)
	if err != nil {
		t.Fatalf("decode explicit generate message: %v", err)
	}
	missingAllocs, err := decodeAllocs(missingMessage)
	if err != nil {
		t.Fatalf("decode missing generate message: %v", err)
	}
	if explicitAllocs > missingAllocs {
		t.Fatalf("explicit generate allocations=%v, missing generate allocations=%v; explicit value should reuse its decoded pointer", explicitAllocs, missingAllocs)
	}
}
