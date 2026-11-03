package repository

import (
	"context"
	"fmt"
	"math"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	usageCostRecalculationPageSize = 1000
	usageCostRecalculationIDChunk  = 900 // 留在 SQLite 旧版 999 参数限制之内。
	usageCostRecalculationKey      = "bucket_start = ? AND api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ? AND service_tier = ? AND response_service_tier = ? AND reasoning_effort = ? AND endpoint = ? AND executor_type = ?"
)

// UsageCostRecalculationScope fixes [Start, End) and both retained table ID bounds.
// Hour alignment, configuration revision and overview catch-up are ensured by the coordinator.
type UsageCostRecalculationScope struct {
	Start             time.Time
	End               time.Time
	MaxEventID        int64
	MaxArchiveEventID int64
}

// validate checks internal bounds; admission checks alignment and revision.
func (scope UsageCostRecalculationScope) validate() error {
	if scope.Start.IsZero() || !scope.Start.Before(scope.End) || scope.MaxEventID < 0 || scope.MaxArchiveEventID < 0 {
		return fmt.Errorf("invalid usage cost recalculation scope")
	}
	return nil
}

// LoadUsageCostRecalculationPage 从 Reader 按 ID 扫最多 1000 条热事件，并以 Go instant 筛选固定 S/T。
// cursor 是已扫描 ID 而非已选事件数；无目标事件的页仍推进，避免 ID 空洞或乱序时间漏读。
func LoadUsageCostRecalculationPage(ctx context.Context, reader *gorm.DB, scope UsageCostRecalculationScope, afterID int64) ([]entities.UsageEvent, int64, bool, error) {
	return loadUsageCostRecalculationPage(ctx, reader, scope, afterID, false)
}

func LoadArchivedUsageCostRecalculationPage(ctx context.Context, reader *gorm.DB, scope UsageCostRecalculationScope, afterID int64) ([]entities.UsageEvent, int64, bool, error) {
	return loadUsageCostRecalculationPage(ctx, reader, scope, afterID, true)
}

func loadUsageCostRecalculationPage(ctx context.Context, reader *gorm.DB, scope UsageCostRecalculationScope, afterID int64, archive bool) ([]entities.UsageEvent, int64, bool, error) {
	maxID, table := scope.MaxEventID, "usage_events"
	if archive {
		maxID, table = scope.MaxArchiveEventID, "usage_events_archive"
	}
	if reader == nil {
		return nil, 0, false, fmt.Errorf("usage cost recalculation reader is nil")
	}
	if err := scope.validate(); err != nil {
		return nil, 0, false, err
	}
	if afterID < 0 || afterID > maxID {
		return nil, 0, false, fmt.Errorf("invalid usage cost recalculation cursor %d", afterID)
	}
	if afterID == maxID {
		return []entities.UsageEvent{}, afterID, true, nil
	}
	var scanned []entities.UsageEvent
	if err := reader.Clauses(dbresolver.Read).WithContext(ctx).Table(table).
		Select(entities.UsageAggregationEventProjectionColumns).
		Where("id > ? AND id <= ?", afterID, maxID).
		Order("id ASC").Limit(usageCostRecalculationPageSize).Find(&scanned).Error; err != nil {
		return nil, 0, false, fmt.Errorf("load usage cost recalculation page: %w", err)
	}
	next := maxID
	if len(scanned) > 0 {
		next = scanned[len(scanned)-1].ID
	}
	selected := make([]entities.UsageEvent, 0, len(scanned))
	for _, event := range scanned {
		if !event.Timestamp.Before(scope.Start) && event.Timestamp.Before(scope.End) {
			selected = append(selected, event)
		}
	}
	return selected, next, len(scanned) < usageCostRecalculationPageSize, nil
}

// CountUsageCostRecalculationEvents 用相同有界页和实际时刻筛选统计任务真实目标数，不假设 ID 连续。
func CountUsageCostRecalculationEvents(ctx context.Context, reader *gorm.DB, scope UsageCostRecalculationScope) (int64, error) {
	var count int64
	for _, archive := range []bool{false, true} {
		for after := int64(0); ; {
			events, next, done, err := loadUsageCostRecalculationPage(ctx, reader, scope, after, archive)
			if err != nil {
				return 0, err
			}
			count += int64(len(events))
			if done {
				break
			}
			after = next
		}
	}
	return count, nil
}

