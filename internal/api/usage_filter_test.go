package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseUsageFilterQueryPresetRange(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rangeVal string
		duration time.Duration
	}{
		{name: "24h", rangeVal: "24h", duration: 24 * time.Hour},
		{name: "30d", rangeVal: "30d", duration: 30 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/usage/overview?range="+tc.rangeVal, nil)
			anchor := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

			filter, err := parseUsageFilterQuery(req, anchor)
			if err != nil {
				t.Fatalf("parseUsageFilterQuery returned error: %v", err)
			}
			if filter.Range != tc.rangeVal {
				t.Fatalf("expected range to be preserved, got %+v", filter)
			}
			if filter.StartTime == nil || filter.EndTime == nil {
				t.Fatalf("expected preset range to resolve concrete times, got %+v", filter)
			}
			if !filter.EndTime.Equal(anchor) {
				t.Fatalf("expected preset range end to use anchor time, got %+v", filter)
			}
			if !filter.StartTime.Equal(anchor.Add(-tc.duration)) {
				t.Fatalf("expected preset range start to subtract %s, got %+v", tc.duration, filter)
			}
		})
	}
}

func TestParseUsageFilterQueryIgnoresRealtimeWindow(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=24h&realtime_window=45m", nil)
	anchor := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	filter, err := parseUsageFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error for ignored realtime_window: %v", err)
	}
	if filter.RealtimeWindow != "" || filter.RealtimeEndTime != nil {
		t.Fatalf("expected main overview filter not to carry realtime fields, got %+v", filter)
	}
}

func TestParseUsageFilterQueryTodayRangeUsesLocalDayBoundary(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=today", nil)
	anchor := time.Date(2026, 4, 22, 12, 34, 56, 0, time.UTC)

	filter, err := parseUsageFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Range != "today" {
		t.Fatalf("expected today range to be preserved, got %+v", filter)
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		t.Fatalf("expected today range to resolve concrete times, got %+v", filter)
	}
	expectedStart := time.Date(2026, 4, 22, 0, 0, 0, 0, location)
	expectedEnd := time.Date(2026, 4, 23, 0, 0, 0, 0, location).Add(-time.Nanosecond)
	if !filter.StartTime.Equal(expectedStart) {
		t.Fatalf("expected today start %s, got %s", expectedStart, *filter.StartTime)
	}
	if filter.StartTime.Location().String() != location.String() {
		t.Fatalf("expected today start to keep project timezone, got %s", filter.StartTime.Location())
	}
	if !filter.EndTime.Equal(expectedEnd) {
		t.Fatalf("expected today end %s, got %s", expectedEnd, *filter.EndTime)
	}
	if filter.EndTime.Location().String() != location.String() {
		t.Fatalf("expected today end to keep project timezone, got %s", filter.EndTime.Location())
	}
}

func TestParseUsageFilterQueryYesterdayRangeUsesPreviousLocalDayBoundary(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=yesterday", nil)
	anchor := time.Date(2026, 4, 22, 12, 34, 56, 0, time.UTC)

	filter, err := parseUsageFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Range != "yesterday" {
		t.Fatalf("expected yesterday range to be preserved, got %+v", filter)
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		t.Fatalf("expected yesterday range to resolve concrete times, got %+v", filter)
	}
	expectedStart := time.Date(2026, 4, 21, 0, 0, 0, 0, location)
	expectedEnd := time.Date(2026, 4, 22, 0, 0, 0, 0, location).Add(-time.Nanosecond)
	if !filter.StartTime.Equal(expectedStart) {
		t.Fatalf("expected yesterday start %s, got %s", expectedStart, *filter.StartTime)
	}
	if filter.StartTime.Location().String() != location.String() {
		t.Fatalf("expected yesterday start to keep project timezone, got %s", filter.StartTime.Location())
	}
	if !filter.EndTime.Equal(expectedEnd) {
		t.Fatalf("expected yesterday end %s, got %s", expectedEnd, *filter.EndTime)
	}
	if filter.EndTime.Location().String() != location.String() {
		t.Fatalf("expected yesterday end to keep project timezone, got %s", filter.EndTime.Location())
	}
}

func TestParseUsageFilterQueryTodayRangeUsesLocalDSTBoundary(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=today", nil)
	anchor := time.Date(2026, 3, 8, 12, 0, 0, 0, location)

	filter, err := parseUsageFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		t.Fatalf("expected today range to resolve concrete times, got %+v", filter)
	}
	expectedStart := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
	expectedEnd := time.Date(2026, 3, 9, 0, 0, 0, 0, location).Add(-time.Nanosecond)
	if !filter.StartTime.Equal(expectedStart) {
		t.Fatalf("expected DST today start %s, got %s", expectedStart, *filter.StartTime)
	}
	if filter.StartTime.Location().String() != location.String() {
		t.Fatalf("expected DST today start to keep project timezone, got %s", filter.StartTime.Location())
	}
	if !filter.EndTime.Equal(expectedEnd) {
		t.Fatalf("expected DST today end %s, got %s", expectedEnd, *filter.EndTime)
	}
	if filter.EndTime.Location().String() != location.String() {
		t.Fatalf("expected DST today end to keep project timezone, got %s", filter.EndTime.Location())
	}
}

func TestParseUsageFilterQueryCustomRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=custom&start=2026-04-20T00:00:00Z&end=2026-04-21T23:59:59Z", nil)

	filter, err := parseUsageFilterQuery(req, time.Time{})
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		t.Fatalf("expected custom range bounds, got %+v", filter)
	}
	if !filter.StartTime.Equal(time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected custom start: %+v", filter)
	}
	if !filter.EndTime.Equal(time.Date(2026, 4, 21, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("unexpected custom end: %+v", filter)
	}
}

