package test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPricingLegacyConversionChecksOriginalAndReplayedGroups(t *testing.T) {
	db, backup, baseline := pricingLegacyConversionFixture(t)
	ctx := context.Background()
	if err := repository.VerifyPublishedPricingMigration(ctx, backup, db, baseline); err != nil {
		t.Fatalf("valid old five-dimension replay rejected: %v", err)
	}
	// M3 固定数据不属于 M1 原证据，费用列的加入也不影响 M2 复验。
	baseline.Fixed = &repository.PricingMigrationFixedBaseline{OverviewCursor: 2}
	if err := db.Exec("ALTER TABLE usage_events ADD COLUMN cost_usd REAL").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE usage_events SET cost_usd = 1 WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyPublishedPricingMigration(ctx, backup, db, baseline); err != nil {
		t.Fatalf("M4 restart verification rejected: %v", err)
	}
	if err := db.Exec("UPDATE usage_events SET total_tokens = total_tokens + 1 WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyPublishedPricingMigration(ctx, backup, db, baseline); err == nil || !strings.Contains(err.Error(), "total_tokens") {
		t.Fatalf("changed original event count escaped M2: %v", err)
	}
}

func TestPricingLegacyConversionRejectsLostReplayedGroup(t *testing.T) {
	db, backup, baseline := pricingLegacyConversionFixture(t)
	if err := db.Exec("UPDATE usage_overview_hourly_stats SET request_count = request_count + 1 WHERE model = 'model-a'").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyPublishedPricingMigration(context.Background(), backup, db, baseline); err == nil || !strings.Contains(err.Error(), "request_count") {
		t.Fatalf("changed replayed group escaped M2: %v", err)
	}
}

func TestPricingLegacyConversionUsesAbsoluteHourInHalfHourZone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	db, backup, baseline := pricingLegacyConversionFixture(t)
	if err := repository.VerifyPublishedPricingMigration(context.Background(), backup, db, baseline); err != nil {
		t.Fatalf("valid half-hour-zone replay rejected: %v", err)
	}
	var bucket string
	if err := db.Table("usage_overview_hourly_stats").Select("bucket_start").Order("bucket_start").Limit(1).Scan(&bucket).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(bucket, "2026-09-22T09:30:00") {
		t.Fatalf("published migration did not use absolute hour: %s", bucket)
	}
}

func TestPricingLegacyConversionSeparatesRepeatedDSTHour(t *testing.T) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	first := time.Date(2026, 11, 1, 8, 30, 0, 0, time.UTC).In(location)
	second := first.Add(time.Hour)
	if first.Hour() != second.Hour() {
		t.Fatal("fixture did not cross a repeated local hour")
	}
	db, backup, baseline := pricingLegacyConversionFixtureTimes(t, first, second)
	if err := repository.VerifyPublishedPricingMigration(context.Background(), backup, db, baseline); err != nil {
		t.Fatalf("valid repeated-hour replay rejected: %v", err)
	}
	var buckets []string
	if err := db.Table("usage_overview_hourly_stats").Order("bucket_start").Pluck("bucket_start", &buckets).Error; err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 2 || buckets[0] == buckets[1] {
		t.Fatalf("repeated hours collapsed: %v", buckets)
	}
}

func TestPricingLegacyConversionDoesNotRoundNanoBoundaryIntoNextHour(t *testing.T) {
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })
	first := time.Date(2026, 9, 23, 10, 59, 59, 999999999, time.UTC)
	second := first.Add(time.Nanosecond)
	db, backup, baseline := pricingLegacyConversionFixtureTimes(t, first, second)
	if err := repository.VerifyPublishedPricingMigration(context.Background(), backup, db, baseline); err != nil {
		t.Fatalf("nanosecond boundary rounded into wrong hour: %v", err)
	}
}

