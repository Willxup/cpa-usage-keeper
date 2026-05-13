package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func migrateUsageIdentitiesMetadataMigration(tx *gorm.DB) error {
	now := timeutil.NormalizeStorageTime(time.Now())
	if tx.Migrator().HasTable("auth_files") {
		isDeletedSelect, deletedAtSelect := legacyDeletedStateSelect(tx, "auth_files")
		if err := tx.Exec(`
			INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, is_deleted, created_at, updated_at, deleted_at)
			SELECT COALESCE(NULLIF(TRIM(email), ''), NULLIF(TRIM(label), ''), NULLIF(TRIM(name), ''), auth_index),
				?, ?, auth_index, type, provider, `+isDeletedSelect+`, COALESCE(created_at, ?), ?, `+deletedAtSelect+`
			FROM auth_files
			WHERE auth_index IS NOT NULL AND TRIM(auth_index) != ''
			ON CONFLICT(auth_type, identity) DO UPDATE SET
				name = excluded.name,
				auth_type_name = excluded.auth_type_name,
				type = excluded.type,
				provider = excluded.provider,
				is_deleted = excluded.is_deleted,
				deleted_at = excluded.deleted_at,
				updated_at = excluded.updated_at`, entities.UsageIdentityAuthTypeAuthFile, "oauth", now, now).Error; err != nil {
			return fmt.Errorf("migrate auth_files to usage_identities: %w", err)
		}
	}
	if tx.Migrator().HasTable("provider_metadata") {
		isDeletedSelect, deletedAtSelect := legacyDeletedStateSelect(tx, "provider_metadata")
		if err := tx.Exec(`
			INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, is_deleted, created_at, updated_at, deleted_at)
			SELECT display_name, ?, ?, lookup_key, provider_type, display_name, `+isDeletedSelect+`, COALESCE(created_at, ?), ?, `+deletedAtSelect+`
			FROM provider_metadata
			WHERE lookup_key IS NOT NULL AND TRIM(lookup_key) != ''
			ON CONFLICT(auth_type, identity) DO UPDATE SET
				name = excluded.name,
				auth_type_name = excluded.auth_type_name,
				type = excluded.type,
				provider = excluded.provider,
				is_deleted = excluded.is_deleted,
				deleted_at = excluded.deleted_at,
				updated_at = excluded.updated_at`, entities.UsageIdentityAuthTypeAIProvider, "apikey", now, now).Error; err != nil {
			return fmt.Errorf("migrate provider_metadata to usage_identities: %w", err)
		}
	}
	return nil
}
