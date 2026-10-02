package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cpa-usage-keeper/internal/benchmark/capacity"
	_ "github.com/mattn/go-sqlite3"
)

const (
	pricingBenchLatest  = "latest"
	pricingBenchFiveDim = "five-dim"
)

type datasetSpec struct {
	Path           string
	Events         int64
	Scenario       string
	Seed           uint64
	Anchor         time.Time
	Hot            int64
	Archive        int64
	Recent30       int64
	Models         int
	Rules          int
	TotalTokens    int64
	HourlyRequests int64
	DailyRequests  int64
	OverviewCursor int64
	Generated      capacity.DatasetResult
}

// preparePricingDataset 复用正式容量数据的时间与维度生成器，再把专用文件变成真实旧物理 schema。
// 转换前的生成成本不计入升级；本函数只处理调用者指定的全新文件，不覆盖既有数据。
func preparePricingDataset(ctx context.Context, path string, events int64, scenario string, seed uint64, anchor time.Time) (datasetSpec, error) {
	spec := datasetSpec{Path: path, Events: events, Scenario: scenario, Seed: seed, Anchor: anchor}
	if events < 120 || (scenario != pricingBenchLatest && scenario != pricingBenchFiveDim) {
		return spec, fmt.Errorf("pricing benchmark requires at least 120 events and scenario latest or five-dim")
	}
	if anchor.IsZero() || anchor.Location() != time.UTC || !anchor.Equal(anchor.Truncate(time.Hour)) {
		return spec, fmt.Errorf("pricing benchmark anchor must be an exact UTC hour")
	}
	if _, err := os.Lstat(path); err == nil {
		return spec, fmt.Errorf("pricing benchmark database already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return spec, fmt.Errorf("inspect pricing benchmark database: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return spec, err
	}
	spec.Archive = events / 4
	if scenario == pricingBenchFiveDim {
		spec.Archive = 0
	}
	spec.Hot = events - spec.Archive
	spec.Recent30 = spec.Hot / 3
	spec.Models = min(50, max(2, int(events/10)))
	identities := min(500, max(spec.Models, int(events/2)))
	apiKeys := min(50, max(2, spec.Models))
	archiveDays := 30
	if scenario == pricingBenchFiveDim {
		archiveDays = 0
	}
	generated, err := capacity.GenerateDataset(ctx, capacity.GenerateOptions{
		Path: path, HotEvents: spec.Hot, Recent30DayEvents: spec.Recent30,
		ArchiveEvents: spec.Archive, HotDays: 90, ArchiveDays: archiveDays,
		FailureRate: 0.01, Seed: seed, Now: anchor,
		Cardinality: capacity.Cardinality{Identities: identities, Models: spec.Models, APIKeys: apiKeys},
		TrafficTiers: []capacity.TrafficTier{
			{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
			{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
			{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
		},
		InsertBatchSize: 10_000, AggregatePage: 5_000, Vacuum: false,
	})
	if err != nil {
		return spec, fmt.Errorf("generate pricing benchmark dataset: %w", err)
	}
	spec.Generated = generated
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return spec, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=15000"); err != nil {
		return spec, err
	}
	// 旧配置表仍保留有效规则；模型数不能被当作规则数，实际行数单独记录。
	for modelID := 1; modelID <= spec.Models; modelID++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO model_price_rules
			(model_price_setting_id, key, value, multiplier, created_at, updated_at)
			VALUES (?, 'auth_index', 'bench-auth-0001', 1.05, ?, ?)`, modelID, anchor, anchor); err != nil {
			return spec, fmt.Errorf("seed old pricing rule for model %d: %w", modelID, err)
		}
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM model_price_rules").Scan(&spec.Rules); err != nil {
		return spec, err
	}
	if err := downgradePricingPhysicalSchema(ctx, db, scenario); err != nil {
		return spec, err
	}
	if err := verifyOldPricingPhysicalSchema(ctx, db, &spec); err != nil {
		return spec, err
	}
	return spec, nil
}

// downgradePricingPhysicalSchema 仅回退本次价格存储增列；five-dim 额外恢复两个旧 Overview 物理形态。
func downgradePricingPhysicalSchema(ctx context.Context, db *sql.DB, scenario string) error {
	for _, step := range []string{
		"DROP TABLE pricing_state", "DROP TABLE pricing_migration_state",
		"DELETE FROM schema_migrations WHERE version = '20261002_pricing_storage_structure'",
		"ALTER TABLE usage_events DROP COLUMN cost_usd",
		"ALTER TABLE usage_events DROP COLUMN cost_available",
		"ALTER TABLE usage_events_archive DROP COLUMN cost_usd",
		"ALTER TABLE usage_events_archive DROP COLUMN cost_available",
		"ALTER TABLE usage_overview_hourly_stats DROP COLUMN cost_usd",
		"ALTER TABLE usage_overview_hourly_stats DROP COLUMN unavailable_cost_count",
		"ALTER TABLE usage_overview_daily_stats DROP COLUMN cost_usd",
		"ALTER TABLE usage_overview_daily_stats DROP COLUMN unavailable_cost_count",
		"ALTER TABLE model_price_settings DROP COLUMN branches_json",
	} {
		if _, err := db.ExecContext(ctx, step); err != nil {
			return fmt.Errorf("project generated data to old pricing schema (%s): %w", step, err)
		}
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			return fmt.Errorf("checkpoint old schema projection: %w", err)
		}
	}
	if scenario == pricingBenchFiveDim {
		if err := downgradeFiveDimensionOverview(ctx, db); err != nil {
			return err
		}
	}
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=DELETE").Scan(&journalMode); err != nil {
		return fmt.Errorf("finalize old schema database journal mode: %w", err)
	}
	if journalMode != "delete" {
		return fmt.Errorf("finalize old schema database journal mode: got %s, want delete", journalMode)
	}
	return nil
}

// downgradeFiveDimensionOverview 创建按旧五键合并的物理桶及两个历史 checkpoint。
// Latency 在合并迁移时原本从零回填，因此同步移除现代统计并重跑它的历史迁移。
func downgradeFiveDimensionOverview(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		temporary := "pricingbench_old_" + table
		if _, err := db.ExecContext(ctx, `CREATE TABLE `+temporary+` (
			id INTEGER PRIMARY KEY AUTOINCREMENT, bucket_start DATETIME, api_group_key TEXT,
			model TEXT, auth_index TEXT, model_alias TEXT, request_count INTEGER,
			success_count INTEGER, failure_count INTEGER, input_tokens INTEGER,
			output_tokens INTEGER, reasoning_tokens INTEGER, cached_tokens INTEGER,
			cache_read_tokens INTEGER, cache_creation_tokens INTEGER, total_tokens INTEGER,
			created_at DATETIME, updated_at DATETIME
		)`); err != nil {
			return fmt.Errorf("create five-dimension %s: %w", table, err)
		}
		dimensions := "bucket_start, api_group_key, model, auth_index, model_alias"
		metrics := "request_count, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens"
		if _, err := db.ExecContext(ctx, `INSERT INTO `+temporary+` (`+dimensions+`, `+metrics+`, created_at, updated_at)
			SELECT `+dimensions+`, SUM(request_count), SUM(success_count), SUM(failure_count),
			SUM(input_tokens), SUM(output_tokens), SUM(reasoning_tokens), SUM(cached_tokens),
			SUM(cache_read_tokens), SUM(cache_creation_tokens), SUM(total_tokens),
			MIN(created_at), MAX(updated_at) FROM `+table+` GROUP BY `+dimensions); err != nil {
			return fmt.Errorf("merge ten-dimension %s into five keys: %w", table, err)
		}
		if _, err := db.ExecContext(ctx, "DROP TABLE "+table); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "ALTER TABLE "+temporary+" RENAME TO "+table); err != nil {
			return err
		}
		index := "uniq_" + table + "_bucket_api_model_auth_alias"
		if _, err := db.ExecContext(ctx, "CREATE UNIQUE INDEX "+index+" ON "+table+" ("+dimensions+")"); err != nil {
			return fmt.Errorf("restore old %s unique index: %w", table, err)
		}
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			return err
		}
	}
	for _, target := range []struct{ table, name string }{
		{"usage_overview_aggregation_checkpoints", "overview"},
		{"usage_activity_aggregation_checkpoints", "activity"},
	} {
		if _, err := db.ExecContext(ctx, `CREATE TABLE `+target.table+` (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
			last_aggregated_usage_event_id INTEGER NOT NULL DEFAULT 0,
			stats_updated_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
		)`); err != nil {
			return fmt.Errorf("restore old %s: %w", target.table, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO `+target.table+`
			(name, last_aggregated_usage_event_id, stats_updated_at, created_at, updated_at)
			SELECT name, last_aggregated_usage_event_id, stats_updated_at, created_at, updated_at
			FROM usage_aggregation_checkpoints WHERE name = ?`, target.name); err != nil {
			return fmt.Errorf("restore old %s row: %w", target.table, err)
		}
	}
	for _, step := range []string{
		"DROP TABLE usage_aggregation_checkpoints",
		"DROP TABLE usage_latency_stats",
		"DELETE FROM schema_migrations WHERE version IN ('20260723_usage_overview_five_dimensions', '20260726_usage_aggregation_checkpoints', '20260726_usage_latency_stats')",
	} {
		if _, err := db.ExecContext(ctx, step); err != nil {
			return fmt.Errorf("project five-dimension migration state: %w", err)
		}
	}
	return nil
}

// verifyOldPricingPhysicalSchema 在升级计时前核对真实列、版本、冷热行数与旧水位。
func verifyOldPricingPhysicalSchema(ctx context.Context, db *sql.DB, spec *datasetSpec) error {
	for _, table := range []string{"usage_events", "usage_events_archive", "usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		columns, err := pricingBenchColumns(ctx, db, table)
		if err != nil {
			return err
		}
		if columns["cost_usd"] || columns["cost_available"] || columns["unavailable_cost_count"] {
			return fmt.Errorf("%s still contains new pricing storage columns", table)
		}
		if spec.Scenario == pricingBenchFiveDim && (table == "usage_overview_hourly_stats" || table == "usage_overview_daily_stats") && columns["service_tier"] {
			return fmt.Errorf("%s still has ten-dimension fields", table)
		}
	}
	modelColumns, err := pricingBenchColumns(ctx, db, "model_price_settings")
	if err != nil {
		return err
	}
	if modelColumns["branches_json"] {
		return fmt.Errorf("old model price settings still have branches_json")
	}
	if spec.Scenario == pricingBenchFiveDim {
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			index := "uniq_" + table + "_bucket_api_model_auth_alias"
			var found int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("old five-dimension index %s missing: %d", index, found)
			}
		}
	}
	var control, applied int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('pricing_state','pricing_migration_state')").Scan(&control); err != nil {
		return err
	}
	if control != 0 {
		return fmt.Errorf("pricing control tables remain before old migration: %d", control)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version='20261002_pricing_storage_structure'").Scan(&applied); err != nil {
		return err
	}
	if applied != 0 {
		return fmt.Errorf("pricing storage migration remains applied: %d", applied)
	}
	for _, target := range []struct {
		table string
		want  int64
	}{{"usage_events", spec.Hot}, {"usage_events_archive", spec.Archive}} {
		var count int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+target.table).Scan(&count); err != nil {
			return err
		}
		if count != target.want {
			return fmt.Errorf("old %s rows=%d want=%d", target.table, count, target.want)
		}
		var tokens int64
		if err := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_tokens), 0) FROM "+target.table).Scan(&tokens); err != nil {
			return err
		}
		spec.TotalTokens += tokens
	}
	checkpointTable := "usage_aggregation_checkpoints"
	if spec.Scenario == pricingBenchFiveDim {
		checkpointTable = "usage_overview_aggregation_checkpoints"
	}
	if err := db.QueryRowContext(ctx, "SELECT last_aggregated_usage_event_id FROM "+checkpointTable+" WHERE name='overview'").Scan(&spec.OverviewCursor); err != nil {
		return fmt.Errorf("read old overview cursor: %w", err)
	}
	if spec.OverviewCursor != spec.Events {
		return fmt.Errorf("old overview cursor=%d want=%d", spec.OverviewCursor, spec.Events)
	}
	for _, target := range []struct {
		table string
		value *int64
	}{{"usage_overview_hourly_stats", &spec.HourlyRequests}, {"usage_overview_daily_stats", &spec.DailyRequests}} {
		if err := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(request_count), 0) FROM "+target.table).Scan(target.value); err != nil {
			return err
		}
		if *target.value != spec.Events {
			return fmt.Errorf("old %s requests=%d want=%d", target.table, *target.value, spec.Events)
		}
	}
	return nil
}

func pricingBenchColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var index, notNull, primaryKey int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&index, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}
