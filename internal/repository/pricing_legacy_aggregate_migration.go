package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	pricingMigrationPhaseOverviewBackfilling = "overview_backfilling"
	pricingMigrationPhaseDataComplete        = "data_complete"
	pricingMigrationOverviewPageSize         = 1000
)

const pricingMigrationOverviewKey = "bucket_start = ? AND api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ? AND service_tier = ? AND response_service_tier = ? AND reasoning_effort = ? AND endpoint = ? AND executor_type = ?"

type pricingMigrationFeeBucket struct {
	args        []any
	cost        float64
	unavailable int64
}

// CompleteLegacyPricingData 在 M4 明细费用完成后，只回填固定 C 内旧 Overview 桶费用。
// 每页冷热事件的小时、日费用和恢复游标同事务；全部核对成功后仅标 data_complete，业务 ready 留给启动阶段。
func CompleteLegacyPricingData(ctx context.Context, writer, reader *gorm.DB, baseline PricingLegacyBaseline) error {
	if writer == nil || reader == nil {
		return fmt.Errorf("pricing overview migration database is missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stored, cursors, cursorJSON, done, err := beginLegacyPricingOverview(ctx, writer, reader, baseline)
	if err != nil || done {
		return err
	}
	for _, target := range []struct {
		table string
		bound int64
	}{
		{"usage_events", pricingOverviewBound(stored.Fixed.OverviewCursor, stored.Fixed.HotMaxID)},
		{"usage_events_archive", pricingOverviewBound(stored.Fixed.OverviewCursor, stored.Fixed.ArchiveMaxID)},
	} {
		for {
			after := cursors.Overview.HotAfterID
			if target.table == "usage_events_archive" {
				after = cursors.Overview.ArchiveAfterID
			}
			if after >= target.bound {
				break
			}
			events, err := loadLegacyPricingOverviewPage(ctx, reader, target.table, after, target.bound)
			if err != nil {
				return err
			}
			next := cursors
			nextOverview := *cursors.Overview
			next.Overview = &nextOverview
			nextAfter := target.bound
			if len(events) > 0 {
				nextAfter = events[len(events)-1].ID
			}
			if target.table == "usage_events" {
				next.Overview.HotAfterID = nextAfter
			} else {
				next.Overview.ArchiveAfterID = nextAfter
			}
			nextBytes, err := json.Marshal(next)
			if err != nil {
				return fmt.Errorf("encode pricing overview cursor: %w", err)
			}
			if err := applyLegacyPricingOverviewPage(ctx, writer, events, cursorJSON, string(nextBytes)); err != nil {
				return fmt.Errorf("backfill %s overview after %d: %w", target.table, after, err)
			}
			cursors, cursorJSON = next, string(nextBytes)
		}
	}
	// 只读全范围核对不占唯一 writer；任何事件、分组或金额缺口都阻止 data_complete。
	if err := VerifyLegacyPricingData(ctx, reader, stored); err != nil {
		return fmt.Errorf("verify completed pricing data: %w", err)
	}
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND init_kind = ? AND phase = ? AND schema_complete = ? AND data_complete = ? AND cursors_json = ?", 1, PricingInitKindLegacy, pricingMigrationPhaseOverviewBackfilling, true, false, cursorJSON).
			Updates(map[string]any{"phase": pricingMigrationPhaseDataComplete, "data_complete": true})
		if result.Error != nil {
			return fmt.Errorf("mark pricing data complete: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("pricing overview cursor changed before data completion")
		}
		return nil
	})
}

// beginLegacyPricingOverview 用持久 fixed、M4 游标和 phase 判定恢复点。
// 首次进入只在 reader 证明旧桶费用全为 NULL 后以短 writer 事务记录新游标；已有费用无游标不能静默再加。
func beginLegacyPricingOverview(ctx context.Context, writer, reader *gorm.DB, baseline PricingLegacyBaseline) (PricingLegacyBaseline, PricingMigrationEventCursors, string, bool, error) {
	var state entities.PricingMigrationState
	if err := writer.Clauses(dbresolver.Write).WithContext(ctx).Where("id = ?", 1).Take(&state).Error; err != nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("load pricing overview state: %w", err)
	}
	if state.InitKind != PricingInitKindLegacy || !state.SchemaComplete || state.BaselineJSON == nil || state.CursorsJSON == nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("pricing overview requires completed legacy event schema")
	}
	stored, err := decodePricingLegacyBaseline(*state.BaselineJSON)
	if err != nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, err
	}
	if stored.Fixed == nil || !reflect.DeepEqual(stored.Fixed, baseline.Fixed) {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("pricing overview fixed baseline changed")
	}
	cursors, err := decodePricingMigrationEventCursors(*state.CursorsJSON, stored.Fixed)
	if err != nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, err
	}
	if cursors.HotAfterID != stored.Fixed.HotMaxID || cursors.ArchiveAfterID != stored.Fixed.ArchiveMaxID {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("pricing event backfill is incomplete")
	}
	if state.DataComplete {
		if state.Phase != pricingMigrationPhaseDataComplete || cursors.Overview == nil {
			return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("completed pricing state has no overview cursor")
		}
		return stored, cursors, *state.CursorsJSON, true, nil
	}
	if state.Phase == pricingMigrationPhaseOverviewBackfilling {
		if cursors.Overview == nil {
			return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("pricing overview phase has no cursor")
		}
		if err := validatePricingOverviewCursor(cursors.Overview, stored.Fixed); err != nil {
			return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, err
		}
		return stored, cursors, *state.CursorsJSON, false, nil
	}
	if state.Phase != pricingMigrationPhaseEventsBackfilled || cursors.Overview != nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("pricing overview initial phase/cursor is inconsistent")
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var dirty int64
		if err := reader.Clauses(dbresolver.Read).WithContext(ctx).Table(table).
			Select("COUNT(*)").Where("cost_usd IS NOT NULL OR unavailable_cost_count IS NOT NULL").Scan(&dirty).Error; err != nil {
			return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("check %s initial fee state: %w", table, err)
		}
		if dirty != 0 {
			return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("%s has amount without pricing overview cursor", table)
		}
	}
	cursors.Overview = &PricingMigrationOverviewCursors{}
	nextBytes, err := json.Marshal(cursors)
	if err != nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("encode pricing overview initial cursor: %w", err)
	}
	if err := writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND phase = ? AND data_complete = ? AND cursors_json = ?", 1, pricingMigrationPhaseEventsBackfilled, false, *state.CursorsJSON).
			Updates(map[string]any{"phase": pricingMigrationPhaseOverviewBackfilling, "cursors_json": string(nextBytes)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("pricing overview state changed before start")
		}
		return nil
	}); err != nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, err
	}
	return stored, cursors, string(nextBytes), false, nil
}