func TestPricingLegacyConversionNormalizesEarlyOffsetlessEventBeforeRollup(t *testing.T) {
	location, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	ctx := context.Background()
	db, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "offsetless-old.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := reader.DB(); err == nil {
			_ = pool.Close()
		}
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
	})
	for _, statement := range []string{
		`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, event_key TEXT, source TEXT, auth_index TEXT,
			api_group_key TEXT, model TEXT, model_alias TEXT, provider TEXT, auth_type TEXT, endpoint TEXT, request_id TEXT,
			service_tier TEXT, response_service_tier TEXT, reasoning_effort TEXT, executor_type TEXT,
			timestamp TEXT, failed BOOLEAN, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER,
			cached_tokens INTEGER, cache_read_tokens INTEGER, cache_creation_tokens INTEGER, total_tokens INTEGER)`,
		`INSERT INTO usage_events (id,event_key,source,auth_index,api_group_key,model,model_alias,provider,auth_type,endpoint,service_tier,response_service_tier,reasoning_effort,executor_type,timestamp,failed,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens)
			VALUES (1,'old-no-offset','','auth','api','model-a','','openai','oauth','/v1/chat','','','','','2026-09-23 10:15:00',0,3,2,0,0,0,0,5)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"20260512_normalize_storage_times_to_project_tz", "20260723_usage_overview_five_dimensions"} {
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
			t.Fatal(err)
		}
	}
	state, err := repository.BootstrapPricingInitialization(ctx, db)
	if err != nil || state.InitKind != repository.PricingInitKindLegacy {
		t.Fatalf("bootstrap: %+v %v", state, err)
	}
	if err := repository.EnsurePricingBootstrapInbox(ctx, db); err != nil {
		t.Fatal(err)
	}
	baseline, err := repository.ProtectPricingLegacyMigration(ctx, db, reader, filepath.Join(t.TempDir(), "backup"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var stored entities.PricingMigrationState
	if err := db.Where("id = 1").Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	backup, err := gorm.Open(sqlite.Open(helper.BuildSQLiteFileURI(*stored.BackupPath)+"?mode=ro&_query_only=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := backup.DB(); err == nil {
			_ = pool.Close()
		}
	})
	if err := migration.RunPublished(db); err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyPublishedPricingMigration(ctx, backup, db, baseline); err != nil {
		t.Fatalf("valid early offsetless event rejected: %v", err)
	}
	var eventTimestamp, hourlyBucket string
	if err := db.Table("usage_events").Select("timestamp").Where("id = 1").Scan(&eventTimestamp).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("usage_overview_hourly_stats").Select("bucket_start").Limit(1).Scan(&hourlyBucket).Error; err != nil {
		t.Fatal(err)
	}
	if eventTimestamp != "2026-09-23T10:15:00+05:30" || hourlyBucket != "2026-09-23T09:30:00+05:30" {
		t.Fatalf("unexpected local normalization and absolute hour: event=%s bucket=%s", eventTimestamp, hourlyBucket)
	}
}

func TestPricingLegacyConversionUsesPublishedTokenPredicates(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		provider  string
		authType  string
		old       [7]int64
		converted [7]int64
		applied   []string
	}{
		{
			name: "Claude OAuth cache split", provider: "claude", authType: "oauth",
			old: [7]int64{5, 4, 0, 3, 2, 0, 0}, converted: [7]int64{10, 4, 3, 3, 2, 0, 14},
			applied: []string{"20260605_backfill_gemini_codex_token_format", "20260710_backfill_cache_read_tokens"},
		},
		{
			name: "Gemini api_key provider fallback", provider: "gemini", authType: "api_key",
			old: [7]int64{5, 3, 0, 0, 0, 2, 10}, converted: [7]int64{5, 5, 0, 0, 0, 2, 10},
			applied: []string{"20260601_backfill_claude_usage_tokens", "20260710_backfill_cache_read_tokens"},
		},
		{
			name: "Gemini provider trailing space stays unmatched", provider: "gemini ", authType: "api_key",
			old: [7]int64{5, 3, 0, 0, 0, 2, 10}, converted: [7]int64{5, 3, 0, 0, 0, 2, 10},
			applied: []string{"20260601_backfill_claude_usage_tokens", "20260710_backfill_cache_read_tokens"},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			backup, live, baseline := pricingM2TokenFixture(t, scenario.provider, scenario.authType, scenario.old, scenario.converted)
			baseline.SchemaMigrations = append(baseline.SchemaMigrations, scenario.applied...)
			if err := repository.VerifyPublishedPricingMigration(context.Background(), backup, live, baseline); err != nil {
				t.Fatalf("published token rule rejected: %v", err)
			}
			if err := live.Exec("UPDATE usage_events SET output_tokens = output_tokens + 1 WHERE id = 1").Error; err != nil {
				t.Fatal(err)
			}
			if err := repository.VerifyPublishedPricingMigration(context.Background(), backup, live, baseline); err == nil || !strings.Contains(err.Error(), "output_tokens") {
				t.Fatalf("wrong output tokens escaped: %v", err)
			}
		})
	}
}

func TestPricingLegacyConversionReplaysPublishedInboxAndIdentityBackfills(t *testing.T) {
	db, backup, baseline := pricingM2MetadataMigrationFixture(t)
	ctx := context.Background()
	if err := repository.VerifyPublishedPricingMigration(ctx, backup, db, baseline); err != nil {
		t.Fatalf("published metadata backfills rejected: %v", err)
	}
	var rows []struct {
		ID        int64
		Provider  string
		Endpoint  string
		AuthType  string
		RequestID string
	}
	if err := db.Table("usage_events").Select("id, provider, endpoint, auth_type, request_id").Order("id").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Provider != "from-inbox" || rows[0].Endpoint != "/v1/chat" || rows[0].AuthType != "apikey" || rows[0].RequestID != "request-a" ||
		rows[1].Endpoint != "/v1/responses" || rows[1].RequestID != "request-b" || rows[1].AuthType != "oauth" ||
		rows[2].Provider != "from-identity" || rows[2].AuthType != "apikey" || rows[2].Endpoint != "/v1/null" {
		t.Fatalf("unexpected historical metadata conversion: %+v", rows)
	}
	if err := db.Exec("UPDATE usage_events SET endpoint = '/wrong' WHERE id = 2").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyPublishedPricingMigration(ctx, backup, db, baseline); err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("incorrect fallback endpoint escaped M2: %v", err)
	}
}

func pricingM2MetadataMigrationFixture(t *testing.T) (*gorm.DB, *gorm.DB, repository.PricingLegacyBaseline) {
	t.Helper()
	ctx := context.Background()
	db, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "metadata-old.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := reader.DB(); err == nil {
			_ = pool.Close()
		}
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
	})
	for _, statement := range []string{
		`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, event_key TEXT, source TEXT, auth_index TEXT,
			provider TEXT, endpoint TEXT, auth_type TEXT, request_id TEXT, timestamp TEXT, failed BOOLEAN,
			input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, cached_tokens INTEGER,
			cache_read_tokens INTEGER, cache_creation_tokens INTEGER, total_tokens INTEGER)`,
		`CREATE TABLE usage_identities (id INTEGER PRIMARY KEY, auth_type INTEGER, identity TEXT, provider TEXT, type TEXT)`,
		`CREATE TABLE redis_usage_inboxes (id INTEGER PRIMARY KEY, queue_key TEXT NOT NULL, message_hash TEXT NOT NULL,
			raw_message TEXT NOT NULL, status TEXT NOT NULL, attempt_count INTEGER, last_error TEXT,
			usage_event_key TEXT, popped_at TEXT, processed_at TEXT, created_at TEXT, updated_at TEXT)`,
		`INSERT INTO usage_events (id,event_key,source,auth_index,provider,endpoint,auth_type,request_id,timestamp,failed,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens)
			VALUES (1,'event-a','provider-key','auth-a','','','','','2026-09-22T10:00:00Z',0,1,2,0,0,0,0,3),
			(2,'request-b','','oauth-key','','','','','2026-09-22T10:01:00Z',0,1,2,0,0,0,0,3),
			(3,'NULL','NULL','auth-c','','','','','2026-09-22T10:02:00Z',0,1,2,0,0,0,0,3)`,
		`INSERT INTO usage_identities (id,auth_type,identity,provider,type)
			VALUES (1,2,'provider-key','from-identity','openai'),(2,1,'oauth-key','oauth-provider','openai'),(3,2,'NULL','from-identity','openai')`,
		`INSERT INTO redis_usage_inboxes (id,queue_key,message_hash,raw_message,status,attempt_count,usage_event_key,popped_at)
			VALUES (1,'usage','hash-1','{"provider":" from-inbox ","endpoint":" /v1/chat ","auth_type":"api_key","request_id":"request-a"}','processed',0,'event-a','2026-09-22T10:00:00Z'),
			(2,'usage','hash-2','{"endpoint":"/should-not-fallback","request_id":"request-b"}','processed',0,'event-a','2026-09-22T10:00:00Z'),
			(3,'usage','hash-3','{"endpoint":" /v1/responses ","request_id":"request-b"}','processed',0,'missing-key','2026-09-22T10:00:00Z'),
			(4,'usage','hash-4','{"endpoint":"/v1/null"}','processed',0,'NULL','2026-09-22T10:00:00Z')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed published old schema: %v", err)
		}
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"20260503_backfill_usage_event_redis_fields", "20260504_backfill_usage_event_identity_fields", "20260612_replace_redis_inbox_queue_key_with_source"} {
		if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
			t.Fatal(err)
		}
	}
	state, err := repository.BootstrapPricingInitialization(ctx, db)
	if err != nil || state.InitKind != repository.PricingInitKindLegacy {
		t.Fatalf("bootstrap: %+v %v", state, err)
	}
	if err := repository.EnsurePricingBootstrapInbox(ctx, db); err != nil {
		t.Fatal(err)
	}
	baseline, err := repository.ProtectPricingLegacyMigration(ctx, db, reader, filepath.Join(t.TempDir(), "backup"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var stored entities.PricingMigrationState
	if err := db.Where("id = 1").Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	backup, err := gorm.Open(sqlite.Open(helper.BuildSQLiteFileURI(*stored.BackupPath)+"?mode=ro&_query_only=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := backup.DB(); err == nil {
			_ = pool.Close()
		}
	})
	if err := migration.RunPublished(db); err != nil {
		t.Fatal(err)
	}
	return db, backup, baseline
}

