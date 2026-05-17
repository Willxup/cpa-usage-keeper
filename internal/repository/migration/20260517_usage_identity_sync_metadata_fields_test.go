package migration

import (
	"database/sql"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddUsageIdentitySyncMetadataFieldsMigrationAddsNullableColumns(t *testing.T) {
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

	if err := addUsageIdentitySyncMetadataFieldsMigration(db); err != nil {
		t.Fatalf("add usage identity sync metadata fields: %v", err)
	}
	if err := addUsageIdentitySyncMetadataFieldsMigration(db); err != nil {
		t.Fatalf("add usage identity sync metadata fields should be idempotent: %v", err)
	}

	for _, column := range []string{"priority", "disabled", "note"} {
		if !db.Migrator().HasColumn(&entities.UsageIdentity{}, column) {
			t.Fatalf("expected usage_identities.%s column to exist", column)
		}
	}

	var priority sql.NullInt64
	var disabled sql.NullBool
	var note sql.NullString
	err = db.Raw(`SELECT priority, disabled, note FROM usage_identities WHERE identity = ?`, "codex-auth").Row().Scan(&priority, &disabled, &note)
	if err != nil {
		t.Fatalf("scan sync metadata fields: %v", err)
	}
	if priority.Valid || disabled.Valid || note.Valid {
		t.Fatalf("expected legacy sync metadata fields to default NULL, got priority=%+v disabled=%+v note=%+v", priority, disabled, note)
	}
}
