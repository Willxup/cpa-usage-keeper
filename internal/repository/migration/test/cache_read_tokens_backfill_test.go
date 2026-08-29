package test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const cacheReadTokensBackfillVersion = "20260710_backfill_cache_read_tokens"

type cacheTokenRow struct {
	EventKey            string
	InputTokens         int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

type overviewCacheTokenRow struct {
	BucketStart         string
	AuthIndex           string
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

type identityCacheTokenRow struct {
	ID                         int64
	CachedTokens               int64
	CacheReadTokens            int64
	LastAggregatedUsageEventID int64
}

type modelPriceCacheRow struct {
	Model                   string
	CacheReadPricePer1M     float64
	CacheCreationPricePer1M float64
}

func TestCacheReadTokensBackfillNormalizesRawAndCursorSafeAggregates(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() {
		time.Local = previousLocal
	})

	db := openCacheReadTokensBackfillDatabase(t)
	createCacheReadTokensBackfillSchema(t, db)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", cacheReadTokensBackfillVersion).Error; err != nil {
		t.Fatalf("enable cache read migration: %v", err)
	}
	seedCacheReadTokensBackfillData(t, db)

	if err := migration.Run(db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	assertEventCacheTokens(t, db, "codex-fallback", 1_000, 100, 100, 10, 1_200)
	assertEventCacheTokens(t, db, "openai-explicit", 2_000, 30, 80, 20, 2_400)
	assertEventCacheTokens(t, db, "gemini-pending", 3_000, 60, 60, 30, 3_600)
	assertEventCacheTokens(t, db, "claude-normalized", 4_000, 40, 40, 90, 4_800)
	assertEventCacheTokens(t, db, "custom-unknown", 5_000, 70, 70, 50, 6_000)
	assertEventCacheTokens(t, db, "missing-identity", 6_000, 50, 50, 60, 7_200)

	for _, identityType := range cacheReadBackfillAliasTypes() {
		assertEventCacheTokens(t, db, "alias-"+identityType, 100, 1, 1, 2, 120)
	}

	assertOverviewCacheTokens(t, db, "usage_overview_hourly_stats", "2026-07-06T08:00:00Z", "shared-cache", 200, 250, 15)
	assertOverviewCacheTokens(t, db, "usage_overview_daily_stats", "2026-07-06T00:00:00Z", "shared-cache", 200, 250, 15)
	assertIdentityCacheTokens(t, db, 1, 170, 170, 2)
	assertIdentityCacheTokens(t, db, 2, 50, 100, 2)
	assertIdentityCacheTokens(t, db, 3, 0, 0, 2)
	assertIdentityCacheTokens(t, db, 4, 40, 40, 100)
	assertIdentityCacheTokens(t, db, 5, 70, 70, 100)
	assertIdentityCacheTokens(t, db, 6, 90, 90, 100)
	assertModelPriceCacheColumns(t, db, "gpt-cache", 0.25, 3.125)

	var pendingRollups int64
	if err := db.Table("usage_overview_hourly_stats").Where("auth_index = ?", "gemini-pending").Count(&pendingRollups).Error; err != nil {
		t.Fatalf("count pending hourly rollups: %v", err)
	}
	if pendingRollups != 0 {
		t.Fatalf("expected pending event to remain outside overview rollups, got %d rows", pendingRollups)
	}

	firstSnapshot := cacheReadTokensBackfillSnapshot(t, db)
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", cacheReadTokensBackfillVersion).Error; err != nil {
		t.Fatalf("force cache read migration rerun: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun migrations: %v", err)
	}
	secondSnapshot := cacheReadTokensBackfillSnapshot(t, db)
	if secondSnapshot != firstSnapshot {
		t.Fatalf("expected forced migration rerun to be idempotent\nfirst:  %s\nsecond: %s", firstSnapshot, secondSnapshot)
	}

	var migrationCount int64
	if err := db.Table("schema_migrations").Where("version = ?", cacheReadTokensBackfillVersion).Count(&migrationCount).Error; err != nil {
		t.Fatalf("count cache read migration record: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected one cache read migration record, got %d", migrationCount)
	}
}

