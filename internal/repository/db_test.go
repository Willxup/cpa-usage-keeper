package repository

import (
	"bytes"
	"context"
	"cpa-usage-keeper/internal/repository/dto"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestOpenDatabaseAutoMigratesCoreTables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	cfg := config.Config{
		SQLitePath: dbPath,
	}

	db, err := OpenDatabase(cfg)
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if db.Migrator().HasTable("snapshot_runs") {
		t.Fatal("expected legacy snapshot_runs table not to exist")
	}
	if !db.Migrator().HasTable("usage_events") {
		t.Fatal("expected usage_events table to exist")
	}
	if !db.Migrator().HasTable("redis_usage_inboxes") {
		t.Fatal("expected redis_usage_inboxes table to exist")
	}
}

func TestOpenDatabaseCreatesFreshDatabaseFromCurrentSchemaWithoutRunningMigrations(t *testing.T) {
	logs := captureRepositoryLogs(t)
	dbPath := filepath.Join(t.TempDir(), "app.db")

	db, err := OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	var count int64
	if err := db.Table("schema_migrations").Count(&count).Error; err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if count != 34 {
		t.Fatalf("expected fresh database to mark 34 migrations applied, got %d", count)
	}
	if strings.Contains(logs.String(), "schema migration started") {
		t.Fatalf("expected fresh database creation not to run version migrations, got logs:\n%s", logs.String())
	}
	for _, indexName := range []string{
		"idx_usage_events_api_group_key",
		"idx_usage_events_auth_index",
		"idx_usage_events_model",
		"idx_usage_events_auth_type_auth_index_id",
		"uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias",
		"idx_usage_overview_hourly_stats_api_bucket",
		"idx_usage_overview_hourly_stats_api_model_bucket",
		"idx_usage_overview_hourly_stats_auth_bucket",
		"idx_usage_overview_hourly_stats_model_alias_bucket",
		"uniq_usage_overview_daily_stats_bucket_api_model_auth_alias",
		"idx_usage_overview_daily_stats_api_bucket",
		"idx_usage_overview_daily_stats_api_model_bucket",
		"idx_usage_overview_daily_stats_auth_bucket",
		"idx_usage_overview_daily_stats_model_alias_bucket",
		"uniq_usage_overview_health_stats_bucket_span_api",
		"idx_usage_overview_health_stats_api_bucket_span",
	} {
		assertSQLiteIndexExists(t, db, indexName)
	}
	for _, indexName := range []string{
		"idx_usage_events_source",
		"idx_usage_events_auth_type_source_id",
	} {
		if repositorySQLiteIndexExists(t, db, indexName) {
			t.Fatalf("expected sqlite index %s not to exist", indexName)
		}
	}
}

func assertSQLiteIndexExists(t *testing.T, db *gorm.DB, indexName string) {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).Scan(&count).Error; err != nil {
		t.Fatalf("check sqlite index %s: %v", indexName, err)
	}
	if count != 1 {
		t.Fatalf("expected sqlite index %s to exist, got %d", indexName, count)
	}
}

func TestOpenDatabaseConfiguresSQLiteRuntime(t *testing.T) {
	db := openTestDatabase(t)

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL journal mode, got %q", journalMode)
	}

	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatalf("read busy timeout: %v", err)
	}
	if busyTimeout < 5000 {
		t.Fatalf("expected busy timeout at least 5000ms, got %d", busyTimeout)
	}

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read foreign keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign keys to be enabled, got %d", foreignKeys)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}
	if stats := sqlDB.Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("expected sqlite max open connections to be 1, got %+v", stats)
	}
}

func TestInsertUsageEventsPersistsDuplicateEventKeys(t *testing.T) {
	db := openTestDatabase(t)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-opus", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 20},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-haiku", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 30},
	}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 3 || deduped != 0 {
		t.Fatalf("expected inserted=3 deduped=0, got inserted=%d deduped=%d", inserted, deduped)
	}

	var rows []entities.UsageEvent
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("list usage events: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 persisted usage events, got %d", len(rows))
	}
	if rows[0].EventKey != "event-1" || rows[0].Model != "claude-sonnet" || rows[1].EventKey != "event-1" || rows[1].Model != "claude-opus" {
		t.Fatalf("expected duplicate event_key rows to preserve their own models, got %+v", rows)
	}
}

