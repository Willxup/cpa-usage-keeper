package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const usageAggregationCheckpointsMigrationVersion = "20260726_usage_aggregation_checkpoints"

func TestUsageAggregationCheckpointsMigrationCopiesLegacyRowsAndDropsLegacyTables(t *testing.T) {
	// 固定旧行时间，确保 migration 复制的是原值而不是执行时刻。
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.Local)
	overviewStatsAt := now.Add(-2 * time.Hour)
	activityStatsAt := now.Add(-time.Hour)
	overviewCreatedAt := now.Add(-4 * time.Hour)
	overviewUpdatedAt := now.Add(-3 * time.Hour)
	activityCreatedAt := now.Add(-3 * time.Hour)
	activityUpdatedAt := now.Add(-2 * time.Hour)
	db := openUsageAggregationCheckpointMigrationDatabase(t, "copy.db")
	createLegacyUsageAggregationCheckpointTables(t, db)
	markOnlyUsageAggregationCheckpointsMigrationPending(t, db)

	// 两张旧表各放一行不同水位，证明迁移按名称映射而不是取最大值。
	if err := db.Create(&entities.UsageOverviewAggregationCheckpoint{ID: 11, Name: "overview", LastAggregatedUsageEventID: 101, StatsUpdatedAt: &overviewStatsAt, CreatedAt: overviewCreatedAt, UpdatedAt: overviewUpdatedAt}).Error; err != nil {
		t.Fatalf("seed overview checkpoint: %v", err)
	}
	if err := db.Create(&entities.UsageActivityAggregationCheckpoint{ID: 22, Name: "activity", LastAggregatedUsageEventID: 202, StatsUpdatedAt: &activityStatsAt, CreatedAt: activityCreatedAt, UpdatedAt: activityUpdatedAt}).Error; err != nil {
		t.Fatalf("seed activity checkpoint: %v", err)
	}

	// 只运行新 migration；成功后旧表和新表不能并存为两套事实来源。
	if err := migration.Run(db); err != nil {
		t.Fatalf("run checkpoint migration: %v", err)
	}
	if db.Migrator().HasTable(&entities.UsageOverviewAggregationCheckpoint{}) || db.Migrator().HasTable(&entities.UsageActivityAggregationCheckpoint{}) {
		t.Fatal("expected legacy checkpoint tables to be dropped")
	}

	// Overview、Activity 原时间和水位必须完整复制；Latency 首次初始化为 0。
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointOverview, 101, &overviewStatsAt)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointActivity, 202, &activityStatsAt)
	assertUsageAggregationCheckpointTimestamps(t, db, entities.UsageAggregationCheckpointOverview, overviewCreatedAt, overviewUpdatedAt)
	assertUsageAggregationCheckpointTimestamps(t, db, entities.UsageAggregationCheckpointActivity, activityCreatedAt, activityUpdatedAt)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointLatency, 0, nil)
	assertUsageAggregationCheckpointMigrationApplied(t, db, true)
}

func assertUsageAggregationCheckpointTimestamps(t *testing.T, db *gorm.DB, name entities.UsageAggregationCheckpointName, createdAt time.Time, updatedAt time.Time) {
	// migration 必须保留旧 checkpoint 生命周期时间，不能用迁移执行时刻覆盖审计信息。
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", name).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load %s checkpoint timestamps: %v", name, err)
	}
	if !checkpoint.CreatedAt.Equal(createdAt) || !checkpoint.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("expected %s timestamps created=%s updated=%s, got created=%s updated=%s", name, createdAt, updatedAt, checkpoint.CreatedAt, checkpoint.UpdatedAt)
	}
}

func TestUsageAggregationCheckpointsMigrationInitializesMissingLegacyRowsAtZero(t *testing.T) {
	// Overview 旧表缺失、Activity 旧表为空都属于支持的历史形态。
	db := openUsageAggregationCheckpointMigrationDatabase(t, "missing.db")
	if err := db.AutoMigrate(&entities.UsageActivityAggregationCheckpoint{}); err != nil {
		t.Fatalf("create empty activity checkpoint table: %v", err)
	}
	markOnlyUsageAggregationCheckpointsMigrationPending(t, db)

	// migration 必须创建三行零水位，不能要求旧表预先完整存在。
	if err := migration.Run(db); err != nil {
		t.Fatalf("run missing checkpoint migration: %v", err)
	}
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointOverview, 0, nil)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointActivity, 0, nil)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointLatency, 0, nil)
}

