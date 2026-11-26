package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	// DefaultQuotaHistoryDirtyScanBatchSize 控制每轮事件游标扫描页大小，避免单次占住 Reader 或 Writer。
	DefaultQuotaHistoryDirtyScanBatchSize = 1000
	// DefaultQuotaHistoryRecomputeBatchSize 控制每 turn 最多重算的限额周期数，保持低优先级与可停稳。
	DefaultQuotaHistoryRecomputeBatchSize = 4
	quotaCycleUpdateIDBatchSize           = 200
)

// QuotaHistoryDirtyScanResult 记录一次按 usage_events.id 推进标脏游标的结果。
type QuotaHistoryDirtyScanResult struct {
	ScannedEvents     int
	NextCursor        int64
	DirtyCyclesMarked int
	DirtyCycleIDs     []int64
	HasMore           bool
	DeferredForInbox  bool
}

// QuotaCycleUsageTotals 保存单个周期或下降段的完整 11 项持久化统计值。
type QuotaCycleUsageTotals struct {
	Requests             int64
	SuccessfulRequests   int64
	FailedRequests       int64
	InputTokens          int64
	OutputTokens         int64
	ReasoningTokens      int64
	CacheReadTokens      int64
	CacheCreationTokens  int64
	TotalTokens          int64
	CostUSD              float64
	UnavailableCostCount int64
}

// QuotaSegmentComputedUsage 保存单条百分比段的下降区间统计。
type QuotaSegmentComputedUsage struct {
	SegmentID            int64
	IntervalStartedAt    time.Time
	IntervalEndedAt      time.Time
	TransitionUsageReady bool
	Usage                QuotaCycleUsageTotals
}

// QuotaCycleComputedUsage 保存 Reader 池为单个周期算出的持久化结果与版本快照。
type QuotaCycleComputedUsage struct {
	CycleID            int64
	BoundaryVersion    int64
	DirtyVersion       int64
	BeyondHotRetention bool
	Active             bool
	// NextFutureEventAt 记录已入库但时间戳不早于本次结算截点 now 的最早本周期事件时刻（如上游时钟偏快）。
	NextFutureEventAt time.Time
	Usage             QuotaCycleUsageTotals
	SegmentUsages     []QuotaSegmentComputedUsage
}

type quotaHistoryDirtyEventRow struct {
	ID        int64
	Provider  string
	AuthType  string
	AuthIndex string
	Timestamp codexQuotaEfficiencyEventTime
}

type quotaHistoryProviderAuthKey struct {
	Provider  string
	AuthIndex string
}

// AdvanceQuotaHistoryDirtyCursor 按 usage_events.id 增量扫描已提交 OAuth 事件，把命中的周期标脏并推进游标。
func AdvanceQuotaHistoryDirtyCursor(ctx context.Context, db *gorm.DB, now time.Time, batchSize int) (QuotaHistoryDirtyScanResult, error) {
	return AdvanceQuotaHistoryDirtyCursorWithDefer(ctx, db, now, batchSize, nil)
}

