package test

import (
	. "cpa-usage-keeper/internal/entities"
	_ "cpa-usage-keeper/internal/timeutil"
	"reflect"
	"testing"

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
		&APIKeyAuthFileScope{},
		&UsageOverviewHourlyStat{},
		&UsageOverviewDailyStat{},
		&UsageOverviewHealthStat{},
		&UsageOverviewAggregationCheckpoint{},
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
