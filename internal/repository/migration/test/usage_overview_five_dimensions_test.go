package test

import (
	"bytes"
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const usageOverviewFiveDimensionsMigrationVersion = "20260723_usage_overview_five_dimensions"

type usageOverviewFiveDimensionRow struct {
	ServiceTier         string
	ResponseServiceTier string
	ReasoningEffort     string
	Endpoint            string
	ExecutorType        string
	RequestCount        int64
	SuccessCount        int64
	FailureCount        int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

func TestUsageOverviewFiveDimensionsMigrationRebuildsFromCurrentUsageEvents(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	db.NowFunc = func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) }
	createLegacyUsageOverviewFiveDimensionSchema(t, db)
	seedUsageOverviewFiveDimensionMigrationData(t, db)

	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", usageOverviewFiveDimensionsMigrationVersion).Error; err != nil {
		t.Fatalf("enable five-dimension migration: %v", err)
	}
	var statements bytes.Buffer
	originalLogger := db.Logger
	db.Logger = gormlogger.New(log.New(&statements, "", 0), gormlogger.Config{LogLevel: gormlogger.Info})
	if err := migration.Run(db); err != nil {
		t.Fatalf("run five-dimension migration: %v", err)
	}
	db.Logger = originalLogger

	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {

		var staleCount int64
		if err := db.Table(table).Where("api_group_key = ?", "stale-api").Count(&staleCount).Error; err != nil {
			t.Fatalf("count stale %s rows: %v", table, err)
		}
		if staleCount != 0 {
			t.Fatalf("expected stale %s rows to be removed, got %d", table, staleCount)
		}
	}

	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		index := "uniq_" + table + "_dimensions"
		assertUsageOverviewFiveDimensionMigrationRows(t, db, table)
		assertUsageOverviewFiveDimensionIndex(t, db, table, index)
		assertUsageOverviewIndexCreatedAfterClear(t, statements.String(), table, index)
		oldIndex := "uniq_" + table + "_bucket_api_model_auth_alias"
		if db.Migrator().HasIndex(table, oldIndex) {
			t.Fatalf("expected old index %s to be removed", oldIndex)
		}
	}

	assertUsageOverviewMigrationCheckpoint(t, db, 4)
	assertUsageOverviewMigrationVersionCount(t, db, 1)
}

func TestUsageOverviewFiveDimensionsMigrationRestartsCleanlyAfterBatchFailure(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	db.NowFunc = func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) }
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
		failed                                                            bool
		input, output, reasoning, cached, cacheRead, cacheCreation, total int64
	}
	fixtures := []fixture{
		{1, dayOne, "default", "default", "xhigh", "CodexWebsocketsExecutor", "GET /v1/responses", false, 10, 2, 1, 7, 3, 1, 12},
		{2, dayOne.Add(10 * time.Minute), "priority", "priority", "max", "CodexExecutor", "POST /v1/responses", true, 20, 3, 2, 8, 4, 2, 23},
		{3, dayOne.Add(20 * time.Minute), " default ", " default ", " xhigh ", " CodexWebsocketsExecutor ", " GET /v1/responses ", false, 30, 4, 3, 9, 5, 3, 34},
		{4, dayTwo, "default", "default", "xhigh", "CodexWebsocketsExecutor", nil, false, 40, 5, 4, 10, 6, 4, 45},
	}
	for _, row := range fixtures {
		if err := db.Exec(`INSERT INTO usage_events (
			id, api_group_key, model, model_alias, auth_index, timestamp, failed,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens,
			service_tier, response_service_tier, reasoning_effort, endpoint, executor_type
		) VALUES (?, 'api-a', 'gpt-a', 'gpt-alias', 'auth-a', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id, timeutil.FormatStorageTime(row.timestamp), row.failed, row.input, row.output, row.reasoning, row.cached, row.cacheRead, row.cacheCreation, row.total,
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
		Select("service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, request_count, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens").
		Order("bucket_start ASC, service_tier ASC").
		Find(&rows).Error; err != nil {
		t.Fatalf("load rebuilt %s rows: %v", table, err)
	}
	want := []usageOverviewFiveDimensionRow{
		{ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", Endpoint: "GET /v1/responses", ExecutorType: "CodexWebsocketsExecutor",
			RequestCount: 2, SuccessCount: 2, InputTokens: 40, OutputTokens: 6, ReasoningTokens: 4, CachedTokens: 16, CacheReadTokens: 8, CacheCreationTokens: 4, TotalTokens: 46},
		{ServiceTier: "priority", ResponseServiceTier: "priority", ReasoningEffort: "max", Endpoint: "POST /v1/responses", ExecutorType: "CodexExecutor",
			RequestCount: 1, FailureCount: 1, InputTokens: 20, OutputTokens: 3, ReasoningTokens: 2, CachedTokens: 8, CacheReadTokens: 4, CacheCreationTokens: 2, TotalTokens: 23},
		{ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "xhigh", ExecutorType: "CodexWebsocketsExecutor",
			RequestCount: 1, SuccessCount: 1, InputTokens: 40, OutputTokens: 5, ReasoningTokens: 4, CachedTokens: 10, CacheReadTokens: 6, CacheCreationTokens: 4, TotalTokens: 45},
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("unexpected rebuilt %s rows:\n got=%+v\nwant=%+v", table, rows, want)
	}
}

func assertUsageOverviewFiveDimensionIndex(t *testing.T, db *gorm.DB, table string, name string) {
	t.Helper()
	var unique bool
	if err := db.Raw(`SELECT "unique" FROM pragma_index_list(?) WHERE name = ?`, table, name).Scan(&unique).Error; err != nil {
		t.Fatalf("look up %s index: %v", table, err)
	}
	if !unique {
		t.Fatalf("expected %s to be a unique index on %s", name, table)
	}
	var got []string
	if err := db.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", name).Scan(&got).Error; err != nil {
		t.Fatalf("load index %s columns: %v", name, err)
	}
	want := []string{"bucket_start", "api_group_key", "model", "auth_index", "model_alias", "service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type"}

	if !slices.Equal(got, want) {
		t.Fatalf("unexpected %s columns: got %v want %v", name, got, want)
	}
}

func assertUsageOverviewIndexCreatedAfterClear(t *testing.T, statements string, table string, index string) {
	t.Helper()
	normalized := strings.ToLower(statements)
	normalized = strings.NewReplacer("`", "", "\"", "").Replace(normalized)
	clearPosition := strings.Index(normalized, "delete from "+table)
	indexPosition := strings.Index(normalized, "create unique index "+index)
	if clearPosition < 0 || indexPosition < 0 {
		t.Fatalf("expected migration SQL to clear %s and create %s, got:\n%s", table, index, normalized)
	}
	if indexPosition < clearPosition {
		t.Fatalf("expected migration to clear %s before creating %s", table, index)
	}
}
