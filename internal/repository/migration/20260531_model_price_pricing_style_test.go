package migration

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelPricePricingStyleMigrationAddsDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy-pricing.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE model_price_settings (
		id integer PRIMARY KEY,
		model text,
		prompt_price_per1_m real,
		completion_price_per1_m real,
		cache_price_per1_m real,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy model_price_settings table: %v", err)
	}
	if err := db.Exec(`INSERT INTO model_price_settings (id, model, prompt_price_per1_m, completion_price_per1_m, cache_price_per1_m)
		VALUES (?, ?, ?, ?, ?)`, int64(1), "claude-sonnet", 3.0, 15.0, 0.3).Error; err != nil {
		t.Fatalf("seed legacy model price setting: %v", err)
	}

	if err := addModelPricePricingStyleMigration(db); err != nil {
		t.Fatalf("add pricing style migration: %v", err)
	}
	if err := addModelPricePricingStyleMigration(db); err != nil {
		t.Fatalf("add pricing style migration should be idempotent: %v", err)
	}

	for _, column := range []string{"pricing_style", "cache_creation_price_per1_m"} {
		if !db.Migrator().HasColumn("model_price_settings", column) {
			t.Fatalf("expected model_price_settings.%s column to exist", column)
		}
	}

	var row struct {
		PricingStyle            string
		CachePricePer1M         float64
		CacheCreationPricePer1M float64
	}
	if err := db.Raw(`SELECT pricing_style, cache_price_per1_m, cache_creation_price_per1_m FROM model_price_settings WHERE id = ?`, int64(1)).Scan(&row).Error; err != nil {
		t.Fatalf("scan migrated pricing setting: %v", err)
	}
	if row.PricingStyle != "openai" || row.CachePricePer1M != 0.3 || row.CacheCreationPricePer1M != 0 {
		t.Fatalf("unexpected migrated pricing setting: %+v", row)
	}
}