func TestParseUsageFilterQueryCustomDateRangeUsesLocalDayBoundary(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=custom&start=2026-04-20&end=2026-04-21", nil)

	filter, err := parseUsageFilterQuery(req, time.Time{})
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		t.Fatalf("expected custom date range bounds, got %+v", filter)
	}
	expectedStart := time.Date(2026, 4, 20, 0, 0, 0, 0, location)
	expectedEnd := time.Date(2026, 4, 22, 0, 0, 0, 0, location).Add(-time.Nanosecond)
	if !filter.StartTime.Equal(expectedStart) {
		t.Fatalf("expected custom date start %s, got %s", expectedStart, *filter.StartTime)
	}
	if filter.StartTime.Location().String() != location.String() {
		t.Fatalf("expected custom date start to keep project timezone, got %s", filter.StartTime.Location())
	}
	if !filter.EndTime.Equal(expectedEnd) {
		t.Fatalf("expected custom date end %s, got %s", expectedEnd, *filter.EndTime)
	}
	if filter.EndTime.Location().String() != location.String() {
		t.Fatalf("expected custom date end to keep project timezone, got %s", filter.EndTime.Location())
	}
}

func TestParseUsageFilterQueryRejectsInvalidCustomRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=custom&start=2026-04-21T00:00:00Z&end=2026-04-20T23:59:59Z", nil)

	_, err := parseUsageFilterQuery(req, time.Time{})
	if err == nil {
		t.Fatal("expected invalid custom range error")
	}
}

func TestParseUsageFilterQueryRejectsCustomRangeBeforeRetentionStart(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location
	anchor := time.Date(2026, 6, 16, 9, 0, 0, 0, location)

	for _, tc := range []struct {
		name      string
		path      string
		wantError bool
	}{
		{name: "before retention", path: "/api/v1/usage/overview?range=custom&start=2026-04-30&end=2026-05-01", wantError: true},
		{name: "at retention boundary", path: "/api/v1/usage/overview?range=custom&start=2026-05-01&end=2026-05-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			filter, err := parseUsageFilterQuery(req, anchor)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected custom range before retention start to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected retention boundary custom range to be accepted: %v", err)
			}
			expectedStart := time.Date(2026, 5, 1, 0, 0, 0, 0, location)
			if filter.StartTime == nil || !filter.StartTime.Equal(expectedStart) {
				t.Fatalf("expected boundary start %s, got %+v", expectedStart, filter)
			}
		})
	}
}

func TestParseUsageFilterQueryRejectsMissingRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events", nil)

	_, err := parseUsageFilterQuery(req, time.Time{})
	if err == nil {
		t.Fatal("expected missing range error")
	}
}

func TestParseUsageFilterQueryDefaultsEventsPagination(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h", nil)

	filter, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Page != 1 || filter.PageSize != 100 || filter.Offset != 0 {
		t.Fatalf("expected default pagination, got %+v", filter)
	}
}

func TestParseUsageFilterQueryAcceptsEventsPaginationAndFilters(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h&page=3&page_size=100&model=%20claude-sonnet%20&source=%20source-a%20&auth_index=%202%20", nil)

	filter, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Page != 3 || filter.PageSize != 100 || filter.Offset != 200 {
		t.Fatalf("expected page 3/page size 100 offset 200, got %+v", filter)
	}
	if filter.Model != "claude-sonnet" || filter.Source != "source-a" || filter.AuthIndex != "2" {
		t.Fatalf("expected trimmed server-side filters, got %+v", filter)
	}
}

func TestParseUsageFilterQueryAcceptsAPIKeyID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h&api_key_id=%201234567890123456789%20", nil)
	anchor := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	filter, err := parseUsageFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.APIKeyID != "1234567890123456789" {
		t.Fatalf("expected api key id to be preserved as string, got %+v", filter)
	}

	timeFilter, err := parseUsageTimeFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("parseUsageTimeFilterQuery returned error: %v", err)
	}
	if timeFilter.APIKeyID != "1234567890123456789" {
		t.Fatalf("expected time filter to preserve api key id, got %+v", timeFilter)
	}
}

func TestParseUsageFilterQueryRejectsInvalidAPIKeyID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h&api_key_id=not-an-id", nil)

	_, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected invalid api_key_id error")
	}
}

func TestParseUsageRealtimeFilterQueryRejectsInvalidAPIKeyID(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview/realtime?window=60m&api_key_id=not-an-id", nil)

	_, err := parseUsageRealtimeFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected invalid realtime api_key_id error")
	}
}

func TestParseUsageFilterQueryUsesLimitAsPageSizeAlias(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h&limit=20", nil)

	filter, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Page != 1 || filter.PageSize != 20 || filter.Offset != 0 {
		t.Fatalf("expected limit alias to set page size, got %+v", filter)
	}
}

func TestParseUsageFilterQueryPrefersPageSizeOverLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h&page_size=50&limit=20", nil)

	filter, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.PageSize != 50 {
		t.Fatalf("expected page_size to win over limit, got %+v", filter)
	}
}

func TestParseUsageFilterQueryRejectsInvalidEventsPagination(t *testing.T) {
	tests := []string{
		"/api/v1/usage/events?range=24h&page=0",
		"/api/v1/usage/events?range=24h&page_size=25",
	}
	for _, path := range tests {
		req := httptest.NewRequest("GET", path, nil)
		if _, err := parseUsageFilterQuery(req, time.Time{}); err == nil {
			t.Fatalf("expected pagination error for %s", path)
		}
	}
}
