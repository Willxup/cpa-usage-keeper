package migration

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func migrateAIProviderIdentitiesToAuthIndexMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) || !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&entities.UsageIdentity{}, "lookup_key") {
		return nil
	}
	for _, column := range []string{"source", "provider", "auth_index", "auth_type"} {
		if !tx.Migrator().HasColumn(&entities.UsageEvent{}, column) {
			return nil
		}
	}

	var identities []aiProviderIdentityMigrationRow
	if err := tx.Raw(`
		SELECT id, name, auth_type_name, identity, type, provider, lookup_key
		FROM usage_identities
		WHERE auth_type_name = 'apikey'
			AND TRIM(COALESCE(lookup_key, '')) != ''
			AND TRIM(COALESCE(identity, '')) = TRIM(COALESCE(lookup_key, ''))
		ORDER BY created_at DESC, id DESC`).Scan(&identities).Error; err != nil {
		return fmt.Errorf("list raw AI provider usage identities: %w", err)
	}

	for _, identity := range identities {
		authIndexes, err := findAIProviderAuthIndexesForRawIdentity(tx, identity)
		if err != nil {
			return err
		}
		if len(authIndexes) != 1 {
			if err := tx.Delete(&entities.UsageIdentity{}, identity.ID).Error; err != nil {
				return fmt.Errorf("delete unmapped AI provider usage identity %q: %w", identity.Identity, err)
			}
			continue
		}
		if err := migrateAIProviderIdentityToAuthIndex(tx, identity, authIndexes[0]); err != nil {
			return err
		}
	}

	return backfillAIProviderUsageIdentityStatsByAuthIndex(tx)
}

type aiProviderIdentityMigrationRow struct {
	ID           uint   `gorm:"column:id"`
	Name         string `gorm:"column:name"`
	AuthTypeName string `gorm:"column:auth_type_name"`
	Identity     string `gorm:"column:identity"`
	Type         string `gorm:"column:type"`
	Provider     string `gorm:"column:provider"`
	LookupKey    string `gorm:"column:lookup_key"`
}

func findAIProviderAuthIndexesForRawIdentity(tx *gorm.DB, identity aiProviderIdentityMigrationRow) ([]string, error) {
	var authIndexes []string
	if err := tx.Raw(`
		SELECT DISTINCT TRIM(auth_index)
		FROM usage_events
		WHERE TRIM(COALESCE(source, '')) = TRIM(?)
			AND LOWER(TRIM(COALESCE(provider, ''))) = LOWER(TRIM(?))
			AND TRIM(COALESCE(auth_index, '')) != ''
			AND (auth_type = 'apikey' OR TRIM(COALESCE(auth_type, '')) = '')`, identity.LookupKey, identity.Name).
		Scan(&authIndexes).Error; err != nil {
		return nil, fmt.Errorf("find AI provider auth indexes for %q: %w", identity.Identity, err)
	}
	return authIndexes, nil
}

func migrateAIProviderIdentityToAuthIndex(tx *gorm.DB, oldIdentity aiProviderIdentityMigrationRow, targetIdentity string) error {
	lookupKey := strings.TrimSpace(oldIdentity.LookupKey)
	if lookupKey == "" {
		lookupKey = strings.TrimSpace(oldIdentity.Identity)
	}
	var target entities.UsageIdentity
	result := tx.Where("auth_type_name = ? AND identity = ? AND id <> ?", "apikey", targetIdentity, oldIdentity.ID).Limit(1).Find(&target)
	if result.Error != nil {
		return fmt.Errorf("load target AI provider usage identity %q: %w", targetIdentity, result.Error)
	}
	if result.RowsAffected > 0 {
		authTypeName := strings.TrimSpace(oldIdentity.AuthTypeName)
		if authTypeName == "" {
			authTypeName = "apikey"
		}
		if err := tx.Exec(`
			UPDATE usage_identities
			SET lookup_key = COALESCE(NULLIF(TRIM(lookup_key), ''), ?),
				name = COALESCE(NULLIF(TRIM(name), ''), ?),
				auth_type_name = COALESCE(NULLIF(TRIM(auth_type_name), ''), ?),
				type = COALESCE(NULLIF(TRIM(type), ''), ?),
				provider = COALESCE(NULLIF(TRIM(provider), ''), ?),
				total_requests = 0,
				success_count = 0,
				failure_count = 0,
				input_tokens = 0,
				output_tokens = 0,
				reasoning_tokens = 0,
				cached_tokens = 0,
				total_tokens = 0,
				first_used_at = NULL,
				last_used_at = NULL,
				stats_updated_at = NULL,
				last_aggregated_usage_event_id = 0
			WHERE id = ?`, lookupKey, oldIdentity.Name, authTypeName, oldIdentity.Type, oldIdentity.Provider, target.ID).Error; err != nil {
			return fmt.Errorf("merge AI provider usage identity %q into %q: %w", oldIdentity.Identity, targetIdentity, err)
		}
		if err := tx.Delete(&entities.UsageIdentity{}, oldIdentity.ID).Error; err != nil {
			return fmt.Errorf("delete merged AI provider usage identity %q: %w", oldIdentity.Identity, err)
		}
		return nil
	}

	if err := tx.Exec(`
		UPDATE usage_identities
		SET identity = ?,
			lookup_key = ?,
			total_requests = 0,
			success_count = 0,
			failure_count = 0,
			input_tokens = 0,
			output_tokens = 0,
			reasoning_tokens = 0,
			cached_tokens = 0,
			total_tokens = 0,
			first_used_at = NULL,
			last_used_at = NULL,
			stats_updated_at = NULL,
			last_aggregated_usage_event_id = 0
		WHERE id = ?`, targetIdentity, lookupKey, oldIdentity.ID).Error; err != nil {
		return fmt.Errorf("migrate AI provider usage identity %q to auth-index %q: %w", oldIdentity.Identity, targetIdentity, err)
	}
	return nil
}

