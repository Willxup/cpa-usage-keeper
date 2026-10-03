package quota

import (
	"net/http"
	"testing"
	"time"
)

func TestCoalescedWindowsKeepOwnCaptureAndRejectOlder(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	build := func(window, value string, when time.Time, source RefreshSource) *UsageHeaderSnapshot {
		h := http.Header{}
		h.Set("X-Codex-"+window+"-Used-Percent", value)
		h.Set("X-Codex-"+window+"-Window-Minutes", "300")
		h.Set("X-Codex-"+window+"-Reset-After-Seconds", "60")
		snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: "a", Provider: "codex", ObservedAt: when, Source: source, Headers: h})
		if !ok {
			t.Fatal("missing snapshot")
		}
		return snapshot
	}
	a := build("Primary", "10", at, "api_response_headers")
	b := build("Secondary", "20", at.Add(time.Minute), "websocket_event")
	merged := mergePendingUsageHeaderCacheSnapshot(a, b)
	merged = mergePendingUsageHeaderCacheSnapshot(merged, build("Primary", "99", at.Add(-time.Minute), "manual_provider_query"))
	if len(merged.cacheRows) != 2 {
		t.Fatal("independent window lost")
	}
	for _, row := range merged.cacheRows {
		c := merged.rowCapture[row.Key]
		if row.UsedPercent != nil && *row.UsedPercent == 10 {
			if !c.At.Equal(at) || c.Source != "api_response_headers" {
				t.Fatal("capture changed")
			}
		} else if row.UsedPercent == nil || *row.UsedPercent != 20 || !c.At.Equal(at.Add(time.Minute)) {
			t.Fatal("late capture or wrong window")
		}
	}
}
