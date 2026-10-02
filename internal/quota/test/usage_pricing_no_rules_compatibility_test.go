package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestQuotaWindowWithoutRulesReadsPersistedCost(t *testing.T) {
	db := openQuotaTestDB(t)
	_, err := repository.UpsertModelPriceSetting(db, repositorydto.ModelPriceSettingInput{
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
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	defer service.StopRefreshTasks()

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	if err := db.Create(&entities.UsageEvent{
		EventKey:            "quota-no-rules",
		AuthIndex:           "auth-no-rules",
		Model:               "priced-model",
		Timestamp:           now.Add(-time.Hour),
		InputTokens:         1_000_000,
		OutputTokens:        500_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
		TotalTokens:         1_500_000,
		CostUSD:             floatPtr(4.125),
		CostAvailable:       boolPtr(true),
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

	row := findQuotaRow(t, response.Quota, "rate_limit.primary_window")
	assertWindowUsage(t, row, 1_500_000, 4.125)
}
