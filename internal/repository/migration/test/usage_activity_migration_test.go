package test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const usageActivityMigrationVersion = "20260719_usage_activity_stats"

type usageActivityTotals struct {
	SuccessCount        int64
	FailureCount        int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

func TestUsageActivityMigrationBackfillsRetainedGrainsWithoutChangingOverviewRollups(t *testing.T) {
	// 准备：固定项目时区，并构造旧 schema、retention 分层事件和旧聚合 sentinel。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })

	// 创建只包含 migration 之前 schema 的旧数据库。
	db := openUsageActivityMigrationDatabase(t, "usage-activity.db")
	createUsageActivityLegacySchema(t, db)
	markOnlyUsageActivityMigrationPending(t, db)

	// 固定相对当前时间的事件，分别覆盖四种 retention gate。
	now := timeutil.NormalizeStorageTime(time.Now().UTC().Truncate(time.Second))
	events := []entities.UsageEvent{
		usageActivityMigrationEvent(1, " recent-group ", now.Add(-time.Hour), false, 100, 20, 5, 999, 10, 3, 138),
		usageActivityMigrationEvent(2, " recent-group ", now.Add(-30*time.Minute), true, 200, 30, 6, 888, 20, 4, 260),
		usageActivityMigrationEvent(3, " recent-group ", now.Add(-4*24*time.Hour), false, 300, 40, 7, 777, 30, 5, 382),
		usageActivityMigrationEvent(4, " recent-group ", now.Add(-10*24*time.Hour), false, 400, 50, 8, 666, 40, 6, 504),
		usageActivityMigrationEvent(5, " recent-group ", now.Add(-40*24*time.Hour), true, 500, 60, 9, 555, 50, 7, 626),
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}

	// 预置旧 Overview 聚合结果和 checkpoint，后续逐字段证明 migration 没有改动它们。
	seedUsageActivityOverviewSentinels(t, db, now)
	beforeHourly := loadUsageActivityHourlyRows(t, db)
	beforeDaily := loadUsageActivityDailyRows(t, db)
	beforeOverviewCheckpoint := loadUsageActivityOverviewCheckpoint(t, db)

	// 旧 Health 只用于确认最终 schema 已删除误导性的旧表。
	if err := db.Create(&usageActivityLegacyHealthStat{
		BucketStart: now.Add(-15 * time.Minute), SpanSeconds: 900, APIGroupKey: "legacy-health", SuccessCount: 7,
	}).Error; err != nil {
		t.Fatalf("seed legacy health row: %v", err)
	}

	// 执行：运行唯一待处理的 Activity migration。
	if err := migration.Run(db); err != nil {
		t.Fatalf("run usage activity migration: %v", err)
	}

	// 断言：最终 schema、四层 Activity、独立 checkpoint 与三个旧聚合快照全部符合约束。
	if !db.Migrator().HasTable(&entities.UsageActivityStat{}) {
		t.Fatal("expected usage_activity_stats after migration")
	}
	if db.Migrator().HasTable("usage_overview_health_stats") {
		t.Fatal("expected legacy usage_overview_health_stats to be removed")
	}
	// Activity 必须复用现有 Overview 的 API group 规范化语义，去掉 fixture 两侧空格。
	var normalizedGroupRows int64
	if err := db.Model(&entities.UsageActivityStat{}).Where("api_group_key = ?", "recent-group").Count(&normalizedGroupRows).Error; err != nil {
		t.Fatalf("count normalized activity group rows: %v", err)
	}
	if normalizedGroupRows == 0 {
		t.Fatal("expected trimmed recent-group activity rows")
	}

	// short 只包含最近 3 天内的两条事件，并忽略 cached_tokens。
	assertUsageActivityTotals(t, db, entities.UsageActivityGrainShort, usageActivityTotals{
		SuccessCount: 1, FailureCount: 1, InputTokens: 300, OutputTokens: 50, ReasoningTokens: 11,
		CacheReadTokens: 30, CacheCreationTokens: 7, TotalTokens: 398,
	})
	// medium 包含最近 8 天内的三条事件。
	assertUsageActivityTotals(t, db, entities.UsageActivityGrainMedium, usageActivityTotals{
		SuccessCount: 2, FailureCount: 1, InputTokens: 600, OutputTokens: 90, ReasoningTokens: 18,
		CacheReadTokens: 60, CacheCreationTokens: 12, TotalTokens: 780,
	})
	// long 包含最近 31 天内的四条事件。
	assertUsageActivityTotals(t, db, entities.UsageActivityGrainLong, usageActivityTotals{
		SuccessCount: 3, FailureCount: 1, InputTokens: 1000, OutputTokens: 140, ReasoningTokens: 26,
		CacheReadTokens: 100, CacheCreationTokens: 18, TotalTokens: 1284,
	})
	// daily 永久层回填数据库中仍存在的全部五条事件。
	assertUsageActivityTotals(t, db, entities.UsageActivityGrainDaily, usageActivityTotals{
		SuccessCount: 3, FailureCount: 2, InputTokens: 1500, OutputTokens: 200, ReasoningTokens: 35,
		CacheReadTokens: 150, CacheCreationTokens: 25, TotalTokens: 1910,
	})

	// Activity checkpoint 必须推进到 migration 开始时可见的最大 event ID。
	checkpoint := loadUsageActivityCheckpoint(t, db)
	if checkpoint.LastAggregatedUsageEventID != 5 {
		t.Fatalf("expected activity checkpoint 5, got %+v", checkpoint)
	}

	// Overview hourly/daily 和原 checkpoint 必须逐字段保持 migration 前快照。
	if after := loadUsageActivityHourlyRows(t, db); !reflect.DeepEqual(after, beforeHourly) {
		t.Fatalf("hourly rollups changed during activity migration:\n before=%+v\n after=%+v", beforeHourly, after)
	}
	if after := loadUsageActivityDailyRows(t, db); !reflect.DeepEqual(after, beforeDaily) {
		t.Fatalf("daily rollups changed during activity migration:\n before=%+v\n after=%+v", beforeDaily, after)
	}
	if after := loadUsageActivityOverviewCheckpoint(t, db); !reflect.DeepEqual(after, beforeOverviewCheckpoint) {
		t.Fatalf("overview checkpoint changed during activity migration:\n before=%+v\n after=%+v", beforeOverviewCheckpoint, after)
	}

	// migration 版本必须记录，第二次 Run 只能跳过且不能重复累计。
	assertUsageActivityMigrationApplied(t, db, true)
	beforeActivity := loadUsageActivityRows(t, db)
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun usage activity migration: %v", err)
	}
	if afterActivity := loadUsageActivityRows(t, db); !reflect.DeepEqual(afterActivity, beforeActivity) {
		t.Fatalf("activity rows changed after idempotent rerun:\n before=%+v\n after=%+v", beforeActivity, afterActivity)
	}
}

