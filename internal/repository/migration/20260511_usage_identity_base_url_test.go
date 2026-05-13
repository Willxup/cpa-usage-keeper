package migration

import (
	"database/sql"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageIdentityBaseURLMigrationAddsNullableColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "legacy.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_identities (
		id integer PRIMARY KEY AUTOINCREMENT,
		name text,
		auth_type integer,
		auth_type_name text,
		identity text,
		type text,
		provider text,
		lookup_key text,
		prefix text,
		is_deleted numeric
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_identities table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, lookup_key, prefix, is_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "Codex", entities.UsageIdentityAuthTypeAIProvider, "apikey", "codex-auth", "codex", "codex", "codex-key", "team", false).Error; err != nil {
		t.Fatalf("seed legacy usage identity: %v", err)
	}

	if err := addUsageIdentityBaseURLMigration(db); err != nil {
		t.Fatalf("add usage identity base_url: %v", err)
	}
	if err := addUsageIdentityBaseURLMigration(db); err != nil {
		t.Fatalf("add usage identity base_url should be idempotent: %v", err)
	}
	if !db.Migrator().HasColumn(&entities.UsageIdentity{}, "base_url") {
		t.Fatal("expected usage_identities.base_url column to exist")
	}

	var baseURL sql.NullString
	err = db.Raw(`SELECT base_url FROM usage_identities WHERE identity = ?`, "codex-auth").Row().Scan(&baseURL)
	if err != nil {
		t.Fatalf("scan base_url: %v", err)
	}
	if baseURL.Valid {
		t.Fatalf("expected legacy base_url to default NULL, got %+v", baseURL)
	}
}