func pricingM2TokenFixture(t *testing.T, provider, authType string, original, converted [7]int64) (*gorm.DB, *gorm.DB, repository.PricingLegacyBaseline) {
	t.Helper()
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = pool.Close() })
		for _, statement := range []string{
			`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, event_key TEXT, source TEXT, auth_index TEXT, auth_type TEXT, provider TEXT, endpoint TEXT, request_id TEXT,
			input_tokens INTEGER, output_tokens INTEGER, cached_tokens INTEGER, cache_read_tokens INTEGER,
			cache_creation_tokens INTEGER, reasoning_tokens INTEGER, total_tokens INTEGER)`,
			`CREATE TABLE usage_identities (id INTEGER PRIMARY KEY, auth_type INTEGER, identity TEXT, type TEXT)`,
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
		return db
	}
	backup, live := open(), open()
	insert := func(db *gorm.DB, tokens [7]int64) {
		if err := db.Exec(`INSERT INTO usage_events (id,event_key,source,auth_index,auth_type,provider,input_tokens,output_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,reasoning_tokens,total_tokens)
			VALUES (1,'event-1','','auth',?,?,?,?,?,?,?,?,?)`, authType, provider, tokens[0], tokens[1], tokens[2], tokens[3], tokens[4], tokens[5], tokens[6]).Error; err != nil {
			t.Fatal(err)
		}
	}
	insert(backup, original)
	insert(live, converted)
	return backup, live, repository.PricingLegacyBaseline{
		SchemaVersion:    1,
		SchemaMigrations: []string{"20260723_usage_overview_five_dimensions", "20260512_normalize_storage_times_to_project_tz", "20260503_backfill_usage_event_redis_fields", "20260504_backfill_usage_event_identity_fields"},
		SchemaColumns:    map[string][]string{"usage_events": {"id", "event_key", "source", "auth_index", "auth_type", "provider", "endpoint", "request_id", "input_tokens", "output_tokens", "cached_tokens", "cache_read_tokens", "cache_creation_tokens", "reasoning_tokens", "total_tokens"}},
		Hot:              repository.PricingLegacyEventEvidence{Count: 1, MinID: 1, MaxID: 1},
	}
}

