package test

import (
	"testing"

	"cpa-usage-keeper/internal/repository/migration"
)

func TestUsageIdentitySIWCFieldsMigrationPreservesLegacyRows(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	for _, statement := range []string{
		`CREATE TABLE usage_identities (id INTEGER PRIMARY KEY, identity TEXT, alias TEXT, total_requests INTEGER)`,
		`INSERT INTO usage_identities VALUES (1, 'auth-1', 'local alias', 42)`,
		`CREATE TRIGGER guard_identity BEFORE UPDATE ON usage_identities BEGIN SELECT RAISE(ABORT, 'legacy row rewrite'); END`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	const version = "20261029_usage_identity_siwc_fields"
	runOnlyMigration(t, db, version)
	var count int64
	if err := db.Raw(`SELECT count(*) FROM usage_identities WHERE id = 1 AND identity = 'auth-1' AND alias = 'local alias' AND total_requests = 42 AND siwc_bind_supported IS NULL AND siwc_bound IS NULL AND use_siwc IS NULL AND siwc_status IS NULL AND siwc_expires_at IS NULL`).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("legacy values changed or SIWC fields did not default to NULL")
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", version).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
}
