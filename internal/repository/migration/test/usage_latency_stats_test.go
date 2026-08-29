package test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/gorm"
)

const usageLatencyStatsMigrationVersion = "20260726_usage_latency_stats"

func TestUsageLatencyStatsMigrationMatchesRuntimeAggregation(t *testing.T) {
	// 相同事件页分别走 migration 和运行时 Apply，最终业务字段与 BLOB 必须完全一致。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		latencyMigrationEvent(1, "key-a", now.Add(-time.Hour), 100, 900),
		latencyMigrationEvent(2, "key-a", now.Add(-30*time.Minute), 200, 1200),
		latencyMigrationEvent(3, "key-b", now.Add(-15*time.Minute), 300, 1500),
	}

	// migration 数据库只具备新 migration 所需的最小前置 schema。
	migrationDB := openUsageLatencyMigrationDatabaseAt(t, "latency-migration.db", now)
	prepareUsageLatencyMigrationDatabase(t, migrationDB, events)
	if err := migration.Run(migrationDB); err != nil {
		t.Fatalf("run latency stats migration: %v", err)
	}
	migrationRows := loadUsageLatencyMigrationRows(t, migrationDB)
	assertUsageLatencyMigrationCursor(t, migrationDB, 3)
	assertUsageLatencyMigrationApplied(t, migrationDB, true)

	// 运行时数据库直接用同一 BuildRows 和 Apply 入口处理同一页。
	runtimeDB := openUsageLatencyMigrationDatabaseAt(t, "latency-runtime.db", now)
	if err := runtimeDB.AutoMigrate(&entities.UsageAggregationCheckpoint{}, &entities.UsageLatencyStat{}); err != nil {
		t.Fatalf("create runtime latency schema: %v", err)
	}
	runtimeRows, err := latency.BuildRows(events, now)
	if err != nil {
		t.Fatalf("build runtime latency rows: %v", err)
	}
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), runtimeDB, 0, 3, runtimeRows, now); err != nil {
		t.Fatalf("apply runtime latency page: %v", err)
	}
	assertUsageLatencyRowsEqual(t, migrationRows, loadUsageLatencyMigrationRows(t, runtimeDB))
}

