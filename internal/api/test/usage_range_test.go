package test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
)

func TestUsageRoutesAcceptBoundedRollingRanges(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rangeVal string
		duration time.Duration
	}{
		{name: "minimum hours", rangeVal: "5h", duration: 5 * time.Hour},
		{name: "arbitrary hours", rangeVal: "13h", duration: 13 * time.Hour},
		{name: "maximum hours", rangeVal: "24h", duration: 24 * time.Hour},
		{name: "one day", rangeVal: "1d", duration: 24 * time.Hour},
		{name: "arbitrary days", rangeVal: "17d", duration: 17 * 24 * time.Hour},
		{name: "maximum days", rangeVal: "30d", duration: 30 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			resp := serveAPIGet(router, "/api/v1/usage/events?range="+tc.rangeVal)

			if resp.Code != http.StatusOK {
				t.Fatalf("expected rolling range %q to return 200, got %d body=%s", tc.rangeVal, resp.Code, resp.Body.String())
			}
			if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
				t.Fatalf("expected concrete rolling bounds, got %+v", provider.lastFilter)
			}
			if got := provider.lastFilter.EndTime.Sub(*provider.lastFilter.StartTime); got != tc.duration {
				t.Fatalf("expected %q duration %s, got %s", tc.rangeVal, tc.duration, got)
			}
		})
	}
}

func TestUsageRoutesRejectOutOfBoundsRollingRanges(t *testing.T) {
	for _, rangeVal := range []string{"0h", "1h", "4h", "25h", "0d", "31d", "01h", "1w"} {
		t.Run(rangeVal, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			resp := serveAPIGet(router, "/api/v1/usage/events?range="+rangeVal)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected rolling range %q to return 400, got %d body=%s", rangeVal, resp.Code, resp.Body.String())
			}
			if provider.filterCalls != 0 {
				t.Fatalf("expected invalid range not to reach provider, got %d calls", provider.filterCalls)
			}
		})
	}
}

func TestUsageRoutesAcceptCustomHourAndDaySlots(t *testing.T) {
	now := time.Now().In(time.Local)
	currentHour := now.Truncate(time.Hour)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		unit, format             string
		start, end, exclusiveEnd time.Time
	}{
		{"hour", time.RFC3339, currentHour.Add(-4 * time.Hour), currentHour, currentHour.Add(time.Hour)},
		{"day", time.DateOnly, today.AddDate(0, 0, -29), today, today.AddDate(0, 0, 1)},
	} {
		t.Run(tc.unit, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			query := url.Values{"range": {"custom"}, "unit": {tc.unit}, "start": {tc.start.Format(tc.format)}, "end": {tc.end.Format(tc.format)}}
			resp := serveAPIGet(router, "/api/v1/usage/events?"+query.Encode())
			if resp.Code != http.StatusOK {
				t.Fatalf("custom %s status=%d body=%s", tc.unit, resp.Code, resp.Body.String())
			}
			filter := provider.lastFilter
			if filter.StartTime == nil || !filter.StartTime.Equal(tc.start) || filter.EndTime == nil || !filter.EndTime.Equal(tc.exclusiveEnd) || !filter.EndExclusive || filter.CustomUnit != tc.unit {
				t.Fatalf("custom %s filter=%+v, want %s..%s exclusive", tc.unit, filter, tc.start, tc.exclusiveEnd)
			}
		})
	}
}

func TestUsageRoutesRejectCustomRangesOutsideProductBounds(t *testing.T) {
	now := time.Now().In(time.Local)
	currentHour := now.Truncate(time.Hour)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	testCases := []struct {
		name       string
		unit       string
		start      string
		end        string
		wantStatus int
	}{
		{name: "four hour slots", unit: "hour", start: currentHour.Add(-3 * time.Hour).Format(time.RFC3339), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
		{name: "hour before horizon", unit: "hour", start: currentHour.Add(-24 * time.Hour).Format(time.RFC3339), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
		{name: "future hour", unit: "hour", start: currentHour.Add(-3 * time.Hour).Format(time.RFC3339), end: currentHour.Add(time.Hour).Format(time.RFC3339), wantStatus: http.StatusConflict},
		{name: "unaligned hour", unit: "hour", start: currentHour.Add(-4*time.Hour + time.Minute).Format(time.RFC3339), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
		{name: "366 day slots", unit: "day", start: today.AddDate(0, 0, -365).Format(time.DateOnly), end: today.Format(time.DateOnly), wantStatus: http.StatusBadRequest},
		{name: "future day", unit: "day", start: today.Format(time.DateOnly), end: today.AddDate(0, 0, 1).Format(time.DateOnly), wantStatus: http.StatusConflict},
		{name: "mixed day and hour", unit: "day", start: today.Format(time.DateOnly), end: currentHour.Format(time.RFC3339), wantStatus: http.StatusBadRequest},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			query := url.Values{"range": {"custom"}, "unit": {tc.unit}, "start": {tc.start}, "end": {tc.end}}
			resp := serveAPIGet(router, "/api/v1/usage/events?"+query.Encode())

			if resp.Code != tc.wantStatus {
				t.Fatalf("expected invalid custom range to return %d, got %d body=%s", tc.wantStatus, resp.Code, resp.Body.String())
			}
			if provider.filterCalls != 0 {
				t.Fatalf("expected invalid custom range not to reach provider, got %d calls", provider.filterCalls)
			}
		})
	}
}

func TestKeyOverviewAcceptsRollingAndCustomRanges(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	custom := url.Values{"range": {"custom"}, "unit": {"day"}, "start": {today.AddDate(0, 0, -6).Format(time.DateOnly)}, "end": {today.Format(time.DateOnly)}}
	for _, tc := range []struct{ query, unit string }{
		{"range=13d", ""},
		{custom.Encode(), "day"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			provider := &usageEventsStub{}
			router, cookie := newUsageViewerRouter(t, provider)
			resp := serveAPIGet(router, "/api/v1/key-overview?"+tc.query, cookie)
			if resp.Code != http.StatusOK {
				t.Fatalf("key overview status=%d body=%s", resp.Code, resp.Body.String())
			}
			if provider.lastFilter.APIKeyID != "42" || provider.lastFilter.CustomUnit != tc.unit || (tc.unit != "" && !provider.lastFilter.EndExclusive) {
				t.Fatalf("expected key overview to preserve range and force viewer key, got %+v", provider.lastFilter)
			}
		})
	}
}
