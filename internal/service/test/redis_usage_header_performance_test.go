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
		name, authType, authIndex string
		wantSnapshot              bool
	}{
		{"oauth", " OAuth ", "auth-1", true},
		{"api_key", "api_key", "auth-1", false},
		{"missing_identity", "oauth", "  ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"request_id":"header-eligibility","auth_type":%q,"auth_index":%q,"provider":"unknown","response_headers":{"X-Codex-Primary-Used-Percent":["5"],"X-Codex-Primary-Window-Minutes":["300"],"X-Codex-Primary-Reset-After-Seconds":["60"]}}`, tc.authType, tc.authIndex)
			// provider 提示不能代替身份来源判定；有效 OAuth Header 仍需进入快照。
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
