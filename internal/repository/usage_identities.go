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

	// 先统一清洗和去重输入，后续 upsert 与 stale 判断都使用同一组 identity。
	normalized, incomingIdentities := normalizeUsageIdentities(identities, authType)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先写入或恢复本次同步到的身份，确保 CPA 返回的 deleted row 会重新变为 active。
		if err := upsertUsageIdentities(tx, normalized); err != nil {
			return err
		}

		// 再按 auth_type 范围只对当前 active 身份做 stale 对比；未返回且已 deleted 的历史行不刷新 deleted_at。
		return markStaleUsageIdentitiesDeleted(
			tx,
			tx.Model(&models.UsageIdentity{}).Where("auth_type = ? AND is_deleted = ?", authType, false),
			incomingIdentities,
			now,
			"mark stale usage identities deleted",
		)
	})
}

func ReplaceUsageIdentitiesForProviderTypes(ctx context.Context, db *gorm.DB, identities []models.UsageIdentity, providerTypes []string, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	// Provider metadata 只允许刷新 AI provider 身份，输入类型和 identity 先统一规范化。
	normalized, incomingIdentities := normalizeUsageIdentities(identities, models.UsageIdentityAuthTypeAIProvider)
	types := normalizeProviderTypes(providerTypes)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先 upsert 本次成功拉到的 provider identity，CPA 返回的历史 deleted provider 会在这里恢复 active。
		if err := upsertUsageIdentities(tx, normalized); err != nil {
			return err
		}
		if len(types) == 0 {
			return nil
		}

		// fetched provider type 也按批次切分，避免极端情况下 type IN 变量过多。
		for start := 0; start < len(types); start += defaultRepositoryInsertBatchSize {
			end := min(start+defaultRepositoryInsertBatchSize, len(types))
			// 每批只处理本次成功 fetch 的 provider type；未返回且仍 active 的身份才会被标记 deleted。
			query := tx.Model(&models.UsageIdentity{}).
				Where("auth_type = ? AND is_deleted = ?", models.UsageIdentityAuthTypeAIProvider, false).
				Where("type IN ?", types[start:end])
			if err := markStaleUsageIdentitiesDeleted(tx, query, incomingIdentities, now, "mark stale provider usage identities deleted"); err != nil {
				return err
			}
		}

		return nil
	})
}

func ListUsageIdentities(ctx context.Context, db *gorm.DB) ([]models.UsageIdentity, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	// usage identities 页面需要展示 active/deleted 全量历史，因此这里不加 is_deleted 条件。
	var identities []models.UsageIdentity
	if err := db.WithContext(ctx).Order("auth_type asc, name asc, id asc").Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("list usage identities: %w", err)
	}
	return identities, nil
}

func ListActiveUsageIdentities(ctx context.Context, db *gorm.DB) ([]models.UsageIdentity, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	// 解析和筛选场景只需要活跃身份，直接在 SQL 层过滤 deleted rows，避免无效数据进入内存 resolver。
	var identities []models.UsageIdentity
	if err := db.WithContext(ctx).Where("is_deleted = ?", false).Order("auth_type asc, name asc, id asc").Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("list active usage identities: %w", err)
	}
	return identities, nil
}

func AggregateUsageIdentityStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	// 聚合统计需要覆盖 active/deleted 全量身份，避免历史已删除身份停止累计对应 usage_events。
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
		authIndex := strings.TrimSpace(identity.Identity)
		if authIndex == "" {
			continue
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		incomingIdentities = append(incomingIdentities, authIndex)

		identity.ID = 0
		identity.AuthType = authType
		identity.Identity = authIndex
		identity.Name = strings.TrimSpace(identity.Name)
		identity.AuthTypeName = strings.TrimSpace(identity.AuthTypeName)
		identity.Type = strings.TrimSpace(identity.Type)
		identity.Provider = strings.TrimSpace(identity.Provider)
		identity.LookupKey = strings.TrimSpace(identity.LookupKey)
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

func markStaleUsageIdentitiesDeleted(tx *gorm.DB, query *gorm.DB, incomingIdentities []string, now time.Time, context string) error {
	// 把本次同步到的 identity 放进内存集合，避免生成超大的 identity NOT IN SQL。
	incoming := make(map[string]struct{}, len(incomingIdentities))
	for _, identity := range incomingIdentities {
		incoming[identity] = struct{}{}
	}

	// 只从数据库读取候选行的最小字段，后续在 Go 中判断哪些行已经 stale。
	var candidates []struct {
		ID       uint
		Identity string
	}
	if err := query.Select("id, identity").Find(&candidates).Error; err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}

	// 候选行中没有出现在本次输入里的 ID，就是需要标记删除的 stale 数据。
	staleIDs := make([]uint, 0)
	for _, candidate := range candidates {
		if _, ok := incoming[candidate.Identity]; ok {
			continue
		}
		staleIDs = append(staleIDs, candidate.ID)
	}

	// stale ID 也按批次更新，避免 id IN 在数据量大时再次触发 SQLite 变量上限。
	for start := 0; start < len(staleIDs); start += defaultRepositoryInsertBatchSize {
		end := min(start+defaultRepositoryInsertBatchSize, len(staleIDs))
		if err := tx.Model(&models.UsageIdentity{}).
			Where("id IN ?", staleIDs[start:end]).
			Updates(map[string]any{"is_deleted": true, "deleted_at": now}).Error; err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
	}
	return nil
}

func upsertUsageIdentities(tx *gorm.DB, identities []models.UsageIdentity) error {
	if len(identities) == 0 {
		return nil
	}

	// 使用相同的冲突更新语义，CPA 本次返回的身份需要恢复为 active，同时保留历史统计字段。
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "auth_type"}, {Name: "identity"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name":           gorm.Expr("excluded.name"),
			"auth_type_name": gorm.Expr("excluded.auth_type_name"),
			"type":           gorm.Expr("excluded.type"),
			"provider":       gorm.Expr("excluded.provider"),
			"lookup_key":     gorm.Expr("excluded.lookup_key"),
			"is_deleted":     false,
			"deleted_at":     nil,
			"updated_at":     gorm.Expr("excluded.updated_at"),
		}),
	}).CreateInBatches(&identities, defaultRepositoryInsertBatchSize).Error; err != nil {
		return fmt.Errorf("upsert usage identities: %w", err)
	}
	return nil
}
