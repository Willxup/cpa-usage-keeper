package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	migrationAddUsageEventRedisFields               = "20260503_add_usage_event_redis_fields"
	migrationBackfillUsageEventRedisFields          = "20260503_backfill_usage_event_redis_fields"
	migrationDropSnapshotRuns                       = "20260503_drop_snapshot_runs"
	migrationDropLegacySnapshotRunColumns           = "20260504_drop_legacy_snapshot_run_columns"
	migrationCreateUsageIdentities                  = "20260504_create_usage_identities"
	migrationMigrateUsageIdentitiesMetadata         = "20260504_migrate_usage_identities_metadata"
	migrationBackfillUsageEventIdentityFields       = "20260504_backfill_usage_event_identity_fields"
	migrationBackfillUsageIdentityStats             = "20260504_backfill_usage_identity_stats"
	migrationDropLegacyMetadataTables               = "20260504_drop_legacy_metadata_tables"
	migrationRemovePrefixUsageIdentities            = "20260504_remove_prefix_usage_identities"
	migrationAddUsageIdentityLookupKey              = "20260505_add_usage_identity_lookup_key"
	migrationMigrateAIProviderIdentitiesToAuthIndex = "20260505_migrate_ai_provider_identities_to_auth_index"
	migrationAddUsagePerformanceIndexes             = "20260506_add_usage_performance_indexes"
	migrationAddUsageIdentityMetadataFields         = "20260507_add_usage_identity_metadata_fields"
	migrationAddUsageEventModelAlias                = "20260508_add_usage_event_model_alias"
	migrationUpdateUsageIdentityQuotaFields         = "20260509_update_usage_identity_quota_fields"
	migrationRemoveUsageIdentityQuotaFields         = "20260510_remove_usage_identity_quota_fields"
	migrationAddUsageIdentityBaseURL                = "20260511_add_usage_identity_base_url"
	migrationNormalizeStorageTimesToProjectTZ       = "20260512_normalize_storage_times_to_project_tz"
	migrationUseInt64PrimaryKeys                    = "20260513_use_int64_primary_keys"
	migrationCreateCPAAPIKeys                       = "20260513_create_cpa_api_keys"
	migrationAddUsageEventCacheTokenFields          = "20260514_add_usage_event_cache_token_fields"
	migrationAddUsageEventPlainDimensionIndexes     = "20260514_add_usage_event_plain_dimension_indexes"
	migrationCreateUsageOverviewStats               = "20260514_create_usage_overview_stats"
	migrationRemoveUsageEventEventKeyUniqueIndex    = "20260514_remove_usage_event_event_key_unique_index"
	migrationAddUsageIdentitySyncMetadataFields     = "20260517_add_usage_identity_sync_metadata_fields"
	migrationUsageOverviewRollupDimensions          = "20260518_usage_overview_rollup_dimensions"
	migrationAddUsageEventReasoningEffort           = "20260519_add_usage_event_reasoning_effort"
)

type schemaMigration struct {
	Version   string    `gorm:"primaryKey;column:version"`
	AppliedAt time.Time `gorm:"serializer:storageTime;not null;column:applied_at"`
}

func (schemaMigration) TableName() string {
	return "schema_migrations"
}

type databaseMigration struct {
	version            string
	run                func(*gorm.DB) error
	disableTransaction bool
}

func Run(db *gorm.DB) error {
	if err := createSchemaMigrationsTable(db); err != nil {
		return err
	}

	for _, migration := range orderedMigrations() {
		if err := runSchemaMigration(db, migration); err != nil {
			return err
		}
	}
	return nil
}

func MarkAllAsApplied(db *gorm.DB) error {
	if err := createSchemaMigrationsTable(db); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		now := timeutil.NormalizeStorageTime(time.Now())
		for _, migration := range orderedMigrations() {
			if err := tx.Exec("INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, ?)", migration.version, now).Error; err != nil {
				return fmt.Errorf("mark schema migration %s applied: %w", migration.version, err)
			}
		}
		return nil
	})
}

func createSchemaMigrationsTable(db *gorm.DB) error {
	if err := db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME NOT NULL)").Error; err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}
	return nil
}