// AdvanceQuotaHistoryDirtyCursorWithDefer 在只读扫描与候选定位完成后、真正申请唯一 Writer 前允许为 CPA inbox 让路。
func AdvanceQuotaHistoryDirtyCursorWithDefer(ctx context.Context, db *gorm.DB, now time.Time, batchSize int, shouldDefer func(context.Context) (bool, error)) (QuotaHistoryDirtyScanResult, error) {
	result := QuotaHistoryDirtyScanResult{}
	if db == nil {
		return result, fmt.Errorf("advance quota history dirty cursor: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if batchSize <= 0 {
		batchSize = DefaultQuotaHistoryDirtyScanBatchSize
	}
	if now.IsZero() {
		now = time.Now()
	}

	cursor, err := LoadQuotaHistoryAggregationCheckpoint(ctx, db)
	if err != nil {
		return result, err
	}

	// 独立轻量投影只读 5 列，COALESCE 兼容旧迁移遗留的可空身份列，避免无关 NULL 行阻塞游标推进。
	rows, err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("usage_events").
		Select("id, COALESCE(provider, ''), COALESCE(auth_type, ''), COALESCE(auth_index, ''), +timestamp AS timestamp").
		Where("id > ?", cursor).
		Order("id ASC").
		Limit(batchSize).
		Rows()
	if err != nil {
		return result, fmt.Errorf("scan usage events for quota history dirty cursor: %w", err)
	}
	defer rows.Close()

	events := make([]quotaHistoryDirtyEventRow, 0, min(batchSize, 64))
	for rows.Next() {
		var row quotaHistoryDirtyEventRow
		if err := rows.Scan(&row.ID, &row.Provider, &row.AuthType, &row.AuthIndex, &row.Timestamp); err != nil {
			return result, fmt.Errorf("scan usage event row for quota history dirty cursor: %w", err)
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate usage events for quota history dirty cursor: %w", err)
	}
	if len(events) == 0 {
		result.NextCursor = cursor
		return result, nil
	}

	result.ScannedEvents = len(events)
	result.NextCursor = events[len(events)-1].ID
	result.HasMore = len(events) == batchSize

	timestampsByAccount := make(map[quotaHistoryProviderAuthKey][]time.Time)
	for _, event := range events {
		if !strings.EqualFold(strings.TrimSpace(event.AuthType), "oauth") {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(event.Provider))
		if provider != "codex" && provider != "claude" {
			continue
		}
		authIndex := strings.TrimSpace(event.AuthIndex)
		if authIndex == "" {
			continue
		}
		key := quotaHistoryProviderAuthKey{Provider: provider, AuthIndex: authIndex}
		timestampsByAccount[key] = append(timestampsByAccount[key], timeutil.NormalizeStorageTime(event.Timestamp.Time))
	}

	affectedCycleIDs := make([]int64, 0)
	if len(timestampsByAccount) > 0 {
		affectedSet := make(map[int64]struct{})
		for key, timestamps := range timestampsByAccount {
			minAt, maxAt := timestamps[0], timestamps[0]
			for _, ts := range timestamps[1:] {
				if ts.Before(minAt) {
					minAt = ts
				}
				if ts.After(maxAt) {
					maxAt = ts
				}
			}
			var candidates []entities.QuotaCycle
			if err := db.WithContext(ctx).Clauses(dbresolver.Read).
				Select("id, window_started_at, reset_at").
				Where("provider = ? AND auth_index = ? AND reset_at > ? AND window_started_at <= ?",
					key.Provider, key.AuthIndex,
					timeutil.FormatSortableStorageTime(minAt),
					timeutil.FormatSortableStorageTime(maxAt)).
				Find(&candidates).Error; err != nil {
				return result, fmt.Errorf("find quota cycles hit by usage events: %w", err)
			}
			for _, cycle := range candidates {
				if _, exists := affectedSet[cycle.ID]; exists {
					continue
				}
				for _, ts := range timestamps {
					if !ts.Before(cycle.WindowStartedAt) && ts.Before(cycle.ResetAt) {
						affectedSet[cycle.ID] = struct{}{}
						affectedCycleIDs = append(affectedCycleIDs, cycle.ID)
						break
					}
				}
			}
		}
		sort.Slice(affectedCycleIDs, func(i, j int) bool { return affectedCycleIDs[i] < affectedCycleIDs[j] })
	}

	if shouldDefer != nil {
		deferred, deferErr := shouldDefer(ctx)
		if deferErr != nil {
			return result, deferErr
		}
		if deferred {
			result.ScannedEvents = 0
			result.NextCursor = cursor
			result.HasMore = true
			result.DeferredForInbox = true
			return result, nil
		}
	}

	normalizedNow := timeutil.NormalizeStorageTime(now)
	formattedNow := timeutil.FormatStorageTime(normalizedNow)
	err = db.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(affectedCycleIDs); start += quotaCycleUpdateIDBatchSize {
			end := min(start+quotaCycleUpdateIDBatchSize, len(affectedCycleIDs))
			if err := tx.Model(&entities.QuotaCycle{}).
				Where("id IN ?", affectedCycleIDs[start:end]).
				Updates(map[string]any{
					"dirty":         true,
					"dirty_version": gorm.Expr("dirty_version + 1"),
					"updated_at":    formattedNow,
				}).Error; err != nil {
				return fmt.Errorf("mark quota cycles dirty from usage events: %w", err)
			}
		}
		return AdvanceUsageAggregationCheckpoint(ctx, tx, entities.UsageAggregationCheckpointQuotaHistory, cursor, result.NextCursor, normalizedNow)
	})
	if err != nil {
		return result, err
	}
	result.DirtyCyclesMarked = len(affectedCycleIDs)
	result.DirtyCycleIDs = affectedCycleIDs
	return result, nil
}

