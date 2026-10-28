package test

import (
	"testing"
)

func TestOIDCIdentityMigrationAddsNullableColumnsWithoutChangingLegacyRows(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	if err := db.Exec(`CREATE TABLE auth_sessions (token_hash TEXT PRIMARY KEY, role TEXT, source TEXT, alias TEXT, expires_at TEXT, created_at TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO auth_sessions VALUES ('old','admin','standard','Saved alias','2026-10-21 12:00:00','2026-10-20 12:00:00')`).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		runOnlyMigration(t, db, "20261020_add_auth_session_oidc_identity")
	}
	var row struct {
		Alias, Source, ExpiresAt, CreatedAt   string
		OIDCIssuer, OIDCSubject, OIDCClientID *string
	}
	if err := db.Table("auth_sessions").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Alias != "Saved alias" || row.Source != "standard" || row.ExpiresAt != "2026-10-21 12:00:00" || row.CreatedAt != "2026-10-20 12:00:00" || row.OIDCIssuer != nil || row.OIDCSubject != nil || row.OIDCClientID != nil {
		t.Fatalf("legacy session changed: %+v", row)
	}
	for _, column := range []string{"oidc_issuer", "oidc_subject", "oidc_client_id"} {
		if !db.Migrator().HasColumn("auth_sessions", column) {
			t.Fatalf("missing %s", column)
		}
	}
}