func pricingLegacyConversionFixture(t *testing.T) (*gorm.DB, *gorm.DB, repository.PricingLegacyBaseline) {
	first := time.Date(2026, 9, 22, 10, 15, 0, 0, time.Local)
	return pricingLegacyConversionFixtureTimes(t, first, first.Add(2*time.Minute))
}

func pricingLegacyConversionFixtureTimes(t *testing.T, first, second time.Time) (*gorm.DB, *gorm.DB, repository.PricingLegacyBaseline) {
	t.Helper()
	ctx := context.Background()
	db, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "legacy.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := reader.DB(); err == nil {
			_ = pool.Close()
		}
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
	})
	statColumns := `id INTEGER PRIMARY KEY, bucket_start TEXT, api_group_key TEXT, model TEXT, auth_index TEXT, model_alias TEXT,
		request_count INTEGER, success_count INTEGER, failure_count INTEGER, input_tokens INTEGER, output_tokens INTEGER,
		reasoning_tokens INTEGER, cached_tokens INTEGER, cache_read_tokens INTEGER, cache_creation_tokens INTEGER, total_tokens INTEGER,
		created_at TEXT, updated_at TEXT`
	hour := first.Truncate(time.Hour)
	day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.Local)
	hourText := timeutil.FormatStorageTime(hour)
	dayText := timeutil.FormatStorageTime(day)
	firstText := timeutil.FormatStorageTime(first)
	secondText := timeutil.FormatStorageTime(second)
	for _, statement := range []string{
		`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, event_key TEXT, source TEXT, request_id TEXT, api_group_key TEXT, model TEXT, model_alias TEXT, auth_index TEXT, provider TEXT, auth_type TEXT,
			service_tier TEXT, response_service_tier TEXT, reasoning_effort TEXT, endpoint TEXT, executor_type TEXT,
			timestamp TEXT, failed BOOLEAN, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER,
			cached_tokens INTEGER, cache_read_tokens INTEGER, cache_creation_tokens INTEGER, total_tokens INTEGER)`,
		"CREATE TABLE usage_overview_hourly_stats (" + statColumns + ")",
		"CREATE TABLE usage_overview_daily_stats (" + statColumns + ")",
		`CREATE TABLE usage_overview_aggregation_checkpoints (id INTEGER PRIMARY KEY, name TEXT, last_aggregated_usage_event_id INTEGER,
			stats_updated_at TEXT, created_at TEXT, updated_at TEXT)`,
		fmt.Sprintf(`INSERT INTO usage_events (id,api_group_key,model,model_alias,auth_index,provider,auth_type,service_tier,response_service_tier,reasoning_effort,endpoint,executor_type,timestamp,failed,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens)
			VALUES (1,'api','model-a','','auth','openai','oauth','standard','','','/v1/chat','','%s',0,10,5,0,0,0,0,15),
			(2,'api','model-a','','auth','openai','oauth','priority','','','/v1/chat','','%s',1,20,6,0,0,0,0,26)`, firstText, secondText),
		fmt.Sprintf(`INSERT INTO usage_overview_hourly_stats (bucket_start,api_group_key,model,auth_index,model_alias,request_count,success_count,failure_count,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,created_at,updated_at)
			VALUES ('%s','api','model-a','auth','',1,1,0,10,5,0,0,0,0,15,'%s','%s')`, hourText, hourText, hourText),
		fmt.Sprintf(`INSERT INTO usage_overview_daily_stats (bucket_start,api_group_key,model,auth_index,model_alias,request_count,success_count,failure_count,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,created_at,updated_at)
			VALUES ('%s','api','model-a','auth','',1,1,0,10,5,0,0,0,0,15,'%s','%s')`, dayText, hourText, hourText),
		fmt.Sprintf(`INSERT INTO usage_overview_aggregation_checkpoints (id,name,last_aggregated_usage_event_id,created_at,updated_at)
			VALUES (1,'overview',1,'%s','%s')`, hourText, hourText),
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy conversion: %v", err)
		}
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", "20260723_usage_overview_five_dimensions").Error; err != nil {
		t.Fatal(err)
	}
	state, err := repository.BootstrapPricingInitialization(ctx, db)
	if err != nil || state.InitKind != repository.PricingInitKindLegacy {
		t.Fatalf("bootstrap: %+v %v", state, err)
	}
	if err := repository.EnsurePricingBootstrapInbox(ctx, db); err != nil {
		t.Fatal(err)
	}
	baseline, err := repository.ProtectPricingLegacyMigration(ctx, db, reader, filepath.Join(t.TempDir(), "backup"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var stored entities.PricingMigrationState
	if err := db.Where("id = 1").Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	backup, err := gorm.Open(sqlite.Open(helper.BuildSQLiteFileURI(*stored.BackupPath)+"?mode=ro&_query_only=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := backup.DB(); err == nil {
			_ = pool.Close()
		}
	})
	if err := migration.RunPublished(db); err != nil {
		t.Fatal(err)
	}
	return db, backup, baseline
}

