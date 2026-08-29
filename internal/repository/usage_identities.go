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
	"gorm.io/plugin/dbresolver"
)

// HasPendingUsageIdentityAggregation 只判断是否存在超过任一身份行水位的匹配事件。
func HasPendingUsageIdentityAggregation(ctx context.Context, db *gorm.DB) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("database is nil")
	}
	pending, err := hasPendingUsageIdentityAggregation(db.Clauses(dbresolver.Read).WithContext(ctx))
	if err != nil {
		return false, fmt.Errorf("check pending usage identity aggregation: %w", err)
	}
	return pending, nil
}

func hasPendingUsageIdentityAggregation(db *gorm.DB) (bool, error) {
	var pending int
	err := db.Raw(`SELECT EXISTS (
		SELECT 1
		FROM usage_identities AS identity
		JOIN usage_events AS event
		  ON event.id > identity.last_aggregated_usage_event_id
		 AND event.auth_index = identity.identity
		 AND ((identity.auth_type = ? AND event.auth_type = ?) OR (identity.auth_type = ? AND event.auth_type = ?))
		LIMIT 1
	)`, entities.UsageIdentityAuthTypeAuthFile, "oauth", entities.UsageIdentityAuthTypeAIProvider, "apikey").Scan(&pending).Error
	if err != nil {
		return false, err
	}
	return pending != 0, nil
}

func ReplaceUsageIdentitiesForAuthType(ctx context.Context, db *gorm.DB, identities []entities.UsageIdentity, authType entities.UsageIdentityAuthType, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 零时间无法形成可靠的 metadata 版本，必须在任何数据库读取或写入前拒绝。
	if now.IsZero() {
		// 明确错误阻止调用方把零值继续传播到 identity 行。
		return fmt.Errorf("usage identity sync time is zero")
	}
	// 本轮所有 create、refresh、restore 与 stale 路径共用同一个项目存储时区时间。
	normalizedNow := timeutil.NormalizeStorageTime(now)

	// 先统一清洗和去重输入，后续 upsert 与 stale 判断都使用同一组 identity。
	normalized, incomingIdentities := normalizeUsageIdentities(identities, authType)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existingRows, err := listUsageIdentitySyncRows(tx.Model(&entities.UsageIdentity{}).Where("auth_type = ?", authType))
		if err != nil {
			return fmt.Errorf("list usage identities for sync: %w", err)
		}
		// 先写入或恢复本次同步到的身份，确保 CPA 返回的 deleted row 会重新变为 active。
		if err := syncUsageIdentities(tx, normalized, existingRows, normalizedNow); err != nil {
			return err
		}

		// 再按 auth_type 范围只对当前 active 身份做 stale 对比；未返回且已 deleted 的历史行不刷新 deleted_at。
		return markStaleUsageIdentityRowsDeleted(tx, existingRows, incomingIdentities, normalizedNow, "mark stale usage identities deleted")
	})
}

