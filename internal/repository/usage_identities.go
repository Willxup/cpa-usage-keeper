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
	// 在独立 reader 上完成全量 metadata 比较，只有实际变化才占用 writer。
	changes, err := prepareUsageIdentitySync(db.Clauses(dbresolver.Read).WithContext(ctx), normalized, incomingIdentities, authType, nil, false, normalizedNow)
	if err != nil {
		return fmt.Errorf("list usage identities for sync: %w", err)
	}
	return applyUsageIdentitySync(db.WithContext(ctx), changes, normalizedNow, "mark stale usage identities deleted")
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

	// 成功的 provider type 决定 stale 范围；失败来源的旧行保留。
	changes, err := prepareUsageIdentitySync(db.Clauses(dbresolver.Read).WithContext(ctx), normalized, incomingIdentities, entities.UsageIdentityAuthTypeAIProvider, types, true, normalizedNow)
	if err != nil {
		return fmt.Errorf("list provider usage identities for sync: %w", err)
	}
	return applyUsageIdentitySync(db.WithContext(ctx), changes, normalizedNow, "mark stale provider usage identities deleted")
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

const usageIdentityReadColumns = "id, name, alias, auth_type, auth_type_name, identity, type, provider, lookup_key, prefix, base_url, file_name, file_path, priority, disabled, note, account_id, project_id, xai_user_id, active_start, active_until, plan_type, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at, stats_updated_at, is_deleted, created_at, updated_at, deleted_at, stats_reset_at, reset_total_requests, reset_success_count, reset_failure_count, reset_input_tokens, reset_cache_read_tokens, reset_total_tokens"

const usageIdentityAggregationColumns = "id, auth_type, identity, total_requests, success_count, failure_count, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, total_tokens, last_aggregated_usage_event_id, first_used_at, last_used_at"

// UsageIdentityAggregationBatchSize 限制单个 Identity 写事务最多处理 25 行。
const UsageIdentityAggregationBatchSize = 25

const activeAuthFileUsageIdentityLookupBatchSize = 500
const openAIProviderPriorityUpdateBatchSize = 500

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

