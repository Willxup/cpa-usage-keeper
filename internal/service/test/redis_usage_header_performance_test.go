package test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestUsageHeaderSnapshotRequiresOAuthIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, authType, authIndex, provider string
		wantSnapshot                        bool
	}{
		{"oauth_codex", " OAuth ", "auth-1", " CoDeX ", true},
		{"api_key", "api_key", "auth-1", "codex", false},
		{"missing_identity", "oauth", "  ", "codex", false},
		{"unknown_provider", "oauth", "auth-1", "unknown", false},
		{"missing_provider", "oauth", "auth-1", "", false},
		{"claude_provider_codex_header", "oauth", "auth-1", "claude", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"request_id":"header-eligibility","auth_type":%q,"auth_index":%q,"provider":%q,"executor_type":"CodexExecutor","response_headers":{"X-Codex-Primary-Used-Percent":["5"],"X-Codex-Primary-Window-Minutes":["300"],"X-Codex-Primary-Reset-After-Seconds":["60"]}}`, tc.authType, tc.authIndex, tc.provider)
			// usage 内容照常解码；额度快照需要 OAuth、auth_index 和精确 provider。
			event, raw, snapshot, err := service.DecodeRedisUsageMessageWithHeaders(payload, time.Now())
			if err != nil || event.RequestID != "header-eligibility" || string(raw) != payload {
				t.Fatalf("usage event changed: request=%q err=%v", event.RequestID, err)
			}
			if (snapshot != nil) != tc.wantSnapshot {
				t.Fatalf("snapshot presence=%v, want %v", snapshot != nil, tc.wantSnapshot)
			}
		})
	}
}

func BenchmarkDecodeAPIKeyUsageWithResponseHeaders(b *testing.B) {
	headers := make(map[string][]string, 24)
	for i := range 24 {
		headers[fmt.Sprintf("X-Synthetic-%02d", i)] = []string{strings.Repeat("v", 32)}
	}
	rawHeaders, err := json.Marshal(headers)
	if err != nil {
		b.Fatal(err)
	}
	payload := `{"request_id":"benchmark","auth_type":"apikey","auth_index":"key-1","response_headers":` + string(rawHeaders) + `}`
	fetchedAt := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, snapshot, err := service.DecodeRedisUsageMessageWithHeaders(payload, fetchedAt); err != nil || snapshot != nil {
			b.Fatalf("unexpected snapshot or error: %v", err)
		}
	}
}
