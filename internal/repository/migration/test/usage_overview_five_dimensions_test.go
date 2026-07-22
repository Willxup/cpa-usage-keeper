package test

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const usageOverviewFiveDimensionsMigrationVersion = "20260723_usage_overview_five_dimensions"

type usageOverviewFiveDimensionRow struct {
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
	RequestCount        int64
	TotalTokens         int64
}

func TestUsageOverviewFiveDimensionsMigrationRebuildsFromCurrentUsageEvents(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "five-dimensions.db")), &gorm.Config{NowFunc: func() time.Time {
		return timeutil.NormalizeStorageTime(time.Now())
	}})
	if err != nil {
		t.Fatalf("open five-dimension migration database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	createLegacyUsageOverviewFiveDimensionSchema(t, db)
	seedUsageOverviewFiveDimensionMigrationData(t, db)

	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", usageOverviewFiveDimensionsMigrationVersion).Error; err != nil {
		t.Fatalf("enable five-dimension migration: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run five-dimension migration: %v", err)
	}

	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		for _, column := range []string{"service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type"} {
			if !db.Migrator().HasColumn(table, column) {
				t.Fatalf("expected %s.%s after migration", table, column)
			}
		}
		var staleCount int64
		if err := db.Table(table).Where("api_group_key = ?", "stale-api").Count(&staleCount).Error; err != nil {
			t.Fatalf("count stale %s rows: %v", table, err)
		}
		if staleCount != 0 {
			t.Fatalf("expected stale %s rows to be removed, got %d", table, staleCount)
		}
	}

	assertUsageOverviewFiveDimensionMigrationRows(t, db, "usage_overview_hourly_stats")
	assertUsageOverviewFiveDimensionMigrationRows(t, db, "usage_overview_daily_stats")
	assertUsageOverviewFiveDimensionIndex(t, db, "uniq_usage_overview_hourly_stats_dimensions")
	assertUsageOverviewFiveDimensionIndex(t, db, "uniq_usage_overview_daily_stats_dimensions")
	for _, oldIndex := range []string{
		"uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias",
		"uniq_usage_overview_daily_stats_bucket_api_model_auth_alias",
	} {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", oldIndex).Scan(&count).Error; err != nil {
			t.Fatalf("check old index %s: %v", oldIndex, err)
		}
		if count != 0 {
			t.Fatalf("expected old index %s to be removed", oldIndex)
		}
	}

	var checkpoint struct {
		LastAggregatedUsageEventID int64
	}
	if err := db.Table("usage_overview_aggregation_checkpoints").Select("last_aggregated_usage_event_id").Where("name = ?", "overview").Take(&checkpoint).Error; err != nil {
		t.Fatalf("load rebuilt overview checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 4 {
		t.Fatalf("expected rebuilt overview checkpoint 4, got %d", checkpoint.LastAggregatedUsageEventID)
	}

	var applied int64
	if err := db.Table("schema_migrations").Where("version = ?", usageOverviewFiveDimensionsMigrationVersion).Count(&applied).Error; err != nil {
		t.Fatalf("count five-dimension migration version: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected five-dimension migration to be recorded once, got %d", applied)
	}
}

