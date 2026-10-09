package test

import (
	_ "cpa-usage-keeper/internal/api"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	_ "unsafe"
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

func TestParseUsageFilterQueryUsesLocalCalendarBounds(t *testing.T) {
	for _, tc := range []struct {
		name, zone, query, rangeVal, startDate, endDate, unit string
		anchor                                                time.Time
		exclusive                                             bool
	}{
		{name: "today", zone: "Asia/Shanghai", query: "range=today", rangeVal: "today", startDate: "2026-04-22", endDate: "2026-04-23", anchor: time.Date(2026, 4, 22, 12, 34, 56, 0, time.UTC)},
		{name: "yesterday", zone: "Asia/Shanghai", query: "range=yesterday", rangeVal: "yesterday", startDate: "2026-04-21", endDate: "2026-04-22", anchor: time.Date(2026, 4, 22, 12, 34, 56, 0, time.UTC)},
		{name: "DST today", zone: "America/New_York", query: "range=today", rangeVal: "today", startDate: "2026-03-08", endDate: "2026-03-09", anchor: time.Date(2026, 3, 8, 16, 0, 0, 0, time.UTC)},
		{name: "inferred custom day", zone: "Asia/Shanghai", query: "range=custom&start=2026-04-20&end=2026-04-21", rangeVal: "custom", startDate: "2026-04-20", endDate: "2026-04-22", unit: "day", anchor: time.Date(2026, 4, 21, 4, 0, 0, 0, time.UTC), exclusive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatalf("load location: %v", err)
			}
			previous := time.Local
			time.Local = location
			t.Cleanup(func() { time.Local = previous })
			start, err := time.ParseInLocation(time.DateOnly, tc.startDate, location)
			if err != nil {
				t.Fatalf("parse expected start: %v", err)
			}
			end, err := time.ParseInLocation(time.DateOnly, tc.endDate, location)
			if err != nil {
				t.Fatalf("parse expected end: %v", err)
			}
			if !tc.exclusive {
				end = end.Add(-time.Nanosecond)
			}
			filter, err := parseUsageFilterQuery(httptest.NewRequest("GET", "/api/v1/usage/overview?"+tc.query, nil), tc.anchor)
			if err != nil {
				t.Fatalf("parseUsageFilterQuery: %v", err)
			}
			if filter.StartTime == nil || filter.EndTime == nil || !filter.StartTime.Equal(start) || !filter.EndTime.Equal(end) {
				t.Fatalf("expected %s..%s, got %+v", start, end, filter)
			}
			if filter.StartTime.Location().String() != tc.zone || filter.EndTime.Location().String() != tc.zone {
				t.Fatalf("expected project timezone %s, got %+v", tc.zone, filter)
			}
			if filter.Range != tc.rangeVal || filter.CustomUnit != tc.unit || filter.EndExclusive != tc.exclusive {
				t.Fatalf("unexpected calendar range metadata: %+v", filter)
			}
		})
	}
}

func TestParseUsageFilterQueryCustomRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=custom&unit=hour&start=2026-04-20T00:00:00Z&end=2026-04-20T04:00:00Z", nil)

	filter, err := parseUsageFilterQuery(req, time.Date(2026, 4, 20, 4, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		t.Fatalf("expected custom range bounds, got %+v", filter)
	}
	if !filter.StartTime.Equal(time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected custom start: %+v", filter)
	}
	if !filter.EndTime.Equal(time.Date(2026, 4, 20, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected custom end: %+v", filter)
	}
	if filter.CustomUnit != "hour" || !filter.EndExclusive {
		t.Fatalf("expected exclusive custom hour range, got %+v", filter)
	}
}

func TestParseUsageFilterQueryRejectsInvalidCustomRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=custom&start=2026-04-21T00:00:00Z&end=2026-04-20T23:59:59Z", nil)

	_, err := parseUsageFilterQuery(req, time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected invalid custom range error")
	}
}

func TestParseUsageFilterQueryRejectsCustomDayRangeBeyondNinetyDays(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location
	anchor := time.Date(2026, 6, 16, 9, 0, 0, 0, location)

	today := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, location)
	start := today.AddDate(0, 0, -90)
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=custom&unit=day&start="+start.Format(time.DateOnly)+"&end="+today.Format(time.DateOnly), nil)

	_, err = parseUsageFilterQuery(req, anchor)
	if err == nil {
		t.Fatal("expected 91-day custom Events range to be rejected")
	}
}