func pricingOverviewBound(cursor, maxID int64) int64 {
	if cursor < maxID {
		return cursor
	}
	return maxID
}

// validatePricingOverviewCursor 限定已提交的冷热扫描点，不把 ID 空洞误认作事件条数。
func validatePricingOverviewCursor(cursors *PricingMigrationOverviewCursors, fixed *PricingMigrationFixedBaseline) error {
	if cursors == nil || cursors.HotAfterID < 0 || cursors.ArchiveAfterID < 0 ||
		cursors.HotAfterID > pricingOverviewBound(fixed.OverviewCursor, fixed.HotMaxID) ||
		cursors.ArchiveAfterID > pricingOverviewBound(fixed.OverviewCursor, fixed.ArchiveMaxID) {
		return fmt.Errorf("invalid pricing overview cursor")
	}
	return nil
}

// loadLegacyPricingOverviewPage 从只读池取最多 1000 条固定 C 内已存事件，供纯内存分桶。
func loadLegacyPricingOverviewPage(ctx context.Context, reader *gorm.DB, table string, afterID, bound int64) ([]entities.UsageEvent, error) {
	var events []entities.UsageEvent
	if err := reader.Clauses(dbresolver.Read).WithContext(ctx).Table(table).
		Select(entities.UsageAggregationEventProjectionColumns).
		Where("id > ? AND id <= ?", afterID, bound).Order("id ASC").Limit(pricingMigrationOverviewPageSize).
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("read %s overview event page: %w", table, err)
	}
	return events, nil
}