func TestUsageActivityMigrationResumesAfterCommittedBatchWithoutDoubleCounting(t *testing.T) {
	// 准备：固定项目时区，构造 1001 条事件和只阻断第二批的 SQLite trigger。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })

	// 预先创建最终 Activity 表，便于安装只阻断第二批的 SQLite trigger。
	db := openUsageActivityMigrationDatabase(t, "usage-activity-resume.db")
	createUsageActivityLegacySchema(t, db)
	if err := db.AutoMigrate(&entities.UsageActivityStat{}, &entities.UsageActivityAggregationCheckpoint{}); err != nil {
		t.Fatalf("create activity schema: %v", err)
	}
	markOnlyUsageActivityMigrationPending(t, db)

	// 第一批 1000 条使用 ok-group，第二批唯一事件使用 fail-group。
	now := timeutil.NormalizeStorageTime(time.Now().UTC().Truncate(time.Second))
	events := make([]entities.UsageEvent, 0, 1001)
	for id := 1; id <= 1001; id++ {
		apiGroupKey := "ok-group"
		if id == 1001 {
			apiGroupKey = "fail-group"
		}
		events = append(events, usageActivityMigrationEvent(int64(id), apiGroupKey, now.Add(-time.Hour), false, 1, 0, 0, 99, 1, 0, 1))
	}
	if err := db.CreateInBatches(&events, 200).Error; err != nil {
		t.Fatalf("seed batched usage events: %v", err)
	}
	if err := db.Create(&usageActivityLegacyHealthStat{BucketStart: now, SpanSeconds: 900, APIGroupKey: "legacy"}).Error; err != nil {
		t.Fatalf("seed legacy health row: %v", err)
	}

	// trigger 只让第二批 fail-group Activity INSERT 失败，第一批事务应已独立提交。
	if err := db.Exec(`CREATE TRIGGER fail_usage_activity_second_batch
		BEFORE INSERT ON usage_activity_stats
		WHEN NEW.api_group_key = 'fail-group'
		BEGIN
			SELECT RAISE(ABORT, 'forced usage activity migration failure');
		END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	// 执行：第一次 Run 在第二批强制失败，保留第一批已提交事务。
	if err := migration.Run(db); err == nil {
		t.Fatal("expected forced usage activity migration failure")
	}
	// 断言：checkpoint 只到 1000，旧 Health 和未完成 migration 标记都必须保留。
	checkpoint := loadUsageActivityCheckpoint(t, db)
	if checkpoint.LastAggregatedUsageEventID != 1000 {
		t.Fatalf("expected committed checkpoint 1000 after failure, got %+v", checkpoint)
	}
	if !db.Migrator().HasTable("usage_overview_health_stats") {
		t.Fatal("expected legacy health table to remain until migration catches up")
	}
	assertUsageActivityMigrationApplied(t, db, false)

	// 执行：移除故障后重跑，migration 从 1000 继续。
	if err := db.Exec("DROP TRIGGER fail_usage_activity_second_batch").Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("resume usage activity migration: %v", err)
	}

	// 断言：最终请求数精确等于 1001，旧 Health 删除且 migration 只记录一次。
	assertUsageActivityTotals(t, db, entities.UsageActivityGrainShort, usageActivityTotals{
		SuccessCount: 1001, InputTokens: 1001, CacheReadTokens: 1001, TotalTokens: 1001,
	})
	checkpoint = loadUsageActivityCheckpoint(t, db)
	if checkpoint.LastAggregatedUsageEventID != 1001 {
		t.Fatalf("expected final checkpoint 1001, got %+v", checkpoint)
	}
	if db.Migrator().HasTable("usage_overview_health_stats") {
		t.Fatal("expected legacy health table removed after successful resume")
	}
	assertUsageActivityMigrationApplied(t, db, true)
}

func TestUsageActivityMigrationKeepsHealthWhenCapturedTargetDisappears(t *testing.T) {
	// 准备：构造只有一条 raw event 的旧库，并让 checkpoint 首次创建时删除已经捕获的 target。
	db := openUsageActivityMigrationDatabase(t, "usage-activity-missing-target.db")
	createUsageActivityLegacySchema(t, db)
	markOnlyUsageActivityMigrationPending(t, db)
	now := timeutil.NormalizeStorageTime(time.Now().UTC().Truncate(time.Second))
	event := usageActivityMigrationEvent(1, "missing-target", now, false, 1, 0, 0, 9, 1, 0, 1)
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("seed missing target event: %v", err)
	}
	if err := db.Create(&usageActivityLegacyHealthStat{BucketStart: now, SpanSeconds: 900, APIGroupKey: "legacy"}).Error; err != nil {
		t.Fatalf("seed missing target health: %v", err)
	}
	if err := db.AutoMigrate(&entities.UsageActivityStat{}, &entities.UsageActivityAggregationCheckpoint{}); err != nil {
		t.Fatalf("create missing target Activity schema: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER delete_usage_activity_target_after_checkpoint
		AFTER INSERT ON usage_activity_aggregation_checkpoints
		BEGIN
			DELETE FROM usage_events WHERE id = 1;
		END`).Error; err != nil {
		t.Fatalf("create missing target trigger: %v", err)
	}

	// 执行：运行固定 target 已经消失的 Activity migration。
	err := migration.Run(db)

	// 断言：checkpoint 未达到 target 时 migration 必须失败，旧 Health 与未完成版本都要保留。
	if err == nil {
		t.Fatal("expected usage activity migration to reject an unreached target")
	}
	if !db.Migrator().HasTable("usage_overview_health_stats") {
		t.Fatal("expected legacy Health table to remain for unreached target")
	}
	assertUsageActivityMigrationApplied(t, db, false)
}

