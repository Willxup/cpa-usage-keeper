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

func TestAttachWindowUsageStatsKeepsStoredCostAcrossPricingChanges(t *testing.T) {
	db := openQuotaTestDB(t)
	if _, err := repository.UpsertModelPriceSetting(db, repositorydto.ModelPriceSettingInput{Model: "priced-model", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	if err := db.Create(&[]entities.UsageEvent{
		{EventKey: "quota-stored-available", AuthIndex: "stored-auth", Model: "priced-model", Timestamp: now.Add(-time.Hour), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: floatPtr(1.25), CostAvailable: boolPtr(true)},
		{EventKey: "quota-stored-unavailable", AuthIndex: "unavailable-auth", Model: "priced-model", Timestamp: now.Add(-time.Hour), InputTokens: 500, TotalTokens: 500, CostUSD: floatPtr(0.5), CostAvailable: boolPtr(false)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	t.Cleanup(service.StopRefreshTasks)
	windowSeconds := int64(5 * time.Hour / time.Second)
	rows := func() []QuotaRow {
		return []QuotaRow{
			{Key: "rate_limit.primary_window", Scope: "window", Window: &QuotaWindow{Seconds: &windowSeconds}, ResetAt: timeutil.FormatStorageTime(resetAt), WindowUsageTokens: intPtr(11), WindowUsageCost: floatPtr(0.42)},
			{Key: "rate_limit.secondary_window", Scope: "window", Window: &QuotaWindow{Seconds: &windowSeconds}, ResetAt: timeutil.FormatStorageTime(resetAt), WindowUsageTokens: intPtr(12)},
		}
	}
	assertStored := func() {
		t.Helper()
		result := attachWindowUsageStats(service, context.Background(), "stored-auth", CheckResponse{ID: "stored-auth", Quota: rows()}, now)
		// 完整上游 pair 优先；单边上游值则整对回填同一批已存事件事实。
		assertWindowUsage(t, findQuotaRow(t, result.Quota, "rate_limit.primary_window"), 11, 0.42)
		assertWindowUsage(t, findQuotaRow(t, result.Quota, "rate_limit.secondary_window"), 1_000_000, 1.25)
		unavailable := attachWindowUsageStats(service, context.Background(), "unavailable-auth", CheckResponse{ID: "unavailable-auth", Quota: []QuotaRow{{
			Key: "rate_limit.primary_window", Scope: "window", Window: &QuotaWindow{Seconds: &windowSeconds}, ResetAt: timeutil.FormatStorageTime(resetAt),
		}}}, now)
		// 普通窗口延续原有数值 pair 发布口径；本步只把金额来源换成已存列。
		assertWindowUsage(t, unavailable.Quota[0], 500, 0.5)
	}
	assertStored()
	if _, err := repository.UpsertModelPriceSetting(db, repositorydto.ModelPriceSettingInput{Model: "priced-model", PromptPricePer1M: 90}); err != nil {
		t.Fatal(err)
	}
	assertStored()
	if err := repository.DeleteModelPriceSettingRequired(db, "priced-model"); err != nil {
		t.Fatal(err)
	}
	assertStored()
}