func TestInsertUsageEventsBatchesLargeInsertSet(t *testing.T) {
	db := openTestDatabase(t)
	events := make([]entities.UsageEvent, 0, 300)
	baseTime := time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 300; i++ {
		events = append(events, entities.UsageEvent{
			EventKey:    fmt.Sprintf("event-%03d", i),
			APIGroupKey: "provider-a",
			Model:       "claude-sonnet",
			Timestamp:   baseTime.Add(time.Duration(i) * time.Minute),
			Source:      "source-a",
			AuthIndex:   "auth-1",
			TotalTokens: int64(i + 1),
		})
	}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != len(events) || deduped != 0 {
		t.Fatalf("expected inserted=%d deduped=0, got inserted=%d deduped=%d", len(events), inserted, deduped)
	}

	var count int64
	if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage events: %v", err)
	}
	if count != int64(len(events)) {
		t.Fatalf("expected %d persisted usage events, got %d", len(events), count)
	}
}

func TestInsertUsageEventsPersistsModelAlias(t *testing.T) {
	db := openTestDatabase(t)
	modelAlias := "claude-sonnet-alias"
	events := []entities.UsageEvent{{
		EventKey:    "event-alias",
		APIGroupKey: "provider-a",
		Model:       "claude-sonnet",
		ModelAlias:  &modelAlias,
		Timestamp:   time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
		Source:      "source-a",
		AuthIndex:   "auth-1",
		TotalTokens: 10,
	}}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 1 || deduped != 0 {
		t.Fatalf("expected inserted=1 deduped=0, got inserted=%d deduped=%d", inserted, deduped)
	}

	var got entities.UsageEvent
	if err := db.Where("event_key = ?", "event-alias").First(&got).Error; err != nil {
		t.Fatalf("load usage event: %v", err)
	}
	if got.ModelAlias == nil || *got.ModelAlias != "claude-sonnet-alias" {
		t.Fatalf("expected model alias persisted, got %+v", got.ModelAlias)
	}
}

func TestInsertUsageEventsPersistsTTFTMS(t *testing.T) {
	db := openTestDatabase(t)
	ttftMS := int64(456)
	events := []entities.UsageEvent{{
		EventKey:    "event-ttft",
		APIGroupKey: "provider-a",
		Model:       "claude-sonnet",
		TTFTMS:      &ttftMS,
		Timestamp:   time.Date(2026, 5, 28, 8, 0, 0, 0, time.UTC),
		Source:      "source-a",
		AuthIndex:   "auth-1",
		TotalTokens: 10,
	}}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 1 || deduped != 0 {
		t.Fatalf("expected inserted=1 deduped=0, got inserted=%d deduped=%d", inserted, deduped)
	}

	var got struct {
		TTFTMS *int64 `gorm:"column:ttft_ms"`
	}
	if err := db.Table("usage_events").Select("ttft_ms").Where("event_key = ?", "event-ttft").First(&got).Error; err != nil {
		t.Fatalf("load usage event ttft_ms: %v", err)
	}
	if got.TTFTMS == nil || *got.TTFTMS != 456 {
		t.Fatalf("expected ttft_ms to persist, got %+v", got.TTFTMS)
	}
}

func TestInsertUsageEventsPersistsServiceTier(t *testing.T) {
	db := openTestDatabase(t)
	events := []entities.UsageEvent{{
		EventKey:    "event-service-tier",
		APIGroupKey: "provider-a",
		Model:       "claude-sonnet",
		ServiceTier: "standard",
		Timestamp:   time.Date(2026, 5, 29, 8, 0, 0, 0, time.UTC),
		Source:      "source-a",
		AuthIndex:   "auth-1",
		TotalTokens: 10,
	}}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 1 || deduped != 0 {
		t.Fatalf("expected inserted=1 deduped=0, got inserted=%d deduped=%d", inserted, deduped)
	}

	var got struct {
		ServiceTier string `gorm:"column:service_tier"`
	}
	if err := db.Table("usage_events").Select("service_tier").Where("event_key = ?", "event-service-tier").First(&got).Error; err != nil {
		t.Fatalf("load usage event service_tier: %v", err)
	}
	if got.ServiceTier != "standard" {
		t.Fatalf("expected service_tier to persist, got %q", got.ServiceTier)
	}
}