func TestUsageLatencyStatsMigrationResumesAfterCommittedPage(t *testing.T) {
	// 1001 条事件强制形成 1000+1 两页，第二页 trigger 失败时第一页必须已经独立提交。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	events := make([]entities.UsageEvent, 0, 1001)
	for id := int64(1); id <= 1001; id++ {
		apiGroupKey := "ok-key"
		if id == 1001 {
			apiGroupKey = "fail-key"
		}
		events = append(events, latencyMigrationEvent(id, apiGroupKey, now.Add(-time.Minute), 100, 900))
	}
	db := openUsageLatencyMigrationDatabaseAt(t, "latency-resume.db", now)
	prepareUsageLatencyMigrationDatabase(t, db, events)
	if err := db.AutoMigrate(&entities.UsageLatencyStat{}); err != nil {
		t.Fatalf("create latency table before trigger: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_latency_second_page
		BEFORE INSERT ON usage_latency_stats
		WHEN NEW.api_group_key = 'fail-key'
		BEGIN
			SELECT RAISE(ABORT, 'forced latency migration failure');
		END`).Error; err != nil {
		t.Fatalf("create latency failure trigger: %v", err)
	}

	// 第一次运行在第二页失败，版本不记录，但第一页 rows/cursor 保留。
	if err := migration.Run(db); err == nil {
		t.Fatal("expected forced latency migration failure")
	}
	assertUsageLatencyMigrationCursor(t, db, 1000)
	assertUsageLatencyMigrationApplied(t, db, false)
	rowsAfterFailure := loadUsageLatencyMigrationRows(t, db)
	if len(rowsAfterFailure) != 2 || rowsAfterFailure[0].SampleCount != 1000 || rowsAfterFailure[1].SampleCount != 1000 {
		t.Fatalf("expected committed first-page hour/day rows, got %+v", rowsAfterFailure)
	}

	// 移除故障后从 cursor=1000 继续，不能再次累计前 1000 条。
	if err := db.Exec("DROP TRIGGER fail_latency_second_page").Error; err != nil {
		t.Fatalf("drop latency failure trigger: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("resume latency migration: %v", err)
	}
	assertUsageLatencyMigrationCursor(t, db, 1001)
	assertUsageLatencyMigrationApplied(t, db, true)
	var totalSamples int64
	for _, row := range loadUsageLatencyMigrationRows(t, db) {
		if row.BucketType == entities.UsageLatencyBucketHour {
			totalSamples += row.SampleCount
		}
	}
	if totalSamples != 1001 {
		t.Fatalf("expected exactly 1001 hourly samples after resume, got %d", totalSamples)
	}
}

func TestUsageLatencyStatsMigrationEnablesUsageEventArchive(t *testing.T) {
	// Latency migration 只有推进到当前最大事件 ID，才应与另外两类水位共同放行 raw archive。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		latencyMigrationEvent(1, "key-a", now.AddDate(0, 0, -92), 100, 900),
		latencyMigrationEvent(2, "key-a", now.AddDate(0, 0, -91), 200, 1200),
	}
	db := openUsageLatencyMigrationDatabaseAt(t, "latency-cleanup.db", now)
	prepareUsageLatencyMigrationDatabase(t, db, events)
	// Cleanup 后续步骤依赖这些当前实体表；保持空表即可，不掺入其它业务数据。
	if err := db.AutoMigrate(&entities.RedisUsageInbox{}, &entities.UsageActivityStat{}, &entities.UsageIdentity{}); err != nil {
		t.Fatalf("create cleanup support schema: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run latency migration before cleanup: %v", err)
	}
	// Overview/Activity 在真实升级中来自旧水位迁移；本用例固定它们已追平，只观察 Latency 门禁。
	if err := db.Create(&[]entities.UsageAggregationCheckpoint{
		{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 2, CreatedAt: now, UpdatedAt: now},
		{Name: entities.UsageAggregationCheckpointActivity, LastAggregatedUsageEventID: 2, CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed caught-up overview/activity checkpoints: %v", err)
	}

	result, err := repository.CleanupStorage(db, now)
	if err != nil {
		t.Fatalf("archive after latency migration: %v", err)
	}
	if result.UsageEventsArchived != 2 {
		t.Fatalf("expected latency migration to allow archiving two old events, got %+v", result)
	}
}

func prepareUsageLatencyMigrationDatabase(t *testing.T, db *gorm.DB, events []entities.UsageEvent) {
	// 只创建 migration 的真实前置表，避免无关历史 migration 参与结果。
	t.Helper()
	if err := db.AutoMigrate(&entities.UsageEvent{}, &entities.UsageEventArchive{}, &entities.UsageAggregationCheckpoint{}); err != nil {
		t.Fatalf("create latency migration schema: %v", err)
	}
	if err := db.CreateInBatches(&events, 200).Error; err != nil {
		t.Fatalf("seed latency migration events: %v", err)
	}
	if err := db.Create(&entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointLatency}).Error; err != nil {
		t.Fatalf("seed latency migration checkpoint: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark latency migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", usageLatencyStatsMigrationVersion).Error; err != nil {
		t.Fatalf("mark latency migration pending: %v", err)
	}
}

func latencyMigrationEvent(id int64, apiGroupKey string, timestamp time.Time, ttftMS, latencyMS int64) entities.UsageEvent {
	// migration fixture 直接给出最终合法事件字段，聚焦回填分页和可恢复事务。
	generate := true
	return entities.UsageEvent{ID: id, EventKey: fmt.Sprintf("latency-%d", id), APIGroupKey: apiGroupKey, Timestamp: timestamp, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: latencyMS}
}

func openUsageLatencyMigrationDatabaseAt(t *testing.T, name string, now time.Time) *gorm.DB {
	t.Helper()
	db := openUsageAggregationCheckpointMigrationDatabase(t, name)
	db.NowFunc = func() time.Time { return now }
	return db
}

func loadUsageLatencyMigrationRows(t *testing.T, db *gorm.DB) []entities.UsageLatencyStat {
	// 固定唯一键顺序，便于 migration/runtime 逐行比较。
	t.Helper()
	var rows []entities.UsageLatencyStat
	if err := db.Order("bucket_type asc, bucket_start asc, api_group_key asc").Find(&rows).Error; err != nil {
		t.Fatalf("load latency migration rows: %v", err)
	}
	return rows
}

func assertUsageLatencyRowsEqual(t *testing.T, expected, actual []entities.UsageLatencyStat) {
	// ID 和时间戳来自不同数据库，不参与业务一致性；其余字段必须完全相同。
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("latency row count mismatch: expected=%d actual=%d", len(expected), len(actual))
	}
	for index := range expected {
		left := expected[index]
		right := actual[index]
		left.ID, right.ID = 0, 0
		left.CreatedAt, right.CreatedAt = time.Time{}, time.Time{}
		left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(left, right) {
			t.Fatalf("latency row %d mismatch:\n migration=%+v\n runtime=%+v", index, left, right)
		}
	}
}

func assertUsageLatencyMigrationCursor(t *testing.T, db *gorm.DB, want int64) {
	// Latency migration 与运行时共享同一通用 checkpoint 行。
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointLatency).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load latency migration checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != want {
		t.Fatalf("expected latency migration cursor %d, got %+v", want, checkpoint)
	}
}

func assertUsageLatencyMigrationApplied(t *testing.T, db *gorm.DB, want bool) {
	// 未完成的分页回填不能提前写 schema_migrations 版本。
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageLatencyStatsMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count latency migration version: %v", err)
	}
	if got := count == 1; got != want {
		t.Fatalf("expected latency migration applied=%t, got count=%d", want, count)
	}
}