func openUsageActivityMigrationDatabase(t *testing.T, name string) *gorm.DB {
	// 每个用例使用独立磁盘 SQLite 文件，覆盖真实 migration 事务行为。
	t.Helper()
	dsn := filepath.Join(t.TempDir(), name) + "?_busy_timeout=5000&_foreign_keys=on"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) }})
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		if dbErr != nil {
			t.Errorf("load sql db: %v", dbErr)
			return
		}
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close sql db: %v", closeErr)
		}
	})
	return db
}

func createUsageActivityLegacySchema(t *testing.T, db *gorm.DB) {
	// 旧库保留 usage_events、Overview rollups、旧 Health 和 Overview checkpoint。
	t.Helper()
	if err := db.AutoMigrate(
		&entities.UsageEvent{},
		&entities.UsageOverviewHourlyStat{},
		&entities.UsageOverviewDailyStat{},
		&usageActivityLegacyHealthStat{},
		&entities.UsageOverviewAggregationCheckpoint{},
	); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
}

type usageActivityLegacyHealthStat struct {
	// ID 是测试旧 Health 行的自增主键。
	ID int64 `gorm:"primaryKey"`
	// BucketStart 复刻历史表的时间桶起点。
	BucketStart time.Time `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_overview_health_stats_bucket_span_api,priority:1"`
	// SpanSeconds 复刻历史表的固定跨度。
	SpanSeconds int64 `gorm:"not null;uniqueIndex:uniq_usage_overview_health_stats_bucket_span_api,priority:2"`
	// APIGroupKey 复刻历史表唯一业务维度。
	APIGroupKey string `gorm:"not null;uniqueIndex:uniq_usage_overview_health_stats_bucket_span_api,priority:3"`
	// SuccessCount 保存测试迁移前成功数。
	SuccessCount int64 `gorm:"not null;default:0"`
	// FailureCount 保存测试迁移前失败数。
	FailureCount int64 `gorm:"not null;default:0"`
}

