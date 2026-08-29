package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestAttachWindowUsageStatsBackfillsOnlyXAIWeeklyBilling(t *testing.T) {
	db := openQuotaUsageStatsTestDB(t)
	if _, err := repository.UpsertModelPriceSetting(db, repositorydto.ModelPriceSettingInput{
		Model:                "grok-priced",
		PricingStyle:         entities.ModelPricingStyleOpenAI,
		PromptPricePer1M:     2,
		CompletionPricePer1M: 10,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting: %v", err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadPricingSnapshot: %v", err)
	}
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), pricing.NewCatalog(snapshot))
	defer service.StopRefreshTasks()

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	weeklyResetAt := now.Add(48 * time.Hour)
	weeklyStart := weeklyResetAt.Add(-7 * 24 * time.Hour)
	for _, event := range []entities.UsageEvent{
		{
			EventKey:     "xai-weekly-current",
			AuthIndex:    "xai-auth",
			Model:        "grok-priced",
			Timestamp:    now.Add(-30 * time.Minute),
			InputTokens:  1_000_000,
			OutputTokens: 500_000,
			TotalTokens:  1_500_000,
		},
		{
			EventKey:     "xai-weekly-expired",
			AuthIndex:    "xai-auth",
			Model:        "grok-priced",
			Timestamp:    weeklyStart.Add(-time.Minute),
			InputTokens:  2_000_000,
			OutputTokens: 1_000_000,
			TotalTokens:  3_000_000,
		},
		{
			EventKey:     "xai-weekly-other-auth",
			AuthIndex:    "other-xai-auth",
			Model:        "grok-priced",
			Timestamp:    now.Add(-30 * time.Minute),
			InputTokens:  4_000_000,
			OutputTokens: 2_000_000,
			TotalTokens:  6_000_000,
		},
	} {
		if err := db.Create(&event).Error; err != nil {
			t.Fatalf("seed usage event %q: %v", event.EventKey, err)
		}
	}

	weeklySeconds := int64(7 * 24 * time.Hour / time.Second)
	monthlySeconds := int64(30 * 24 * time.Hour / time.Second)
	response := attachWindowUsageStats(service, context.Background(), "xai-auth", CheckResponse{
		ID: "xai-auth",
		Quota: []QuotaRow{
			{
				Key:         "billing.weekly",
				Label:       "Weekly",
				Scope:       "billing",
				Metric:      "weekly",
				UsedPercent: floatPtr(25),
				Window:      &QuotaWindow{Seconds: &weeklySeconds},
				ResetAt:     timeutil.FormatStorageTime(weeklyResetAt),
			},
			{
				Key:     "billing.monthly",
				Label:   "Monthly Spend",
				Scope:   "billing",
				Metric:  "usd_cents",
				Window:  &QuotaWindow{Seconds: &monthlySeconds},
				ResetAt: timeutil.FormatStorageTime(now.Add(10 * 24 * time.Hour)),
			},
			{
				Key:     "billing.on_demand",
				Label:   "Pay-as-you-go",
				Scope:   "billing",
				Metric:  "usd_cents",
				Window:  &QuotaWindow{Seconds: &monthlySeconds},
				ResetAt: timeutil.FormatStorageTime(now.Add(10 * 24 * time.Hour)),
			},
			{
				Key:         "billing.weekly.product.grokbuild",
				Label:       "GrokBuild Usage",
				Scope:       "product",
				Metric:      "GrokBuild",
				UsedPercent: floatPtr(40),
				Window:      &QuotaWindow{Seconds: &weeklySeconds},
				ResetAt:     timeutil.FormatStorageTime(weeklyResetAt),
			},
		},
	}, now)

	weekly := findQuotaUsageStatsRow(t, response.Quota, "billing.weekly")
	if weekly.WindowUsageTokens == nil || *weekly.WindowUsageTokens != 1_500_000 {
		t.Fatalf("xAI weekly tokens = %#v, want 1500000", weekly.WindowUsageTokens)
	}
	const wantWeeklyCost = 7.0
	if weekly.WindowUsageCost == nil || math.Abs(*weekly.WindowUsageCost-wantWeeklyCost) > 1e-9 {
		t.Fatalf("xAI weekly cost = %#v, want %.2f", weekly.WindowUsageCost, wantWeeklyCost)
	}

	for _, key := range []string{"billing.monthly", "billing.on_demand", "billing.weekly.product.grokbuild"} {
		row := findQuotaUsageStatsRow(t, response.Quota, key)
		if row.WindowUsageTokens != nil || row.WindowUsageCost != nil {
			t.Fatalf("%s must not receive auth-level window usage, got tokens=%#v cost=%#v", key, row.WindowUsageTokens, row.WindowUsageCost)
		}
	}
}
