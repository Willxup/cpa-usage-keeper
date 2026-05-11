package migration

import (
	"database/sql"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateUsageIdentityQuotaFieldsMigrationAddsNullableProjectIDColumn(t *testing.T) {
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
		account_id text,
		plan_type text,
		total_requests integer,
		success_count integer,
		failure_count integer,
		input_tokens integer,
		output_tokens integer,
		reasoning_tokens integer,
		cached_tokens integer,
		total_tokens integer,
		last_aggregated_usage_event_id integer,
		first_used_at datetime,
		last_used_at datetime,
		stats_updated_at datetime,
		is_deleted numeric,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy usage_identities table: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, lookup_key, account_id, plan_type, is_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "Legacy", entities.UsageIdentityAuthTypeAuthFile, "oauth", "legacy-auth", "codex", "codex", "", "acct-1", "pro", false).Error; err != nil {
		t.Fatalf("seed legacy usage identity: %v", err)
	}

	if err := updateUsageIdentityQuotaFieldsMigration(db); err != nil {
		t.Fatalf("add usage identity project id: %v", err)
	}
	if err := updateUsageIdentityQuotaFieldsMigration(db); err != nil {
		t.Fatalf("add usage identity project id twice: %v", err)
	}

	if !db.Migrator().HasColumn(&entities.UsageIdentity{}, "project_id") {
		t.Fatal("expected usage_identities.project_id column to exist")
	}

	var projectID sql.NullString
	if err := db.Raw(`SELECT project_id FROM usage_identities WHERE identity = ?`, "legacy-auth").Row().Scan(&projectID); err != nil {
		t.Fatalf("scan project_id: %v", err)
	}
	if projectID.Valid {
		t.Fatalf("expected existing row project_id to remain NULL, got %q", projectID.String)
	}
}
