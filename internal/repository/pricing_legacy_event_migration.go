package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	pricingMigrationEventCursorSchemaVersion = 1
	pricingMigrationEventPageSize            = 1000
	pricingMigrationPhaseBaselineFixed       = "baseline_fixed"
	pricingMigrationPhaseSchemaComplete      = "schema_complete"
	pricingMigrationPhaseEventsBackfilled    = "events_backfilled"
)

var pricingMigrationEventColumns = []string{
	"id", "api_group_key", "model", "model_alias", "auth_index", "service_tier",
	"response_service_tier", "reasoning_effort", "endpoint", "executor_type", "timestamp",
	"input_tokens", "output_tokens", "cache_read_tokens", "cache_creation_tokens",
}

// PricingMigrationEventCursors 保存冷热明细及后续汇总费用已提交的最后 ID；ID 空洞不作数量解释。
type PricingMigrationEventCursors struct {
	SchemaVersion  int                              `json:"schema_version"`
	HotAfterID     int64                            `json:"hot_after_id"`
	ArchiveAfterID int64                            `json:"archive_after_id"`
	Overview       *PricingMigrationOverviewCursors `json:"overview,omitempty"`
}

// PricingMigrationOverviewCursors 仅在 M5 持久进入回填阶段后存在，区分旧 NULL 初态和已提交的部分金额。
type PricingMigrationOverviewCursors struct {
	HotAfterID     int64 `json:"hot_after_id"`
	ArchiveAfterID int64 `json:"archive_after_id"`
}

type pricingMigrationEventCost struct {
	id        int64
	amount    float64
	available bool
}