func TestInsertUsageEventsPersistsExecutorType(t *testing.T) {
	db := openTestDatabase(t)
	events := []entities.UsageEvent{{
		EventKey:     "event-executor-type",
		APIGroupKey:  "provider-a",
		Model:        "claude-sonnet",
		ExecutorType: "responses",
		Timestamp:    time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC),
		Source:       "source-a",
		AuthIndex:    "auth-1",
		TotalTokens:  10,
	}}

	inserted, deduped, err := InsertUsageEvents(db, events)
	if err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if inserted != 1 || deduped != 0 {
		t.Fatalf("expected inserted=1 deduped=0, got inserted=%d deduped=%d", inserted, deduped)
	}

	var got struct {
		ExecutorType string `gorm:"column:executor_type"`
	}
	if err := db.Table("usage_events").Select("executor_type").Where("event_key = ?", "event-executor-type").First(&got).Error; err != nil {
		t.Fatalf("load usage event executor_type: %v", err)
	}
	if got.ExecutorType != "responses" {
		t.Fatalf("expected executor_type to persist, got %q", got.ExecutorType)
	}
}

func TestDatabaseTimeFieldsUseProjectTimezoneRFC3339Nano(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)

	storageTime := time.Date(2026, 5, 12, 21, 59, 18, 353569620, location)
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "event-storage-time",
		APIGroupKey: "provider-a",
		Model:       "claude-sonnet",
		Timestamp:   storageTime,
		AuthType:    "oauth",
		AuthIndex:   "auth-1",
		TotalTokens: 1,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "claude-sonnet", PromptPricePer1M: 1}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	inboxRows, err := InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{{QueueKey: cpa.ManagementUsageQueueKey, RawMessage: `{"request_id":"event-storage-time"}`, PoppedAt: storageTime}})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if err := MarkRedisUsageInboxProcessed(db, inboxRows[0].ID, "event-storage-time", storageTime); err != nil {
		t.Fatalf("MarkRedisUsageInboxProcessed returned error: %v", err)
	}
	activeStart := storageTime
	activeUntil := storageTime.Add(time.Hour)
	if err := ReplaceUsageIdentitiesForAuthType(context.Background(), db, []entities.UsageIdentity{{
		Name:        "Auth 1",
		Identity:    "auth-1",
		ActiveStart: &activeStart,
		ActiveUntil: &activeUntil,
	}}, entities.UsageIdentityAuthTypeAuthFile, storageTime); err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForAuthType returned error: %v", err)
	}
	if err := AggregateUsageIdentityStats(context.Background(), db, storageTime); err != nil {
		t.Fatalf("AggregateUsageIdentityStats returned error: %v", err)
	}
	if err := ReplaceUsageIdentitiesForAuthType(context.Background(), db, nil, entities.UsageIdentityAuthTypeAuthFile, storageTime); err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForAuthType delete returned error: %v", err)
	}

	for _, check := range []struct {
		table string
		field string
		where string
	}{
		{table: "usage_events", field: "timestamp", where: "event_key = 'event-storage-time'"},
		{table: "usage_events", field: "created_at", where: "event_key = 'event-storage-time'"},
		{table: "model_price_settings", field: "created_at", where: "model = 'claude-sonnet'"},
		{table: "model_price_settings", field: "updated_at", where: "model = 'claude-sonnet'"},
		{table: "redis_usage_inboxes", field: "popped_at", where: "usage_event_key = 'event-storage-time'"},
		{table: "redis_usage_inboxes", field: "processed_at", where: "usage_event_key = 'event-storage-time'"},
		{table: "redis_usage_inboxes", field: "created_at", where: "usage_event_key = 'event-storage-time'"},
		{table: "redis_usage_inboxes", field: "updated_at", where: "usage_event_key = 'event-storage-time'"},
		{table: "usage_identities", field: "active_start", where: "identity = 'auth-1'"},
		{table: "usage_identities", field: "active_until", where: "identity = 'auth-1'"},
		{table: "usage_identities", field: "first_used_at", where: "identity = 'auth-1'"},
		{table: "usage_identities", field: "last_used_at", where: "identity = 'auth-1'"},
		{table: "usage_identities", field: "stats_updated_at", where: "identity = 'auth-1'"},
		{table: "usage_identities", field: "created_at", where: "identity = 'auth-1'"},
		{table: "usage_identities", field: "updated_at", where: "identity = 'auth-1'"},
		{table: "usage_identities", field: "deleted_at", where: "identity = 'auth-1'"},
		{table: "schema_migrations", field: "applied_at", where: "version = '20260503_add_usage_event_redis_fields'"},
	} {
		assertProjectTimezoneStorageValue(t, rawSQLiteTimeValue(t, db, check.table, check.field, check.where), check.table+"."+check.field)
	}
}

