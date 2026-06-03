package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

func ReplaceUsageIdentitiesForAuthType(ctx context.Context, db *gorm.DB, identities []entities.UsageIdentity, authType entities.UsageIdentityAuthType, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	// 先统一清洗和去重输入，后续 upsert 与 stale 判断都使用同一组 identity。
	normalized, incomingIdentities := normalizeUsageIdentities(identities, authType)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existingRows, err := listUsageIdentitySyncRows(tx.Model(&entities.UsageIdentity{}).Where("auth_type = ?", authType))
		if err != nil {
			return fmt.Errorf("list usage identities for sync: %w", err)
		}
		// 先写入或恢复本次同步到的身份，确保 CPA 返回的 deleted row 会重新变为 active。
		if err := syncUsageIdentities(tx, normalized, existingRows); err != nil {
			return err
		}

		// 再按 auth_type 范围只对当前 active 身份做 stale 对比；未返回且已 deleted 的历史行不刷新 deleted_at。
		return markStaleUsageIdentityRowsDeleted(tx, existingRows, incomingIdentities, now, "mark stale usage identities deleted")
	})
}

func ReplaceUsageIdentitiesForProviderTypes(ctx context.Context, db *gorm.DB, identities []entities.UsageIdentity, providerTypes []string, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	// Provider metadata 只允许刷新 AI provider 身份，输入类型和 identity 先统一规范化。
	normalized, incomingIdentities := normalizeUsageIdentities(identities, entities.UsageIdentityAuthTypeAIProvider)
	types := normalizeProviderTypes(providerTypes)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existingRows, err := listUsageIdentitySyncRows(tx.Model(&entities.UsageIdentity{}).Where("auth_type = ?", entities.UsageIdentityAuthTypeAIProvider))
		if err != nil {
			return fmt.Errorf("list provider usage identities for sync: %w", err)
		}
		// 先同步本次成功拉到的 provider identity，CPA 返回的历史 deleted provider 会在这里恢复 active。
		if err := syncUsageIdentities(tx, normalized, existingRows); err != nil {
			return err
		}
		if len(types) == 0 {
			return nil
		}

		// fetched provider type 也按批次切分，避免极端情况下 type IN 变量过多。
		for start := 0; start < len(types); start += insertBatchSize(entities.UsageIdentity{}) {
			end := min(start+insertBatchSize(entities.UsageIdentity{}), len(types))
			// 每批只处理本次成功 fetch 的 provider type；未返回且仍 active 的身份才会被标记 deleted。
			staleRows, err := listUsageIdentitySyncRows(tx.Model(&entities.UsageIdentity{}).
				Where("auth_type = ? AND is_deleted = ?", entities.UsageIdentityAuthTypeAIProvider, false).
				Where("type IN ?", types[start:end]))
			if err != nil {
				return fmt.Errorf("list stale provider usage identities: %w", err)
			}
			if err := markStaleUsageIdentityRowsDeleted(tx, staleRows, incomingIdentities, now, "mark stale provider usage identities deleted"); err != nil {
				return err
			}
		}

		return nil
	})
}

type ListUsageIdentitiesPageRequest struct {
	AuthType   *entities.UsageIdentityAuthType
	ActiveOnly *bool
	Types      []string
	Sort       string
	Page       int
	PageSize   int
}

const (
	UsageIdentityPageSortPriority      = "priority"
	UsageIdentityPageSortTotalRequests = "total_requests"
	UsageIdentityPageSortTotalTokens   = "total_tokens"
)

const usageIdentityReadColumns = "id, name, auth_type, auth_type_name, identity, type, provider, lookup_key, prefix, base_url, file_name, file_path, priority, disabled, note, account_id, project_id, active_start, active_until, plan_type, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at, stats_updated_at, is_deleted, created_at, updated_at, deleted_at"

const usageIdentityAggregationColumns = "id, auth_type, identity, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at"

func ListUsageIdentities(ctx context.Context, db *gorm.DB) ([]entities.UsageIdentity, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	// usage identities 页面需要展示 active/deleted 全量历史，因此这里不加 is_deleted 条件。
	var identities []entities.UsageIdentity
	if err := db.WithContext(ctx).Select(usageIdentityReadColumns).Order("auth_type asc, name asc, id asc").Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("list usage identities: %w", err)
	}
	return identities, nil
}

func ListActiveUsageIdentities(ctx context.Context, db *gorm.DB) ([]entities.UsageIdentity, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	// 解析和筛选场景只需要活跃身份，直接在 SQL 层过滤 deleted rows，避免无效数据进入内存 resolver。
	var identities []entities.UsageIdentity
	if err := activeUsageIdentitiesQuery(db.WithContext(ctx), nil).Select(usageIdentityReadColumns).Order("auth_type asc, name asc, id asc").Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("list active usage identities: %w", err)
	}
	return identities, nil
}