// MigrateLegacyPricingEvents 按 M1→M4 恢复首次旧库升级，不启动正常事件处理或汇总回填。
// M1 唯一备份先于任何旧 migration；M2 原转换需通过备份对账；M3 先固定 C/H/旧价再增列；M4 逐页提交费用和游标。
// 本阶段只标记 schema 已完成，data_complete 由后续汇总费用和全量核对阶段决定。
func MigrateLegacyPricingEvents(ctx context.Context, writer, reader *gorm.DB, backupDir string, now time.Time) (PricingLegacyBaseline, error) {
	if writer == nil || reader == nil {
		return PricingLegacyBaseline{}, fmt.Errorf("pricing migration database is missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	baseline, err := ProtectPricingLegacyMigration(ctx, writer, reader, backupDir, now)
	if err != nil {
		return PricingLegacyBaseline{}, fmt.Errorf("protect legacy pricing migration: %w", err)
	}
	if baseline.Fixed == nil {
		path, pathErr := pricingMigrationBackupPath(ctx, writer)
		if pathErr != nil {
			return baseline, pathErr
		}
		// Protect 已对同一路径完成 quick_check 和原基线复验；M2 复用只读备份，不再次全文件扫描。
		backupDB, closeBackup, openErr := openPricingBackupReadOnly(path)
		if openErr != nil {
			return baseline, openErr
		}
		defer closeBackup()
		options := migration.RunOptions{BeforeDestructiveMigration: func(hookCtx context.Context, _ string) error {
			// 历史清表门禁复用已验证的 M1 文件；每次仅核对路径和句柄仍可用，不重写唯一备份。
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("protected pricing backup is unavailable: %w", err)
			}
			var alive int
			if err := backupDB.WithContext(hookCtx).Raw("SELECT 1").Scan(&alive).Error; err != nil || alive != 1 {
				return fmt.Errorf("protected pricing backup is unreadable: %v", err)
			}
			return nil
		}}
		if err := migration.RunPublished(writer.WithContext(ctx), options); err != nil {
			return baseline, fmt.Errorf("run published migrations: %w", err)
		}
		if err := VerifyPublishedPricingMigration(ctx, backupDB, reader, baseline); err != nil {
			return baseline, fmt.Errorf("verify published migrations: %w", err)
		}
	}
	baseline, err = fixPublishedPricingBaseline(ctx, writer)
	if err != nil {
		return baseline, fmt.Errorf("fix published pricing baseline: %w", err)
	}
	if err := migration.RunPricingStorageStructure(writer.WithContext(ctx)); err != nil {
		return baseline, fmt.Errorf("add pricing storage structure: %w", err)
	}
	if err := markPricingMigrationSchemaComplete(ctx, writer); err != nil {
		return baseline, fmt.Errorf("record pricing schema completion: %w", err)
	}
	if err := backfillLegacyPricingEvents(ctx, writer, reader, baseline); err != nil {
		return baseline, fmt.Errorf("backfill legacy event costs: %w", err)
	}
	return baseline, nil
}

// pricingMigrationBackupPath 只从持久状态取 M1 唯一路径，不从当前 live 库重新选备份源。
func pricingMigrationBackupPath(ctx context.Context, writer *gorm.DB) (string, error) {
	var state entities.PricingMigrationState
	if err := writer.Clauses(dbresolver.Write).WithContext(ctx).Where("id = ?", 1).Take(&state).Error; err != nil {
		return "", fmt.Errorf("load pricing protection path: %w", err)
	}
	if state.BackupPath == nil || *state.BackupPath == "" || state.BaselineJSON == nil {
		return "", fmt.Errorf("pricing legacy protection is incomplete")
	}
	return *state.BackupPath, nil
}

// fixPublishedPricingBaseline 在同一短事务中读取 M2 后实际旧列、C 和冷热 H，仅首次保存。
// 重启看到 fixed 时只验证已存配置，不重新读取会变化的价格或事件上限。
func fixPublishedPricingBaseline(ctx context.Context, writer *gorm.DB) (PricingLegacyBaseline, error) {
	var baseline PricingLegacyBaseline
	err := writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state entities.PricingMigrationState
		if err := tx.Where("id = ?", 1).Take(&state).Error; err != nil {
			return err
		}
		if state.InitKind != PricingInitKindLegacy || state.BaselineJSON == nil || state.BackupPath == nil {
			return fmt.Errorf("legacy pricing baseline is not protected")
		}
		stored, err := decodePricingLegacyBaseline(*state.BaselineJSON)
		if err != nil {
			return err
		}
		if stored.Fixed != nil {
			if state.CursorsJSON == nil {
				return fmt.Errorf("fixed pricing baseline has no event cursors")
			}
			if _, err := compileFixedPricingBaseline(stored.Fixed); err != nil {
				return err
			}
			baseline = stored
			return nil
		}
		var structureCount int64
		if err := tx.Table("schema_migrations").Where("version = ?", "20261002_pricing_storage_structure").Count(&structureCount).Error; err != nil {
			return err
		}
		if structureCount > 0 {
			return fmt.Errorf("pricing structure exists without fixed pre-structure baseline")
		}
		for _, table := range []string{"usage_events", "usage_events_archive", "usage_aggregation_checkpoints"} {
			if !tx.Migrator().HasTable(table) {
				return fmt.Errorf("published migration did not create %s", table)
			}
		}
		var cursor int64
		if err := tx.Table("usage_aggregation_checkpoints").Select("last_aggregated_usage_event_id").Where("name = ?", entities.UsageAggregationCheckpointOverview).Scan(&cursor).Error; err != nil {
			return fmt.Errorf("read post-migration overview cursor: %w", err)
		}
		if cursor < 0 {
			return fmt.Errorf("post-migration overview cursor is negative")
		}
		hotMax, err := pricingMigrationTableMaxID(tx, "usage_events")
		if err != nil {
			return err
		}
		archiveMax, err := pricingMigrationTableMaxID(tx, "usage_events_archive")
		if err != nil {
			return err
		}
		configs, err := LoadPublishedPricingConfigs(ctx, tx)
		if err != nil {
			return err
		}
		fixed := &PricingMigrationFixedBaseline{OverviewCursor: cursor, HotMaxID: hotMax, ArchiveMaxID: archiveMax, Configs: configs}
		if _, err := compileFixedPricingBaseline(fixed); err != nil {
			return err
		}
		stored.Fixed = fixed
		baselineJSON, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		cursorJSON, err := json.Marshal(PricingMigrationEventCursors{SchemaVersion: pricingMigrationEventCursorSchemaVersion})
		if err != nil {
			return err
		}
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND init_kind = ? AND baseline_json = ? AND cursors_json IS NULL", 1, PricingInitKindLegacy, *state.BaselineJSON).
			Updates(map[string]any{"baseline_json": string(baselineJSON), "cursors_json": string(cursorJSON), "phase": pricingMigrationPhaseBaselineFixed})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("pricing baseline changed before fixation")
		}
		baseline = stored
		return nil
	})
	return baseline, err
}