func (usageActivityLegacyHealthStat) TableName() string {
	// 测试结构必须精确指向历史表名，避免重新引入业务 entity。
	return "usage_overview_health_stats"
}

func markOnlyUsageActivityMigrationPending(t *testing.T, db *gorm.DB) {
	// 先把当前 migration 列表全部标记完成，再单独打开 Activity migration。
	t.Helper()
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", usageActivityMigrationVersion).Error; err != nil {
		t.Fatalf("enable usage activity migration: %v", err)
	}
}

func usageActivityMigrationEvent(id int64, apiGroupKey string, timestamp time.Time, failed bool, input, output, reasoning, cached, cacheRead, cacheCreation, total int64) entities.UsageEvent {
	// 每个 fixture 显式分离 cached_tokens 与 cache_read_tokens，防止 Activity 误用兼容字段。
	return entities.UsageEvent{
		ID: id, EventKey: fmt.Sprintf("activity-event-%d", id), APIGroupKey: apiGroupKey,
		Model: "activity-model", Timestamp: timestamp, Failed: failed,
		InputTokens: input, OutputTokens: output, ReasoningTokens: reasoning, CachedTokens: cached,
		CacheReadTokens: cacheRead, CacheCreationTokens: cacheCreation, TotalTokens: total,
	}
}