func ListActiveUsageIdentitiesPage(ctx context.Context, db *gorm.DB, request ListUsageIdentitiesPageRequest) ([]entities.UsageIdentity, int64, []dto.UsageIdentityTypeCount, error) {
	if db == nil {
		return nil, 0, nil, fmt.Errorf("database is nil")
	}
	page := request.Page
	if page <= 0 {
		page = 1
	}
	pageSize := request.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	types := normalizeUsageIdentityTypes(request.Types)

	// type_counts 只受 auth_type/active_only 影响，不受当前 type 筛选影响，方便前端保持完整筛选按钮。
	typeCounts, err := ListActiveUsageIdentityTypeCounts(ctx, db, request)
	if err != nil {
		return nil, 0, nil, err
	}

	// 先在同一过滤条件下统计总数，再追加 offset/limit 取当前页数据。
	query := activeUsageIdentitiesPageBaseQuery(db.WithContext(ctx), request.AuthType, request.ActiveOnly)
	query = applyUsageIdentityTypesFilter(query, types)
	var total int64
	if err := query.Model(&entities.UsageIdentity{}).Count(&total).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("count active usage identities page: %w", err)
	}
	var identities []entities.UsageIdentity
	if err := applyUsageIdentityPageSort(query.Select(usageIdentityReadColumns), request.Sort, request.AuthType).Offset((page - 1) * pageSize).Limit(pageSize).Find(&identities).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("list active usage identities page: %w", err)
	}
	return identities, total, typeCounts, nil
}

func ListActiveUsageIdentityTypeCounts(ctx context.Context, db *gorm.DB, request ListUsageIdentitiesPageRequest) ([]dto.UsageIdentityTypeCount, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	var counts []dto.UsageIdentityTypeCount
	// 按数据库原始 type 聚合，不做 lower/alias/归一化；展示归并交给前端映射层。
	if err := activeUsageIdentitiesPageBaseQuery(db.WithContext(ctx), request.AuthType, request.ActiveOnly).
		Model(&entities.UsageIdentity{}).
		Select("type, COUNT(*) AS count").
		Group("type").
		Order("type ASC").
		Scan(&counts).Error; err != nil {
		return nil, fmt.Errorf("count active usage identity types: %w", err)
	}
	return counts, nil
}

func activeUsageIdentitiesQuery(db *gorm.DB, authType *entities.UsageIdentityAuthType) *gorm.DB {
	// 把活跃条件和可选 auth_type 条件集中到一个查询构造器，避免 count/list 条件漂移。
	query := db.Where("is_deleted = ?", false)
	if authType != nil {
		query = query.Where("auth_type = ?", *authType)
	}
	return query
}

func activeUsageIdentitiesPageBaseQuery(db *gorm.DB, authType *entities.UsageIdentityAuthType, activeOnly *bool) *gorm.DB {
	query := activeUsageIdentitiesQuery(db, authType)
	if activeOnly != nil && *activeOnly {
		query = query.Where("disabled IS NULL OR disabled = ?", false)
	}
	return query
}

func applyUsageIdentityTypesFilter(query *gorm.DB, types []string) *gorm.DB {
	switch len(types) {
	case 0:
		return query
	case 1:
		return query.Where("type = ?", types[0])
	default:
		return query.Where("type IN ?", types)
	}
}

func applyUsageIdentityPageSort(query *gorm.DB, sort string, authType *entities.UsageIdentityAuthType) *gorm.DB {
	switch sort {
	case UsageIdentityPageSortPriority:
		// Auth Files 的 priority 同分需要稳定按名称排列；AI Provider 只保留同步顺序兜底。
		query = query.Order("priority IS NULL ASC").Order("priority DESC")
		if authType != nil && *authType == entities.UsageIdentityAuthTypeAuthFile {
			query = query.Order("LOWER(name) ASC")
		}
		return query.Order("id ASC")
	case UsageIdentityPageSortTotalTokens:
		return query.Order("total_tokens DESC").Order("id ASC")
	default:
		return query.Order("total_requests DESC").Order("id ASC")
	}
}

func GetActiveAuthFileUsageIdentityByAuthIndex(ctx context.Context, db *gorm.DB, authIndex string) (entities.UsageIdentity, error) {
	var identity entities.UsageIdentity
	if db == nil {
		return identity, fmt.Errorf("database is nil")
	}
	if err := db.WithContext(ctx).
		Select(usageIdentityReadColumns).
		Where("auth_type = ? AND identity = ? AND is_deleted = ?", entities.UsageIdentityAuthTypeAuthFile, strings.TrimSpace(authIndex), false).
		First(&identity).Error; err != nil {
		return identity, fmt.Errorf("get active auth file usage identity by auth index: %w", err)
	}
	return identity, nil
}

func AggregateUsageIdentityStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}

	// 聚合统计需要覆盖 active/deleted 全量身份，避免历史已删除身份停止累计对应 usage_events。
	var identities []entities.UsageIdentity
	if err := db.WithContext(ctx).Select(usageIdentityAggregationColumns).Find(&identities).Error; err != nil {
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
				"first_used_at":                  formatStorageTimePtr(firstUsedAt),
				"last_used_at":                   formatStorageTimePtr(lastUsedAt),
				"stats_updated_at":               timeutil.FormatStorageTime(now),
				"last_aggregated_usage_event_id": delta.MaxUsageEventID,
			}
			if err := tx.Model(&entities.UsageIdentity{}).Where("id = ?", identity.ID).Updates(updates).Error; err != nil {
				return fmt.Errorf("update usage identity stats for %q: %w", identity.Identity, err)
			}
		}
		return nil
	})
}

func aggregateUsageIdentityDelta(tx *gorm.DB, identity entities.UsageIdentity) (dto.UsageIdentityStatsDelta, error) {
	var delta dto.UsageIdentityStatsDelta
	// 先按 identity 类型生成 usage_events 过滤条件，避免对无关事件做聚合。
	query, ok := usageIdentityEventsQuery(tx.Model(&entities.UsageEvent{}), identity)
	if !ok {
		return delta, nil
	}

	// 再用 last_aggregated_usage_event_id 做增量游标，只累计上次之后的新事件。
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

	// 统计总量不包含首尾时间，首尾时间用同一组身份过滤条件分别取最早和最晚事件。
	var firstEvent struct {
		Timestamp time.Time
	}
	firstQuery, _ := usageIdentityEventsQuery(tx.Model(&entities.UsageEvent{}), identity)
	if err := firstQuery.Select("timestamp").Where("id > ?", identity.LastAggregatedUsageEventID).Order("timestamp asc, id asc").First(&firstEvent).Error; err != nil {
		return delta, fmt.Errorf("find first usage identity event for %q: %w", identity.Identity, err)
	}
	firstUsedAt := firstEvent.Timestamp
	delta.FirstUsedAt = &firstUsedAt

	var lastEvent struct {
		Timestamp time.Time
	}
	lastQuery, _ := usageIdentityEventsQuery(tx.Model(&entities.UsageEvent{}), identity)
	if err := lastQuery.Select("timestamp").Where("id > ?", identity.LastAggregatedUsageEventID).Order("timestamp desc, id desc").First(&lastEvent).Error; err != nil {
		return delta, fmt.Errorf("find last usage identity event for %q: %w", identity.Identity, err)
	}
	lastUsedAt := lastEvent.Timestamp
	delta.LastUsedAt = &lastUsedAt

	return delta, nil
}

func usageIdentityEventsQuery(query *gorm.DB, identity entities.UsageIdentity) (*gorm.DB, bool) {
	var eventAuthType string
	switch identity.AuthType {
	case entities.UsageIdentityAuthTypeAuthFile:
		eventAuthType = "oauth"
	case entities.UsageIdentityAuthTypeAIProvider:
		eventAuthType = "apikey"
	default:
		return query, false
	}

	// usage_events 和 usage_identities 只通过 auth_index 与 identity 精确关联。
	return query.Where("auth_type = ? AND auth_index = ?", eventAuthType, identity.Identity), true
}

func normalizeUsageIdentities(identities []entities.UsageIdentity, authType entities.UsageIdentityAuthType) ([]entities.UsageIdentity, []string) {
	normalized := make([]entities.UsageIdentity, 0, len(identities))
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
		identity.Prefix = strings.TrimSpace(identity.Prefix)
		identity.BaseURL = strings.TrimSpace(identity.BaseURL)
		identity.FileName = trimOptionalString(identity.FileName)
		identity.FilePath = trimOptionalString(identity.FilePath)
		identity.Note = trimOptionalString(identity.Note)
		identity.AccountID = trimOptionalString(identity.AccountID)
		identity.ProjectID = trimOptionalString(identity.ProjectID)
		identity.PlanType = trimOptionalString(identity.PlanType)
		identity.IsDeleted = false
		identity.ActiveStart = normalizeStorageTimePtr(identity.ActiveStart)
		identity.ActiveUntil = normalizeStorageTimePtr(identity.ActiveUntil)
		identity.DeletedAt = nil
		normalized = append(normalized, identity)
	}

	return normalized, incomingIdentities
}

func normalizeStorageTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := timeutil.NormalizeStorageTime(*value)
	return &normalized
}

func formatStorageTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return timeutil.FormatStorageTime(*value)
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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

func normalizeUsageIdentityTypes(identityTypes []string) []string {
	seen := make(map[string]struct{}, len(identityTypes))
	types := make([]string, 0, len(identityTypes))
	for _, identityType := range identityTypes {
		identityType = strings.TrimSpace(identityType)
		if identityType == "" {
			continue
		}
		if _, ok := seen[identityType]; ok {
			continue
		}
		seen[identityType] = struct{}{}
		types = append(types, identityType)
	}
	return types
}

type usageIdentitySyncRow struct {
	ID        int64
	AuthType  entities.UsageIdentityAuthType
	Identity  string
	IsDeleted bool
}

func listUsageIdentitySyncRows(query *gorm.DB) ([]usageIdentitySyncRow, error) {
	var rows []usageIdentitySyncRow
	if err := query.Select("id, auth_type, identity, is_deleted").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func markStaleUsageIdentityRowsDeleted(tx *gorm.DB, rows []usageIdentitySyncRow, incomingIdentities []string, now time.Time, context string) error {
	// 把本次同步到的 identity 放进内存集合，避免生成超大的 identity NOT IN SQL。
	incoming := make(map[string]struct{}, len(incomingIdentities))
	for _, identity := range incomingIdentities {
		incoming[identity] = struct{}{}
	}

	// 候选行中没有出现在本次输入里的 active ID，就是需要标记删除的 stale 数据。
	staleIDs := make([]int64, 0)
	for _, row := range rows {
		if row.IsDeleted {
			continue
		}
		if _, ok := incoming[row.Identity]; ok {
			continue
		}
		staleIDs = append(staleIDs, row.ID)
	}

	// stale ID 也按批次更新，避免 id IN 在数据量大时再次触发 SQLite 变量上限。
	for start := 0; start < len(staleIDs); start += insertBatchSize(entities.UsageIdentity{}) {
		end := min(start+insertBatchSize(entities.UsageIdentity{}), len(staleIDs))
		if err := tx.Model(&entities.UsageIdentity{}).
			Where("id IN ?", staleIDs[start:end]).
			Updates(map[string]any{"is_deleted": true, "deleted_at": timeutil.FormatStorageTime(now)}).Error; err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
	}
	return nil
}

func syncUsageIdentities(tx *gorm.DB, identities []entities.UsageIdentity, existingRows []usageIdentitySyncRow) error {
	if len(identities) == 0 {
		return nil
	}

	existingByKey := make(map[string]usageIdentitySyncRow, len(existingRows))
	for _, row := range existingRows {
		existingByKey[usageIdentitySyncKey(row.AuthType, row.Identity)] = row
	}

	toCreate := make([]entities.UsageIdentity, 0)
	for _, identity := range identities {
		if existing, ok := existingByKey[usageIdentitySyncKey(identity.AuthType, identity.Identity)]; ok {
			if err := tx.Model(&entities.UsageIdentity{}).Where("id = ?", existing.ID).Updates(usageIdentityMetadataUpdates(identity)).Error; err != nil {
				return fmt.Errorf("update usage identity: %w", err)
			}
			continue
		}
		toCreate = append(toCreate, identity)
	}
	if len(toCreate) == 0 {
		return nil
	}
	if err := tx.CreateInBatches(&toCreate, insertBatchSize(entities.UsageIdentity{})).Error; err != nil {
		return fmt.Errorf("create usage identities: %w", err)
	}
	return nil
}

func usageIdentitySyncKey(authType entities.UsageIdentityAuthType, identity string) string {
	return fmt.Sprintf("%d:%s", authType, identity)
}

func usageIdentityMetadataUpdates(identity entities.UsageIdentity) map[string]any {
	return map[string]any{
		"name":           identity.Name,
		"auth_type_name": identity.AuthTypeName,
		"type":           identity.Type,
		"provider":       identity.Provider,
		"lookup_key":     identity.LookupKey,
		"prefix":         identity.Prefix,
		"base_url":       identity.BaseURL,
		"file_name":      identity.FileName,
		"file_path":      identity.FilePath,
		"priority":       identity.Priority,
		"disabled":       identity.Disabled,
		"note":           identity.Note,
		"account_id":     identity.AccountID,
		"project_id":     identity.ProjectID,
		"active_start":   identity.ActiveStart,
		"active_until":   identity.ActiveUntil,
		"plan_type":      identity.PlanType,
		"is_deleted":     false,
		"deleted_at":     nil,
		"updated_at":     identity.UpdatedAt,
	}
}