// pricingMigrationTableMaxID 在 M3 事务内固定单表上限；空表为 0，ID 空洞不影响回填范围。
func pricingMigrationTableMaxID(tx *gorm.DB, table string) (int64, error) {
	var maxID int64
	if err := tx.Table(table).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		return 0, fmt.Errorf("read post-migration %s max ID: %w", table, err)
	}
	return maxID, nil
}

// compileFixedPricingBaseline 从持久基线恢复只读配置，不读取现库单价，异常基线阻止继续回填。
func compileFixedPricingBaseline(fixed *PricingMigrationFixedBaseline) (*pricing.Snapshot, error) {
	if fixed == nil || fixed.OverviewCursor < 0 || fixed.HotMaxID < 0 || fixed.ArchiveMaxID < 0 || fixed.Configs == nil {
		return nil, fmt.Errorf("invalid fixed pricing migration baseline")
	}
	snapshot, err := pricing.CompilePricingSnapshot(fixed.Configs, time.Local)
	if err != nil {
		return nil, fmt.Errorf("compile fixed pricing migration baseline: %w", err)
	}
	return snapshot, nil
}

// markPricingMigrationSchemaComplete 在结构版本已提交后记录真实状态；崩溃重启可从版本补齐该标志。
func markPricingMigrationSchemaComplete(ctx context.Context, writer *gorm.DB) error {
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var versionCount int64
		if err := tx.Table("schema_migrations").Where("version = ?", "20261002_pricing_storage_structure").Count(&versionCount).Error; err != nil {
			return err
		}
		if versionCount != 1 {
			return fmt.Errorf("pricing storage structure version is not committed")
		}
		var state entities.PricingMigrationState
		if err := tx.Where("id = ?", 1).Take(&state).Error; err != nil {
			return err
		}
		if state.InitKind != PricingInitKindLegacy || state.BaselineJSON == nil || state.CursorsJSON == nil {
			return fmt.Errorf("pricing fixed baseline is missing before schema completion")
		}
		updates := map[string]any{"schema_complete": true}
		if state.Phase == pricingMigrationPhaseBaselineFixed {
			updates["phase"] = pricingMigrationPhaseSchemaComplete
		}
		return tx.Model(&entities.PricingMigrationState{}).Where("id = ?", 1).Updates(updates).Error
	})
}

