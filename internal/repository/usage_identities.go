package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ReplaceUsageIdentitiesForAuthType(ctx context.Context, db *gorm.DB, identities []models.UsageIdentity, authType models.UsageIdentityAuthType, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	normalized, incomingIdentities := normalizeUsageIdentities(identities, authType)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsertUsageIdentities(tx, normalized); err != nil {
			return err
		}

		query := tx.Model(&models.UsageIdentity{}).Where("auth_type = ?", authType)
		if len(incomingIdentities) > 0 {
			query = query.Where("identity NOT IN ?", incomingIdentities)
		}
		if err := query.Updates(map[string]any{
			"is_deleted": true,
			"deleted_at": now,
		}).Error; err != nil {
			return fmt.Errorf("mark stale usage identities deleted: %w", err)
		}

		return nil
	})
}

func ReplaceUsageIdentitiesForProviderTypes(ctx context.Context, db *gorm.DB, identities []models.UsageIdentity, providerTypes []string, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	normalized, incomingIdentities := normalizeUsageIdentities(identities, models.UsageIdentityAuthTypeAIProvider)
	types := normalizeProviderTypes(providerTypes)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsertUsageIdentities(tx, normalized); err != nil {
			return err
		}
		if len(types) == 0 {
			return nil
		}

		query := tx.Model(&models.UsageIdentity{}).
			Where("auth_type = ?", models.UsageIdentityAuthTypeAIProvider).
			Where("type IN ?", types)
		if len(incomingIdentities) > 0 {
			query = query.Where("identity NOT IN ?", incomingIdentities)
		}
		if err := query.Updates(map[string]any{
			"is_deleted": true,
			"deleted_at": now,
		}).Error; err != nil {
			return fmt.Errorf("mark stale provider usage identities deleted: %w", err)
		}

		return nil
	})
}

func ListUsageIdentities(ctx context.Context, db *gorm.DB) ([]models.UsageIdentity, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	var identities []models.UsageIdentity
	if err := db.WithContext(ctx).Order("auth_type asc, name asc, id asc").Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("list usage identities: %w", err)
	}
	return identities, nil
}

func AggregateUsageIdentityStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	var identities []models.UsageIdentity
	if err := db.WithContext(ctx).Find(&identities).Error; err != nil {
		return fmt.Errorf("list usage identities for aggregation: %w", err)
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, identity := range identities {
			delta, err := aggregateUsageIdentityDelta(tx, identity)
			if err != nil {
				return err
			}
			if delta.TotalRequests == 0 {
				continue
			}

			firstUsedAt := identity.FirstUsedAt
			if delta.FirstUsedAt != nil && (firstUsedAt == nil || delta.FirstUsedAt.Before(*firstUsedAt)) {
				first := *delta.FirstUsedAt
				firstUsedAt = &first
			}

			lastUsedAt := identity.LastUsedAt
			if delta.LastUsedAt != nil && (lastUsedAt == nil || delta.LastUsedAt.After(*lastUsedAt)) {
				last := *delta.LastUsedAt
				lastUsedAt = &last
			}

			updates := map[string]any{
				"total_requests":                 identity.TotalRequests + delta.TotalRequests,
				"success_count":                  identity.SuccessCount + delta.SuccessCount,
				"failure_count":                  identity.FailureCount + delta.FailureCount,
				"input_tokens":                   identity.InputTokens + delta.InputTokens,
				"output_tokens":                  identity.OutputTokens + delta.OutputTokens,
				"reasoning_tokens":               identity.ReasoningTokens + delta.ReasoningTokens,
				"cached_tokens":                  identity.CachedTokens + delta.CachedTokens,
				"total_tokens":                   identity.TotalTokens + delta.TotalTokens,
				"first_used_at":                  firstUsedAt,
				"last_used_at":                   lastUsedAt,
				"stats_updated_at":               now,
				"last_aggregated_usage_event_id": delta.MaxUsageEventID,
			}
			if err := tx.Model(&models.UsageIdentity{}).Where("id = ?", identity.ID).Updates(updates).Error; err != nil {
				return fmt.Errorf("update usage identity stats for %q: %w", identity.Identity, err)
			}
		}
		return nil
	})
}

type usageIdentityStatsDelta struct {
	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
	FirstUsedAt     *time.Time
	LastUsedAt      *time.Time
	MaxUsageEventID uint
}