type usageCostRecalculationDelta struct {
	cost        float64
	unavailable int64
}

type usageCostRecalculationFee struct {
	cost      float64
	available bool
}

// ApplyUsageCostRecalculationBatch 由已停稳处理/聚合/维护且 Overview 覆盖 H 的协调器调用。
// 固定 Resolver 只计算本页新总费用；Writer 事务重读实际旧值并同事务更新事件、小时和日的费用差额。
// 任一写入失败整页回滚，已提交的早期页保留；不重放请求数、Token 或 checkpoint。
func ApplyUsageCostRecalculationBatch(ctx context.Context, writer *gorm.DB, scope UsageCostRecalculationScope, events []entities.UsageEvent, resolver pricing.Resolver) error {
	return applyUsageCostRecalculationBatch(ctx, writer, scope, events, resolver, false)
}

func ApplyArchivedUsageCostRecalculationBatch(ctx context.Context, writer *gorm.DB, scope UsageCostRecalculationScope, events []entities.UsageEvent, resolver pricing.Resolver) error {
	return applyUsageCostRecalculationBatch(ctx, writer, scope, events, resolver, true)
}

func applyUsageCostRecalculationBatch(ctx context.Context, writer *gorm.DB, scope UsageCostRecalculationScope, events []entities.UsageEvent, resolver pricing.Resolver, archive bool) error {
	maxID, table := scope.MaxEventID, "usage_events"
	if archive {
		maxID, table = scope.MaxArchiveEventID, "usage_events_archive"
	}
	if writer == nil {
		return fmt.Errorf("usage cost recalculation writer is nil")
	}
	if err := scope.validate(); err != nil {
		return err
	}
	if len(events) > usageCostRecalculationPageSize {
		return fmt.Errorf("usage cost recalculation batch exceeds %d events", usageCostRecalculationPageSize)
	}
	if len(events) == 0 {
		return nil
	}
	fees := make(map[int64]usageCostRecalculationFee, len(events))
	for _, event := range events {
		if event.ID <= 0 || event.ID > maxID || event.Timestamp.Before(scope.Start) || !event.Timestamp.Before(scope.End) {
			return fmt.Errorf("usage cost recalculation event %d is outside fixed scope", event.ID)
		}
		if _, exists := fees[event.ID]; exists {
			return fmt.Errorf("usage cost recalculation event %d repeats in one batch", event.ID)
		}
		fee := resolver.CalculateFee(UsageEventCostSubject(event))
		if !finiteUsageRecalculationCost(fee.TotalCostUSD) {
			return fmt.Errorf("usage event %d recalculated cost is not finite", event.ID)
		}
		fees[event.ID] = usageCostRecalculationFee{cost: fee.TotalCostUSD, available: fee.Available}
	}
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		oldFees, err := loadUsageRecalculationOldFees(tx, events, table)
		if err != nil {
			return err
		}
		hourly := make(map[overview.BucketKey]usageCostRecalculationDelta)
		daily := make(map[overview.BucketKey]usageCostRecalculationDelta)
		for _, event := range events {
			old, exists := oldFees[event.ID]
			if !exists || old.CostUSD == nil || old.CostAvailable == nil || !finiteUsageRecalculationCost(*old.CostUSD) {
				return fmt.Errorf("usage event %d old cost is missing or not finite", event.ID)
			}
			// Historical unavailable rows may have missing input data; their completeness
			// cannot be reconstructed from the stored token counts.
			newFee := fees[event.ID]
			if *old.CostAvailable && !newFee.available {
				return fmt.Errorf("usage event %d has no complete price for recalculation", event.ID)
			}
			if !*old.CostAvailable {
				newFee = usageCostRecalculationFee{cost: *old.CostUSD, available: false}
			}
			delta := newFee.cost - *old.CostUSD
			if !finiteUsageRecalculationCost(delta) {
				return fmt.Errorf("usage event %d fee delta is not finite", event.ID)
			}
			unavailableDelta := int64(0)
			if !newFee.available {
				unavailableDelta++
			}
			if !*old.CostAvailable {
				unavailableDelta--
			}
			hourKey, dayKey := overview.BucketKeysForEvent(event)
			if err := addUsageRecalculationDelta(hourly, hourKey, delta, unavailableDelta); err != nil {
				return err
			}
			if err := addUsageRecalculationDelta(daily, dayKey, delta, unavailableDelta); err != nil {
				return err
			}
			result := tx.Table(table).Where("id = ?", event.ID).
				UpdateColumns(map[string]any{"cost_usd": newFee.cost, "cost_available": newFee.available})
			if result.Error != nil {
				return fmt.Errorf("write usage event %d recalculated fee: %w", event.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("usage event %d disappeared during recalculation", event.ID)
			}
		}
		for _, grain := range []struct {
			table string
			rows  map[overview.BucketKey]usageCostRecalculationDelta
		}{
			{"usage_overview_hourly_stats", hourly},
			{"usage_overview_daily_stats", daily},
		} {
			for key, delta := range grain.rows {
				if err := addUsageRecalculationBucketFee(tx, grain.table, key, delta); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type usageCostRecalculationOldFee struct {
	ID            int64
	CostUSD       *float64
	CostAvailable *bool
}

// loadUsageRecalculationOldFees 在 writer 事务中重读实际旧费用，IN 列表分片避开 999 参数上限。
func loadUsageRecalculationOldFees(tx *gorm.DB, events []entities.UsageEvent, table string) (map[int64]usageCostRecalculationOldFee, error) {
	old := make(map[int64]usageCostRecalculationOldFee, len(events))
	for first := 0; first < len(events); first += usageCostRecalculationIDChunk {
		last := min(first+usageCostRecalculationIDChunk, len(events))
		ids := make([]int64, 0, last-first)
		for _, event := range events[first:last] {
			ids = append(ids, event.ID)
		}
		var rows []usageCostRecalculationOldFee
		if err := tx.Table(table).Select("id, cost_usd, cost_available").Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("read old usage cost: %w", err)
		}
		for _, row := range rows {
			old[row.ID] = row
		}
	}
	return old, nil
}

// addUsageRecalculationDelta 把本页同桶费用差额合并，溢出在进入事务提交前返回错误。
func addUsageRecalculationDelta(group map[overview.BucketKey]usageCostRecalculationDelta, key overview.BucketKey, cost float64, unavailable int64) error {
	previous := group[key]
	previous.cost += cost
	if !finiteUsageRecalculationCost(previous.cost) {
		return fmt.Errorf("usage overview fee delta is not finite")
	}
	previous.unavailable += unavailable
	group[key] = previous
	return nil
}

// addUsageRecalculationBucketFee 只写现有桶的费用列，缺桶或累计成非有限值使整页回滚。
func addUsageRecalculationBucketFee(tx *gorm.DB, table string, key overview.BucketKey, delta usageCostRecalculationDelta) error {
	result := tx.Table(table).Where(usageCostRecalculationKey, usageCostRecalculationKeyArgs(key)...).
		Where("cost_usd IS NOT NULL AND unavailable_cost_count IS NOT NULL AND cost_usd + ? BETWEEN ? AND ? AND unavailable_cost_count + ? >= 0", delta.cost, -math.MaxFloat64, math.MaxFloat64, delta.unavailable).
		Updates(map[string]any{
			"cost_usd":               gorm.Expr("cost_usd + ?", delta.cost),
			"unavailable_cost_count": gorm.Expr("unavailable_cost_count + ?", delta.unavailable),
		})
	if result.Error != nil {
		return fmt.Errorf("update %s recalculation delta: %w", table, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%s expected fee bucket is missing, unbackfilled or not finite", table)
	}
	return nil
}

// usageCostRecalculationKeyArgs 按 Overview 唯一索引全部维度定位小时或日的同一行。
func usageCostRecalculationKeyArgs(key overview.BucketKey) []any {
	return []any{timeutil.FormatStorageTime(key.BucketStart), key.APIGroupKey, key.Model, key.AuthIndex, key.ModelAlias,
		key.ServiceTier, key.ResponseServiceTier, key.ReasoningEffort, key.Endpoint, key.ExecutorType}
}

func finiteUsageRecalculationCost(cost float64) bool {
	return !math.IsNaN(cost) && !math.IsInf(cost, 0)
}
