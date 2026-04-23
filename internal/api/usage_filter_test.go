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
