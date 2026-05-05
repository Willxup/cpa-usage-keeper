package migration

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/models"
)

func TestOpenDatabaseUsageIdentityMigrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-identities.db")
	seedLegacyUsageIdentityTables(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	closeOpenedDatabase(t, db)
	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var identities []models.UsageIdentity
	if err := db.Order("auth_type asc, identity asc").Find(&identities).Error; err != nil {
		t.Fatalf("load usage identities after reopen: %v", err)
	}
	if len(identities) != 2 {
		t.Fatalf("expected unmatched AI provider raw identities to stay deleted after reopen, got %d: %+v", len(identities), identities)
	}
	oauth := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAuthFile, "auth-1")
	if oauth.TotalRequests != 3 || oauth.TotalTokens != 90 || oauth.LastAggregatedUsageEventID != 3 {
		t.Fatalf("expected oauth stats not to double-add after reopen, got %+v", oauth)
	}
	if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, "api-source-1") != 0 {
		t.Fatal("expected unmatched active provider raw identity to stay deleted after reopen")
	}
	deletedOAuth := findUsageIdentity(t, identities, models.UsageIdentityAuthTypeAuthFile, "auth-deleted")
	if !deletedOAuth.IsDeleted || deletedOAuth.TotalRequests != 1 || deletedOAuth.TotalTokens != 100 || deletedOAuth.LastAggregatedUsageEventID != 6 {
		t.Fatalf("expected deleted oauth stats not to double-add after reopen, got %+v", deletedOAuth)
	}
	if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, "api-deleted") != 0 {
		t.Fatal("expected unmatched deleted provider raw identity to stay deleted after reopen")
	}

	var duplicateVersions int64
	if err := db.Table("schema_migrations").Select("COUNT(*) - COUNT(DISTINCT version)").Scan(&duplicateVersions).Error; err != nil {
		t.Fatalf("count duplicate schema migration versions: %v", err)
	}
	if duplicateVersions != 0 {
		t.Fatalf("expected no duplicate schema migration versions, got %d", duplicateVersions)
	}
	for _, version := range []string{"20260504_create_usage_identities", "20260504_migrate_usage_identities_metadata", "20260504_backfill_usage_event_identity_fields", "20260504_backfill_usage_identity_stats", "20260504_drop_legacy_metadata_tables", "20260504_drop_legacy_snapshot_run_columns", "20260504_remove_prefix_usage_identities"} {
		var count int64
		if err := db.Table("schema_migrations").Where("version = ?", version).Count(&count).Error; err != nil {
			t.Fatalf("count schema migration %s: %v", version, err)
		}
		if count != 1 {
			t.Fatalf("expected schema migration %s to be recorded once, got %d", version, count)
		}
	}
	if db.Migrator().HasTable("auth_files") || db.Migrator().HasTable("provider_metadata") {
		t.Fatal("expected old metadata tables to stay dropped after reopen")
	}
}
