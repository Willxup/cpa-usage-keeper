package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageOverviewAggregationPreservesAllExistingHourlyAndDailyFields(t *testing.T) {
	// 准备：该 characterization fixture 固定拆分前 Overview 的时区、维度和全部旧字段。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)
	alias := "alias-a"

	// 两条事件落入同一 hourly/daily key，并显式覆盖全部旧 Token 字段和成功失败计数。
	events := []entities.UsageEvent{
		{EventKey: "overview-parity-1", APIGroupKey: "provider-a", Model: "model-a", ModelAlias: &alias, AuthIndex: "auth-a", Timestamp: now.Add(-time.Hour), Failed: false, InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 90, CacheReadTokens: 10, CacheCreationTokens: 3, TotalTokens: 138},
		{EventKey: "overview-parity-2", APIGroupKey: "provider-a", Model: "model-a", ModelAlias: &alias, AuthIndex: "auth-a", Timestamp: now.Add(-30 * time.Minute), Failed: true, InputTokens: 200, OutputTokens: 30, ReasoningTokens: 6, CachedTokens: 80, CacheReadTokens: 20, CacheCreationTokens: 4, TotalTokens: 260},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert overview parity events: %v", err)
	}

	// 执行：调用保留的完整 Overview catch-up 聚合同一组固定事件。
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	// 断言：hourly 必须逐字段保留现有累计，包括 Activity 不用但旧表继续维护的 cached_tokens。
	var hourly entities.UsageOverviewHourlyStat
	if err := db.Where("api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ?", "provider-a", "model-a", "auth-a", "alias-a").Take(&hourly).Error; err != nil {
		t.Fatalf("load hourly parity row: %v", err)
	}
	assertUsageOverviewParityFields(t, "hourly", hourly.RequestCount, hourly.SuccessCount, hourly.FailureCount, hourly.InputTokens, hourly.OutputTokens, hourly.ReasoningTokens, hourly.CachedTokens, hourly.CacheReadTokens, hourly.CacheCreationTokens, hourly.TotalTokens)

	// daily 必须与 hourly 对同一 fixture 得到完全相同的旧字段总计。
	var daily entities.UsageOverviewDailyStat
	if err := db.Where("api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ?", "provider-a", "model-a", "auth-a", "alias-a").Take(&daily).Error; err != nil {
		t.Fatalf("load daily parity row: %v", err)
	}
	assertUsageOverviewParityFields(t, "daily", daily.RequestCount, daily.SuccessCount, daily.FailureCount, daily.InputTokens, daily.OutputTokens, daily.ReasoningTokens, daily.CachedTokens, daily.CacheReadTokens, daily.CacheCreationTokens, daily.TotalTokens)

	// Overview checkpoint 继续精确推进到最后一条 usage event。
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load overview parity checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 2 {
		t.Fatalf("expected overview checkpoint 2, got %+v", checkpoint)
	}
	if checkpoint.StatsUpdatedAt == nil || !checkpoint.StatsUpdatedAt.Equal(now) {
		t.Fatalf("expected overview stats_updated_at %s, got %+v", now, checkpoint.StatsUpdatedAt)
	}
}

func TestUsageOverviewAggregationRollsBackWhenDailyInsertAndRetryBothMiss(t *testing.T) {
	// 准备：写入一条事件，并用 trigger 强制旧 daily rollup 的首次 INSERT 失败。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)
	events := []entities.UsageEvent{{EventKey: "overview-daily-failure", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-time.Minute), InputTokens: 10, CachedTokens: 7, CacheReadTokens: 2, TotalTokens: 12}}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert daily failure event: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_overview_daily_insert
		BEFORE INSERT ON usage_overview_daily_stats
		BEGIN
			SELECT RAISE(ABORT, 'forced overview daily insert failure');
		END`).Error; err != nil {
		t.Fatalf("create daily failure trigger: %v", err)
	}

	// 执行：运行一个包含 hourly、daily 和旧 checkpoint 的 Overview 事务。
	err := repository.AggregateUsageOverviewStats(context.Background(), db, now)

	// 断言：daily INSERT 与 retry UPDATE 都未落行时必须返回错误，并回滚 hourly 与旧 checkpoint。
	if err == nil {
		t.Fatal("expected overview aggregation error after forced daily insert failure")
	}
	var hourlyCount int64
	if countErr := db.Model(&entities.UsageOverviewHourlyStat{}).Count(&hourlyCount).Error; countErr != nil {
		t.Fatalf("count rolled back hourly rows: %v", countErr)
	}
	var checkpointCount int64
	if countErr := db.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&checkpointCount).Error; countErr != nil {
		t.Fatalf("count rolled back overview checkpoints: %v", countErr)
	}
	if hourlyCount != 0 || checkpointCount != 0 {
		t.Fatalf("overview failure committed partial state: hourly=%d checkpoints=%d", hourlyCount, checkpointCount)
	}
}

func assertUsageOverviewParityFields(t *testing.T, name string, requests, success, failure, input, output, reasoning, cached, cacheRead, cacheCreation, total int64) {
	// 每个旧字段使用固定期望值，任何异步拆分造成的遗漏或重复都会直接失败。
	t.Helper()
	if requests != 2 || success != 1 || failure != 1 || input != 300 || output != 50 || reasoning != 11 || cached != 170 || cacheRead != 30 || cacheCreation != 7 || total != 398 {
		t.Fatalf("unexpected %s parity fields: requests=%d success=%d failure=%d input=%d output=%d reasoning=%d cached=%d cacheRead=%d cacheCreation=%d total=%d", name, requests, success, failure, input, output, reasoning, cached, cacheRead, cacheCreation, total)
	}
}