func seedUsageActivityOverviewSentinels(t *testing.T, db *gorm.DB, now time.Time) {
	// sentinel 使用所有旧字段，确保 migration 不会只保留部分 Overview 数据。
	t.Helper()
	hourStart := now.Truncate(time.Hour)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	hourly := entities.UsageOverviewHourlyStat{
		BucketStart: hourStart, APIGroupKey: "sentinel", Model: "model", AuthIndex: "auth", ModelAlias: "alias",
		RequestCount: 11, SuccessCount: 7, FailureCount: 4, InputTokens: 101, OutputTokens: 102,
		ReasoningTokens: 103, CachedTokens: 104, CacheReadTokens: 105, CacheCreationTokens: 106, TotalTokens: 107,
	}
	daily := entities.UsageOverviewDailyStat{
		BucketStart: dayStart, APIGroupKey: "sentinel", Model: "model", AuthIndex: "auth", ModelAlias: "alias",
		RequestCount: 21, SuccessCount: 17, FailureCount: 4, InputTokens: 201, OutputTokens: 202,
		ReasoningTokens: 203, CachedTokens: 204, CacheReadTokens: 205, CacheCreationTokens: 206, TotalTokens: 207,
	}
	checkpoint := entities.UsageOverviewAggregationCheckpoint{
		Name: "overview", LastAggregatedUsageEventID: 4242, StatsUpdatedAt: &now,
	}
	if err := db.Create(&hourly).Error; err != nil {
		t.Fatalf("seed hourly sentinel: %v", err)
	}
	if err := db.Create(&daily).Error; err != nil {
		t.Fatalf("seed daily sentinel: %v", err)
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatalf("seed overview checkpoint sentinel: %v", err)
	}
}

func assertUsageActivityTotals(t *testing.T, db *gorm.DB, grain entities.UsageActivityGrain, want usageActivityTotals) {
	// 汇总同一 grain 的稀疏行，避免测试依赖事件恰好落入几个 bucket。
	t.Helper()
	var got usageActivityTotals
	if err := db.Model(&entities.UsageActivityStat{}).
		Select(`COALESCE(SUM(success_count), 0) AS success_count,
			COALESCE(SUM(failure_count), 0) AS failure_count,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
			COALESCE(SUM(total_tokens), 0) AS total_tokens`).
		Where("grain = ?", grain).
		Scan(&got).Error; err != nil {
		t.Fatalf("sum %s activity rows: %v", grain, err)
	}
	if got != want {
		t.Fatalf("unexpected %s totals:\n got=%+v\nwant=%+v", grain, got, want)
	}
}

func loadUsageActivityRows(t *testing.T, db *gorm.DB) []entities.UsageActivityStat {
	// 稳定排序后快照 Activity rows，用于证明重跑幂等。
	t.Helper()
	var rows []entities.UsageActivityStat
	if err := db.Order("grain asc, bucket_start asc, api_group_key asc").Find(&rows).Error; err != nil {
		t.Fatalf("load activity rows: %v", err)
	}
	return rows
}

func loadUsageActivityHourlyRows(t *testing.T, db *gorm.DB) []entities.UsageOverviewHourlyStat {
	// 读取完整 hourly 行，不遗漏旧 cached_tokens 等兼容字段。
	t.Helper()
	var rows []entities.UsageOverviewHourlyStat
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("load hourly rows: %v", err)
	}
	return rows
}

func loadUsageActivityDailyRows(t *testing.T, db *gorm.DB) []entities.UsageOverviewDailyStat {
	// 读取完整 daily 行，不遗漏旧 cached_tokens 等兼容字段。
	t.Helper()
	var rows []entities.UsageOverviewDailyStat
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("load daily rows: %v", err)
	}
	return rows
}

func loadUsageActivityOverviewCheckpoint(t *testing.T, db *gorm.DB) entities.UsageOverviewAggregationCheckpoint {
	// 单独读取 Overview checkpoint，证明 Activity migration 没有推进旧 cursor。
	t.Helper()
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", "overview").Take(&checkpoint).Error; err != nil {
		t.Fatalf("load overview checkpoint: %v", err)
	}
	return checkpoint
}

func loadUsageActivityCheckpoint(t *testing.T, db *gorm.DB) entities.UsageActivityAggregationCheckpoint {
	// Activity migration 只允许推进自己 name=activity 的独立 cursor。
	t.Helper()
	var checkpoint entities.UsageActivityAggregationCheckpoint
	if err := db.Where("name = ?", "activity").Take(&checkpoint).Error; err != nil {
		t.Fatalf("load activity checkpoint: %v", err)
	}
	return checkpoint
}

func assertUsageActivityMigrationApplied(t *testing.T, db *gorm.DB, want bool) {
	// schema_migrations 是判断整个 migration 是否完成的最终标志。
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageActivityMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count usage activity migration version: %v", err)
	}
	if got := count == 1; got != want {
		t.Fatalf("usage activity migration applied=%v, want %v", got, want)
	}
}