func orderedMigrations() []databaseMigration {
	return []databaseMigration{
		{version: migrationAddUsageEventRedisFields, run: addUsageEventRedisFieldsMigration},
		{version: migrationBackfillUsageEventRedisFields, run: backfillUsageEventRedisFieldsMigration},
		{version: migrationDropSnapshotRuns, run: dropSnapshotRunsMigration},
		{version: migrationDropLegacySnapshotRunColumns, run: dropLegacySnapshotRunColumnsMigration},
		{version: migrationCreateUsageIdentities, run: createUsageIdentitiesMigration},
		{version: migrationMigrateUsageIdentitiesMetadata, run: migrateUsageIdentitiesMetadataMigration},
		{version: migrationBackfillUsageEventIdentityFields, run: backfillUsageEventIdentityFieldsMigration},
		{version: migrationBackfillUsageIdentityStats, run: backfillUsageIdentityStatsMigration},
		{version: migrationDropLegacyMetadataTables, run: dropLegacyMetadataTablesMigration},
		{version: migrationRemovePrefixUsageIdentities, run: removePrefixUsageIdentitiesMigration},
		{version: migrationAddUsageIdentityLookupKey, run: addUsageIdentityLookupKeyMigration},
		{version: migrationMigrateAIProviderIdentitiesToAuthIndex, run: migrateAIProviderIdentitiesToAuthIndexMigration},
		{version: migrationAddUsagePerformanceIndexes, run: addUsagePerformanceIndexesMigration},
		{version: migrationAddUsageIdentityMetadataFields, run: addUsageIdentityMetadataFieldsMigration},
		{version: migrationAddUsageEventModelAlias, run: addUsageEventModelAliasMigration},
		{version: migrationUpdateUsageIdentityQuotaFields, run: updateUsageIdentityQuotaFieldsMigration},
		{version: migrationRemoveUsageIdentityQuotaFields, run: removeUsageIdentityQuotaFieldsMigration},
		{version: migrationAddUsageIdentityBaseURL, run: addUsageIdentityBaseURLMigration},
		{version: migrationNormalizeStorageTimesToProjectTZ, run: normalizeStorageTimesToProjectTZMigration},
		{version: migrationUseInt64PrimaryKeys, run: useInt64PrimaryKeysMigration},
		{version: migrationCreateCPAAPIKeys, run: createCPAAPIKeysMigration},
		{version: migrationAddUsageEventCacheTokenFields, run: addUsageEventCacheTokenFieldsMigration},
		{version: migrationAddUsageEventPlainDimensionIndexes, run: addUsageEventPlainDimensionIndexesMigration},
		{version: migrationCreateUsageOverviewStats, run: createUsageOverviewStatsMigration},
		{version: migrationRemoveUsageEventEventKeyUniqueIndex, run: removeUsageEventEventKeyUniqueIndexMigration},
		{version: migrationAddUsageIdentitySyncMetadataFields, run: addUsageIdentitySyncMetadataFieldsMigration},
		{version: migrationUsageOverviewRollupDimensions, run: usageOverviewRollupDimensionsMigration, disableTransaction: true},
		{version: migrationAddUsageEventReasoningEffort, run: addUsageEventReasoningEffortMigration},
	}
}

func runSchemaMigration(db *gorm.DB, migration databaseMigration) error {
	if migration.disableTransaction {
		return runSchemaMigrationWithoutTransaction(db, migration)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return runSchemaMigrationBody(tx, migration)
	})
}

func runSchemaMigrationWithoutTransaction(db *gorm.DB, migration databaseMigration) error {
	return runSchemaMigrationBody(db, migration)
}

func runSchemaMigrationBody(db *gorm.DB, migration databaseMigration) error {
	logger := logrus.WithField("version", migration.version)
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", migration.version).Count(&count).Error; err != nil {
		logger.WithError(err).Error("schema migration failed")
		return fmt.Errorf("check schema migration %s: %w", migration.version, err)
	}
	if count > 0 {
		logger.Info("schema migration skipped")
		return nil
	}
	logger.Info("schema migration started")
	if err := migration.run(db); err != nil {
		logger.WithError(err).Error("schema migration failed")
		return fmt.Errorf("run schema migration %s: %w", migration.version, err)
	}
	if err := db.Create(&schemaMigration{Version: migration.version, AppliedAt: timeutil.NormalizeStorageTime(time.Now())}).Error; err != nil {
		logger.WithError(err).Error("schema migration failed")
		return fmt.Errorf("record schema migration %s: %w", migration.version, err)
	}
	logger.Info("schema migration applied")
	return nil
}