// HasAnyQuotaCycles 检查库中是否存在任意限额周期，供调度启动决定是否启用限额历史事件标脏。
func HasAnyQuotaCycles(ctx context.Context, db *gorm.DB) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("check quota cycles existence: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var anyCycleID int64
	if err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("quota_cycles").
		Select("id").
		Limit(1).
		Scan(&anyCycleID).Error; err != nil {
		return false, fmt.Errorf("check quota cycles existence: %w", err)
	}
	return anyCycleID > 0, nil
}

// HasPendingQuotaHistoryWork 检查是否存在未扫描事件、dirty 周期或 90 天热表范围内的待回填周期。
func HasPendingQuotaHistoryWork(ctx context.Context, db *gorm.DB, now time.Time) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("check pending quota history work: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}
	var anyCycleID int64
	if err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("quota_cycles").
		Select("id").
		Limit(1).
		Scan(&anyCycleID).Error; err != nil {
		return false, fmt.Errorf("check quota cycles existence: %w", err)
	}
	if anyCycleID <= 0 {
		return false, nil
	}
	hotCutoff := timeutil.FormatSortableStorageTime(usageEventsArchiveCutoff(now))

	var pendingCycleCount int64
	if err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Model(&entities.QuotaCycle{}).
		Where("dirty = ? OR (usage_ready = ? AND window_started_at >= ?)", true, false, hotCutoff).
		Limit(1).
		Count(&pendingCycleCount).Error; err != nil {
		return false, fmt.Errorf("check pending quota cycles: %w", err)
	}
	if pendingCycleCount > 0 {
		return true, nil
	}

	cursor, err := LoadQuotaHistoryAggregationCheckpoint(ctx, db)
	if err != nil {
		return false, err
	}
	var pendingEventID int64
	if err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("usage_events").
		Select("id").
		Where("id > ?", cursor).
		Order("id ASC").
		Limit(1).
		Scan(&pendingEventID).Error; err != nil {
		return false, fmt.Errorf("check pending usage events for quota history: %w", err)
	}
	return pendingEventID > 0, nil
}