func ReplaceUsageIdentitiesForProviderTypes(ctx context.Context, db *gorm.DB, identities []entities.UsageIdentity, providerTypes []string, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// Provider replace 同样在事务前拒绝零时间，避免部分 provider scope 被错误刷新。
	if now.IsZero() {
		// 与 Auth File 入口返回同一明确错误，保持调用方处理简单。
		return fmt.Errorf("usage identity sync time is zero")
	}
	// provider 的所有成功类型共用一轮规范化时间，不能各自读取系统时钟。
	normalizedNow := timeutil.NormalizeStorageTime(now)

	// Provider metadata 只允许刷新 AI provider 身份，输入类型和 identity 先统一规范化。
	normalized, incomingIdentities := normalizeUsageIdentities(identities, entities.UsageIdentityAuthTypeAIProvider)
	types := normalizeProviderTypes(providerTypes)

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existingRows, err := listUsageIdentitySyncRows(tx.Model(&entities.UsageIdentity{}).Where("auth_type = ?", entities.UsageIdentityAuthTypeAIProvider))
		if err != nil {
			return fmt.Errorf("list provider usage identities for sync: %w", err)
		}
		// 先同步本次成功拉到的 provider identity，CPA 返回的历史 deleted provider 会在这里恢复 active。
		if err := syncUsageIdentities(tx, normalized, existingRows, normalizedNow); err != nil {
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
			if err := markStaleUsageIdentityRowsDeleted(tx, staleRows, incomingIdentities, normalizedNow, "mark stale provider usage identities deleted"); err != nil {
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
	UsageIdentityPageSortLastUsedAt    = "last_used_at"
)

const usageIdentityReadColumns = "id, name, alias, auth_type, auth_type_name, identity, type, provider, lookup_key, prefix, base_url, file_name, file_path, priority, disabled, note, account_id, project_id, xai_user_id, active_start, active_until, plan_type, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at, stats_updated_at, is_deleted, created_at, updated_at, deleted_at"

const usageIdentityAggregationColumns = "id, auth_type, identity, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at"

// UsageIdentityAggregationBatchSize 限制单个 Identity 写事务最多处理 25 行。
const UsageIdentityAggregationBatchSize = 25

const activeAuthFileUsageIdentityLookupBatchSize = 500

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

func FindUsageIdentityByID(ctx context.Context, db *gorm.DB, id int64) (entities.UsageIdentity, error) {
	var identity entities.UsageIdentity
	if db == nil {
		return identity, fmt.Errorf("database is nil")
	}
	if err := db.WithContext(ctx).
		Select(usageIdentityReadColumns).
		Where("id = ?", id).
		First(&identity).Error; err != nil {
		return identity, fmt.Errorf("find usage identity by id: %w", err)
	}
	return identity, nil
}

func UpdateUsageIdentityAlias(ctx context.Context, db *gorm.DB, id int64, alias string) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	trimmed := strings.TrimSpace(alias)
	var value any
	if trimmed != "" {
		value = trimmed
	}
	result := db.WithContext(ctx).
		Model(&entities.UsageIdentity{}).
		Where("id = ? AND is_deleted = ?", id, false).
		Update("alias", value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
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
		// Auth Files 的 priority 同分按 CPA 文件名排列；AI Provider 只保留同步顺序兜底。
		query = query.Order("priority IS NULL ASC").Order("priority DESC")
		if authType != nil && *authType == entities.UsageIdentityAuthTypeAuthFile {
			query = query.Order("LOWER(file_name) ASC")
		}
		return query.Order("id ASC")
	case UsageIdentityPageSortTotalTokens:
		return query.Order("total_tokens DESC").Order("id ASC")
	case UsageIdentityPageSortLastUsedAt:
		return query.Order("last_used_at IS NULL ASC").Order("last_used_at DESC").Order("id ASC")
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

func ListActiveAuthFileUsageIdentitiesByAuthIndexes(ctx context.Context, db *gorm.DB, authIndexes []string) ([]entities.UsageIdentity, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	authIndexes = normalizeUniqueAuthIndexes(authIndexes)
	if len(authIndexes) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	identities := make([]entities.UsageIdentity, 0, len(authIndexes))
	queryDB := db.WithContext(ctx)
	for start := 0; start < len(authIndexes); start += activeAuthFileUsageIdentityLookupBatchSize {
		end := min(start+activeAuthFileUsageIdentityLookupBatchSize, len(authIndexes))
		var batch []entities.UsageIdentity
		if err := queryDB.
			Select(usageIdentityReadColumns).
			Where("auth_type = ? AND identity IN ? AND is_deleted = ?", entities.UsageIdentityAuthTypeAuthFile, authIndexes[start:end], false).
			Find(&batch).Error; err != nil {
			return nil, fmt.Errorf("list active auth file usage identities by auth indexes: %w", err)
		}
		identities = append(identities, batch...)
	}
	return identities, nil
}

func normalizeUniqueAuthIndexes(authIndexes []string) []string {
	normalized := make([]string, 0, len(authIndexes))
	seen := make(map[string]struct{}, len(authIndexes))
	for _, authIndex := range authIndexes {
		authIndex = strings.TrimSpace(authIndex)
		if authIndex == "" {
			continue
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		normalized = append(normalized, authIndex)
	}
	return normalized
}

// UsageIdentityAggregationBatchResult 把 repository 页面 cursor 交给单 writer runner 继续调度。
type UsageIdentityAggregationBatchResult struct {
	// ProcessedIdentities 是本事务真正写入新 usage delta 的 identity 行数。
	ProcessedIdentities int
	// LastIdentityID 是下一批 id > cursor 查询使用的内存 cursor。
	LastIdentityID int64
	// ReachedEnd 表示当前 ID 扫描已经到达一轮末尾。
	ReachedEnd bool
}

// AggregateUsageIdentityStats 循环执行有界 identity pages，保留现有完整 catch-up API。
func AggregateUsageIdentityStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	// nil 数据库无法执行 identity catch-up。
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 完整 catch-up 固定同一个项目时区 now，保持所有 batch 的 stats_updated_at 一致。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	// 每次完整扫描从最小 identity ID 开始。
	afterIdentityID := int64(0)
	// 每轮只提交一个最多 25 identities 的事务。
	for {
		// 单批函数返回下一页 cursor 和是否已到一轮末尾。
		result, err := AggregateUsageIdentityStatsBatch(ctx, db, normalizedNow, afterIdentityID)
		// 任一 batch 失败立即停止，前面已提交 identity cursors 供下次幂等恢复。
		if err != nil {
			return err
		}
		// 到达当前 identity ID 末尾后完整 catch-up 结束。
		if result.ReachedEnd {
			return nil
		}
		// 下一批只读取本批最后 ID 之后的 identities。
		afterIdentityID = result.LastIdentityID
	}
}

// AggregateUsageIdentityStatsBatch 在一个短事务内处理一页 active/deleted identities。
func AggregateUsageIdentityStatsBatch(ctx context.Context, db *gorm.DB, now time.Time, afterIdentityID int64) (UsageIdentityAggregationBatchResult, error) {
	// 默认保留调用方 cursor，空页也能安全返回同一个位置。
	result := UsageIdentityAggregationBatchResult{LastIdentityID: afterIdentityID}
	// nil 数据库不能开启 identity 事务。
	if db == nil {
		return result, fmt.Errorf("database is nil")
	}
	// 负 cursor 没有合法 identity 语义，直接拒绝而不是静默重扫。
	if afterIdentityID < 0 {
		return result, fmt.Errorf("usage identity aggregation cursor is negative")
	}
	// 单批入口也归一化 now，保证 runner 直接调用时与完整入口一致。
	normalizedNow := timeutil.NormalizeStorageTime(now)

	// identity 列表读取、delta 查询和该页所有 identity 更新在同一短事务提交。
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 按 ID 升序读取固定一页，且不加 is_deleted 条件以保留 deleted 聚合语义。
		var identities []entities.UsageIdentity
		if err := tx.Select(usageIdentityAggregationColumns).
			Where("id > ?", afterIdentityID).
			Order("id asc").
			Limit(UsageIdentityAggregationBatchSize).
			Find(&identities).Error; err != nil {
			return fmt.Errorf("list usage identities for aggregation batch: %w", err)
		}
		// 空页明确表示当前扫描已到末尾。
		if len(identities) == 0 {
			result.ReachedEnd = true
			return nil
		}

		// 复用原逐 identity delta 和字段更新语义处理当前页，并记录真正更新的行数。
		processedIdentities, err := aggregateUsageIdentityRows(tx, identities, normalizedNow)
		// 任一 identity 查询或写入失败时整页回滚。
		if err != nil {
			return err
		}
		// 只记录存在新 delta 的 identities，避免已追平尾页被误判为持续工作。
		result.ProcessedIdentities = processedIdentities
		// identities 已按 ID 升序，最后一行就是下一页 cursor。
		result.LastIdentityID = identities[len(identities)-1].ID
		// 少于固定页大小表示本页已经覆盖当前末尾。
		result.ReachedEnd = len(identities) < UsageIdentityAggregationBatchSize
		return nil
	})
	// 事务失败时返回 error，调用方不得推进 in-memory cursor。
	return result, err
}

func aggregateUsageIdentityRows(tx *gorm.DB, identities []entities.UsageIdentity, now time.Time) (int, error) {
	// processedIdentities 只统计真正执行旧字段 UPDATE 的 identity。
	processedIdentities := 0
	// 当前页逐 identity 保留原 delta 查询和更新顺序。
	for _, identity := range identities {
		// delta 继续按 auth type、auth index 和每行 event cursor 查询。
		delta, err := aggregateUsageIdentityDelta(tx, identity)
		if err != nil {
			return 0, err
		}
		// 没有新事件时保持该 identity 所有统计和 cursor 不变。
		if delta.TotalRequests == 0 {
			continue
		}

		// 先保留 identity 已有 first_used_at。
		firstUsedAt := identity.FirstUsedAt
		// delta 更早或原值为空时才更新 first_used_at。
		if delta.FirstUsedAt != nil && (firstUsedAt == nil || delta.FirstUsedAt.Before(*firstUsedAt)) {
			first := *delta.FirstUsedAt
			firstUsedAt = &first
		}

		// 先保留 identity 已有 last_used_at。
		lastUsedAt := identity.LastUsedAt
		// delta 更晚或原值为空时才更新 last_used_at。
		if delta.LastUsedAt != nil && (lastUsedAt == nil || delta.LastUsedAt.After(*lastUsedAt)) {
			last := *delta.LastUsedAt
			lastUsedAt = &last
		}

		// 所有旧字段继续使用“已有总量 + 本次 delta”的原公式。
		updates := map[string]any{
			// total_requests 保留原有总量加 delta 公式。
			"total_requests": identity.TotalRequests + delta.TotalRequests,
			// success_count 保留原有总量加 delta 公式。
			"success_count": identity.SuccessCount + delta.SuccessCount,
			// failure_count 保留原有总量加 delta 公式。
			"failure_count": identity.FailureCount + delta.FailureCount,
			// input_tokens 保留原有总量加 delta 公式。
			"input_tokens": identity.InputTokens + delta.InputTokens,
			// output_tokens 保留原有总量加 delta 公式。
			"output_tokens": identity.OutputTokens + delta.OutputTokens,
			// reasoning_tokens 保留原有总量加 delta 公式。
			"reasoning_tokens": identity.ReasoningTokens + delta.ReasoningTokens,
			// cached_tokens 必须继续保留原有总量加 delta 公式。
			"cached_tokens": identity.CachedTokens + delta.CachedTokens,
			// cache_read_tokens 保留原有总量加 delta 公式。
			"cache_read_tokens": identity.CacheReadTokens + delta.CacheReadTokens,
			// total_tokens 保留原有总量加 delta 公式。
			"total_tokens": identity.TotalTokens + delta.TotalTokens,
			// first_used_at 只在前面比较后写回规范化值。
			"first_used_at": formatStorageTimePtr(firstUsedAt),
			// last_used_at 只在前面比较后写回规范化值。
			"last_used_at": formatStorageTimePtr(lastUsedAt),
			// stats_updated_at 使用当前有界事务固定 now。
			"stats_updated_at": timeutil.FormatStorageTime(now),
			// 每行 cursor 只推进到该 identity delta 的最大事件 ID。
			"last_aggregated_usage_event_id": delta.MaxUsageEventID,
		}
		// 单行 update 与该页其它 identities 共用事务，失败时整页回滚。
		if err := tx.Model(&entities.UsageIdentity{}).Where("id = ?", identity.ID).Updates(updates).Error; err != nil {
			return 0, fmt.Errorf("update usage identity stats for %q: %w", identity.Identity, err)
		}
		// UPDATE 成功后才把当前 identity 计入真正处理行数。
		processedIdentities++
	}
	// 返回当前页实际发生旧统计变化的 identity 数量。
	return processedIdentities, nil
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
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
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
		// alias 是 Keeper-only 展示覆盖，不参与 CPA 同步输入。
		identity.Alias = nil
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
		// stale 状态与 metadata 更新时间必须使用同一个调用方 now，不能依赖 GORM 隐式时钟。
		if err := tx.Model(&entities.UsageIdentity{}).
			Where("id IN ?", staleIDs[start:end]).
			Updates(map[string]any{"is_deleted": true, "deleted_at": timeutil.FormatStorageTime(now), "updated_at": timeutil.FormatStorageTime(now)}).Error; err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
	}
	return nil
}

