package migration

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelPriceServiceTierMigrationAddsColumnAndRebuildsIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "model-price-tier.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	statements := []string{
		`CREATE TABLE model_price_settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			model TEXT NOT NULL,
			pricing_style TEXT NOT NULL DEFAULT 'openai',
			prompt_price_per1_m REAL NOT NULL DEFAULT 0,
			completion_price_per1_m REAL NOT NULL DEFAULT 0,
			cache_price_per1_m REAL NOT NULL DEFAULT 0,
			cache_creation_price_per1_m REAL NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE UNIQUE INDEX uniq_model_price_settings_model ON model_price_settings (model)`,
		`INSERT INTO model_price_settings (model, pricing_style, prompt_price_per1_m, completion_price_per1_m, cache_price_per1_m, cache_creation_price_per1_m, created_at, updated_at)
		 VALUES ('gpt-4o', 'openai', 2.5, 10, 1.25, 0, '2026-06-18 00:00:00', '2026-06-18 00:00:00')`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed legacy model price schema with %q: %v", stmt, err)
		}
	}

	if err := modelPriceServiceTierMigration(db); err != nil {
		t.Fatalf("modelPriceServiceTierMigration returned error: %v", err)
	}
	if err := modelPriceServiceTierMigration(db); err != nil {
		t.Fatalf("modelPriceServiceTierMigration should be idempotent: %v", err)
	}

	if !db.Migrator().HasColumn("model_price_settings", "service_tier") {
		t.Fatal("expected model_price_settings.service_tier column to exist")
	}
	if migrationSQLiteIndexExists(t, db, "uniq_model_price_settings_model") {
		t.Fatal("expected legacy uniq_model_price_settings_model index to be removed")
	}
	if !migrationSQLiteIndexExists(t, db, "uniq_model_price_settings_model_tier") {
		t.Fatal("expected uniq_model_price_settings_model_tier index to exist")
	}

	var got string
	if err := db.Raw(`SELECT service_tier FROM model_price_settings WHERE model = ?`, "gpt-4o").Row().Scan(&got); err != nil {
		t.Fatalf("load model_price_settings.service_tier: %v", err)
	}
	if got != "" {
		t.Fatalf("expected legacy model price service_tier default to empty string, got %q", got)
	}
}