// SelectQuotaCyclesForUsageSync 优先选取 dirty 周期，再由近及远选取 90 天热表保留期内的待回填周期。
func SelectQuotaCyclesForUsageSync(ctx context.Context, db *gorm.DB, now time.Time, limit int, excludeIDs []int64) ([]entities.QuotaCycle, bool, error) {
	if db == nil {
		return nil, false, fmt.Errorf("select quota cycles for usage sync: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = DefaultQuotaHistoryRecomputeBatchSize
	}
	if now.IsZero() {
		now = time.Now()
	}

	selected := make([]entities.QuotaCycle, 0, limit)
	dirtyQuery := db.WithContext(ctx).Clauses(dbresolver.Read).
		Where("dirty = ?", true)
	if len(excludeIDs) > 0 {
		dirtyQuery = dirtyQuery.Where("id NOT IN ?", excludeIDs)
	}
	var dirtyCycles []entities.QuotaCycle
	if err := dirtyQuery.Order("reset_at DESC, id DESC").Limit(limit + 1).Find(&dirtyCycles).Error; err != nil {
		return nil, false, fmt.Errorf("select dirty quota cycles: %w", err)
	}
	if len(dirtyCycles) > limit {
		return dirtyCycles[:limit], true, nil
	}
	selected = append(selected, dirtyCycles...)

	remaining := limit - len(selected)
	if remaining <= 0 {
		// 检查是否还有待回填周期，供调度决定是否继续排下一轮。
		hasMore, err := hasBackfillQuotaCycles(ctx, db, now, excludeIDs)
		return selected, hasMore, err
	}

	hotCutoff := timeutil.FormatSortableStorageTime(usageEventsArchiveCutoff(now))
	backfillQuery := db.WithContext(ctx).Clauses(dbresolver.Read).
		Where("usage_ready = ? AND dirty = ? AND window_started_at >= ?", false, false, hotCutoff)
	if len(excludeIDs) > 0 {
		backfillQuery = backfillQuery.Where("id NOT IN ?", excludeIDs)
	}
	var backfillCycles []entities.QuotaCycle
	if err := backfillQuery.Order("reset_at DESC, id DESC").Limit(remaining + 1).Find(&backfillCycles).Error; err != nil {
		return nil, false, fmt.Errorf("select backfill quota cycles: %w", err)
	}
	hasMore := false
	if len(backfillCycles) > remaining {
		hasMore = true
		backfillCycles = backfillCycles[:remaining]
	}
	selected = append(selected, backfillCycles...)
	return selected, hasMore, nil
}

func hasBackfillQuotaCycles(ctx context.Context, db *gorm.DB, now time.Time, excludeIDs []int64) (bool, error) {
	hotCutoff := timeutil.FormatSortableStorageTime(usageEventsArchiveCutoff(now))
	query := db.WithContext(ctx).Clauses(dbresolver.Read).
		Model(&entities.QuotaCycle{}).
		Where("usage_ready = ? AND dirty = ? AND window_started_at >= ?", false, false, hotCutoff)
	if len(excludeIDs) > 0 {
		query = query.Where("id NOT IN ?", excludeIDs)
	}
	var count int64
	if err := query.Limit(1).Count(&count).Error; err != nil {
		return false, fmt.Errorf("count backfill quota cycles: %w", err)
	}
	return count > 0, nil
}

// ComputeQuotaCyclePersistedUsage 在 Reader 池按有效边界流式重算单个周期及各下降段的 11 项统计。
// maxEventID > 0 时仅统计 id <= maxEventID 的热表事件（供显式价格重算复用）。
func ComputeQuotaCyclePersistedUsage(ctx context.Context, db *gorm.DB, cycle entities.QuotaCycle, now time.Time, maxEventID int64) (QuotaCycleComputedUsage, error) {
	computed := QuotaCycleComputedUsage{
		CycleID:         cycle.ID,
		BoundaryVersion: cycle.BoundaryVersion,
		DirtyVersion:    cycle.DirtyVersion,
	}
	if db == nil {
		return computed, fmt.Errorf("compute quota cycle usage: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = timeutil.NormalizeStorageTime(now)

	// 加载同账号同角色的兄弟周期以精确复用 buildCodexQuotaEfficiencyCyclePeriods 有效边界规则。
	var siblings []entities.QuotaCycle
	if err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Where("provider = ? AND auth_index = ? AND quota_key = ?", cycle.Provider, cycle.AuthIndex, cycle.QuotaKey).
		Order("reset_at DESC, id DESC").
		Find(&siblings).Error; err != nil {
		return computed, fmt.Errorf("load sibling quota cycles for cycle %d: %w", cycle.ID, err)
	}
	if len(siblings) == 0 {
		return computed, nil
	}

	latestID := latestCodexQuotaEfficiencyCycleID(siblings)
	currentCycleID := int64(0)
	for _, sibling := range siblings {
		if sibling.ID == latestID && !now.Before(sibling.WindowStartedAt) && now.Before(sibling.ResetAt) {
			currentCycleID = sibling.ID
			break
		}
	}
	periods := buildCodexQuotaEfficiencyCyclePeriods(siblings, currentCycleID, now)
	period, found := periods[cycle.ID]
	if !found {
		return computed, nil
	}
	computed.Active = period.current

	// 早于 90 天热表归档线的周期不扫描冷表，保持 usage_ready = false。
	hotCutoff := usageEventsArchiveCutoff(now)
	if period.start.Before(hotCutoff) {
		computed.BeyondHotRetention = true
		return computed, nil
	}

	var segments []codexQuotaEfficiencySegmentRow
	if err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("quota_percent_segments").
		Select("id, cycle_id, remaining_percent, +first_observed_at AS first_observed_at, +last_observed_at AS last_observed_at, observation_count").
		Where("cycle_id = ?", cycle.ID).
		Order("quota_percent_segments.first_observed_at ASC, quota_percent_segments.id ASC").
		Find(&segments).Error; err != nil {
		return computed, fmt.Errorf("load quota percent segments for cycle %d: %w", cycle.ID, err)
	}

	computed.SegmentUsages = make([]QuotaSegmentComputedUsage, len(segments))
	transitionIndices := make([]int, 0, max(0, len(segments)-1))
	for i, seg := range segments {
		entry := QuotaSegmentComputedUsage{SegmentID: seg.ID}
		if i > 0 && segments[i-1].RemainingPercent > seg.RemainingPercent {
			entry.IntervalStartedAt = segments[i-1].FirstObservedAt.Time
			entry.IntervalEndedAt = seg.FirstObservedAt.Time
			entry.TransitionUsageReady = true
			transitionIndices = append(transitionIndices, i)
		}
		computed.SegmentUsages[i] = entry
	}

	queryStart := period.start
	queryEnd := period.end
	if period.current {
		queryEnd = now
	}
	readEnd := period.end
	if !period.current {
		readEnd = queryEnd
	}
	if !queryStart.Before(readEnd) {
		return computed, nil
	}
	if !period.current && !queryStart.Before(queryEnd) {
		return computed, nil
	}

	for _, read := range quotaCycleEventTimeReads(queryStart, readEnd) {
		if err := streamQuotaCyclePersistedUsageRead(ctx, db, cycle.Provider, cycle.AuthIndex, read, queryStart, queryEnd, period.current, period.end, maxEventID, &computed, transitionIndices); err != nil {
			return computed, err
		}
	}
	return computed, nil
}

// quotaCycleEventTimeReads 在保留按时区段有界索引扫描的同时，把候选上下界扩展覆盖历史遗留的 UTC Z 文本格式；
// 行级扫描随后由 Go 真实 instant (read.contains 与 [queryStart, queryEnd)) 精确裁剪，避免调整部署 TZ 后漏算历史 Z 事件。
func quotaCycleEventTimeReads(start, end time.Time) []usageEventTimeRead {
	reads := usageEventTimeReads(start, end, false)
	const secondPrefix = "2006-01-02T15:04:05"
	for index := range reads {
		reads[index].suffix = ""
		utcLower := reads[index].start.UTC().Format(secondPrefix)
		utcUpper := reads[index].end.UTC().Truncate(time.Second).Add(time.Second).Format(secondPrefix)
		if utcLower < reads[index].lower {
			reads[index].lower = utcLower
		}
		if utcUpper > reads[index].upper {
			reads[index].upper = utcUpper
			reads[index].upperInclusive = false
		}
	}
	return reads
}

func streamQuotaCyclePersistedUsageRead(
	ctx context.Context,
	db *gorm.DB,
	provider string,
	authIndex string,
	read usageEventTimeRead,
	queryStart time.Time,
	queryEnd time.Time,
	periodCurrent bool,
	periodEnd time.Time,
	maxEventID int64,
	computed *QuotaCycleComputedUsage,
	transitionIndices []int,
) error {
	base := db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("usage_events INDEXED BY idx_usage_events_auth_index_timestamp_id").
		Select(`+timestamp AS timestamp, COALESCE(failed, 0), COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), COALESCE(reasoning_tokens, 0),
			cache_read_tokens, cache_creation_tokens, COALESCE(total_tokens, 0), cost_usd, cost_available`).
		Where("auth_type = ? AND auth_index = ? AND provider = ?", "oauth", authIndex, provider)
	if maxEventID > 0 {
		base = base.Where("usage_events.id <= ?", maxEventID)
	}
	rows, err := read.query(base).Order("usage_events.timestamp ASC, usage_events.id ASC").Rows()
	if err != nil {
		return fmt.Errorf("stream quota cycle persisted usage: %w", err)
	}
	defer rows.Close()

	var event codexQuotaEfficiencyUsageEventRow
	var storedTimestamp codexQuotaEfficiencyEventTime
	var storedCost sql.NullFloat64
	var storedAvailable sql.NullBool
	var previousAt time.Time
	transitionPos := 0

	for rows.Next() {
		if err := rows.Scan(
			&storedTimestamp, &event.Failed, &event.InputTokens, &event.OutputTokens, &event.ReasoningTokens,
			&event.CacheReadTokens, &event.CacheCreationTokens, &event.TotalTokens, &storedCost, &storedAvailable,
		); err != nil {
			return fmt.Errorf("scan quota cycle persisted usage: %w", err)
		}
		event.Timestamp = timeutil.NormalizeStorageTime(storedTimestamp.Time)
		if !read.contains(event.Timestamp) {
			continue
		}
		if event.Timestamp.Before(queryStart) {
			continue
		}
		if !event.Timestamp.Before(queryEnd) {
			if periodCurrent && event.Timestamp.Before(periodEnd) {
				if computed.NextFutureEventAt.IsZero() || event.Timestamp.Before(computed.NextFutureEventAt) {
					computed.NextFutureEventAt = event.Timestamp
				}
			}
			continue
		}
		if !storedCost.Valid || !storedAvailable.Valid || math.IsNaN(storedCost.Float64) || math.IsInf(storedCost.Float64, 0) {
			return fmt.Errorf("quota cycle persisted usage has unbackfilled or invalid stored cost")
		}
		event.CostUSD, event.CostAvailable = storedCost.Float64, storedAvailable.Bool

		if err := addQuotaCycleUsageTotals(&computed.Usage, event); err != nil {
			return fmt.Errorf("accumulate quota cycle usage: %w", err)
		}

		if len(transitionIndices) == 0 {
			continue
		}
		backwards := event.Timestamp.Before(previousAt)
		previousAt = event.Timestamp
		if backwards {
			transitionPos = sort.Search(len(transitionIndices), func(i int) bool {
				return !event.Timestamp.After(computed.SegmentUsages[transitionIndices[i]].IntervalEndedAt)
			})
		} else {
			for transitionPos < len(transitionIndices) && event.Timestamp.After(computed.SegmentUsages[transitionIndices[transitionPos]].IntervalEndedAt) {
				transitionPos++
			}
		}
		if transitionPos >= len(transitionIndices) {
			continue
		}
		target := &computed.SegmentUsages[transitionIndices[transitionPos]]
		if !event.Timestamp.After(target.IntervalStartedAt) || event.Timestamp.After(target.IntervalEndedAt) {
			continue
		}
		if err := addQuotaCycleUsageTotals(&target.Usage, event); err != nil {
			return fmt.Errorf("accumulate quota transition usage: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate quota cycle persisted usage: %w", err)
	}
	return nil
}

func addQuotaCycleUsageTotals(target *QuotaCycleUsageTotals, event codexQuotaEfficiencyUsageEventRow) error {
	target.Requests++
	if event.Failed {
		target.FailedRequests++
	} else {
		target.SuccessfulRequests++
	}
	target.InputTokens += event.InputTokens
	target.OutputTokens += event.OutputTokens
	target.ReasoningTokens += event.ReasoningTokens
	target.CacheReadTokens += event.CacheReadTokens
	target.CacheCreationTokens += event.CacheCreationTokens
	target.TotalTokens += event.TotalTokens
	target.CostUSD += event.CostUSD
	if math.IsInf(target.CostUSD, 0) {
		return fmt.Errorf("stored cost overflow")
	}
	if !event.CostAvailable {
		target.UnavailableCostCount++
	}
	return nil
}

// ApplyComputedQuotaCycleUsage 在短 Writer 事务内核对 boundary_version 后回写周期与下降段统计。
// clearCostStale 为 false（日常任务）时绝不清除 cost_stale，仅当 dirty_version 未变时清除 dirty。
func ApplyComputedQuotaCycleUsage(ctx context.Context, db *gorm.DB, computed QuotaCycleComputedUsage, now time.Time, clearCostStale bool) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("apply computed quota cycle usage: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}
	formattedNow := timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(now))
	applied := false

	err := db.WithContext(ctx).Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		if computed.BeyondHotRetention {
			updates := map[string]any{"updated_at": formattedNow}
			if clearCostStale {
				updates["cost_stale"] = false
			} else {
				updates["dirty"] = gorm.Expr("CASE WHEN dirty_version = ? THEN 0 ELSE 1 END", computed.DirtyVersion)
			}
			res := tx.Model(&entities.QuotaCycle{}).
				Where("id = ? AND boundary_version = ?", computed.CycleID, computed.BoundaryVersion).
				Updates(updates)
			if res.Error != nil {
				return fmt.Errorf("clear stale flag on beyond-retention quota cycle %d: %w", computed.CycleID, res.Error)
			}
			applied = res.RowsAffected == 1
			return nil
		}

		updates := map[string]any{
			"requests":               computed.Usage.Requests,
			"successful_requests":    computed.Usage.SuccessfulRequests,
			"failed_requests":        computed.Usage.FailedRequests,
			"input_tokens":           computed.Usage.InputTokens,
			"output_tokens":          computed.Usage.OutputTokens,
			"reasoning_tokens":       computed.Usage.ReasoningTokens,
			"cache_read_tokens":      computed.Usage.CacheReadTokens,
			"cache_creation_tokens":  computed.Usage.CacheCreationTokens,
			"total_tokens":           computed.Usage.TotalTokens,
			"cost_usd":               computed.Usage.CostUSD,
			"unavailable_cost_count": computed.Usage.UnavailableCostCount,
			"usage_ready":            true,
			"updated_at":             formattedNow,
		}
		if clearCostStale {
			updates["cost_stale"] = false
		} else if !computed.NextFutureEventAt.IsZero() {
			// 已入库事件的时间戳不早于本次结算截点（如上游时钟略快），保留 dirty 等待时钟越过后补齐。
			updates["dirty"] = true
		} else {
			// 日常任务不得清除 cost_stale；若计算期间有新脏代际到达则保留 dirty=true 等待下一轮。
			updates["dirty"] = gorm.Expr("CASE WHEN dirty_version = ? THEN 0 ELSE 1 END", computed.DirtyVersion)
		}

		res := tx.Model(&entities.QuotaCycle{}).
			Where("id = ? AND boundary_version = ?", computed.CycleID, computed.BoundaryVersion).
			Updates(updates)
		if res.Error != nil {
			return fmt.Errorf("update persisted quota cycle %d usage: %w", computed.CycleID, res.Error)
		}
		if res.RowsAffected != 1 {
			return nil
		}

		for _, seg := range computed.SegmentUsages {
			segUpdates := map[string]any{
				"transition_requests":               seg.Usage.Requests,
				"transition_successful_requests":    seg.Usage.SuccessfulRequests,
				"transition_failed_requests":        seg.Usage.FailedRequests,
				"transition_input_tokens":           seg.Usage.InputTokens,
				"transition_output_tokens":          seg.Usage.OutputTokens,
				"transition_reasoning_tokens":       seg.Usage.ReasoningTokens,
				"transition_cache_read_tokens":      seg.Usage.CacheReadTokens,
				"transition_cache_creation_tokens":  seg.Usage.CacheCreationTokens,
				"transition_total_tokens":           seg.Usage.TotalTokens,
				"transition_cost_usd":               seg.Usage.CostUSD,
				"transition_unavailable_cost_count": seg.Usage.UnavailableCostCount,
				"transition_usage_ready":            seg.TransitionUsageReady,
				"updated_at":                        formattedNow,
			}
			if err := tx.Model(&entities.QuotaPercentSegment{}).
				Where("id = ? AND cycle_id = ?", seg.SegmentID, computed.CycleID).
				Updates(segUpdates).Error; err != nil {
				return fmt.Errorf("update persisted quota segment %d usage: %w", seg.SegmentID, err)
			}
		}

		applied = true
		return nil
	})
	return applied, err
}

// RecomputeAndPersistQuotaCycles 逐周期在 Reader 池计算并在短 Writer 事务落库，返回成功落库的周期数。
func RecomputeAndPersistQuotaCycles(ctx context.Context, db *gorm.DB, cycles []entities.QuotaCycle, now time.Time, maxEventID int64, clearCostStale bool) (int, error) {
	appliedCount := 0
	for _, cycle := range cycles {
		if ctx != nil && ctx.Err() != nil {
			return appliedCount, ctx.Err()
		}
		computed, err := ComputeQuotaCyclePersistedUsage(ctx, db, cycle, now, maxEventID)
		if err != nil {
			return appliedCount, err
		}
		applied, err := ApplyComputedQuotaCycleUsage(ctx, db, computed, now, clearCostStale)
		if err != nil {
			return appliedCount, err
		}
		if applied {
			appliedCount++
		}
	}
	return appliedCount, nil
}