func TestCleanupStorageCleansRedisInboxAndVacuums(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 4, 27, 2, 30, 0, 0, time.UTC)

	inboxRows, err := InsertRedisUsageInboxMessages(db, []dto.RedisInboxInsert{
		{QueueKey: cpa.ManagementUsageQueueKey, RawMessage: `{"request_id":"processed-old"}`, PoppedAt: now.AddDate(0, 0, -2)},
		{QueueKey: cpa.ManagementUsageQueueKey, RawMessage: `{"request_id":"pending"}`, PoppedAt: now.AddDate(0, 0, -2)},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id = ?", inboxRows[0].ID).Updates(map[string]any{"status": RedisUsageInboxStatusProcessed, "processed_at": time.Date(2026, 4, 26, 15, 59, 59, 0, time.UTC)}).Error; err != nil {
		t.Fatalf("seed processed inbox row: %v", err)
	}
	if err := db.Create(&[]entities.UsageOverviewHealthStat{
		{BucketStart: now.Add(-9 * 24 * time.Hour), SpanSeconds: 900, APIGroupKey: "old", SuccessCount: 1},
		{BucketStart: now.Add(-7 * 24 * time.Hour), SpanSeconds: 900, APIGroupKey: "fresh", SuccessCount: 1},
	}).Error; err != nil {
		t.Fatalf("seed health stats: %v", err)
	}

	result, err := CleanupStorage(db, now)
	if err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}
	if result.RedisInbox.ProcessedDeleted != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}

	var inboxRemaining []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&inboxRemaining).Error; err != nil {
		t.Fatalf("load remaining inbox rows: %v", err)
	}
	if len(inboxRemaining) != 1 || inboxRemaining[0].ID != inboxRows[1].ID {
		t.Fatalf("expected only pending inbox row to remain, got %+v", inboxRemaining)
	}
	var healthRemaining []entities.UsageOverviewHealthStat
	if err := db.Order("api_group_key asc").Find(&healthRemaining).Error; err != nil {
		t.Fatalf("load remaining health stats: %v", err)
	}
	if len(healthRemaining) != 1 || healthRemaining[0].APIGroupKey != "fresh" {
		t.Fatalf("expected only fresh health stat row to remain, got %+v", healthRemaining)
	}
}

func openTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}

func rawSQLiteTimeValue(t *testing.T, db *gorm.DB, table string, field string, where string) string {
	t.Helper()
	var value string
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE %s LIMIT 1", field, table, where)).Scan(&value).Error; err != nil {
		t.Fatalf("read raw time value %s.%s: %v", table, field, err)
	}
	if strings.TrimSpace(value) == "" {
		t.Fatalf("expected raw time value for %s.%s", table, field)
	}
	return value
}

func assertProjectTimezoneStorageValue(t *testing.T, value string, field string) {
	t.Helper()
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		t.Fatalf("expected %s to use RFC3339Nano storage format, got %q: %v", field, value, err)
	}
	if !strings.Contains(value, "T") || !strings.Contains(value, "+08:00") || strings.Contains(value, "Z") || strings.Contains(value, "+00:00") {
		t.Fatalf("expected %s to use project timezone offset storage format, got %q", field, value)
	}
}

func closeTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
}

func captureRepositoryLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func repositorySQLiteIndexExists(t *testing.T, db *gorm.DB, indexName string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", indexName).Scan(&count).Error; err != nil {
		t.Fatalf("check sqlite index %s: %v", indexName, err)
	}
	return count == 1
}
