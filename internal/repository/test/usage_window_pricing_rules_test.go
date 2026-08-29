package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageWindowRawGroupingKeepsActiveRuleDimensions(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Hour)
	end := start.Add(time.Hour)
	events := []entities.UsageEvent{
		{EventKey: "raw-priority", AuthIndex: "auth-a", Model: "model-a", ServiceTier: "priority", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000},
		{EventKey: "raw-default", AuthIndex: "auth-a", Model: "model-a", ServiceTier: "default", Timestamp: start.Add(20 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed raw window events: %v", err)
	}
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}})
	stats, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-a", start, &end, resolver)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex: %v", err)
	}
	if stats.Tokens != 2_000_000 || stats.Cost != 3 {
		t.Fatalf("expected raw default cost 1 + priority cost 2, got %+v", stats)
	}
}

func TestUsageWindowLongMergeKeyKeepsActiveDimensionsAcrossRawAndHourly(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Now().AddDate(0, 0, -7).Truncate(time.Hour).Add(30 * time.Minute)
	end := start.Add(7*24*time.Hour + 20*time.Minute)
	if err := db.Create(&entities.UsageEvent{
		EventKey: "left-priority", AuthIndex: "auth-a", Model: "model-a", ServiceTier: "priority", Timestamp: start.Add(10 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed left raw event: %v", err)
	}
	hour := start.Add(2 * time.Hour).Truncate(time.Hour)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: hour, AuthIndex: "auth-a", Model: "model-a", ServiceTier: "default", InputTokens: 1_000_000, TotalTokens: 1_000_000,
	}).Error; err != nil {
		t.Fatalf("seed hourly window row: %v", err)
	}
	resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}})
	stats, err := repository.SumUsageWindowStatsByAuthIndex(context.Background(), db, "auth-a", start, &end, resolver)
	if err != nil {
		t.Fatalf("SumUsageWindowStatsByAuthIndex: %v", err)
	}
	if stats.Tokens != 2_000_000 || stats.Cost != 3 {
		t.Fatalf("expected separated raw/hourly rule dimensions, got %+v", stats)
	}
}