// backfillLegacyPricingEvents 依固定旧价按冷热 ID 各自推进，完成后只记事件阶段，不宣告费用汇总完成。
func backfillLegacyPricingEvents(ctx context.Context, writer, reader *gorm.DB, baseline PricingLegacyBaseline) error {
	snapshot, err := compileFixedPricingBaseline(baseline.Fixed)
	if err != nil {
		return err
	}
	resolver := pricing.NewCatalog(snapshot).NewResolver()
	var state entities.PricingMigrationState
	if err := writer.Clauses(dbresolver.Write).WithContext(ctx).Where("id = ?", 1).Take(&state).Error; err != nil {
		return err
	}
	if !state.SchemaComplete || state.CursorsJSON == nil {
		return fmt.Errorf("pricing event backfill requires complete schema and cursors")
	}
	cursors, err := decodePricingMigrationEventCursors(*state.CursorsJSON, baseline.Fixed)
	if err != nil {
		return err
	}
	cursorJSON := *state.CursorsJSON
	for _, target := range []struct {
		table string
		maxID int64
		after *int64
	}{{"usage_events", baseline.Fixed.HotMaxID, &cursors.HotAfterID}, {"usage_events_archive", baseline.Fixed.ArchiveMaxID, &cursors.ArchiveAfterID}} {
		for *target.after < target.maxID {
			rows, err := loadPricingMigrationEventPage(ctx, reader, target.table, *target.after, target.maxID)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				return fmt.Errorf("%s has no event page after %d before fixed H %d", target.table, *target.after, target.maxID)
			}
			costs := make([]pricingMigrationEventCost, len(rows))
			for index, event := range rows {
				fee := resolver.CalculateFee(UsageEventCostSubject(event))
				if math.IsNaN(fee.TotalCostUSD) || math.IsInf(fee.TotalCostUSD, 0) {
					return fmt.Errorf("calculate %s event %d fee: non-finite amount", target.table, event.ID)
				}
				costs[index] = pricingMigrationEventCost{id: event.ID, amount: fee.TotalCostUSD, available: fee.Available}
			}
			next := cursors
			if target.table == "usage_events" {
				next.HotAfterID = rows[len(rows)-1].ID
			} else {
				next.ArchiveAfterID = rows[len(rows)-1].ID
			}
			nextJSON, err := json.Marshal(next)
			if err != nil {
				return err
			}
			if err := applyPricingMigrationEventPage(ctx, writer, target.table, costs, cursorJSON, string(nextJSON)); err != nil {
				return err
			}
			cursors, cursorJSON = next, string(nextJSON)
		}
	}
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state entities.PricingMigrationState
		if err := tx.Where("id = ?", 1).Take(&state).Error; err != nil {
			return err
		}
		if state.CursorsJSON == nil || *state.CursorsJSON != cursorJSON || !state.SchemaComplete {
			return fmt.Errorf("pricing event cursors changed before completion")
		}
		if state.Phase != pricingMigrationPhaseSchemaComplete {
			// CMT14 及后续阶段的状态只允许向前，重复 M4 不得退回事件回填阶段。
			return nil
		}
		return tx.Model(&entities.PricingMigrationState{}).Where("id = ?", 1).Update("phase", pricingMigrationPhaseEventsBackfilled).Error
	})
}

// decodePricingMigrationEventCursors 校验恢复游标属于固定冷热范围；不根据现库 MAX(id) 扩大目标。
func decodePricingMigrationEventCursors(value string, fixed *PricingMigrationFixedBaseline) (PricingMigrationEventCursors, error) {
	var cursors PricingMigrationEventCursors
	if err := json.Unmarshal([]byte(value), &cursors); err != nil || cursors.SchemaVersion != pricingMigrationEventCursorSchemaVersion ||
		cursors.HotAfterID < 0 || cursors.ArchiveAfterID < 0 || cursors.HotAfterID > fixed.HotMaxID || cursors.ArchiveAfterID > fixed.ArchiveMaxID {
		return PricingMigrationEventCursors{}, fmt.Errorf("invalid pricing event migration cursors")
	}
	return cursors, nil
}

// loadPricingMigrationEventPage 只读固定旧事实列；费用列 NULL 与否不影响计价输入和分页位置。
func loadPricingMigrationEventPage(ctx context.Context, db *gorm.DB, table string, afterID, maxID int64) ([]entities.UsageEvent, error) {
	var rows []entities.UsageEvent
	if err := db.Clauses(dbresolver.Read).WithContext(ctx).Table(table).Select(pricingMigrationEventColumns).
		Where("id > ? AND id <= ?", afterID, maxID).Order("id ASC").Limit(pricingMigrationEventPageSize).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read %s pricing event page: %w", table, err)
	}
	return rows, nil
}

// applyPricingMigrationEventPage 把最多 1000 条费用与该表游标写在同一短事务；错误回滚整页。
func applyPricingMigrationEventPage(ctx context.Context, writer *gorm.DB, table string, costs []pricingMigrationEventCost, oldCursors, nextCursors string) error {
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, cost := range costs {
			result := tx.Table(table).Where("id = ?", cost.id).Updates(map[string]any{
				"cost_usd": cost.amount, "cost_available": cost.available,
			})
			if result.Error != nil {
				return fmt.Errorf("write %s event %d cost: %w", table, cost.id, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%s event %d disappeared before cost write", table, cost.id)
			}
		}
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND cursors_json = ?", 1, oldCursors).
			Update("cursors_json", nextCursors)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("pricing event cursor changed before page commit")
		}
		return nil
	})
}
