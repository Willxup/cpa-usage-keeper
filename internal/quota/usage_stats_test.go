package quota

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func TestQuotaRowUsageWindowParsesStorageTimeResetAt(t *testing.T) {
	windowSeconds := int64(5 * 60 * 60)
	resetAt := "2026-05-26 03:00:00"
	now := time.Date(2026, 5, 26, 2, 0, 0, 0, time.UTC)

	windowStart, windowEnd, ok := quotaRowUsageWindow(QuotaRow{
		ResetAt: resetAt,
		Window:  &QuotaWindow{Seconds: &windowSeconds},
	}, now)

	if !ok {
		t.Fatal("expected storage-time resetAt to produce usage window")
	}
	if windowStart.IsZero() || windowEnd.IsZero() {
		t.Fatalf("expected non-zero window, got [%s, %s)", windowStart, windowEnd)
	}
}

func TestQuotaRowUsageWindowUsesResetAfterSecondsWhenResetAtMissing(t *testing.T) {
	windowSeconds := int64(5 * 60 * 60)
	resetAfterSeconds := int64(60 * 60)
	now := time.Date(2026, 5, 26, 2, 0, 0, 0, time.UTC)

	windowStart, windowEnd, ok := quotaRowUsageWindow(QuotaRow{
		Window:            &QuotaWindow{Seconds: &windowSeconds},
		ResetAfterSeconds: &resetAfterSeconds,
	}, now)

	if !ok {
		t.Fatal("expected reset-after row to produce usage window")
	}
	wantEnd := now
	wantStart := now.Add(time.Duration(resetAfterSeconds-windowSeconds) * time.Second)
	if !windowEnd.Equal(wantEnd) || !windowStart.Equal(wantStart) {
		t.Fatalf("expected window [%s, %s), got [%s, %s)", wantStart, wantEnd, windowStart, windowEnd)
	}
}

func TestQuotaRowUsageWindowRejectsNegativeResetAfterSeconds(t *testing.T) {
	windowSeconds := int64(5 * 60 * 60)
	resetAfterSeconds := int64(-60)
	now := time.Date(2026, 5, 26, 2, 0, 0, 0, time.UTC)

	_, _, ok := quotaRowUsageWindow(QuotaRow{
		Window:            &QuotaWindow{Seconds: &windowSeconds},
		ResetAfterSeconds: &resetAfterSeconds,
	}, now)

	if ok {
		t.Fatal("expected negative reset_after_seconds to be ignored")
	}
}

func TestAttachWindowUsageStatsNilServiceReturnsOriginalResponse(t *testing.T) {
	var service *Service
	response := CheckResponse{ID: "auth-nil", Quota: []QuotaRow{{
		Key:   "rate_limit.primary_window",
		Label: "5h",
	}}}

	got := service.attachWindowUsageStats(context.Background(), "auth-nil", response, time.Now())

	if got.ID != response.ID || len(got.Quota) != 1 || got.Quota[0].Key != response.Quota[0].Key {
		t.Fatalf("expected nil service to return original response, got %#v", got)
	}
}

