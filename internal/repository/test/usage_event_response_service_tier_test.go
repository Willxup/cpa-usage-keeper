package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestUsageEventsPersistAndListResponseServiceTier(t *testing.T) {
	db := openTestDatabase(t)
	events := []entities.UsageEvent{{
		EventKey:            "event-response-service-tier",
		APIGroupKey:         "provider-a",
		Model:               "gpt-5.4",
		ServiceTier:         "auto",
		ResponseServiceTier: "default",
		Timestamp:           time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC),
		Source:              "source-a",
		AuthIndex:           "auth-1",
		TotalTokens:         10,
	}}

	inserted, deduped, err := repository.InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 1 || deduped != 0 {
		t.Fatalf("expected inserted=1 deduped=0, got inserted=%d deduped=%d", inserted, deduped)
	}

	page, err := repository.ListUsageEventsWithFilter(db, dto.UsageQueryFilter{Page: 1, PageSize: 10}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected one usage event, got %d", len(page.Events))
	}
	if page.Events[0].ServiceTier != "auto" || page.Events[0].ResponseServiceTier != "default" {
		t.Fatalf("expected separate request/response tiers, got %+v", page.Events[0])
	}
}
