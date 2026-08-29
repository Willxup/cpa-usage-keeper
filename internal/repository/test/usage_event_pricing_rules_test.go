package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestRequestEventsListAndStreamUseCompleteRuleDimensions(t *testing.T) {
	db := openTestDatabase(t)
	timestamp := time.Now().Add(-time.Minute)
	if err := db.Create(&entities.UsageEvent{
		EventKey: "request-rule", Timestamp: timestamp, APIGroupKey: "group-a", Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh", InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed request event: %v", err)
	}
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
	})
	page, err := repository.ListUsageEventsWithFilter(db, repodto.UsageQueryFilter{PageSize: 10}, resolver)
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].CostUSD != 6 || !page.Events[0].CostAvailable {
		t.Fatalf("unexpected request event price: %+v", page.Events)
	}

	streamed := make([]repodto.UsageEventRecord, 0, 1)
	if err := repository.StreamUsageEventsWithFilter(db, repodto.UsageQueryFilter{}, func(record repodto.UsageEventRecord) error {
		streamed = append(streamed, record)
		return nil
	}, resolver); err != nil {
		t.Fatalf("StreamUsageEventsWithFilter: %v", err)
	}
	if len(streamed) != 1 || streamed[0].CostUSD != 6 {
		t.Fatalf("unexpected streamed request event price: %+v", streamed)
	}
}