func backfillAIProviderUsageIdentityStatsByAuthIndex(tx *gorm.DB) error {
	var identities []entities.UsageIdentity
	if err := tx.Where("auth_type_name = ?", "apikey").Find(&identities).Error; err != nil {
		return fmt.Errorf("list AI provider usage identities for auth-index stats backfill: %w", err)
	}
	for _, identity := range identities {
		stats, err := aggregateAIProviderUsageIdentityFullStatsByAuthIndex(tx, identity)
		if err != nil {
			return err
		}
		var statsUpdatedAt *time.Time
		if stats.TotalRequests > 0 {
			now := timeutil.NormalizeStorageTime(time.Now())
			statsUpdatedAt = &now
		}
		if err := tx.Exec(`
			UPDATE usage_identities
			SET total_requests = ?,
				success_count = ?,
				failure_count = ?,
				input_tokens = ?,
				output_tokens = ?,
				reasoning_tokens = ?,
				cached_tokens = ?,
				total_tokens = ?,
				first_used_at = ?,
				last_used_at = ?,
				stats_updated_at = ?,
				last_aggregated_usage_event_id = ?
			WHERE id = ?`, stats.TotalRequests, stats.SuccessCount, stats.FailureCount, stats.InputTokens, stats.OutputTokens, stats.ReasoningTokens, stats.CachedTokens, stats.TotalTokens, stats.FirstUsedAt, stats.LastUsedAt, statsUpdatedAt, stats.MaxUsageEventID, identity.ID).Error; err != nil {
			return fmt.Errorf("backfill AI provider usage identity stats for %q: %w", identity.Identity, err)
		}
	}
	return nil
}

func aggregateAIProviderUsageIdentityFullStatsByAuthIndex(tx *gorm.DB, identity entities.UsageIdentity) (usageIdentityStatsDelta, error) {
	var stats usageIdentityStatsDelta
	query := tx.Model(&entities.UsageEvent{}).Where("auth_index = ? AND (auth_type = ? OR TRIM(COALESCE(auth_type, '')) = '')", identity.Identity, "apikey")
	if err := query.Select(`
		COUNT(*) AS total_requests,
		COALESCE(SUM(CASE WHEN failed THEN 0 ELSE 1 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN failed THEN 1 ELSE 0 END), 0) AS failure_count,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
		COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
		COALESCE(SUM(total_tokens), 0) AS total_tokens,
		COALESCE(MAX(id), 0) AS max_usage_event_id`).
		Scan(&stats).Error; err != nil {
		return stats, fmt.Errorf("aggregate AI provider usage identity stats by auth-index for %q: %w", identity.Identity, err)
	}
	if stats.TotalRequests == 0 {
		return stats, nil
	}

	var firstEvent entities.UsageEvent
	if err := tx.Model(&entities.UsageEvent{}).Where("auth_index = ? AND (auth_type = ? OR TRIM(COALESCE(auth_type, '')) = '')", identity.Identity, "apikey").Order("timestamp asc, id asc").First(&firstEvent).Error; err != nil {
		return stats, fmt.Errorf("find first AI provider usage identity event by auth-index for %q: %w", identity.Identity, err)
	}
	firstUsedAt := firstEvent.Timestamp
	stats.FirstUsedAt = &firstUsedAt

	var lastEvent entities.UsageEvent
	if err := tx.Model(&entities.UsageEvent{}).Where("auth_index = ? AND (auth_type = ? OR TRIM(COALESCE(auth_type, '')) = '')", identity.Identity, "apikey").Order("timestamp desc, id desc").First(&lastEvent).Error; err != nil {
		return stats, fmt.Errorf("find last AI provider usage identity event by auth-index for %q: %w", identity.Identity, err)
	}
	lastUsedAt := lastEvent.Timestamp
	stats.LastUsedAt = &lastUsedAt
	return stats, nil
}
