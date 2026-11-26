package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
)

const quotaHistoryPersistedStatsMigrationVersion = "20261125_quota_history_persisted_stats"

func TestQuotaHistoryPersistedStatsMigrationUpgradesExistingSchemaAndIsIdempotent(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local)
	overviewStatsAt := now.Add(-2 * time.Hour)
	activityStatsAt := now.Add(-time.Hour)

	// 构造升级前的旧表结构：不含派生统计列，且 usage_aggregation_checkpoints 仅允许 3 类名称。
	for _, stmt := range []string{
		`CREATE TABLE quota_cycles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			auth_index TEXT NOT NULL,
			quota_key TEXT NOT NULL,
			window_seconds INTEGER NOT NULL,
			reset_at_source TEXT NOT NULL,
			window_started_at TEXT NOT NULL,
			reset_at TEXT NOT NULL,
			first_observed_at TEXT NOT NULL,
			last_observed_at TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE UNIQUE INDEX uniq_quota_cycles_identity ON quota_cycles (provider, auth_index, quota_key, window_seconds, reset_at)`,
		`CREATE TABLE quota_percent_segments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cycle_id INTEGER NOT NULL,
			remaining_percent INTEGER NOT NULL,
			first_observed_at TEXT NOT NULL,
			last_observed_at TEXT NOT NULL,
			observation_count INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			CONSTRAINT fk_quota_percent_segments_cycle FOREIGN KEY (cycle_id) REFERENCES quota_cycles(id) ON UPDATE CASCADE ON DELETE RESTRICT
		)`,
		`CREATE UNIQUE INDEX uniq_quota_percent_segments_cycle_percent ON quota_percent_segments (cycle_id, remaining_percent)`,
		`CREATE TABLE usage_aggregation_checkpoints (
			name TEXT PRIMARY KEY CHECK (name IN ('overview','activity','latency')),
			last_aggregated_usage_event_id INTEGER NOT NULL DEFAULT 0,
			stats_updated_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}

	if err := db.Exec(`INSERT INTO quota_cycles
		(provider, auth_index, quota_key, window_seconds, reset_at_source, window_started_at, reset_at, first_observed_at, last_observed_at, created_at, updated_at)
		VALUES ('codex', 'auth-1', 'rate_limit.primary_window', 18000, 'absolute', '2026-11-25 00:00:00.000000000+00:00', '2026-11-25 05:00:00.000000000+00:00', '2026-11-25 01:00:00.000000000+00:00', '2026-11-25 02:00:00.000000000+00:00', ?, ?)`,
		now, now).Error; err != nil {
		t.Fatalf("seed legacy quota cycle: %v", err)
	}
	if err := db.Exec(`INSERT INTO quota_percent_segments
		(cycle_id, remaining_percent, first_observed_at, last_observed_at, observation_count, created_at, updated_at)
		VALUES (1, 90, '2026-11-25 01:00:00.000000000+00:00', '2026-11-25 02:00:00.000000000+00:00', 3, ?, ?)`,
		now, now).Error; err != nil {
		t.Fatalf("seed legacy quota segment: %v", err)
	}
	for _, row := range []entities.UsageAggregationCheckpoint{
		{Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: 101, StatsUpdatedAt: &overviewStatsAt, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
		{Name: entities.UsageAggregationCheckpointActivity, LastAggregatedUsageEventID: 202, StatsUpdatedAt: &activityStatsAt, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{Name: entities.UsageAggregationCheckpointLatency, LastAggregatedUsageEventID: 303, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed legacy checkpoint %s: %v", row.Name, err)
		}
	}

	// 确认旧表确实拒绝 quota_history。
	if err := db.Create(&entities.UsageAggregationCheckpoint{
		Name:      entities.UsageAggregationCheckpointQuotaHistory,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err == nil {
		t.Fatal("expected legacy checkpoint check constraint to reject quota_history before migration")
	}

	runOnlyMigration(t, db, quotaHistoryPersistedStatsMigrationVersion)

	var cycle entities.QuotaCycle
	if err := db.Where("id = ?", 1).Take(&cycle).Error; err != nil {
		t.Fatalf("load upgraded quota cycle: %v", err)
	}
	if cycle.UsageReady || cycle.Dirty || cycle.CostStale || cycle.BoundaryVersion != 0 || cycle.DirtyVersion != 0 || cycle.Requests != 0 || cycle.TotalTokens != 0 {
		t.Fatalf("unexpected upgraded quota cycle defaults: %+v", cycle)
	}
	if !db.Migrator().HasIndex(&entities.QuotaCycle{}, "idx_quota_cycles_usage_sync") {
		t.Fatal("expected idx_quota_cycles_usage_sync index on quota_cycles")
	}

	var segment entities.QuotaPercentSegment
	if err := db.Where("id = ?", 1).Take(&segment).Error; err != nil {
		t.Fatalf("load upgraded quota segment: %v", err)
	}
	if segment.TransitionUsageReady || segment.TransitionRequests != 0 || segment.TransitionTotalTokens != 0 {
		t.Fatalf("unexpected upgraded quota segment defaults: %+v", segment)
	}

	// 原有三行水位必须完整保留，并新增 quota_history 零水位行。
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointOverview, 101, &overviewStatsAt)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointActivity, 202, &activityStatsAt)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointLatency, 303, nil)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointQuotaHistory, 0, nil)

	// 扩充后的 CHECK 约束仍应拒绝非法名称。
	if err := db.Create(&entities.UsageAggregationCheckpoint{
		Name:      "invalid_name",
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err == nil {
		t.Fatal("expected upgraded checkpoint check constraint to reject invalid_name")
	}

	// 推进 quota_history 游标并删除迁移记录后幂等重跑，不能覆盖已有进度。
	if err := db.Model(&entities.UsageAggregationCheckpoint{}).
		Where("name = ?", entities.UsageAggregationCheckpointQuotaHistory).
		Update("last_aggregated_usage_event_id", 456).Error; err != nil {
		t.Fatalf("advance quota_history checkpoint: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", quotaHistoryPersistedStatsMigrationVersion).Error; err != nil {
		t.Fatalf("delete migration version for idempotency check: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun migration idempotently: %v", err)
	}
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointOverview, 101, &overviewStatsAt)
	assertUsageAggregationCheckpoint(t, db, entities.UsageAggregationCheckpointQuotaHistory, 456, nil)
}
