package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func backfillUsageEventIdentityFieldsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) || !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	for _, column := range []string{"auth_type", "provider", "source", "auth_index"} {
		if !tx.Migrator().HasColumn(&entities.UsageEvent{}, column) {
			return nil
		}
	}

	if err := tx.Exec(`
		UPDATE usage_events
		SET auth_type = CASE
				WHEN TRIM(COALESCE(auth_type, '')) = '' THEN ?
				ELSE auth_type
			END,
			provider = CASE
				WHEN TRIM(COALESCE(provider, '')) = '' THEN COALESCE((
					SELECT NULLIF(TRIM(usage_identities.provider), '')
					FROM usage_identities
					WHERE usage_identities.auth_type = ?
						AND usage_identities.identity = usage_events.source
					LIMIT 1
				), provider)
				ELSE provider
			END
		WHERE EXISTS (
			SELECT 1
			FROM usage_identities
			WHERE usage_identities.auth_type = ?
				AND usage_identities.identity = usage_events.source
		)
		AND (TRIM(COALESCE(auth_type, '')) = '' OR TRIM(COALESCE(provider, '')) = '')`, "apikey", entities.UsageIdentityAuthTypeAIProvider, entities.UsageIdentityAuthTypeAIProvider).Error; err != nil {
		return fmt.Errorf("backfill AI provider usage event identity fields: %w", err)
	}

	if err := tx.Exec(`
		UPDATE usage_events
		SET auth_type = ?
		WHERE TRIM(COALESCE(auth_type, '')) = ''
		AND EXISTS (
			SELECT 1
			FROM usage_identities
			WHERE usage_identities.auth_type = ?
				AND usage_identities.identity = usage_events.auth_index
		)`, "oauth", entities.UsageIdentityAuthTypeCredential).Error; err != nil {
		return fmt.Errorf("backfill auth file usage event identity fields: %w", err)
	}
	return nil
}