func TestCacheReadTokensBackfillUsesProjectTimezoneForDSTDailyBucket(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load DST test location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() {
		time.Local = previousLocal
	})

	db := openCacheReadTokensBackfillDatabase(t)
	createCacheReadTokensBackfillSchema(t, db)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", cacheReadTokensBackfillVersion).Error; err != nil {
		t.Fatalf("enable cache read migration: %v", err)
	}

	eventTimestamp := time.Date(2026, time.March, 8, 12, 15, 0, 0, location)
	hourBucket := eventTimestamp.Truncate(time.Hour)
	dayBucket := time.Date(eventTimestamp.Year(), eventTimestamp.Month(), eventTimestamp.Day(), 0, 0, 0, 0, eventTimestamp.Location())
	_, eventOffset := eventTimestamp.Zone()
	_, dayOffset := dayBucket.Zone()
	if eventOffset == dayOffset {
		t.Fatalf("expected event and local midnight to use different DST offsets")
	}

	if err := db.Exec(
		"INSERT INTO usage_identities (id, auth_type, identity, type, cached_tokens, last_aggregated_usage_event_id) VALUES (1, 2, 'dst-cache', 'codex', 100, 1)",
	).Error; err != nil {
		t.Fatalf("seed DST usage identity: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (
		id, event_key, api_group_key, provider, auth_type, model, model_alias, timestamp, auth_index,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens
	) VALUES (1, 'dst-cache', 'dst-group', 'OpenAI', 'apikey', 'dst-model', '', ?, 'dst-cache', 1000, 0, 0, 100, 0, 0, 1000)`,
		timeutil.FormatStorageTime(eventTimestamp),
	).Error; err != nil {
		t.Fatalf("seed DST usage event: %v", err)
	}
	if err := db.Exec("INSERT INTO usage_overview_aggregation_checkpoints (name, last_aggregated_usage_event_id) VALUES ('overview', 1)").Error; err != nil {
		t.Fatalf("seed DST overview checkpoint: %v", err)
	}
	for table, bucketStart := range map[string]string{
		"usage_overview_hourly_stats": timeutil.FormatStorageTime(hourBucket),
		"usage_overview_daily_stats":  timeutil.FormatStorageTime(dayBucket),
	} {
		if err := db.Exec(
			fmt.Sprintf("INSERT INTO %s (bucket_start, api_group_key, model, auth_index, model_alias, cached_tokens, cache_read_tokens, cache_creation_tokens) VALUES (?, 'dst-group', 'dst-model', 'dst-cache', '', 100, 0, 0)", table),
			bucketStart,
		).Error; err != nil {
			t.Fatalf("seed DST %s: %v", table, err)
		}
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("run DST cache read migration: %v", err)
	}

	assertEventCacheTokens(t, db, "dst-cache", 1_000, 100, 100, 0, 1_000)
	assertOverviewCacheTokens(t, db, "usage_overview_hourly_stats", timeutil.FormatStorageTime(hourBucket), "dst-cache", 100, 100, 0)
	assertOverviewCacheTokens(t, db, "usage_overview_daily_stats", timeutil.FormatStorageTime(dayBucket), "dst-cache", 100, 100, 0)
}

func TestCacheReadTokensBackfillSkipsNonCanonicalAPIKeyAuthType(t *testing.T) {
	db := openCacheReadTokensBackfillDatabase(t)
	createCacheReadTokensBackfillSchema(t, db)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", cacheReadTokensBackfillVersion).Error; err != nil {
		t.Fatalf("enable cache read migration: %v", err)
	}

	if err := db.Exec(
		"INSERT INTO usage_identities (id, auth_type, identity, type, cached_tokens, last_aggregated_usage_event_id) VALUES (1, 2, 'legacy-api-key', 'codex', 0, 1)",
	).Error; err != nil {
		t.Fatalf("seed non-canonical usage identity: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_events (
		id, event_key, api_group_key, provider, auth_type, model, model_alias, timestamp, auth_index,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens
	) VALUES (1, 'legacy-api-key', 'legacy-group', 'OpenAI', 'api_key', 'legacy-model', '', '2026-07-06T08:15:00Z', 'legacy-api-key', 1000, 0, 0, 30, 80, 0, 1000)`).Error; err != nil {
		t.Fatalf("seed non-canonical usage event: %v", err)
	}
	if err := db.Exec("INSERT INTO usage_overview_aggregation_checkpoints (name, last_aggregated_usage_event_id) VALUES ('overview', 1)").Error; err != nil {
		t.Fatalf("seed non-canonical overview checkpoint: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("run non-canonical cache read migration: %v", err)
	}

	assertEventCacheTokens(t, db, "legacy-api-key", 1_000, 30, 80, 0, 1_000)
	assertIdentityCacheTokens(t, db, 1, 0, 0, 1)
}