func TestUsageAggregationCheckpointsMigrationRollsBackConflictingCommonRow(t *testing.T) {
	// 通用表若已存在不同 cursor，迁移不能覆盖可能更新的事实水位。
	now := time.Date(2026, 7, 26, 8, 0, 0, 0, time.Local)
	db := openUsageAggregationCheckpointMigrationDatabase(t, "conflict.db")
	createLegacyUsageAggregationCheckpointTables(t, db)
	if err := db.AutoMigrate(&entities.UsageAggregationCheckpoint{}); err != nil {
		t.Fatalf("create common checkpoint table: %v", err)
	}
	markOnlyUsageAggregationCheckpointsMigrationPending(t, db)
	if err := db.Create(&entities.UsageOverviewAggregationCheckpoint{Name: "overview", LastAggregatedUsageEventID: 10, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed legacy overview checkpoint: %v", err)
	}
	if err := db.Create(&entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 11, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed conflicting common checkpoint: %v", err)
	}

	// 不一致必须让默认 migration 事务失败，旧表和版本标记均保留。
	if err := migration.Run(db); err == nil {
		t.Fatal("expected conflicting common checkpoint migration to fail")
	}
	if !db.Migrator().HasTable(&entities.UsageOverviewAggregationCheckpoint{}) {
		t.Fatal("expected legacy overview table to remain after rollback")
	}
	assertUsageAggregationCheckpointMigrationApplied(t, db, false)
}

func openUsageAggregationCheckpointMigrationDatabase(t *testing.T, name string) *gorm.DB {
	// 每个用例使用独立文件，保证 SQLite migration 事务和表删除语义真实执行。
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open checkpoint migration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load checkpoint migration sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func createLegacyUsageAggregationCheckpointTables(t *testing.T, db *gorm.DB) {
	// 旧 Overview/Activity 类型仅用于构造升级前 schema。
	t.Helper()
	if err := db.AutoMigrate(&entities.UsageOverviewAggregationCheckpoint{}, &entities.UsageActivityAggregationCheckpoint{}); err != nil {
		t.Fatalf("create legacy checkpoint tables: %v", err)
	}
}

func markOnlyUsageAggregationCheckpointsMigrationPending(t *testing.T, db *gorm.DB) {
	// 先标记完整迁移列表，再只删除当前版本，避免测试重放无关历史 schema。
	t.Helper()
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", usageAggregationCheckpointsMigrationVersion).Error; err != nil {
		t.Fatalf("mark checkpoint migration pending: %v", err)
	}
}

func assertUsageAggregationCheckpoint(t *testing.T, db *gorm.DB, name entities.UsageAggregationCheckpointName, cursor int64, statsUpdatedAt *time.Time) {
	// 统一按主键读取，确保每类最终只有一行。
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", name).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load %s checkpoint: %v", name, err)
	}
	if checkpoint.LastAggregatedUsageEventID != cursor {
		t.Fatalf("expected %s cursor %d, got %+v", name, cursor, checkpoint)
	}
	if statsUpdatedAt == nil {
		if checkpoint.StatsUpdatedAt != nil {
			t.Fatalf("expected %s stats_updated_at nil, got %v", name, checkpoint.StatsUpdatedAt)
		}
		return
	}
	if checkpoint.StatsUpdatedAt == nil || !checkpoint.StatsUpdatedAt.Equal(*statsUpdatedAt) {
		t.Fatalf("expected %s stats_updated_at %s, got %v", name, statsUpdatedAt, checkpoint.StatsUpdatedAt)
	}
}

func assertUsageAggregationCheckpointMigrationApplied(t *testing.T, db *gorm.DB, want bool) {
	// schema_migrations 是迁移完成的唯一持久化标记。
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageAggregationCheckpointsMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count checkpoint migration version: %v", err)
	}
	if got := count == 1; got != want {
		t.Fatalf("expected checkpoint migration applied=%t, got count=%d", want, count)
	}
}