// syncUsageIdentities 使用入口规范化后的统一 now 创建、刷新或恢复 identity。
func syncUsageIdentities(tx *gorm.DB, identities []entities.UsageIdentity, existingRows []usageIdentitySyncRow, now time.Time) error {
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
			// 既有 active 或 deleted 行都保留 created_at，并只用本轮 now 刷新 updated_at。
			if err := tx.Model(&entities.UsageIdentity{}).Where("id = ?", existing.ID).Updates(usageIdentityMetadataUpdates(identity, now)).Error; err != nil {
				return fmt.Errorf("update usage identity: %w", err)
			}
			continue
		}
		// 新行的 created_at 来自本轮统一时间，避免 GORM 为不同批次生成不同时间。
		identity.CreatedAt = now
		// 新行的 updated_at 与 created_at 完全一致，建立首次 metadata 版本。
		identity.UpdatedAt = now
		// 只有真正不存在的 identity 才进入批量创建，既有 ID 不受影响。
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

// usageIdentityMetadataUpdates 只刷新上游 metadata 与 active 状态，保留 alias、统计、游标和 created_at。
func usageIdentityMetadataUpdates(identity entities.UsageIdentity, now time.Time) map[string]any {
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
		"xai_user_id":    identity.XAIUserID,
		"active_start":   identity.ActiveStart,
		"active_until":   identity.ActiveUntil,
		"plan_type":      identity.PlanType,
		"is_deleted":     false,
		"deleted_at":     nil,
		// updated_at 明确使用入口统一 now，不能读取输入实体通常为空的时间字段。
		"updated_at": timeutil.FormatStorageTime(now),
	}
}