func TestUsageOverviewFiveDimensionsMigrationRestartsCleanlyAfterBatchFailure(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "five-dimensions-retry.db")), &gorm.Config{NowFunc: func() time.Time {
		return timeutil.NormalizeStorageTime(time.Now())
	}})
	if err != nil {
		t.Fatalf("open five-dimension retry database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	createLegacyUsageOverviewFiveDimensionSchema(t, db)
	seedUsageOverviewFiveDimensionBatchEvents(t, db, 1001)

	// 最后一条事件单独形成第二批 row，并由 trigger 强制让该事务失败。
	if err := db.Exec(`CREATE TRIGGER fail_usage_overview_second_batch
		BEFORE UPDATE ON usage_overview_aggregation_checkpoints
		WHEN NEW.last_aggregated_usage_event_id > 1000
		BEGIN
			SELECT RAISE(FAIL, 'forced usage overview second batch failure');
		END`).Error; err != nil {
		t.Fatalf("create five-dimension failure trigger: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark retry migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", usageOverviewFiveDimensionsMigrationVersion).Error; err != nil {
		t.Fatalf("enable retry five-dimension migration: %v", err)
	}

	if err := migration.Run(db); err == nil {
		t.Fatal("expected second five-dimension batch to fail")
	}
	assertUsageOverviewMigrationCheckpoint(t, db, 1000)
	assertUsageOverviewMigrationVersionCount(t, db, 0)
	assertUsageOverviewMigrationRequestCount(t, db, "usage_overview_hourly_stats", 1000)
	assertUsageOverviewMigrationRequestCount(t, db, "usage_overview_daily_stats", 1000)

	// version 缺失时重跑会再次执行 setup：清空首批结果、checkpoint 归零后完整重建。
	if err := db.Exec("DROP TRIGGER fail_usage_overview_second_batch").Error; err != nil {
		t.Fatalf("drop five-dimension failure trigger: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun five-dimension migration: %v", err)
	}
	assertUsageOverviewMigrationCheckpoint(t, db, 1001)
	assertUsageOverviewMigrationVersionCount(t, db, 1)
	assertUsageOverviewMigrationRequestCount(t, db, "usage_overview_hourly_stats", 1001)
	assertUsageOverviewMigrationRequestCount(t, db, "usage_overview_daily_stats", 1001)
}

func seedUsageOverviewFiveDimensionBatchEvents(t *testing.T, db *gorm.DB, count int) {
	t.Helper()
	err := db.Transaction(func(tx *gorm.DB) error {
		for id := 1; id <= count; id++ {
			apiGroupKey := "api-a"
			if id == count {
				apiGroupKey = "fail-api"
			}
			if err := tx.Exec(`INSERT INTO usage_events (
				id, api_group_key, model, model_alias, auth_index, timestamp, failed,
				input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens,
				service_tier, response_service_tier, reasoning_effort, endpoint, executor_type
			) VALUES (?, ?, 'gpt-a', 'gpt-alias', 'auth-a', ?, 0, 1, 0, 0, 0, 0, 0, 1, 'default', 'default', 'xhigh', 'GET /v1/responses', 'CodexExecutor')`,
				id, apiGroupKey, timeutil.FormatStorageTime(time.Date(2026, 7, 20, 10, 10, 0, 0, time.UTC))).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed five-dimension batch events: %v", err)
	}
}

func assertUsageOverviewMigrationCheckpoint(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var checkpoint struct {
		LastAggregatedUsageEventID int64
	}
	if err := db.Table("usage_overview_aggregation_checkpoints").Select("last_aggregated_usage_event_id").Where("name = ?", "overview").Take(&checkpoint).Error; err != nil {
		t.Fatalf("load overview migration checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != want {
		t.Fatalf("expected overview migration checkpoint %d, got %d", want, checkpoint.LastAggregatedUsageEventID)
	}
}

func assertUsageOverviewMigrationVersionCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageOverviewFiveDimensionsMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count five-dimension migration version: %v", err)
	}
	if count != want {
		t.Fatalf("expected five-dimension migration version count %d, got %d", want, count)
	}
}

func assertUsageOverviewMigrationRequestCount(t *testing.T, db *gorm.DB, table string, want int64) {
	t.Helper()
	var total int64
	if err := db.Table(table).Select("COALESCE(SUM(request_count), 0)").Scan(&total).Error; err != nil {
		t.Fatalf("sum %s request count: %v", table, err)
	}
	if total != want {
		t.Fatalf("expected %s request count %d, got %d", table, want, total)
	}
}

func createLegacyUsageOverviewFiveDimensionSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	statColumns := `
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bucket_start DATETIME NOT NULL,
		api_group_key TEXT NOT NULL,
		model TEXT NOT NULL,
		auth_index TEXT NOT NULL DEFAULT '',
		model_alias TEXT NOT NULL DEFAULT '',
		request_count INTEGER NOT NULL DEFAULT 0,
		success_count INTEGER NOT NULL DEFAULT 0,
		failure_count INTEGER NOT NULL DEFAULT 0,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		cache_read_tokens INTEGER NOT NULL DEFAULT 0,
		cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL`
	statements := []string{
		`CREATE TABLE usage_events (
			id INTEGER PRIMARY KEY,
			api_group_key TEXT,
			model TEXT,
			model_alias TEXT,
			auth_index TEXT,
			timestamp DATETIME,
			failed NUMERIC,
			input_tokens INTEGER,
			output_tokens INTEGER,
			reasoning_tokens INTEGER,
			cached_tokens INTEGER,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER,
			service_tier TEXT NOT NULL DEFAULT '',
			response_service_tier TEXT NOT NULL DEFAULT '',
			reasoning_effort TEXT NOT NULL DEFAULT '',
			endpoint TEXT,
			executor_type TEXT NOT NULL DEFAULT ''
		)`,
		"CREATE TABLE usage_overview_hourly_stats (" + statColumns + ")",
		"CREATE TABLE usage_overview_daily_stats (" + statColumns + ")",
		`CREATE UNIQUE INDEX uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias ON usage_overview_hourly_stats (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`CREATE UNIQUE INDEX uniq_usage_overview_daily_stats_bucket_api_model_auth_alias ON usage_overview_daily_stats (bucket_start, api_group_key, model, auth_index, model_alias)`,
		`CREATE TABLE usage_overview_aggregation_checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			last_aggregated_usage_event_id INTEGER NOT NULL DEFAULT 0,
			stats_updated_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create legacy five-dimension schema: %v", err)
		}
	}
}

func seedUsageOverviewFiveDimensionMigrationData(t *testing.T, db *gorm.DB) {
	t.Helper()
	dayOne := time.Date(2026, 7, 20, 10, 10, 0, 0, time.UTC)
	dayTwo := time.Date(2026, 7, 21, 11, 10, 0, 0, time.UTC)
	type fixture struct {
		id                                                                int64
		timestamp                                                         time.Time
		serviceTier, responseTier, effort, executor                       string
		endpoint                                                          any
		input, output, reasoning, cached, cacheRead, cacheCreation, total int64
	}
	fixtures := []fixture{
		{1, dayOne, "default", "default", "xhigh", "CodexWebsocketsExecutor", "GET /v1/responses", 10, 2, 1, 7, 3, 1, 12},
		{2, dayOne.Add(10 * time.Minute), "priority", "priority", "max", "CodexExecutor", "POST /v1/responses", 20, 3, 2, 8, 4, 2, 23},
		{3, dayOne.Add(20 * time.Minute), " default ", " default ", " xhigh ", " CodexWebsocketsExecutor ", " GET /v1/responses ", 30, 4, 3, 9, 5, 3, 34},
		{4, dayTwo, "default", "default", "xhigh", "CodexWebsocketsExecutor", nil, 40, 5, 4, 10, 6, 4, 45},
	}
	for _, row := range fixtures {
		if err := db.Exec(`INSERT INTO usage_events (
			id, api_group_key, model, model_alias, auth_index, timestamp, failed,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens,
			service_tier, response_service_tier, reasoning_effort, endpoint, executor_type
		) VALUES (?, 'api-a', 'gpt-a', 'gpt-alias', 'auth-a', ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id, timeutil.FormatStorageTime(row.timestamp), row.input, row.output, row.reasoning, row.cached, row.cacheRead, row.cacheCreation, row.total,
			row.serviceTier, row.responseTier, row.effort, row.endpoint, row.executor).Error; err != nil {
			t.Fatalf("seed usage event %d: %v", row.id, err)
		}
	}
	now := timeutil.FormatStorageTime(dayTwo)
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		if err := db.Exec("INSERT INTO "+table+" (bucket_start, api_group_key, model, request_count, created_at, updated_at) VALUES (?, 'stale-api', 'stale-model', 99, ?, ?)", now, now, now).Error; err != nil {
			t.Fatalf("seed stale %s row: %v", table, err)
		}
	}
	if err := db.Exec(`INSERT INTO usage_overview_aggregation_checkpoints
		(name, last_aggregated_usage_event_id, stats_updated_at, created_at, updated_at)
		VALUES ('overview', 999, ?, ?, ?)`, now, now, now).Error; err != nil {
		t.Fatalf("seed stale overview checkpoint: %v", err)
	}
}

func assertUsageOverviewFiveDimensionMigrationRows(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var rows []usageOverviewFiveDimensionRow
	if err := db.Table(table).
		Select("service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, request_count, total_tokens").
		Order("bucket_start ASC, service_tier ASC").
		Find(&rows).Error; err != nil {
		t.Fatalf("load rebuilt %s rows: %v", table, err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rebuilt %s rows, got %d: %+v", table, len(rows), rows)
	}
	if got := rows[0]; got.ServiceTier != "default" || got.ResponseServiceTier != "default" || got.ReasoningEffort != "xhigh" || got.Endpoint != "GET /v1/responses" || got.ExecutorType != "CodexWebsocketsExecutor" || got.RequestCount != 2 || got.TotalTokens != 46 {
		t.Fatalf("unexpected first rebuilt %s row: %+v", table, got)
	}
	if got := rows[1]; got.ServiceTier != "priority" || got.ResponseServiceTier != "priority" || got.ReasoningEffort != "max" || got.Endpoint != "POST /v1/responses" || got.ExecutorType != "CodexExecutor" || got.RequestCount != 1 || got.TotalTokens != 23 {
		t.Fatalf("unexpected second rebuilt %s row: %+v", table, got)
	}
	if got := rows[2]; got.ServiceTier != "default" || got.ResponseServiceTier != "default" || got.ReasoningEffort != "xhigh" || got.Endpoint != "" || got.ExecutorType != "CodexWebsocketsExecutor" || got.RequestCount != 1 || got.TotalTokens != 45 {
		t.Fatalf("unexpected third rebuilt %s row: %+v", table, got)
	}
}

func assertUsageOverviewFiveDimensionIndex(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	type indexColumn struct {
		Seqno int
		Name  string
	}
	var rows []indexColumn
	if err := db.Raw("PRAGMA index_info(" + name + ")").Scan(&rows).Error; err != nil {
		t.Fatalf("load index %s columns: %v", name, err)
	}
	want := []string{"bucket_start", "api_group_key", "model", "auth_index", "model_alias", "service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type"}
	got := make([]string, len(rows))
	for index, row := range rows {
		got[index] = row.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected %s columns: got %v want %v", name, got, want)
	}
}
