package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestRequestEventsListAndStreamKeepStoredFeeWithoutCurrentPricing(t *testing.T) {
	db := openTestDatabase(t)
	timestamp := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	storedCost, available := 6.0, true
	if err := db.Create(&entities.UsageEvent{
		EventKey: "request-rule", Timestamp: timestamp, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh", InputTokens: 1_000_000, TotalTokens: 1_000_000,
		CostUSD: &storedCost, CostAvailable: &available,
	}).Error; err != nil {
		t.Fatalf("seed request event: %v", err)
	}
	page, err := repository.ListUsageEventsWithFilter(db, repodto.UsageQueryFilter{PageSize: 10}, emptyPricingSnapshotForTest())
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].CostUSD != storedCost || !page.Events[0].CostAvailable || page.Events[0].PricingStyle != "" {
		t.Fatalf("request event lost stored fee after pricing removal: %+v", page.Events)
	}

	streamed := make([]repodto.UsageEventRecord, 0, 1)
	if err := repository.StreamUsageEventsWithFilter(db, repodto.UsageQueryFilter{}, func(record repodto.UsageEventRecord) error {
		streamed = append(streamed, record)
		return nil
	}, emptyPricingSnapshotForTest()); err != nil {
		t.Fatalf("StreamUsageEventsWithFilter: %v", err)
	}
	if len(streamed) != 1 || streamed[0].CostUSD != storedCost || !streamed[0].CostAvailable {
		t.Fatalf("streamed request event lost stored fee: %+v", streamed)
	}
}