// applyLegacyPricingOverviewPage 只累计已存费用和不可用量，不触碰请求、Token、时间或普通 checkpoint。
// 调用方以同一事务持有 hourly、daily 和本页游标；任一桶缺失或写入失败，整页回滚。
func applyLegacyPricingOverviewPage(ctx context.Context, writer *gorm.DB, events []entities.UsageEvent, oldCursor, nextCursor string) error {
	var hourly []entities.UsageOverviewHourlyStat
	var daily []entities.UsageOverviewDailyStat
	if len(events) > 0 {
		var err error
		hourly, daily, _, err = overview.BuildRows(events)
		if err != nil {
			return err
		}
	}
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, row := range hourly {
			bucket := pricingMigrationFeeBucket{
				args: []any{timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.AuthIndex, row.ModelAlias, row.ServiceTier, row.ResponseServiceTier, row.ReasoningEffort, row.Endpoint, row.ExecutorType},
				cost: *row.CostUSD, unavailable: *row.UnavailableCostCount,
			}
			if err := addLegacyPricingOverviewFee(tx, "usage_overview_hourly_stats", bucket); err != nil {
				return err
			}
		}
		for _, row := range daily {
			bucket := pricingMigrationFeeBucket{
				args: []any{timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.AuthIndex, row.ModelAlias, row.ServiceTier, row.ResponseServiceTier, row.ReasoningEffort, row.Endpoint, row.ExecutorType},
				cost: *row.CostUSD, unavailable: *row.UnavailableCostCount,
			}
			if err := addLegacyPricingOverviewFee(tx, "usage_overview_daily_stats", bucket); err != nil {
				return err
			}
		}
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND phase = ? AND cursors_json = ?", 1, pricingMigrationPhaseOverviewBackfilling, oldCursor).
			Update("cursors_json", nextCursor)
		if result.Error != nil {
			return fmt.Errorf("advance pricing overview cursor: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("pricing overview cursor changed before page commit")
		}
		return nil
	})
}

// addLegacyPricingOverviewFee 仅在首次升级阶段把 NULL 旧桶视为尚未累加的起点。
// 完整键必须已有行，且金额相加后仍有限；普通增量聚合继续拒绝 NULL 桶。
func addLegacyPricingOverviewFee(tx *gorm.DB, table string, bucket pricingMigrationFeeBucket) error {
	if math.IsNaN(bucket.cost) || math.IsInf(bucket.cost, 0) || bucket.unavailable < 0 {
		return fmt.Errorf("%s overview page fee is invalid", table)
	}
	result := tx.Table(table).Where(pricingMigrationOverviewKey, bucket.args...).Where(
		"((cost_usd IS NULL AND unavailable_cost_count IS NULL) OR (cost_usd IS NOT NULL AND unavailable_cost_count IS NOT NULL)) AND COALESCE(cost_usd, 0) + ? BETWEEN ? AND ? AND COALESCE(unavailable_cost_count, 0) <= ?",
		bucket.cost, -math.MaxFloat64, math.MaxFloat64, math.MaxInt64-bucket.unavailable,
	).UpdateColumns(map[string]any{
		"cost_usd":               gorm.Expr("COALESCE(cost_usd, 0) + ?", bucket.cost),
		"unavailable_cost_count": gorm.Expr("COALESCE(unavailable_cost_count, 0) + ?", bucket.unavailable),
	})
	if result.Error != nil {
		return fmt.Errorf("write %s overview fee: %w", table, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%s expected fee bucket is missing or invalid: %v", table, bucket.args)
	}
	return nil
}
