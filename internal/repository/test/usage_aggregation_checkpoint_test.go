package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestLoadUsageAggregationCheckpointSnapshotTreatsMissingRowsAsZero(t *testing.T) {
	// 新库允许通用 checkpoint 表暂时没有任何行，读取不能为此产生 writer I/O。
	db := openTestDatabase(t)

	// 读取空表时应直接得到三个零水位，供首次聚合从 usage event ID 0 开始。
	snapshot, err := repository.LoadUsageAggregationCheckpointSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadUsageAggregationCheckpointSnapshot returned error: %v", err)
	}

	// 三个字段分别断言，防止漏填某一类时被结构体零值偶然掩盖。
	if snapshot.OverviewCursor != 0 || snapshot.ActivityCursor != 0 || snapshot.LatencyCursor != 0 {
		t.Fatalf("expected missing checkpoints to read as zero, got %+v", snapshot)
	}
	if !snapshot.Equal() {
		t.Fatalf("expected three zero checkpoints to be equal, got %+v", snapshot)
	}
}

func TestAdvanceUsageAggregationCheckpointCreatesAndConditionallyAdvances(t *testing.T) {
	// 推进函数必须同时支持缺行初始化和 expected cursor 并发保护。
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 9, 30, 0, 0, time.Local)

	// 第一次推进在同一事务内补出 overview 行，并从约定的 0 水位推进到 5。
	if err := db.Transaction(func(tx *gorm.DB) error {
		return repository.AdvanceUsageAggregationCheckpoint(context.Background(), tx, entities.UsageAggregationCheckpointOverview, 0, 5, now)
	}); err != nil {
		t.Fatalf("AdvanceUsageAggregationCheckpoint returned error: %v", err)
	}

	// 已提交行必须保存精确 cursor 与统计时间，不能只依赖 UpdatedAt 推断进度。
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load created checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 5 || checkpoint.StatsUpdatedAt == nil || !checkpoint.StatsUpdatedAt.Equal(now) {
		t.Fatalf("unexpected created checkpoint: %+v", checkpoint)
	}

	// 旧调用方仍声称 expected=0 时必须得到水位冲突，禁止覆盖已经提交的 5。
	err := db.Transaction(func(tx *gorm.DB) error {
		return repository.AdvanceUsageAggregationCheckpoint(context.Background(), tx, entities.UsageAggregationCheckpointOverview, 0, 9, now.Add(time.Minute))
	})
	if err == nil {
		t.Fatal("expected stale checkpoint advance to fail")
	}

	// 冲突事务不能改变已有 cursor 或 StatsUpdatedAt。
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatalf("reload checkpoint after conflict: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 5 || checkpoint.StatsUpdatedAt == nil || !checkpoint.StatsUpdatedAt.Equal(now) {
		t.Fatalf("stale advance changed checkpoint: %+v", checkpoint)
	}
}

func TestApplyUsageOverviewAggregationPageRollsBackRowsOnCursorConflict(t *testing.T) {
	// 预置比调用方更新的 overview 水位，模拟另一轮已经先提交。
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.Local)
	if err := db.Create(&entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 5, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed overview checkpoint: %v", err)
	}

	// 调用方构建出的 rows 本身有效，但 expected=0 已经过期。
	hourly := []entities.UsageOverviewHourlyStat{{BucketStart: now, APIGroupKey: "key-a", Model: "model-a", RequestCount: 1}}
	daily := []entities.UsageOverviewDailyStat{{BucketStart: now, APIGroupKey: "key-a", Model: "model-a", RequestCount: 1}}
	err := repository.ApplyUsageOverviewAggregationPage(context.Background(), db, 0, 1, hourly, daily, now)
	if err == nil {
		t.Fatal("expected overview apply to fail on stale cursor")
	}

	// cursor 条件更新失败必须回滚同一事务里已经写入的 hourly/daily rows。
	var hourlyCount, dailyCount int64
	if err := db.Model(&entities.UsageOverviewHourlyStat{}).Count(&hourlyCount).Error; err != nil {
		t.Fatalf("count overview hourly rows: %v", err)
	}
	if err := db.Model(&entities.UsageOverviewDailyStat{}).Count(&dailyCount).Error; err != nil {
		t.Fatalf("count overview daily rows: %v", err)
	}
	if hourlyCount != 0 || dailyCount != 0 {
		t.Fatalf("stale overview apply committed rows: hourly=%d daily=%d", hourlyCount, dailyCount)
	}
}

func TestApplyUsageActivityAggregationPageRollsBackRowsOnCursorConflict(t *testing.T) {
	// Activity 使用同一条件推进规则，但保持自己的独立事务和 checkpoint 行。
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.Local)
	if err := db.Create(&entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointActivity, LastAggregatedUsageEventID: 7, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed activity checkpoint: %v", err)
	}

	// 构造一行有效 daily Activity，expected=0 刻意制造事务末尾的 cursor 冲突。
	rows := []entities.UsageActivityStat{{Grain: entities.UsageActivityGrainDaily, BucketStart: now, BucketEnd: now.Add(24 * time.Hour), APIGroupKey: "key-a", SuccessCount: 1}}
	err := repository.ApplyUsageActivityAggregationPage(context.Background(), db, 0, 1, rows, now)
	if err == nil {
		t.Fatal("expected activity apply to fail on stale cursor")
	}

	// Activity row 与 cursor 必须原子回滚，不能留下重复累计的基础。
	var rowCount int64
	if err := db.Model(&entities.UsageActivityStat{}).Count(&rowCount).Error; err != nil {
		t.Fatalf("count activity rows: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("stale activity apply committed %d rows", rowCount)
	}
}
