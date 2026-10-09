package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
)

func TestOpenDatabaseUsageIdentityMigratesLegacyMetadataIdempotently(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-identities.db")
	seedLegacyUsageIdentityTables(t, dbPath)

	for _, stage := range []string{"initial migration", "reopen"} {
		t.Run(stage, func(t *testing.T) {
			db := openMigratedDatabase(t, dbPath)
			defer closeOpenedDatabase(t, db)

			if !db.Migrator().HasTable(&entities.UsageIdentity{}) {
				t.Fatal("expected usage_identities table to exist")
			}
			if db.Migrator().HasTable("auth_files") {
				t.Fatal("expected auth_files table to be dropped")
			}
			if db.Migrator().HasTable("provider_metadata") {
				t.Fatal("expected provider_metadata table to be dropped")
			}

			var identities []entities.UsageIdentity
			if err := db.Order("auth_type asc, identity asc").Find(&identities).Error; err != nil {
				t.Fatalf("load usage identities: %v", err)
			}
			if len(identities) != 2 {
				t.Fatalf("expected unmatched AI provider raw identities to be deleted, got %d: %+v", len(identities), identities)
			}

			oauth := findUsageIdentity(t, identities, entities.UsageIdentityAuthTypeAuthFile, "auth-1")
			if oauth.Name != "person@example.com" || oauth.AuthTypeName != "oauth" || oauth.Type != "claude" || oauth.Provider != "claude" {
				t.Fatalf("unexpected oauth identity mapping: %+v", oauth)
			}
			if oauth.TotalRequests != 3 || oauth.SuccessCount != 2 || oauth.FailureCount != 1 || oauth.InputTokens != 31 || oauth.OutputTokens != 41 || oauth.ReasoningTokens != 11 || oauth.CachedTokens != 7 || oauth.TotalTokens != 90 {
				t.Fatalf("unexpected oauth identity stats: %+v", oauth)
			}
			if oauth.FirstUsedAt == nil || !oauth.FirstUsedAt.Equal(time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)) {
				t.Fatalf("unexpected oauth first used timestamp: %+v", oauth.FirstUsedAt)
			}
			if oauth.LastUsedAt == nil || !oauth.LastUsedAt.Equal(time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)) {
				t.Fatalf("unexpected oauth last used timestamp: %+v", oauth.LastUsedAt)
			}
			if oauth.StatsUpdatedAt == nil {
				t.Fatal("expected oauth stats_updated_at to be set")
			}
			if oauth.LastAggregatedUsageEventID != 3 {
				t.Fatalf("expected oauth last aggregated usage event id 3, got %d", oauth.LastAggregatedUsageEventID)
			}

			deletedOAuth := findUsageIdentity(t, identities, entities.UsageIdentityAuthTypeAuthFile, "auth-deleted")
			if !deletedOAuth.IsDeleted || deletedOAuth.DeletedAt == nil || !deletedOAuth.DeletedAt.Equal(time.Date(2026, 5, 4, 7, 30, 0, 0, time.UTC)) {
				t.Fatalf("expected deleted auth file state to be preserved, got %+v", deletedOAuth)
			}
			if deletedOAuth.TotalRequests != 1 || deletedOAuth.TotalTokens != 100 || deletedOAuth.LastAggregatedUsageEventID != 6 {
				t.Fatalf("expected deleted auth file usage stats to be backfilled, got %+v", deletedOAuth)
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
		})
	}
}

func TestOpenDatabaseSkipsUsageIdentityMetadataMigrationWhenLegacyTablesAreMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "no-legacy-identities.db")
	seedPerformanceIndexMigrationDatabase(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if !db.Migrator().HasTable(&entities.UsageIdentity{}) {
		t.Fatal("expected usage_identities table to exist")
	}
	var count int64
	if err := db.Model(&entities.UsageIdentity{}).Count(&count).Error; err != nil {
		t.Fatalf("count usage identities: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no usage identities without legacy metadata, got %d", count)
	}
	if db.Migrator().HasTable("auth_files") || db.Migrator().HasTable("provider_metadata") {
		t.Fatal("expected legacy metadata tables not to be recreated")
	}
}