// 新主分支的父会话规范化同时覆盖热表和归档；费用迁移仍逐列拒绝其他改动。
func TestPricingLegacyConversionParentSessionNormalization(t *testing.T) {
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		for _, tc := range []struct {
			name               string
			old, current       any
			applied, wantError bool
		}{
			{"pending-empty", "", nil, false, false},
			{"pending-null", nil, nil, false, false},
			{"pending-value", "parent-1", "parent-1", false, false},
			{"pending-whitespace", " ", " ", false, false},
			{"missing-conversion", "", "", false, true},
			{"lost-parent", "parent-1", nil, false, true},
			{"changed-parent", "parent-1", "parent-2", false, true},
			{"already-applied", nil, nil, true, false},
			{"unapproved-conversion", "", nil, true, true},
		} {
			t.Run(table+"/"+tc.name, func(t *testing.T) {
				backup, live, baseline := pricingM2TokenFixture(t, "openai", "apikey", [7]int64{}, [7]int64{})
				for _, db := range []*gorm.DB{backup, live} {
					if table == "usage_events_archive" {
						if err := db.Exec("CREATE TABLE usage_events_archive (id INTEGER PRIMARY KEY)").Error; err != nil {
							t.Fatal(err)
						}
						if err := db.Exec("INSERT INTO usage_events_archive(id) VALUES(1)").Error; err != nil {
							t.Fatal(err)
						}
					}
					if err := db.Exec("ALTER TABLE " + table + " ADD COLUMN parent_session_id TEXT").Error; err != nil {
						t.Fatal(err)
					}
				}
				if table == "usage_events_archive" {
					baseline.SchemaColumns[table] = []string{"id"}
					baseline.Archive = repository.PricingLegacyEventEvidence{Count: 1, MinID: 1, MaxID: 1}
				}
				baseline.SchemaColumns[table] = append(baseline.SchemaColumns[table], "parent_session_id")
				if tc.applied {
					baseline.SchemaMigrations = append(baseline.SchemaMigrations, "20260922_normalize_usage_event_parent_session_null")
				}
				if err := backup.Table(table).Where("id = 1").UpdateColumn("parent_session_id", tc.old).Error; err != nil {
					t.Fatal(err)
				}
				if err := live.Table(table).Where("id = 1").UpdateColumn("parent_session_id", tc.current).Error; err != nil {
					t.Fatal(err)
				}
				if tc.name == "pending-empty" {
					// 从真实空串运行已发布迁移，证明 M2 不提前创建费用列。
					if err := live.Table(table).Where("id = 1").UpdateColumn("parent_session_id", "").Error; err != nil {
						t.Fatal(err)
					}
					if err := migration.MarkAllAsApplied(live); err != nil {
						t.Fatal(err)
					}
					if err := live.Exec("DELETE FROM schema_migrations WHERE version IN (?, ?)", "20260922_normalize_usage_event_parent_session_null", "20261002_pricing_storage_structure").Error; err != nil {
						t.Fatal(err)
					}
					if err := migration.RunPublished(live); err != nil {
						t.Fatal(err)
					}
					if live.Migrator().HasColumn(table, "cost_usd") {
						t.Fatal("published migrations ran pricing structure early")
					}
				}
				err := repository.VerifyPublishedPricingMigration(context.Background(), backup, live, baseline)
				if tc.wantError {
					if err == nil || !strings.Contains(err.Error(), "parent_session_id") {
						t.Fatalf("expected parent conversion rejection, got %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
