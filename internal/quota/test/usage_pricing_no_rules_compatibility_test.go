package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestQuotaWindowWithoutRulesMatchesLegacyTokenCostHelper(t *testing.T) {
	db := openQuotaUsageStatsTestDB(t)
	setting, err := repository.UpsertModelPriceSetting(db, repositorydto.ModelPriceSettingInput{
		Model:                "priced-model",
		PricingStyle:         entities.ModelPricingStyleOpenAI,
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CacheReadPricePer1M:  0.3,
		CacheWritePricePer1M: 3.75,
	})
	if err != nil {
		t.Fatalf("UpsertModelPriceSetting: %v", err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadPricingSnapshot: %v", err)
	}
	catalog := pricing.NewCatalog(snapshot)
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), catalog)
	defer service.StopRefreshTasks()

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	tokens := helper.UsageTokenCostInput{
		InputTokens:         1_000_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
	}
	if err := db.Create(&entities.UsageEvent{
		EventKey:            "quota-no-rules",
		AuthIndex:           "auth-no-rules",
		Model:               "priced-model",
		Timestamp:           now.Add(-time.Hour),
		InputTokens:         tokens.InputTokens,
		OutputTokens:        tokens.OutputTokens,
		CacheReadTokens:     tokens.CacheReadTokens,
		CacheCreationTokens: tokens.CacheCreationTokens,
		TotalTokens:         tokens.InputTokens + tokens.OutputTokens,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	windowSeconds := int64(5 * time.Hour / time.Second)
	response := attachWindowUsageStats(service, context.Background(), "auth-no-rules", CheckResponse{
		ID: "auth-no-rules",
		Quota: []QuotaRow{{
			Key:     "rate_limit.primary_window",
			Label:   "5h",
			Scope:   "window",
			Window:  &QuotaWindow{Seconds: &windowSeconds},
			ResetAt: timeutil.FormatStorageTime(resetAt),
		}},
	}, now)

	row := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.primary_window")
	want := helper.CalculateUsageTokenCostBreakdown(tokens, *setting).TotalCostUSD
	if row.WindowUsageCost == nil || math.Abs(*row.WindowUsageCost-want) > 1e-9 {
		t.Fatalf("quota no-Rules cost = %#v, want %.12f", row.WindowUsageCost, want)
	}
}
