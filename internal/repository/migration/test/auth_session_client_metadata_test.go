package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const authSessionClientMetadataMigrationVersion = "20260813_add_auth_session_client_metadata"

func TestAuthSessionClientMetadataMigrationBackfillsLastSeenForExistingRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-auth-sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	closeMigrationTestDatabase(t, db)

	if err := db.Exec(`CREATE TABLE auth_sessions (
		token_hash TEXT PRIMARY KEY,
		role TEXT NOT NULL,
		source TEXT NOT NULL,
		cpa_api_key_id INTEGER,
		expires_at DATETIME NOT NULL,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create legacy auth_sessions table: %v", err)
	}
	if err := db.Exec(`INSERT INTO auth_sessions
		(token_hash, role, source, cpa_api_key_id, expires_at, created_at, updated_at)
		VALUES ('legacy-hash', 'admin', 'standard', 0, '2026-08-14 10:00:00', '2026-08-13 10:00:00', '2026-08-13 10:00:00')`).Error; err != nil {
		t.Fatalf("seed legacy auth session: %v", err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", authSessionClientMetadataMigrationVersion).Delete(nil).Error; err != nil {
		t.Fatalf("make auth session metadata migration pending: %v", err)
	}

	if err := migration.Run(db); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, column := range []string{"login_ip", "last_seen_ip", "user_agent", "last_seen_at"} {
		if !db.Migrator().HasColumn("auth_sessions", column) {
			t.Fatalf("expected auth_sessions.%s column", column)
		}
	}

	var row struct {
		CreatedAt  string
		LastSeenAt string
		LoginIP    *string
		UserAgent  *string
	}
	if err := db.Table("auth_sessions").Where("token_hash = ?", "legacy-hash").Take(&row).Error; err != nil {
		t.Fatalf("read migrated auth session: %v", err)
	}
	if row.LastSeenAt == "" || row.LastSeenAt != row.CreatedAt {
		t.Fatalf("expected last_seen_at to backfill from created_at, got %+v", row)
	}
	if row.LoginIP != nil || row.UserAgent != nil {
		t.Fatalf("expected unavailable historical metadata to stay null, got %+v", row)
	}
}
