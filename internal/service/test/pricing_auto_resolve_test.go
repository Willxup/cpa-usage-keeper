package test

import (
	"context"
	"testing"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestPricingServiceEnsureModelsPricingPersistsAndPublishesMissingModels(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadPricingSnapshot: %v", err)
	}
	catalog := pricing.NewCatalog(snapshot)
	pricingProvider := service.NewPricingService(db, catalog)
	autoProvider, ok := pricingProvider.(service.AutoPricingProvider)
	if !ok {
		t.Fatal("expected pricing provider to support automatic pricing resolution")
	}

	settings, err := autoProvider.EnsureModelsPricing(context.Background(), []string{
		"claude-sonnet-4-6",
		"deepseek-ai/deepseek-v4-flash",
		"unknown",
		"openai/tts-1",
		"  deepseek-ai/deepseek-v4-flash  ",
	})
	if err != nil {
		t.Fatalf("EnsureModelsPricing: %v", err)
	}
	if len(settings) != 2 || settings[0].Model != "deepseek-ai/deepseek-v4-flash" || settings[1].Model != "claude-sonnet-4-6" {
		t.Fatalf("unexpected ensured models: %+v", settings)
	}
	if settings[0].PricingStyle != "openai" || settings[1].PricingStyle != "claude" {
		t.Fatalf("unexpected pricing styles: %+v", settings)
	}
	if settings[0].PromptPricePer1M != 0.147 || settings[0].CompletionPricePer1M != 0.147 || settings[0].CacheReadPricePer1M != 0.0147 {
		t.Fatalf("unexpected deepseek pricing: %+v", settings[0])
	}
	if settings[1].PromptPricePer1M != 3 || settings[1].CompletionPricePer1M != 15 || settings[1].CacheReadPricePer1M != 0.3 {
		t.Fatalf("unexpected claude pricing: %+v", settings[1])
	}
	assertPricingCatalogCost(t, catalog, "deepseek-ai/deepseek-v4-flash", 0.147)
	assertPricingCatalogCost(t, catalog, "claude-sonnet-4-6", 3)

	repeated, err := autoProvider.EnsureModelsPricing(context.Background(), []string{
		"claude-sonnet-4-6",
		"deepseek-ai/deepseek-v4-flash",
	})
	if err != nil {
		t.Fatalf("repeat EnsureModelsPricing: %v", err)
	}
	if repeated != nil {
		t.Fatalf("expected repeat ensure to be a no-op, got %+v", repeated)
	}
}