func TestParseUsageFilterQueryRejectsMissingRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events", nil)

	_, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected missing range error")
	}
}

func TestParseUsageFilterQueryAcceptsLatestIdentityCursorWithoutRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?cursor_mode=true&page_size=50&source=shared-auth&auth_type=1", nil)

	filter, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if !filter.CursorMode || filter.PageSize != 50 || filter.Source != "shared-auth" || filter.AuthType != "oauth" {
		t.Fatalf("expected latest auth-file cursor filter, got %+v", filter)
	}
	if filter.StartTime != nil || filter.EndTime != nil || filter.Range != "" {
		t.Fatalf("expected latest cursor query without fixed time range, got %+v", filter)
	}
}

func TestParseUsageFilterQueryRejectsInvalidIdentityAuthType(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?cursor_mode=true&source=shared-auth&auth_type=3", nil)

	if _, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected invalid auth_type error")
	}
}

func TestParseUsageFilterQueryResolvesPaginationAndFilters(t *testing.T) {
	for _, tc := range []struct {
		query                    string
		page, size, offset       int
		model, source, authIndex string
	}{
		{query: "", page: 1, size: 100},
		{query: "&limit=20", page: 1, size: 20},
		{query: "&page_size=50&limit=20", page: 1, size: 50},
		{query: "&page=3&page_size=100&model=%20claude-sonnet%20&source=%20source-a%20&auth_index=%202%20", page: 3, size: 100, offset: 200, model: "claude-sonnet", source: "source-a", authIndex: "2"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h"+tc.query, nil)
			filter, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("parseUsageFilterQuery: %v", err)
			}
			if filter.Page != tc.page || filter.PageSize != tc.size || filter.Offset != tc.offset || filter.Model != tc.model || filter.Source != tc.source || filter.AuthIndex != tc.authIndex {
				t.Fatalf("unexpected pagination/filters: %+v, want %+v", filter, tc)
			}
		})
	}
}

func TestParseUsageFilterQueryRejectsInvalidEventsCursor(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=24h&cursor=not-a-cursor", nil)
	if _, err := parseUsageFilterQuery(req, time.Time{}); err == nil {
		t.Fatal("expected invalid cursor error")
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

func TestParseUsageTimeFilterQueryIgnoresEventOnlyParameters(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=2d&page=0&page_size=25&model=x&source=y&auth_index=z&result=bogus&api_key_id=42", nil)
	anchor := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	filter, err := parseUsageTimeFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("time-only parser should ignore Events parameters: %v", err)
	}
	if filter.Range != "2d" || filter.RangeUnit != "day" || filter.RangeCount != 2 {
		t.Fatalf("unexpected normalized time identity: %+v", filter)
	}
	if filter.APIKeyID != "42" {
		t.Fatalf("expected Admin API key scope to remain available: %+v", filter)
	}
	if filter.Page != 0 || filter.PageSize != 0 || filter.Model != "" || filter.Source != "" || filter.AuthIndex != "" || filter.Result != "" {
		t.Fatalf("time-only parser leaked Events fields: %+v", filter)
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

func TestParseUsageFilterQueryRejectsInvalidEventsPagination(t *testing.T) {
	tests := []string{
		"/api/v1/usage/events?range=24h&page=0",
		"/api/v1/usage/events?range=24h&page_size=25",
	}
	for _, path := range tests {
		req := httptest.NewRequest("GET", path, nil)
		if _, err := parseUsageFilterQuery(req, time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)); err == nil {
			t.Fatalf("expected pagination error for %s", path)
		}
	}
}

// 保留固定时间锚点的解析契约；链接仅用于测试，不增加生产导出接口。

//go:linkname parseUsageFilterQuery cpa-usage-keeper/internal/api.parseUsageFilterQuery
func parseUsageFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error)

//go:linkname parseUsageRealtimeFilterQuery cpa-usage-keeper/internal/api.parseUsageRealtimeFilterQuery
func parseUsageRealtimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error)

//go:linkname parseUsageTimeFilterQuery cpa-usage-keeper/internal/api.parseUsageTimeFilterQuery
func parseUsageTimeFilterQuery(req *http.Request, anchor time.Time) (servicedto.UsageFilter, error)