// FindActiveUsageIdentityByAuthTypeAndIdentity 按页面公开的 auth_index 精确读取可操作身份。
func FindActiveUsageIdentityByAuthTypeAndIdentity(ctx context.Context, db *gorm.DB, authType entities.UsageIdentityAuthType, identity string) (entities.UsageIdentity, error) {
	var row entities.UsageIdentity
	if db == nil {
		return row, fmt.Errorf("database is nil")
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return row, fmt.Errorf("usage identity is required")
	}
	if err := db.WithContext(ctx).
		Clauses(dbresolver.Write).
		Select(usageIdentityReadColumns).
		Where("auth_type = ? AND identity = ? AND is_deleted = ?", authType, identity, false).
		First(&row).Error; err != nil {
		return row, fmt.Errorf("find active usage identity: %w", err)
	}
	return row, nil
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

// UpdateUsageIdentityDisabled 在上游状态成功后写回 Keeper 的即时状态。
func UpdateUsageIdentityDisabled(ctx context.Context, db *gorm.DB, authType entities.UsageIdentityAuthType, identity string, disabled bool) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return fmt.Errorf("usage identity is required")
	}
	result := db.WithContext(ctx).
		Model(&entities.UsageIdentity{}).
		Where("auth_type = ? AND identity = ? AND is_deleted = ?", authType, identity, false).
		Update("disabled", disabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateUsageIdentityPriority 写入单条凭证的即时优先级。
func UpdateUsageIdentityPriority(ctx context.Context, db *gorm.DB, authType entities.UsageIdentityAuthType, identity string, priority int) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	result := db.WithContext(ctx).Model(&entities.UsageIdentity{}).
		Where("auth_type = ? AND identity = ? AND is_deleted = ?", authType, strings.TrimSpace(identity), false).
		Update("priority", priority)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateOpenAIProviderPriority 使用单个事务更新同一 CPA provider 下所有已同步的 key。
func UpdateOpenAIProviderPriority(ctx context.Context, db *gorm.DB, authIndexes []string, priority int) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if len(authIndexes) == 0 {
		return fmt.Errorf("openai provider auth indexes are required")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var updated int64
		for start := 0; start < len(authIndexes); start += openAIProviderPriorityUpdateBatchSize {
			end := min(start+openAIProviderPriorityUpdateBatchSize, len(authIndexes))
			result := tx.Model(&entities.UsageIdentity{}).
				Where("auth_type = ? AND type = ? AND identity IN ? AND is_deleted = ?", entities.UsageIdentityAuthTypeAIProvider, "openai", authIndexes[start:end], false).
				Update("priority", priority)
			if result.Error != nil {
				return result.Error
			}
			updated += result.RowsAffected
		}
		if updated == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
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
		return query.Order("(total_tokens - reset_total_tokens) DESC").Order("id ASC")
	case UsageIdentityPageSortLastUsedAt:
		return query.Order("last_used_at IS NULL ASC").Order("last_used_at DESC").Order("id ASC")
	default:
		return query.Order("(total_requests - reset_total_requests) DESC").Order("id ASC")
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

	// 同一次增量查询取齐累计和首尾时间，减少唯一 writer 事务内的重复查询。
	// MIN/MAX 沿用原先 timestamp 排序口径，DTO serializer 兼容新旧存储时间格式。
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
			MIN(timestamp) AS first_used_at,
			MAX(timestamp) AS last_used_at,
			COALESCE(MAX(id), 0) AS max_usage_event_id`).
		Where("id > ?", identity.LastAggregatedUsageEventID).
		Scan(&delta).Error; err != nil {
		return delta, fmt.Errorf("aggregate usage identity stats for %q: %w", identity.Identity, err)
	}
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

const usageIdentitySyncColumns = "id, name, auth_type_name, identity, type, provider, lookup_key, prefix, base_url, file_name, file_path, priority, disabled, note, account_id, project_id, xai_user_id, active_start, active_until, plan_type, is_deleted, deleted_at"

type usageIdentityUpdate struct {
	id          int64
	fields      map[string]any
	oldPriority *int
	oldDisabled *bool
}

type usageIdentityChanges struct {
	create   []entities.UsageIdentity
	update   []usageIdentityUpdate
	staleIDs []int64
}

func prepareUsageIdentitySync(reader *gorm.DB, identities []entities.UsageIdentity, incomingIdentities []string, authType entities.UsageIdentityAuthType, providerTypes []string, providerScoped bool, now time.Time) (usageIdentityChanges, error) {
	var changes usageIdentityChanges
	var existing []entities.UsageIdentity
	if err := reader.Model(&entities.UsageIdentity{}).
		Select(usageIdentitySyncColumns).
		Where("auth_type = ?", authType).
		Find(&existing).Error; err != nil {
		return changes, err
	}

	// 同一次 reader 快照决定新增、字段更新和 stale，避免把统计与本地 alias 纳入 metadata 比较。
	byIdentity := make(map[string]entities.UsageIdentity, len(existing))
	for _, row := range existing {
		byIdentity[row.Identity] = row
	}
	incoming := make(map[string]struct{}, len(incomingIdentities))
	for _, identity := range incomingIdentities {
		incoming[identity] = struct{}{}
	}
	for _, identity := range identities {
		if row, ok := byIdentity[identity.Identity]; ok {
			fields := usageIdentityMetadataDiff(row, identity)
			if len(fields) > 0 {
				fields["updated_at"] = timeutil.FormatStorageTime(now)
				changes.update = append(changes.update, usageIdentityUpdate{id: row.ID, fields: fields, oldPriority: row.Priority, oldDisabled: row.Disabled})
			}
			continue
		}
		identity.CreatedAt = now
		identity.UpdatedAt = now
		changes.create = append(changes.create, identity)
	}

	// Auth File 的成功空列表覆盖整个来源；Provider 只删除本轮成功 fetch 的类型。
	allowedTypes := make(map[string]struct{}, len(providerTypes))
	for _, providerType := range providerTypes {
		allowedTypes[providerType] = struct{}{}
	}
	for _, row := range existing {
		if row.IsDeleted {
			continue
		}
		if _, ok := incoming[row.Identity]; ok {
			continue
		}
		if providerScoped {
			if _, ok := allowedTypes[row.Type]; !ok {
				continue
			}
		}
		changes.staleIDs = append(changes.staleIDs, row.ID)
	}
	return changes, nil
}

func applyUsageIdentitySync(db *gorm.DB, changes usageIdentityChanges, now time.Time, staleContext string) error {
	if len(changes.create) == 0 && len(changes.update) == 0 && len(changes.staleIDs) == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		// 短事务只提交已判定的变化；更新与恢复仍先于新增和 stale 标记。
		for _, change := range changes.update {
			query := tx.Model(&entities.UsageIdentity{}).Where("id = ?", change.id)
			// 本地编辑可能发生在 reader 快照之后；只对本轮将写的可编辑字段检查旧值，冲突时整行跳过。
			if _, changed := change.fields["priority"]; changed {
				if change.oldPriority == nil {
					query = query.Where("priority IS NULL")
				} else {
					query = query.Where("priority = ?", *change.oldPriority)
				}
			}
			if _, changed := change.fields["disabled"]; changed {
				if change.oldDisabled == nil {
					query = query.Where("disabled IS NULL")
				} else {
					query = query.Where("disabled = ?", *change.oldDisabled)
				}
			}
			if err := query.Updates(change.fields).Error; err != nil {
				return fmt.Errorf("update usage identity: %w", err)
			}
		}
		if len(changes.create) > 0 {
			if err := tx.CreateInBatches(&changes.create, insertBatchSize(entities.UsageIdentity{})).Error; err != nil {
				return fmt.Errorf("create usage identities: %w", err)
			}
		}
		return markStaleUsageIdentityRowsDeleted(tx, changes.staleIDs, now, staleContext)
	})
}

func markStaleUsageIdentityRowsDeleted(tx *gorm.DB, staleIDs []int64, now time.Time, context string) error {
	for start := 0; start < len(staleIDs); start += insertBatchSize(entities.UsageIdentity{}) {
		end := min(start+insertBatchSize(entities.UsageIdentity{}), len(staleIDs))
		if err := tx.Model(&entities.UsageIdentity{}).
			Where("id IN ?", staleIDs[start:end]).
			Updates(map[string]any{"is_deleted": true, "deleted_at": timeutil.FormatStorageTime(now), "updated_at": timeutil.FormatStorageTime(now)}).Error; err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
	}
	return nil
}

// usageIdentityMetadataDiff 只比较 CPA 负责的字段，保留本地 alias、统计、游标和创建时间。
func usageIdentityMetadataDiff(existing, incoming entities.UsageIdentity) map[string]any {
	fields := make(map[string]any)
	if existing.Name != incoming.Name {
		fields["name"] = incoming.Name
	}
	if existing.AuthTypeName != incoming.AuthTypeName {
		fields["auth_type_name"] = incoming.AuthTypeName
	}
	if existing.Type != incoming.Type {
		fields["type"] = incoming.Type
	}
	if existing.Provider != incoming.Provider {
		fields["provider"] = incoming.Provider
	}
	if existing.LookupKey != incoming.LookupKey {
		fields["lookup_key"] = incoming.LookupKey
	}
	if existing.Prefix != incoming.Prefix {
		fields["prefix"] = incoming.Prefix
	}
	if existing.BaseURL != incoming.BaseURL {
		fields["base_url"] = incoming.BaseURL
	}
	if !optionalEqual(existing.FileName, incoming.FileName) {
		fields["file_name"] = incoming.FileName
	}
	if !optionalEqual(existing.FilePath, incoming.FilePath) {
		fields["file_path"] = incoming.FilePath
	}
	if !optionalEqual(existing.Priority, incoming.Priority) {
		fields["priority"] = incoming.Priority
	}
	if !optionalEqual(existing.Disabled, incoming.Disabled) {
		fields["disabled"] = incoming.Disabled
	}
	if !optionalEqual(existing.Note, incoming.Note) {
		fields["note"] = incoming.Note
	}
	if !optionalEqual(existing.AccountID, incoming.AccountID) {
		fields["account_id"] = incoming.AccountID
	}
	if !optionalEqual(existing.ProjectID, incoming.ProjectID) {
		fields["project_id"] = incoming.ProjectID
	}
	if !optionalEqual(existing.XAIUserID, incoming.XAIUserID) {
		fields["xai_user_id"] = incoming.XAIUserID
	}
	if !optionalTimeEqual(existing.ActiveStart, incoming.ActiveStart) {
		fields["active_start"] = incoming.ActiveStart
	}
	if !optionalEqual(existing.PlanType, incoming.PlanType) {
		fields["plan_type"] = incoming.PlanType
	}
	if incoming.AuthType == entities.UsageIdentityAuthTypeAuthFile && strings.EqualFold(strings.TrimSpace(incoming.Type), "codex") {
		// 官方订阅时间可在 reader 快照之后更新；SQL 条件仍原子地保留较晚值。
		if incoming.ActiveUntil != nil && (existing.ActiveUntil == nil || incoming.ActiveUntil.After(*existing.ActiveUntil)) {
			value := timeutil.FormatStorageTime(*incoming.ActiveUntil)
			fields["active_until"] = gorm.Expr("CASE WHEN active_until IS NULL OR julianday(?) > julianday(active_until) THEN ? ELSE active_until END", value, value)
		}
	} else if !optionalTimeEqual(existing.ActiveUntil, incoming.ActiveUntil) {
		fields["active_until"] = incoming.ActiveUntil
	}
	if existing.IsDeleted {
		fields["is_deleted"] = false
	}
	if existing.DeletedAt != nil {
		fields["deleted_at"] = nil
	}
	return fields
}

func optionalEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func optionalTimeEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// UpdateCodexUsageIdentityActiveUntil 只写官方订阅时间，并防止旧账号的在途响应落到新账号行。
func UpdateCodexUsageIdentityActiveUntil(ctx context.Context, db *gorm.DB, identity entities.UsageIdentity, activeUntil time.Time) error {
	if identity.ID == 0 || identity.AccountID == nil || strings.TrimSpace(*identity.AccountID) == "" || activeUntil.IsZero() {
		return nil
	}
	return db.WithContext(ctx).Model(&entities.UsageIdentity{}).
		Where("id = ? AND auth_type = ? AND identity = ? AND account_id = ? AND LOWER(TRIM(type)) = ? AND is_deleted = ?", identity.ID, entities.UsageIdentityAuthTypeAuthFile, identity.Identity, *identity.AccountID, "codex", false).
		Update("active_until", timeutil.FormatStorageTime(activeUntil)).Error
}
