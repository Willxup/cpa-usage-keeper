package test

import (
	. "cpa-usage-keeper/internal/entities"
	_ "cpa-usage-keeper/internal/timeutil"
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAllIncludesCoreModels(t *testing.T) {
	items := All()
	expected := []any{
		&UsageEvent{},
		&RedisUsageInbox{},
		&ModelPriceSetting{},
		&UsageIdentity{},
		&CPAAPIKey{},
		&UsageOverviewHourlyStat{},
		&UsageOverviewDailyStat{},
		&UsageOverviewAggregationCheckpoint{},
		// Activity 统计必须随核心模型注册，确保全新数据库直接得到新表。
		&UsageActivityStat{},
		// Activity checkpoint 必须独立注册，不能复用 Overview cursor。
		&UsageActivityAggregationCheckpoint{},
		&AuthSession{},
		&AppSetting{},
	}
	if len(items) != len(expected) {
		t.Fatalf("expected %d registered models, got %d", len(expected), len(items))
	}
	for index := range expected {
		if got, want := reflect.TypeOf(items[index]), reflect.TypeOf(expected[index]); got != want {
			t.Fatalf("expected model %d to be %v, got %v", index, want, got)
		}
	}
}

func TestAppSettingSchemaRequiresTimestamps(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite memory database: %v", err)
	}
	if err := db.AutoMigrate(&AppSetting{}); err != nil {
		t.Fatalf("AutoMigrate AppSetting returned error: %v", err)
	}

	for _, columnName := range []string{"created_at", "updated_at"} {
		columnTypes, err := db.Migrator().ColumnTypes(&AppSetting{})
		if err != nil {
			t.Fatalf("ColumnTypes returned error: %v", err)
		}
		found := false
		for _, columnType := range columnTypes {
			if columnType.Name() != columnName {
				continue
			}
			found = true
			nullable, ok := columnType.Nullable()
			if !ok {
				t.Fatalf("expected nullable metadata for %s", columnName)
			}
			if nullable {
				t.Fatalf("expected %s to be not nullable", columnName)
			}
		}
		if !found {
			t.Fatalf("expected %s column to exist", columnName)
		}
	}
}

func TestCacheTokenSchemaUsesExplicitReadAndWriteNames(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite memory database: %v", err)
	}
	if err := db.AutoMigrate(&UsageIdentity{}, &ModelPriceSetting{}); err != nil {
		t.Fatalf("AutoMigrate cache token schema returned error: %v", err)
	}

	if !db.Migrator().HasColumn(&UsageIdentity{}, "cache_read_tokens") {
		t.Fatal("expected usage_identities.cache_read_tokens to exist")
	}
	if db.Migrator().HasColumn(&UsageIdentity{}, "cache_creation_tokens") {
		t.Fatal("did not expect usage_identities.cache_creation_tokens")
	}
	if !db.Migrator().HasColumn(&ModelPriceSetting{}, "cache_read_price_per1_m") {
		t.Fatal("expected model_price_settings.cache_read_price_per1_m to exist")
	}
	if db.Migrator().HasColumn(&ModelPriceSetting{}, "cache_price_per1_m") {
		t.Fatal("did not expect legacy model_price_settings.cache_price_per1_m")
	}
	if !db.Migrator().HasColumn(&ModelPriceSetting{}, "cache_creation_price_per1_m") {
		t.Fatal("expected model_price_settings.cache_creation_price_per1_m to remain")
	}
}

func TestUsageOverviewSchemaIncludesFiveAggregationDimensions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite memory database: %v", err)
	}
	if err := db.AutoMigrate(&UsageOverviewHourlyStat{}, &UsageOverviewDailyStat{}); err != nil {
		t.Fatalf("AutoMigrate usage overview schema returned error: %v", err)
	}

	dimensions := []string{"service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type"}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		for _, dimension := range dimensions {
			if !db.Migrator().HasColumn(table, dimension) {
				t.Fatalf("expected %s.%s", table, dimension)
			}
		}
	}

	assertUsageOverviewDimensionIndex(t, db, "usage_overview_hourly_stats", "uniq_usage_overview_hourly_stats_dimensions")
	assertUsageOverviewDimensionIndex(t, db, "usage_overview_daily_stats", "uniq_usage_overview_daily_stats_dimensions")
	assertUsageOverviewDuplicateDimensionKeyRejected(t, db)
}

func assertUsageOverviewDimensionIndex(t *testing.T, db *gorm.DB, table string, name string) {
	t.Helper()
	type indexListRow struct {
		Name   string `gorm:"column:name"`
		Unique int    `gorm:"column:unique"`
	}
	var indexes []indexListRow
	if err := db.Raw("PRAGMA index_list(" + table + ")").Scan(&indexes).Error; err != nil {
		t.Fatalf("list %s indexes: %v", table, err)
	}
	foundUnique := false
	for _, index := range indexes {
		if index.Name == name && index.Unique == 1 {
			foundUnique = true
		}
	}
	if !foundUnique {
		t.Fatalf("expected %s to be a unique index on %s", name, table)
	}

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

func assertUsageOverviewDuplicateDimensionKeyRejected(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	hourly := UsageOverviewHourlyStat{BucketStart: now, APIGroupKey: "api-a", Model: "gpt-a", AuthIndex: "auth-a", ModelAlias: "alias-a", ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "high", Endpoint: "/v1/responses", ExecutorType: "CodexExecutor", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&hourly).Error; err != nil {
		t.Fatalf("insert first hourly dimension key: %v", err)
	}
	hourly.ID = 0
	if err := db.Create(&hourly).Error; err == nil {
		t.Fatal("expected duplicate hourly dimension key to fail")
	}

	daily := UsageOverviewDailyStat{BucketStart: now, APIGroupKey: "api-a", Model: "gpt-a", AuthIndex: "auth-a", ModelAlias: "alias-a", ServiceTier: "default", ResponseServiceTier: "default", ReasoningEffort: "high", Endpoint: "/v1/responses", ExecutorType: "CodexExecutor", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&daily).Error; err != nil {
		t.Fatalf("insert first daily dimension key: %v", err)
	}
	daily.ID = 0
	if err := db.Create(&daily).Error; err == nil {
		t.Fatal("expected duplicate daily dimension key to fail")
	}
}
