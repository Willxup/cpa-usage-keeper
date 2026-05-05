package migration

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseAddsUsageIdentityLookupKeyToExistingTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if err := db.Exec(`CREATE TABLE usage_identities (
		id integer PRIMARY KEY AUTOINCREMENT,
		name text,
		auth_type integer,
		auth_type_name text,
		identity text,
		type text,
		provider text,
		total_requests integer DEFAULT 0,
		success_count integer DEFAULT 0,
		failure_count integer DEFAULT 0,
		input_tokens integer DEFAULT 0,
		output_tokens integer DEFAULT 0,
		reasoning_tokens integer DEFAULT 0,
		cached_tokens integer DEFAULT 0,
		total_tokens integer DEFAULT 0,
		last_aggregated_usage_event_id integer DEFAULT 0,
		first_used_at datetime,
		last_used_at datetime,
		stats_updated_at datetime,
		is_deleted numeric DEFAULT false,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_identities table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider)
		VALUES (?, ?, ?, ?, ?, ?)`, "Claude", models.UsageIdentityAuthTypeAIProvider, "apikey", "sk-legacy", "claude", "Claude").Error; err != nil {
		t.Fatalf("seed legacy apikey usage identity: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider)
		VALUES (?, ?, ?, ?, ?, ?)`, "OAuth", models.UsageIdentityAuthTypeAuthFile, "oauth", "auth-index", "claude", "Claude").Error; err != nil {
		t.Fatalf("seed legacy oauth usage identity: %v", err)
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if !db.Migrator().HasColumn(&models.UsageIdentity{}, "lookup_key") {
		t.Fatal("expected usage_identities.lookup_key column to be added")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260505_add_usage_identity_lookup_key").Count(&count).Error; err != nil {
		t.Fatalf("count lookup_key migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected lookup_key migration to be recorded once, got %d", count)
	}

	var apikey models.UsageIdentity
	if err := db.Where("identity = ?", "sk-legacy").First(&apikey).Error; err != nil {
		t.Fatalf("load migrated apikey identity: %v", err)
	}
	if apikey.LookupKey != "sk-legacy" {
		t.Fatalf("expected AutoMigrate to preserve migrated lookup_key, got %+v", apikey)
	}

	var oauth models.UsageIdentity
	if err := db.Where("identity = ?", "auth-index").First(&oauth).Error; err != nil {
		t.Fatalf("load migrated oauth identity: %v", err)
	}
	if oauth.LookupKey != "" {
		t.Fatalf("expected oauth lookup_key to remain empty, got %+v", oauth)
	}
}