func TestCacheReadTokensBackfillProcessesMultipleBucketBatches(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() {
		time.Local = previousLocal
	})

	const eventCount = 205
	db := openCacheReadTokensBackfillDatabase(t)
	createCacheReadTokensBackfillSchema(t, db)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", cacheReadTokensBackfillVersion).Error; err != nil {
		t.Fatalf("enable cache read migration: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO usage_identities (id, auth_type, identity, type, cached_tokens, last_aggregated_usage_event_id) VALUES (1, 2, 'batch-cache', 'codex', ?, ?)",
		eventCount, eventCount,
	).Error; err != nil {
		t.Fatalf("seed batched usage identity: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for id := 1; id <= eventCount; id++ {
			if err := tx.Exec(`INSERT INTO usage_events (
				id, event_key, api_group_key, provider, auth_type, model, model_alias, timestamp, auth_index,
				input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens
			) VALUES (?, ?, 'batch-group', 'OpenAI', 'apikey', 'batch-model', '', '2026-07-06T08:15:00Z', 'batch-cache', 1, 0, 0, 1, 0, 0, 1)`,
				id, fmt.Sprintf("batch-cache-%d", id),
			).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed batched usage events: %v", err)
	}
	if err := db.Exec("INSERT INTO usage_overview_aggregation_checkpoints (name, last_aggregated_usage_event_id) VALUES ('overview', ?)", eventCount).Error; err != nil {
		t.Fatalf("seed batched overview checkpoint: %v", err)
	}
	for table, bucketStart := range map[string]string{
		"usage_overview_hourly_stats": "2026-07-06T08:00:00Z",
		"usage_overview_daily_stats":  "2026-07-06T00:00:00Z",
	} {
		if err := db.Exec(
			fmt.Sprintf("INSERT INTO %s (bucket_start, api_group_key, model, auth_index, model_alias, cached_tokens, cache_read_tokens, cache_creation_tokens) VALUES (?, 'batch-group', 'batch-model', 'batch-cache', '', ?, 0, 0)", table),
			bucketStart, eventCount,
		).Error; err != nil {
			t.Fatalf("seed batched %s: %v", table, err)
		}
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("run batched cache read migration: %v", err)
	}

	var normalizedEvents int64
	if err := db.Table("usage_events").Where("cached_tokens = 1 AND cache_read_tokens = 1").Count(&normalizedEvents).Error; err != nil {
		t.Fatalf("count normalized batched events: %v", err)
	}
	if normalizedEvents != eventCount {
		t.Fatalf("expected %d normalized batched events, got %d", eventCount, normalizedEvents)
	}
	assertOverviewCacheTokens(t, db, "usage_overview_hourly_stats", "2026-07-06T08:00:00Z", "batch-cache", eventCount, eventCount, 0)
	assertOverviewCacheTokens(t, db, "usage_overview_daily_stats", "2026-07-06T00:00:00Z", "batch-cache", eventCount, eventCount, 0)
	assertIdentityCacheTokens(t, db, 1, eventCount, eventCount, eventCount)
}

func openCacheReadTokensBackfillDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cache-read-backfill.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get migration sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close migration database: %v", err)
		}
	})
	return db
}

func createCacheReadTokensBackfillSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE usage_events (
			id INTEGER PRIMARY KEY,
			event_key TEXT NOT NULL,
			api_group_key TEXT,
			provider TEXT,
			auth_type TEXT,
			model TEXT,
			model_alias TEXT,
			timestamp TEXT,
			auth_index TEXT,
			input_tokens INTEGER,
			output_tokens INTEGER,
			reasoning_tokens INTEGER,
			cached_tokens INTEGER,
			cache_read_tokens INTEGER,
			cache_creation_tokens INTEGER,
			total_tokens INTEGER
		)`,
		`CREATE TABLE usage_identities (
			id INTEGER PRIMARY KEY,
			auth_type INTEGER NOT NULL,
			identity TEXT NOT NULL,
			type TEXT,
			cached_tokens INTEGER,
			last_aggregated_usage_event_id INTEGER
		)`,
		`CREATE TABLE model_price_settings (
			id INTEGER PRIMARY KEY,
			model TEXT NOT NULL,
			cache_price_per1_m REAL,
			cache_creation_price_per1_m REAL
		)`,
		`CREATE TABLE usage_overview_hourly_stats (
			bucket_start TEXT NOT NULL,
			api_group_key TEXT NOT NULL,
			model TEXT NOT NULL,
			auth_index TEXT NOT NULL,
			model_alias TEXT NOT NULL,
			cached_tokens INTEGER,
			cache_read_tokens INTEGER,
			cache_creation_tokens INTEGER
		)`,
		`CREATE TABLE usage_overview_daily_stats (
			bucket_start TEXT NOT NULL,
			api_group_key TEXT NOT NULL,
			model TEXT NOT NULL,
			auth_index TEXT NOT NULL,
			model_alias TEXT NOT NULL,
			cached_tokens INTEGER,
			cache_read_tokens INTEGER,
			cache_creation_tokens INTEGER
		)`,
		`CREATE TABLE usage_overview_aggregation_checkpoints (
			name TEXT PRIMARY KEY,
			last_aggregated_usage_event_id INTEGER NOT NULL
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create migration test schema: %v", err)
		}
	}
}

