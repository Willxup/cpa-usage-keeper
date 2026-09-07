package service

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

func TestProcessRedisUsageInboxAutoPopulatesPricingForNewModel(t *testing.T) {
	db := openSyncTestDatabase(t)
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadPricingSnapshot: %v", err)
	}
	catalog := pricing.NewCatalog(snapshot)
	pricingProvider := NewPricingService(db, catalog)
	if _, err := pricingProvider.EnsureModelsPricing(context.Background(), []string{"existing-model"}); err != nil {
		t.Fatalf("seed existing pricing: %v", err)
	}
	if _, err := repository.InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{
		Source:     redisUsageInboxTestSource,
		RawMessage: `{"timestamp":"2026-09-06T08:00:00Z","provider":"openai","auth_type":"api_key","model":"brand-new-model","request_id":"auto-pricing","tokens":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
		PoppedAt:   time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}

	syncService := NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:         "https://cpa.example.com",
		PricingProvider: pricingProvider,
	})
	result, err := syncService.ProcessRedisUsageInbox(context.Background())
	if err != nil {
		t.Fatalf("ProcessRedisUsageInbox: %v", err)
	}
	if result == nil || result.Status != "completed" || result.InsertedEvents != 1 {
		t.Fatalf("unexpected process result: %+v", result)
	}

	settings, err := pricingProvider.ListPricing(context.Background())
	if err != nil {
		t.Fatalf("ListPricing: %v", err)
	}
	if len(settings) != 2 {
		t.Fatalf("expected existing and newly priced models, got %+v", settings)
	}
	assertPricingCatalogCost(t, catalog, "brand-new-model", 1.75)
}

func assertPricingCatalogCost(t *testing.T, catalog *pricing.Catalog, model string, want float64) {
	t.Helper()
	result := catalog.NewResolver().Calculate(
		pricing.NewCostSubject(
			pricing.UsageDimensions{Model: model},
			helper.UsageTokenCostInput{InputTokens: 1_000_000},
		),
	)
	if !result.Available || result.Cost.TotalCostUSD != want {
		t.Fatalf("catalog cost for %q = %+v, want %v", model, result, want)
	}
}
