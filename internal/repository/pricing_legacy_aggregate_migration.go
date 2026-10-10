package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository/overviewstore"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	pricingMigrationPhaseOverviewRebuilding = "overview_rebuilding"
	pricingMigrationPhaseDataComplete       = "data_complete"
	pricingMigrationOverviewPageSize        = 1000
)

// CompleteLegacyPricingData 在 M4 明细费用完成后，从固定 C 内现存冷热事件重建完整 Overview。
// 每页冷热事件的小时、日统计和恢复游标同事务；全部核对成功后仅标 data_complete，业务 ready 留给启动阶段。
func CompleteLegacyPricingData(ctx context.Context, writer, reader *gorm.DB, baseline PricingLegacyBaseline) error {
	if writer == nil || reader == nil {
		return fmt.Errorf("pricing overview migration database is missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stored, cursors, cursorJSON, done, err := beginLegacyPricingOverview(ctx, writer, baseline)
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
				return fmt.Errorf("rebuild %s overview after %d: %w", target.table, after, err)
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
			Where("id = ? AND init_kind = ? AND phase = ? AND schema_complete = ? AND data_complete = ? AND cursors_json = ?", 1, PricingInitKindLegacy, pricingMigrationPhaseOverviewRebuilding, true, false, cursorJSON).
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
// 首次进入在同一事务清空旧桶并记录重建游标；恢复时保留已提交桶，不重清、不重复累计。
func beginLegacyPricingOverview(ctx context.Context, writer *gorm.DB, baseline PricingLegacyBaseline) (PricingLegacyBaseline, PricingMigrationEventCursors, string, bool, error) {
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
	if state.Phase == pricingMigrationPhaseOverviewRebuilding {
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
	cursors.Overview = &PricingMigrationOverviewCursors{}
	nextBytes, err := json.Marshal(cursors)
	if err != nil {
		return PricingLegacyBaseline{}, PricingMigrationEventCursors{}, "", false, fmt.Errorf("encode pricing overview initial cursor: %w", err)
	}
	if err := writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND phase = ? AND data_complete = ? AND cursors_json = ?", 1, pricingMigrationPhaseEventsBackfilled, false, *state.CursorsJSON).
			Updates(map[string]any{"phase": pricingMigrationPhaseOverviewRebuilding, "cursors_json": string(nextBytes)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("pricing overview state changed before start")
		}
		// 阶段/游标与清空原子提交；任一删除失败，旧桶与初始阶段一起回滚。
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			if err := tx.Table(table).Where("1 = 1").Delete(nil).Error; err != nil {
				return fmt.Errorf("clear %s for pricing rebuild: %w", table, err)
			}
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

// applyLegacyPricingOverviewPage 复用正式聚合公式写入请求、Token、已存费用和两种速度，不改普通 checkpoint。
// hourly、daily 与本页恢复游标同事务提交；任一步失败整页回滚。
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
		if err := overviewstore.ApplyRows(tx, hourly, daily, time.Now()); err != nil {
			return err
		}
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND phase = ? AND cursors_json = ?", 1, pricingMigrationPhaseOverviewRebuilding, oldCursor).
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
