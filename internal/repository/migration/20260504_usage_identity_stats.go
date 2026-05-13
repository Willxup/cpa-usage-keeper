package migration

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func backfillUsageIdentityStatsMigration(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&entities.UsageIdentity{}) || !tx.Migrator().HasTable(&entities.UsageEvent{}) {
		return nil
	}
	for _, column := range []string{"auth_type", "source", "auth_index"} {
		if !tx.Migrator().HasColumn(&entities.UsageEvent{}, column) {
			return nil
		}
	}

	var identities []entities.UsageIdentity
	if err := tx.Find(&identities).Error; err != nil {
		return fmt.Errorf("list usage identities for stats backfill: %w", err)
	}
	for _, identity := range identities {
		stats, err := aggregateUsageIdentityFullStats(tx, identity)
		if err != nil {
			return err
		}
		updates := map[string]any{
			"total_requests":                 stats.TotalRequests,
			"success_count":                  stats.SuccessCount,
			"failure_count":                  stats.FailureCount,
			"input_tokens":                   stats.InputTokens,
			"output_tokens":                  stats.OutputTokens,
			"reasoning_tokens":               stats.ReasoningTokens,
			"cached_tokens":                  stats.CachedTokens,
			"total_tokens":                   stats.TotalTokens,
			"first_used_at":                  stats.FirstUsedAt,
			"last_used_at":                   stats.LastUsedAt,
			"stats_updated_at":               nil,
			"last_aggregated_usage_event_id": stats.MaxUsageEventID,
		}
		if stats.TotalRequests > 0 {
			now := timeutil.NormalizeStorageTime(time.Now())
			updates["stats_updated_at"] = now
		}
		if err := tx.Model(&entities.UsageIdentity{}).Where("id = ?", identity.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("backfill usage identity stats for %q: %w", identity.Identity, err)
		}
	}
	return nil
}

func aggregateUsageIdentityFullStats(tx *gorm.DB, identity entities.UsageIdentity) (usageIdentityStatsDelta, error) {
	var stats usageIdentityStatsDelta
	query, ok := usageIdentityBackfillEventsQuery(tx.Model(&entities.UsageEvent{}), identity)
	if !ok {
		return stats, nil
	}
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
		return stats, fmt.Errorf("aggregate full usage identity stats for %q: %w", identity.Identity, err)
	}
	if stats.TotalRequests == 0 {
		return stats, nil
	}

	var firstEvent entities.UsageEvent
	firstQuery, _ := usageIdentityBackfillEventsQuery(tx.Model(&entities.UsageEvent{}), identity)
	if err := firstQuery.Order("timestamp asc, id asc").First(&firstEvent).Error; err != nil {
		return stats, fmt.Errorf("find first usage identity event for %q: %w", identity.Identity, err)
	}
	firstUsedAt := firstEvent.Timestamp
	stats.FirstUsedAt = &firstUsedAt

	var lastEvent entities.UsageEvent
	lastQuery, _ := usageIdentityBackfillEventsQuery(tx.Model(&entities.UsageEvent{}), identity)
	if err := lastQuery.Order("timestamp desc, id desc").First(&lastEvent).Error; err != nil {
		return stats, fmt.Errorf("find last usage identity event for %q: %w", identity.Identity, err)
	}
	lastUsedAt := lastEvent.Timestamp
	stats.LastUsedAt = &lastUsedAt
	return stats, nil
}

func usageIdentityBackfillEventsQuery(query *gorm.DB, identity entities.UsageIdentity) (*gorm.DB, bool) {
	switch identity.AuthType {
	case entities.UsageIdentityAuthTypeAuthFile:
		return query.Where("auth_index = ? AND (auth_type = ? OR TRIM(COALESCE(auth_type, '')) = '')", identity.Identity, "oauth"), true
	case entities.UsageIdentityAuthTypeAIProvider:
		return query.Where("source = ? AND (auth_type = ? OR TRIM(COALESCE(auth_type, '')) = '')", identity.Identity, "apikey"), true
	default:
		return query, false
	}
}