func TestAttachWindowUsageStatsOnlyBackfillsMissingKnownWindowScopeRows(t *testing.T) {
	db := openQuotaUsageStatsTestDB(t)
	service := &Service{db: db}
	windowSeconds := int64(5 * 60 * 60)
	weeklySeconds := int64(7 * 24 * 60 * 60)
	monthlySeconds := int64(30 * 24 * 60 * 60)
	averageMonthlySeconds := quotaWindowAverageMonthSeconds
	unknownSeconds := int64(60 * 60)
	resetAt := time.Date(2026, 6, 2, 5, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC)

	if err := db.Create(&entities.UsageEvent{
		AuthIndex:   "auth-pro",
		Model:       "gpt-codex",
		Timestamp:   now.Add(-time.Hour),
		TotalTokens: 222,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	response := service.attachWindowUsageStats(context.Background(), "auth-pro", CheckResponse{ID: "auth-pro", Quota: []QuotaRow{
		{
			Key:               "rate_limit.primary_window",
			Label:             "5h",
			Scope:             "window",
			Window:            &QuotaWindow{Seconds: &windowSeconds},
			ResetAt:           timeutil.FormatStorageTime(resetAt),
			WindowUsageTokens: intPtr(11),
			WindowUsageCost:   floatPtr(0.42),
		},
		{
			Key:     "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window",
			Label:   "GPT-5.3-Codex-Spark 5h",
			Scope:   "additional",
			Metric:  "codex_bengalfox",
			Window:  &QuotaWindow{Seconds: &windowSeconds},
			ResetAt: timeutil.FormatStorageTime(resetAt),
		},
		{
			Key:     "rate_limit.secondary_window",
			Label:   "Weekly",
			Scope:   "window",
			Window:  &QuotaWindow{Seconds: &weeklySeconds},
			ResetAt: timeutil.FormatStorageTime(resetAt),
		},
		{
			Key:     "rate_limit.monthly_window",
			Label:   "Monthly",
			Scope:   "window",
			Window:  &QuotaWindow{Seconds: &monthlySeconds},
			ResetAt: timeutil.FormatStorageTime(resetAt),
		},
		{
			Key:     "rate_limit.average_monthly_window",
			Label:   "Monthly",
			Scope:   "window",
			Window:  &QuotaWindow{Seconds: &averageMonthlySeconds},
			ResetAt: timeutil.FormatStorageTime(resetAt),
		},
		{
			Key:     "code_review_rate_limit.primary_window",
			Label:   "Code Review 5h",
			Scope:   "code_review",
			Window:  &QuotaWindow{Seconds: &windowSeconds},
			ResetAt: timeutil.FormatStorageTime(resetAt),
		},
		{
			Key:     "rate_limit.unknown_window",
			Label:   "Unknown",
			Scope:   "window",
			Window:  &QuotaWindow{Seconds: &unknownSeconds},
			ResetAt: timeutil.FormatStorageTime(resetAt),
		},
	}}, now)

	providerWindow := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.primary_window")
	if providerWindow.WindowUsageTokens == nil || *providerWindow.WindowUsageTokens != 11 {
		t.Fatalf("expected provider window_usage_tokens to be preserved, got %#v", providerWindow.WindowUsageTokens)
	}
	if providerWindow.WindowUsageCost == nil || math.Abs(*providerWindow.WindowUsageCost-0.42) > 0.000000001 {
		t.Fatalf("expected provider window_usage_cost to be preserved, got %#v", providerWindow.WindowUsageCost)
	}

	additional := findQuotaUsageStatsRow(t, response.Quota, "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window")
	if additional.WindowUsageTokens != nil || additional.WindowUsageCost != nil {
		t.Fatalf("expected additional quota without provider usage to stay empty, got tokens=%#v cost=%#v", additional.WindowUsageTokens, additional.WindowUsageCost)
	}

	weeklyWindow := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.secondary_window")
	if weeklyWindow.WindowUsageTokens == nil || *weeklyWindow.WindowUsageTokens != 222 {
		t.Fatalf("expected missing weekly window usage to be backfilled from usage_events, got %#v", weeklyWindow.WindowUsageTokens)
	}
	if weeklyWindow.WindowUsageCost == nil || *weeklyWindow.WindowUsageCost != 0 {
		t.Fatalf("expected missing weekly window cost to be backfilled from usage_events, got %#v", weeklyWindow.WindowUsageCost)
	}
	monthlyWindow := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.monthly_window")
	if monthlyWindow.WindowUsageTokens == nil || *monthlyWindow.WindowUsageTokens != 222 {
		t.Fatalf("expected missing monthly window usage to be backfilled from usage_events, got %#v", monthlyWindow.WindowUsageTokens)
	}
	if monthlyWindow.WindowUsageCost == nil || *monthlyWindow.WindowUsageCost != 0 {
		t.Fatalf("expected missing monthly window cost to be backfilled from usage_events, got %#v", monthlyWindow.WindowUsageCost)
	}
	averageMonthlyWindow := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.average_monthly_window")
	if averageMonthlyWindow.WindowUsageTokens == nil || *averageMonthlyWindow.WindowUsageTokens != 222 {
		t.Fatalf("expected missing average monthly window usage to be backfilled from usage_events, got %#v", averageMonthlyWindow.WindowUsageTokens)
	}
	if averageMonthlyWindow.WindowUsageCost == nil || *averageMonthlyWindow.WindowUsageCost != 0 {
		t.Fatalf("expected missing average monthly window cost to be backfilled from usage_events, got %#v", averageMonthlyWindow.WindowUsageCost)
	}
	codeReview := findQuotaUsageStatsRow(t, response.Quota, "code_review_rate_limit.primary_window")
	if codeReview.WindowUsageTokens != nil || codeReview.WindowUsageCost != nil {
		t.Fatalf("expected code review quota without provider usage to stay empty, got tokens=%#v cost=%#v", codeReview.WindowUsageTokens, codeReview.WindowUsageCost)
	}
	unknownWindow := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.unknown_window")
	if unknownWindow.WindowUsageTokens != nil || unknownWindow.WindowUsageCost != nil {
		t.Fatalf("expected unknown window quota to stay empty, got tokens=%#v cost=%#v", unknownWindow.WindowUsageTokens, unknownWindow.WindowUsageCost)
	}
}

func TestAttachWindowUsageStatsBackfillsBothFieldsWhenProviderWindowUsageIncomplete(t *testing.T) {
	db := openQuotaUsageStatsTestDB(t)
	service := &Service{db: db}
	windowSeconds := int64(5 * 60 * 60)
	resetAt := time.Date(2026, 6, 2, 5, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC)

	if err := db.Create(&entities.UsageEvent{
		AuthIndex:   "auth-partial",
		Model:       "gpt-codex",
		Timestamp:   now.Add(-time.Hour),
		TotalTokens: 333,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	response := service.attachWindowUsageStats(context.Background(), "auth-partial", CheckResponse{ID: "auth-partial", Quota: []QuotaRow{
		{
			Key:               "rate_limit.primary_window",
			Label:             "5h",
			Scope:             "window",
			Window:            &QuotaWindow{Seconds: &windowSeconds},
			ResetAt:           timeutil.FormatStorageTime(resetAt),
			WindowUsageTokens: intPtr(11),
		},
		{
			Key:             "rate_limit.secondary_window",
			Label:           "5h",
			Scope:           "window",
			Window:          &QuotaWindow{Seconds: &windowSeconds},
			ResetAt:         timeutil.FormatStorageTime(resetAt),
			WindowUsageCost: floatPtr(0.42),
		},
	}}, now)

	tokensOnly := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.primary_window")
	if tokensOnly.WindowUsageTokens == nil || *tokensOnly.WindowUsageTokens != 333 {
		t.Fatalf("expected incomplete provider tokens to be replaced by local usage, got %#v", tokensOnly.WindowUsageTokens)
	}
	if tokensOnly.WindowUsageCost == nil || *tokensOnly.WindowUsageCost != 0 {
		t.Fatalf("expected incomplete provider cost to be replaced by local usage, got %#v", tokensOnly.WindowUsageCost)
	}
	costOnly := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.secondary_window")
	if costOnly.WindowUsageTokens == nil || *costOnly.WindowUsageTokens != 333 {
		t.Fatalf("expected incomplete provider tokens to be replaced by local usage, got %#v", costOnly.WindowUsageTokens)
	}
	if costOnly.WindowUsageCost == nil || *costOnly.WindowUsageCost != 0 {
		t.Fatalf("expected incomplete provider cost to be replaced by local usage, got %#v", costOnly.WindowUsageCost)
	}
}

func TestAttachWindowUsageStatsDropsIncompleteProviderWindowUsageWhenFallbackUnavailable(t *testing.T) {
	db := openQuotaUsageStatsTestDB(t)
	service := &Service{db: db}
	windowSeconds := int64(5 * 60 * 60)

	response := service.attachWindowUsageStats(context.Background(), "auth-partial", CheckResponse{ID: "auth-partial", Quota: []QuotaRow{{
		Key:               "rate_limit.primary_window",
		Label:             "5h",
		Scope:             "window",
		Window:            &QuotaWindow{Seconds: &windowSeconds},
		WindowUsageTokens: intPtr(11),
	}}}, time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC))

	row := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.primary_window")
	if row.WindowUsageTokens != nil || row.WindowUsageCost != nil {
		t.Fatalf("expected incomplete provider usage to be dropped when local fallback is unavailable, got tokens=%#v cost=%#v", row.WindowUsageTokens, row.WindowUsageCost)
	}
}

func TestAttachWindowUsageStatsDoesNotBackfillPartialAdditionalOrCodeReviewRows(t *testing.T) {
	db := openQuotaUsageStatsTestDB(t)
	service := &Service{db: db}
	windowSeconds := int64(5 * 60 * 60)
	resetAt := time.Date(2026, 6, 2, 5, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC)

	if err := db.Create(&entities.UsageEvent{
		AuthIndex:   "auth-special",
		Model:       "gpt-codex",
		Timestamp:   now.Add(-time.Hour),
		TotalTokens: 444,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	response := service.attachWindowUsageStats(context.Background(), "auth-special", CheckResponse{ID: "auth-special", Quota: []QuotaRow{
		{
			Key:               "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window",
			Label:             "GPT-5.3-Codex-Spark 5h",
			Scope:             "additional",
			Metric:            "codex_bengalfox",
			Window:            &QuotaWindow{Seconds: &windowSeconds},
			ResetAt:           timeutil.FormatStorageTime(resetAt),
			WindowUsageTokens: intPtr(11),
		},
		{
			Key:             "code_review_rate_limit.primary_window",
			Label:           "Code Review 5h",
			Scope:           "code_review",
			Window:          &QuotaWindow{Seconds: &windowSeconds},
			ResetAt:         timeutil.FormatStorageTime(resetAt),
			WindowUsageCost: floatPtr(0.42),
		},
	}}, now)

	additional := findQuotaUsageStatsRow(t, response.Quota, "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window")
	if additional.WindowUsageTokens == nil || *additional.WindowUsageTokens != 11 || additional.WindowUsageCost != nil {
		t.Fatalf("expected partial additional provider usage to be preserved without fallback, got tokens=%#v cost=%#v", additional.WindowUsageTokens, additional.WindowUsageCost)
	}
	codeReview := findQuotaUsageStatsRow(t, response.Quota, "code_review_rate_limit.primary_window")
	if codeReview.WindowUsageTokens != nil || codeReview.WindowUsageCost == nil || math.Abs(*codeReview.WindowUsageCost-0.42) > 0.000000001 {
		t.Fatalf("expected partial code review provider usage to be preserved without fallback, got tokens=%#v cost=%#v", codeReview.WindowUsageTokens, codeReview.WindowUsageCost)
	}
}

func TestAttachWindowUsageStatsPreservesProviderZeroWindowUsage(t *testing.T) {
	db := openQuotaUsageStatsTestDB(t)
	service := &Service{db: db}
	windowSeconds := int64(5 * 60 * 60)
	resetAt := time.Date(2026, 6, 2, 5, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 2, 3, 0, 0, 0, time.UTC)
	if _, err := repository.UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "priced", PromptPricePer1M: 10, CompletionPricePer1M: 20, CachePricePer1M: 1}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	if err := db.Create(&entities.UsageEvent{
		AuthIndex:    "auth-zero",
		Model:        "priced",
		Timestamp:    now.Add(-time.Hour),
		InputTokens:  1_000_000,
		OutputTokens: 500_000,
		TotalTokens:  1_500_000,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}

	response := service.attachWindowUsageStats(context.Background(), "auth-zero", CheckResponse{ID: "auth-zero", Quota: []QuotaRow{{
		Key:               "rate_limit.primary_window",
		Label:             "5h",
		Scope:             "window",
		Window:            &QuotaWindow{Seconds: &windowSeconds},
		ResetAt:           timeutil.FormatStorageTime(resetAt),
		WindowUsageTokens: intPtr(0),
		WindowUsageCost:   floatPtr(0),
	}}}, now)

	row := findQuotaUsageStatsRow(t, response.Quota, "rate_limit.primary_window")
	if row.WindowUsageTokens == nil || *row.WindowUsageTokens != 0 {
		t.Fatalf("expected provider zero tokens to be preserved, got %#v", row.WindowUsageTokens)
	}
	if row.WindowUsageCost == nil || *row.WindowUsageCost != 0 {
		t.Fatalf("expected provider zero cost to be preserved, got %#v", row.WindowUsageCost)
	}
}

func openQuotaUsageStatsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "quota-usage-stats.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func findQuotaUsageStatsRow(t *testing.T, rows []QuotaRow, key string) QuotaRow {
	t.Helper()
	for _, row := range rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("missing quota row %q in %#v", key, rows)
	return QuotaRow{}
}
