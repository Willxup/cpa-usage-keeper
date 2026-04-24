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
