package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestUsageEventsPersistListAndExportClientMetadata(t *testing.T) {
	db := openTestDatabase(t)
	clientIP := "192.0.2.10"
	xForwardedFor := "203.0.113.5, 198.51.100.8"
	userAgent := "test-client/1.0"
	event := entities.UsageEvent{
		EventKey:      "event-client-metadata",
		APIGroupKey:   "provider-a",
		Model:         "gpt-5.4",
		Timestamp:     time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC),
		ClientIP:      &clientIP,
		XForwardedFor: &xForwardedFor,
		UserAgent:     &userAgent,
	}

	if inserted, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil || inserted != 1 {
		t.Fatalf("InsertUsageEvents inserted=%d err=%v", inserted, err)
	}

	page, err := repository.ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 1, PageSize: 10}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected one listed event, got %d", len(page.Events))
	}
	assertUsageEventClientMetadata(t, page.Events[0], clientIP, xForwardedFor, userAgent)

	exported, err := repository.ExportUsageEventsWithFilter(db, dto.UsageQueryFilter{}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ExportUsageEventsWithFilter returned error: %v", err)
	}
	if len(exported) != 1 {
		t.Fatalf("expected one exported event, got %d", len(exported))
	}
	assertUsageEventClientMetadata(t, exported[0], clientIP, xForwardedFor, userAgent)
}

func assertUsageEventClientMetadata(t *testing.T, event dto.UsageEventRecord, clientIP, xForwardedFor, userAgent string) {
	t.Helper()
	if event.ClientIP == nil || *event.ClientIP != clientIP || event.XForwardedFor == nil || *event.XForwardedFor != xForwardedFor || event.UserAgent == nil || *event.UserAgent != userAgent {
		t.Fatalf("unexpected client metadata: %+v", event)
	}
}