func aggregateUsageIdentityDelta(tx *gorm.DB, identity models.UsageIdentity) (usageIdentityStatsDelta, error) {
	var delta usageIdentityStatsDelta
	query, ok := usageIdentityEventsQuery(tx.Model(&models.UsageEvent{}), identity)
	if !ok {
		return delta, nil
	}

	if err := query.
		Select(`
			COUNT(*) AS total_requests,
			COALESCE(SUM(CASE WHEN failed THEN 0 ELSE 1 END), 0) AS success_count,
			COALESCE(SUM(CASE WHEN failed THEN 1 ELSE 0 END), 0) AS failure_count,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(reasoning_tokens), 0) AS reasoning_tokens,
			COALESCE(SUM(cached_tokens), 0) AS cached_tokens,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			COALESCE(MAX(id), 0) AS max_usage_event_id`).
		Where("id > ?", identity.LastAggregatedUsageEventID).
		Scan(&delta).Error; err != nil {
		return delta, fmt.Errorf("aggregate usage identity stats for %q: %w", identity.Identity, err)
	}
	if delta.TotalRequests == 0 {
		return delta, nil
	}

	var firstEvent models.UsageEvent
	firstQuery, _ := usageIdentityEventsQuery(tx.Model(&models.UsageEvent{}), identity)
	if err := firstQuery.Where("id > ?", identity.LastAggregatedUsageEventID).Order("timestamp asc, id asc").First(&firstEvent).Error; err != nil {
		return delta, fmt.Errorf("find first usage identity event for %q: %w", identity.Identity, err)
	}
	firstUsedAt := firstEvent.Timestamp
	delta.FirstUsedAt = &firstUsedAt

	var lastEvent models.UsageEvent
	lastQuery, _ := usageIdentityEventsQuery(tx.Model(&models.UsageEvent{}), identity)
	if err := lastQuery.Where("id > ?", identity.LastAggregatedUsageEventID).Order("timestamp desc, id desc").First(&lastEvent).Error; err != nil {
		return delta, fmt.Errorf("find last usage identity event for %q: %w", identity.Identity, err)
	}
	lastUsedAt := lastEvent.Timestamp
	delta.LastUsedAt = &lastUsedAt

	return delta, nil
}

func usageIdentityEventsQuery(query *gorm.DB, identity models.UsageIdentity) (*gorm.DB, bool) {
	switch identity.AuthType {
	case models.UsageIdentityAuthTypeAuthFile:
		return query.Where("auth_type = ? AND auth_index = ?", "oauth", identity.Identity), true
	case models.UsageIdentityAuthTypeAIProvider:
		return query.Where("auth_type = ? AND source = ?", "apikey", identity.Identity), true
	default:
		return query, false
	}
}

func normalizeUsageIdentities(identities []models.UsageIdentity, authType models.UsageIdentityAuthType) ([]models.UsageIdentity, []string) {
	normalized := make([]models.UsageIdentity, 0, len(identities))
	incomingIdentities := make([]string, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))

	for _, identity := range identities {
		key := strings.TrimSpace(identity.Identity)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		incomingIdentities = append(incomingIdentities, key)

		identity.ID = 0
		identity.AuthType = authType
		identity.Identity = key
		identity.Name = strings.TrimSpace(identity.Name)
		identity.AuthTypeName = strings.TrimSpace(identity.AuthTypeName)
		identity.Type = strings.TrimSpace(identity.Type)
		identity.Provider = strings.TrimSpace(identity.Provider)
		identity.IsDeleted = false
		identity.DeletedAt = nil
		normalized = append(normalized, identity)
	}

	return normalized, incomingIdentities
}

func normalizeProviderTypes(providerTypes []string) []string {
	seen := make(map[string]struct{}, len(providerTypes))
	types := make([]string, 0, len(providerTypes))
	for _, providerType := range providerTypes {
		providerType = strings.TrimSpace(providerType)
		if providerType == "" {
			continue
		}
		if _, ok := seen[providerType]; ok {
			continue
		}
		seen[providerType] = struct{}{}
		types = append(types, providerType)
	}
	return types
}

func upsertUsageIdentities(tx *gorm.DB, identities []models.UsageIdentity) error {
	if len(identities) == 0 {
		return nil
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "auth_type"}, {Name: "identity"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name":           gorm.Expr("excluded.name"),
			"auth_type_name": gorm.Expr("excluded.auth_type_name"),
			"type":           gorm.Expr("excluded.type"),
			"provider":       gorm.Expr("excluded.provider"),
			"is_deleted":     false,
			"deleted_at":     nil,
			"updated_at":     gorm.Expr("excluded.updated_at"),
		}),
	}).Create(&identities).Error; err != nil {
		return fmt.Errorf("upsert usage identities: %w", err)
	}
	return nil
}