func seedCacheReadTokensBackfillData(t *testing.T, db *gorm.DB) {
	t.Helper()
	identities := []struct {
		id         int64
		authType   int
		identity   string
		usageType  string
		cached     int64
		lastCursor int64
	}{
		{id: 1, authType: 2, identity: "shared-cache", usageType: "codex", cached: 170, lastCursor: 2},
		{id: 2, authType: 1, identity: "shared-cache", usageType: "openai", cached: 50, lastCursor: 2},
		{id: 3, authType: 2, identity: "gemini-pending", usageType: "gemini", cached: 0, lastCursor: 2},
		{id: 4, authType: 2, identity: "claude-normalized", usageType: "claude", cached: 40, lastCursor: 100},
		{id: 5, authType: 2, identity: "custom-unknown", usageType: "custom", cached: 70, lastCursor: 100},
		{id: 6, authType: 2, identity: "deleted-details", usageType: "openai", cached: 90, lastCursor: 100},
	}
	nextIdentityID := int64(20)
	for _, identityType := range cacheReadBackfillAliasTypes() {
		identities = append(identities, struct {
			id         int64
			authType   int
			identity   string
			usageType  string
			cached     int64
			lastCursor int64
		}{id: nextIdentityID, authType: 2, identity: "alias-" + identityType, usageType: identityType})
		nextIdentityID++
	}
	for _, identity := range identities {
		if err := db.Exec(
			"INSERT INTO usage_identities (id, auth_type, identity, type, cached_tokens, last_aggregated_usage_event_id) VALUES (?, ?, ?, ?, ?, ?)",
			identity.id, identity.authType, identity.identity, identity.usageType, identity.cached, identity.lastCursor,
		).Error; err != nil {
			t.Fatalf("seed usage identity %q: %v", identity.identity, err)
		}
	}

	events := []struct {
		id          int64
		eventKey    string
		authType    string
		authIndex   string
		provider    string
		input       int64
		cached      int64
		read        int64
		creation    int64
		total       int64
		apiGroupKey string
		model       string
		modelAlias  string
	}{
		{id: 1, eventKey: "codex-fallback", authType: "apikey", authIndex: "shared-cache", provider: "OpenAI", input: 1_000, cached: 100, read: 0, creation: 10, total: 1_200, apiGroupKey: "shared-group", model: "shared-model"},
		{id: 2, eventKey: "openai-explicit", authType: "oauth", authIndex: "shared-cache", provider: "OpenAI", input: 2_000, cached: 30, read: 80, creation: 20, total: 2_400, apiGroupKey: "shared-group", model: "shared-model"},
		{id: 3, eventKey: "gemini-pending", authType: "apikey", authIndex: "gemini-pending", provider: "Gemini", input: 3_000, cached: 60, read: 0, creation: 30, total: 3_600, apiGroupKey: "gemini-group", model: "gemini-model"},
		{id: 4, eventKey: "claude-normalized", authType: "apikey", authIndex: "claude-normalized", provider: "Anthropic", input: 4_000, cached: 40, read: 40, creation: 90, total: 4_800, apiGroupKey: "claude-group", model: "claude-model"},
		{id: 5, eventKey: "custom-unknown", authType: "apikey", authIndex: "custom-unknown", provider: "Custom", input: 5_000, cached: 70, read: 0, creation: 50, total: 6_000, apiGroupKey: "custom-group", model: "custom-model"},
		{id: 6, eventKey: "missing-identity", authType: "apikey", authIndex: "missing-identity", provider: "codex", input: 6_000, cached: 50, read: 0, creation: 60, total: 7_200, apiGroupKey: "missing-group", model: "missing-model"},
	}
	nextEventID := int64(20)
	for _, identityType := range cacheReadBackfillAliasTypes() {
		events = append(events, struct {
			id          int64
			eventKey    string
			authType    string
			authIndex   string
			provider    string
			input       int64
			cached      int64
			read        int64
			creation    int64
			total       int64
			apiGroupKey string
			model       string
			modelAlias  string
		}{
			id: nextEventID, eventKey: "alias-" + identityType, authType: "apikey", authIndex: "alias-" + identityType,
			provider: identityType, input: 100, cached: 1, creation: 2, total: 120, apiGroupKey: "alias-group", model: "alias-model",
		})
		nextEventID++
	}
	for _, event := range events {
		if err := db.Exec(`INSERT INTO usage_events (
			id, event_key, api_group_key, provider, auth_type, model, model_alias, timestamp, auth_index,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?, ?)`,
			event.id, event.eventKey, event.apiGroupKey, event.provider, event.authType, event.model, event.modelAlias,
			"2026-07-06T08:15:00Z", event.authIndex, event.input, event.cached, event.read, event.creation, event.total,
		).Error; err != nil {
			t.Fatalf("seed usage event %q: %v", event.eventKey, err)
		}
	}

	if err := db.Exec("INSERT INTO usage_overview_aggregation_checkpoints (name, last_aggregated_usage_event_id) VALUES ('overview', 2)").Error; err != nil {
		t.Fatalf("seed overview checkpoint: %v", err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		bucketStart := "2026-07-06T08:00:00Z"
		if table == "usage_overview_daily_stats" {
			bucketStart = "2026-07-06T00:00:00Z"
		}
		if err := db.Exec(
			fmt.Sprintf("INSERT INTO %s (bucket_start, api_group_key, model, auth_index, model_alias, cached_tokens, cache_read_tokens, cache_creation_tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", table),
			bucketStart, "shared-group", "shared-model", "shared-cache", "", 200, 80, 15,
		).Error; err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}
	if err := db.Exec("INSERT INTO model_price_settings (id, model, cache_price_per1_m, cache_creation_price_per1_m) VALUES (1, 'gpt-cache', 0.25, 3.125)").Error; err != nil {
		t.Fatalf("seed model price setting: %v", err)
	}
}

func cacheReadBackfillAliasTypes() []string {
	return []string{
		"openai-compatible",
		"openai_compatibility",
		"openai-compatibility",
		"openai-compatible-acme",
		"vertex",
		"gemini-cli",
		"gemini-cli-code-assist",
		"gemini-interactions",
		"antigravity",
		"aistudio",
		"ai-studio",
		"kimi",
		"moonshot",
		"xai",
	}
}

func assertEventCacheTokens(t *testing.T, db *gorm.DB, eventKey string, input, cached, read, creation, total int64) {
	t.Helper()
	var row cacheTokenRow
	if err := db.Table("usage_events").Where("event_key = ?", eventKey).Take(&row).Error; err != nil {
		t.Fatalf("load usage event %q: %v", eventKey, err)
	}
	if row.InputTokens != input || row.CachedTokens != cached || row.CacheReadTokens != read || row.CacheCreationTokens != creation || row.TotalTokens != total {
		t.Fatalf("unexpected cache tokens for %q: %+v", eventKey, row)
	}
}

func assertOverviewCacheTokens(t *testing.T, db *gorm.DB, table, bucketStart, authIndex string, cached, read, creation int64) {
	t.Helper()
	var row overviewCacheTokenRow
	if err := db.Table(table).Where("bucket_start = ? AND auth_index = ?", bucketStart, authIndex).Take(&row).Error; err != nil {
		t.Fatalf("load %s row: %v", table, err)
	}
	if row.CachedTokens != cached || row.CacheReadTokens != read || row.CacheCreationTokens != creation {
		t.Fatalf("unexpected %s cache tokens: %+v", table, row)
	}
}

func assertIdentityCacheTokens(t *testing.T, db *gorm.DB, id, cached, read, cursor int64) {
	t.Helper()
	var row identityCacheTokenRow
	if err := db.Table("usage_identities").Where("id = ?", id).Take(&row).Error; err != nil {
		t.Fatalf("load usage identity %d: %v", id, err)
	}
	if row.CachedTokens != cached || row.CacheReadTokens != read || row.LastAggregatedUsageEventID != cursor {
		t.Fatalf("unexpected usage identity %d cache tokens: %+v", id, row)
	}
}

func assertModelPriceCacheColumns(t *testing.T, db *gorm.DB, model string, read, creation float64) {
	t.Helper()
	if db.Migrator().HasColumn("model_price_settings", "cache_price_per1_m") {
		t.Fatal("expected legacy model_price_settings.cache_price_per1_m to be renamed")
	}
	if !db.Migrator().HasColumn("model_price_settings", "cache_read_price_per1_m") {
		t.Fatal("expected model_price_settings.cache_read_price_per1_m to exist")
	}
	if !db.Migrator().HasColumn("model_price_settings", "cache_creation_price_per1_m") {
		t.Fatal("expected model_price_settings.cache_creation_price_per1_m to remain")
	}
	var row modelPriceCacheRow
	if err := db.Table("model_price_settings").Where("model = ?", model).Take(&row).Error; err != nil {
		t.Fatalf("load model price %q: %v", model, err)
	}
	if row.CacheReadPricePer1M != read || row.CacheCreationPricePer1M != creation {
		t.Fatalf("unexpected model price cache values: %+v", row)
	}
}

func cacheReadTokensBackfillSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var events []cacheTokenRow
	if err := db.Table("usage_events").Order("event_key ASC").Find(&events).Error; err != nil {
		t.Fatalf("snapshot usage events: %v", err)
	}
	var hourly []overviewCacheTokenRow
	if err := db.Table("usage_overview_hourly_stats").Order("bucket_start ASC, auth_index ASC").Find(&hourly).Error; err != nil {
		t.Fatalf("snapshot hourly overview: %v", err)
	}
	var daily []overviewCacheTokenRow
	if err := db.Table("usage_overview_daily_stats").Order("bucket_start ASC, auth_index ASC").Find(&daily).Error; err != nil {
		t.Fatalf("snapshot daily overview: %v", err)
	}
	var identities []identityCacheTokenRow
	if err := db.Table("usage_identities").Order("id ASC").Find(&identities).Error; err != nil {
		t.Fatalf("snapshot usage identities: %v", err)
	}
	var prices []modelPriceCacheRow
	if err := db.Table("model_price_settings").Order("model ASC").Find(&prices).Error; err != nil {
		t.Fatalf("snapshot model prices: %v", err)
	}
	return fmt.Sprintf("events=%#v hourly=%#v daily=%#v identities=%#v prices=%#v", events, hourly, daily, identities, prices)
}
