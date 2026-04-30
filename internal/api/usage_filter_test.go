package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseUsageFilterQueryPresetRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=24h", nil)
	anchor := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	filter, err := parseUsageFilterQuery(req, anchor)
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Range != "24h" {
		t.Fatalf("expected range to be preserved, got %+v", filter)
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		t.Fatalf("expected preset range to resolve concrete times, got %+v", filter)
	}
	if !filter.EndTime.Equal(anchor) {
		t.Fatalf("expected preset range end to use anchor time, got %+v", filter)
	}
	if !filter.StartTime.Equal(anchor.Add(-24 * time.Hour)) {
		t.Fatalf("expected preset range start to subtract 24h, got %+v", filter)
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
	expectedStart := time.Date(2026, 4, 22, 0, 0, 0, 0, location).UTC()
	expectedEnd := time.Date(2026, 4, 23, 0, 0, 0, 0, location).Add(-time.Nanosecond).UTC()
	if !filter.StartTime.Equal(expectedStart) {
		t.Fatalf("expected today start %s, got %s", expectedStart, *filter.StartTime)
	}
	if !filter.EndTime.Equal(expectedEnd) {
		t.Fatalf("expected today end %s, got %s", expectedEnd, *filter.EndTime)
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
	expectedStart := time.Date(2026, 3, 8, 0, 0, 0, 0, location).UTC()
	expectedEnd := time.Date(2026, 3, 9, 0, 0, 0, 0, location).Add(-time.Nanosecond).UTC()
	if !filter.StartTime.Equal(expectedStart) {
		t.Fatalf("expected DST today start %s, got %s", expectedStart, *filter.StartTime)
	}
	if !filter.EndTime.Equal(expectedEnd) {
		t.Fatalf("expected DST today end %s, got %s", expectedEnd, *filter.EndTime)
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
	expectedStart := time.Date(2026, 4, 20, 0, 0, 0, 0, location).UTC()
	expectedEnd := time.Date(2026, 4, 22, 0, 0, 0, 0, location).Add(-time.Nanosecond).UTC()
	if !filter.StartTime.Equal(expectedStart) {
		t.Fatalf("expected custom date start %s, got %s", expectedStart, *filter.StartTime)
	}
	if !filter.EndTime.Equal(expectedEnd) {
		t.Fatalf("expected custom date end %s, got %s", expectedEnd, *filter.EndTime)
	}
}

func TestParseUsageFilterQueryRejectsInvalidCustomRange(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/overview?range=custom&start=2026-04-21T00:00:00Z&end=2026-04-20T23:59:59Z", nil)

	_, err := parseUsageFilterQuery(req, time.Time{})
	if err == nil {
		t.Fatal("expected invalid custom range error")
	}
}

func TestParseUsageFilterQueryDefaultsEventsPagination(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?range=all", nil)

	filter, err := parseUsageFilterQuery(req, time.Time{})
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Page != 1 || filter.PageSize != 100 || filter.Offset != 0 {
		t.Fatalf("expected default pagination, got %+v", filter)
	}
}

func TestParseUsageFilterQueryAcceptsEventsPaginationAndFilters(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?page=3&page_size=100&model=%20claude-sonnet%20&source=%20source-a%20&auth_index=%202%20", nil)

	filter, err := parseUsageFilterQuery(req, time.Time{})
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

func TestParseUsageFilterQueryUsesLimitAsPageSizeAlias(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?limit=20", nil)

	filter, err := parseUsageFilterQuery(req, time.Time{})
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.Page != 1 || filter.PageSize != 20 || filter.Offset != 0 {
		t.Fatalf("expected limit alias to set page size, got %+v", filter)
	}
}

func TestParseUsageFilterQueryPrefersPageSizeOverLimit(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/usage/events?page_size=50&limit=20", nil)

	filter, err := parseUsageFilterQuery(req, time.Time{})
	if err != nil {
		t.Fatalf("parseUsageFilterQuery returned error: %v", err)
	}
	if filter.PageSize != 50 {
		t.Fatalf("expected page_size to win over limit, got %+v", filter)
	}
}

func TestParseUsageFilterQueryRejectsInvalidEventsPagination(t *testing.T) {
	tests := []string{
		"/api/v1/usage/events?page=0",
		"/api/v1/usage/events?page_size=25",
	}
	for _, path := range tests {
		req := httptest.NewRequest("GET", path, nil)
		if _, err := parseUsageFilterQuery(req, time.Time{}); err == nil {
			t.Fatalf("expected pagination error for %s", path)
		}
	}
}
