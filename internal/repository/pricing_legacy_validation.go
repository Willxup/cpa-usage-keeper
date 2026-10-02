package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

const (
	pricingLegacyValidationPageSize = 1000
	// 仅金额使用固定 1e-9 绝对 + 1e-12 相对容差；请求、Token、可用性及水位仍须精确相等。
	pricingLegacyCostAbsoluteTolerance = 1e-9
	pricingLegacyCostRelativeTolerance = 1e-12
)

// VerifyLegacyPricingData 是首次升级 M6 的只读完成门禁；调用方已停稳事件处理、聚合与维护。
// 使用 M1 唯一备份及 M2 旧转换合同验证原始事实，再核对固定 C/H 内冷热费用及完整小时/日桶；成功不修改状态。
func VerifyLegacyPricingData(ctx context.Context, reader *gorm.DB, baseline PricingLegacyBaseline) error {
	if reader == nil || baseline.Fixed == nil || baseline.SchemaVersion != pricingLegacyBaselineSchemaVersion {
		return fmt.Errorf("pricing M6 requires a fixed legacy baseline and reader")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var state entities.PricingMigrationState
	if err := reader.WithContext(ctx).Where("id = ?", 1).Take(&state).Error; err != nil {
		return fmt.Errorf("load pricing M6 state: %w", err)
	}
	if state.InitKind != PricingInitKindLegacy || !state.SchemaComplete || state.BackupPath == nil || *state.BackupPath == "" || state.BaselineJSON == nil || state.CursorsJSON == nil {
		return fmt.Errorf("pricing M6 has no protected complete schema or fixed cursors")
	}
	if state.Phase != "overview_backfilling" || state.DataComplete {
		return fmt.Errorf("pricing M6 requires completed overview backfill before data completion")
	}
	stored, err := decodePricingLegacyBaseline(*state.BaselineJSON)
	if err != nil {
		return fmt.Errorf("decode pricing M6 baseline: %w", err)
	}
	if stored.Fixed == nil || stored.Fixed.OverviewCursor != baseline.Fixed.OverviewCursor || stored.Fixed.HotMaxID != baseline.Fixed.HotMaxID || stored.Fixed.ArchiveMaxID != baseline.Fixed.ArchiveMaxID {
		return fmt.Errorf("pricing M6 fixed C/H differ from persisted baseline")
	}
	cursors, err := decodePricingMigrationEventCursors(*state.CursorsJSON, stored.Fixed)
	if err != nil {
		return fmt.Errorf("pricing M6 event cursors are invalid: %w", err)
	}
	if err := validatePricingOverviewCursor(cursors.Overview, stored.Fixed); err != nil {
		return fmt.Errorf("pricing M6 overview cursors are missing or invalid: %w", err)
	}
	fixed := stored.Fixed
	if cursors.HotAfterID != fixed.HotMaxID || cursors.ArchiveAfterID != fixed.ArchiveMaxID ||
		cursors.Overview.HotAfterID != pricingOverviewBound(fixed.OverviewCursor, fixed.HotMaxID) ||
		cursors.Overview.ArchiveAfterID != pricingOverviewBound(fixed.OverviewCursor, fixed.ArchiveMaxID) {
		return fmt.Errorf("pricing M6 event or overview cursors have not reached fixed bounds")
	}
	liveSchema, err := pricingM2Schema(ctx, reader)
	if err != nil {
		return fmt.Errorf("read pricing M6 checkpoint schema: %w", err)
	}
	if !hasPricingBaselineTable(liveSchema, "usage_aggregation_checkpoints") && !hasPricingBaselineTable(liveSchema, "usage_overview_aggregation_checkpoints") {
		return fmt.Errorf("pricing M6 overview checkpoint table is missing")
	}
	checkpoint, _, err := pricingLegacyOverviewCursor(ctx, reader, liveSchema)
	if err != nil || checkpoint != fixed.OverviewCursor {
		return fmt.Errorf("pricing M6 overview checkpoint changed from fixed C %d: current %d, error %v", fixed.OverviewCursor, checkpoint, err)
	}
	for _, target := range []struct {
		table string
		maxID int64
	}{{"usage_events", fixed.HotMaxID}, {"usage_events_archive", fixed.ArchiveMaxID}} {
		if err := verifyPricingLegacyEventCosts(ctx, reader, target.table, target.maxID); err != nil {
			return err
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		if err := verifyPricingLegacyCostGroups(ctx, reader, table, fixed.OverviewCursor); err != nil {
			return err
		}
	}
	// M1 已完成 quick_check 与备份原分组校验；此处复用同一只读文件，不再建镜像或重算价格。
	backupDB, closeBackup, err := openPricingBackupReadOnly(*state.BackupPath)
	if err != nil {
		return err
	}
	defer closeBackup()
	if err := VerifyPublishedPricingMigration(ctx, backupDB, reader, stored); err != nil {
		return fmt.Errorf("pricing M6 original event/count/Token facts: %w", err)
	}
	return nil
}

// verifyPricingLegacyEventCosts 按 ID 有界读取固定热/冷范围；明确零值合法，NULL、非有限值及越界新行失败。
func verifyPricingLegacyEventCosts(ctx context.Context, reader *gorm.DB, table string, maxID int64) error {
	var actualMax int64
	if err := reader.WithContext(ctx).Table(table).Select("COALESCE(MAX(id), 0)").Scan(&actualMax).Error; err != nil {
		return fmt.Errorf("read %s fixed H: %w", table, err)
	}
	if actualMax != maxID {
		return fmt.Errorf("%s max ID changed from fixed H %d to %d", table, maxID, actualMax)
	}
	var afterID int64
	for {
		rows, err := reader.WithContext(ctx).Raw("SELECT id, cost_usd, cost_available FROM "+table+" WHERE id > ? AND id <= ? ORDER BY id LIMIT ?", afterID, maxID, pricingLegacyValidationPageSize).Rows()
		if err != nil {
			return fmt.Errorf("read %s M6 costs: %w", table, err)
		}
		seen := 0
		for rows.Next() {
			var id int64
			var cost sql.NullFloat64
			var available sql.NullBool
			if err := rows.Scan(&id, &cost, &available); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan %s M6 cost: %w", table, err)
			}
			if !cost.Valid || math.IsNaN(cost.Float64) || math.IsInf(cost.Float64, 0) {
				_ = rows.Close()
				return fmt.Errorf("%s event %d cost_usd is NULL or non-finite", table, id)
			}
			if !available.Valid {
				_ = rows.Close()
				return fmt.Errorf("%s event %d cost_available is NULL or invalid", table, id)
			}
			afterID = id
			seen++
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return fmt.Errorf("read %s M6 cost page: %w", table, err)
		}
		if seen < pricingLegacyValidationPageSize {
			break
		}
	}
	return nil
}

type pricingLegacyCostGroup struct {
	key         []string
	cost        sql.NullFloat64
	unavailable sql.NullInt64
	physical    int64
	side        int64
}

// verifyPricingLegacyCostGroups 用一条 SQLite 分组查询对齐热/冷事件与旧桶，Go 仅保留当前一组。
func verifyPricingLegacyCostGroups(ctx context.Context, reader *gorm.DB, table string, cursor int64) error {
	query := pricingLegacyCostGroupsSQL(table)
	rows, err := reader.WithContext(ctx).Raw(query, cursor, cursor).Rows()
	if err != nil {
		return fmt.Errorf("read %s M6 cost groups: %w", table, err)
	}
	defer rows.Close()
	var pending *pricingLegacyCostGroup
	for rows.Next() {
		group := pricingLegacyCostGroup{key: make([]string, len(pricingM2Dimensions)+1)}
		dest := make([]any, 0, len(group.key)+4)
		for i := range group.key {
			dest = append(dest, &group.key[i])
		}
		dest = append(dest, &group.cost, &group.unavailable, &group.physical, &group.side)
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("scan %s M6 cost group: %w", table, err)
		}
		if !group.cost.Valid || math.IsNaN(group.cost.Float64) || math.IsInf(group.cost.Float64, 0) || !group.unavailable.Valid {
			return fmt.Errorf("%s group %v cost_usd or unavailable_cost_count is NULL or invalid", table, group.key)
		}
		if group.physical != 1 {
			return fmt.Errorf("%s group %v has %d physical rows", table, group.key, group.physical)
		}
		if pending == nil {
			if group.side != 0 {
				return fmt.Errorf("%s has stored cost group %v without covered events", table, group.key)
			}
			pending = &group
			continue
		}
		if group.side != 1 || !slices.Equal(pending.key, group.key) {
			return fmt.Errorf("%s covered event group %v has no matching stored cost group", table, pending.key)
		}
		if group.unavailable.Int64 != pending.unavailable.Int64 {
			return fmt.Errorf("%s group %v unavailable count differs: events %d, stored %d", table, group.key, pending.unavailable.Int64, group.unavailable.Int64)
		}
		if !pricingLegacyCloseCost(pending.cost.Float64, group.cost.Float64) {
			return fmt.Errorf("%s group %v cost differs: events %.12g, stored %.12g", table, group.key, pending.cost.Float64, group.cost.Float64)
		}
		pending = nil
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s M6 cost groups: %w", table, err)
	}
	if pending != nil {
		return fmt.Errorf("%s covered event group %v has no stored cost group", table, pending.key)
	}
	return nil
}

func pricingLegacyCloseCost(a, b float64) bool {
	return math.Abs(a-b) <= pricingLegacyCostAbsoluteTolerance+pricingLegacyCostRelativeTolerance*math.Max(math.Abs(a), math.Abs(b))
}

// pricingLegacyCostGroupsSQL 将固定 C 内冷热事件及旧小时/日桶投影为同一键序列，数据库分组后供流式逐组比较。
func pricingLegacyCostGroupsSQL(table string) string {
	keys := []string{"bucket_key"}
	selects := []string{pricingM2BucketExpr("timestamp", table) + " AS bucket_key"}
	statSelects := []string{pricingM2BucketExpr("bucket_start", table) + " AS bucket_key"}
	for _, field := range pricingM2Dimensions {
		keys = append(keys, field)
		selects = append(selects, pricingM2DimensionExpr(field)+" AS "+field)
		statSelects = append(statSelects, pricingM2DimensionExpr(field)+" AS "+field)
	}
	selects = append(selects, "cost_usd", "CASE WHEN cost_available = 0 THEN 1 ELSE 0 END AS unavailable")
	sources := []string{}
	for _, source := range []string{"usage_events", "usage_events_archive"} {
		sources = append(sources, "SELECT "+strings.Join(selects, ", ")+" FROM "+source+" WHERE id <= ?")
	}
	positions := make([]string, len(keys))
	for i := range keys {
		positions[i] = strconv.Itoa(i + 1)
	}
	return "WITH covered AS (" + strings.Join(sources, " UNION ALL ") + "), " +
		"expected AS (SELECT " + strings.Join(keys, ", ") + ", SUM(cost_usd) AS cost_usd, SUM(unavailable) AS unavailable_count, 1 AS physical, 0 AS side FROM covered GROUP BY " + strings.Join(keys, ", ") + "), " +
		"stored AS (SELECT " + strings.Join(statSelects, ", ") + ", SUM(cost_usd) AS cost_usd, SUM(unavailable_cost_count) AS unavailable_count, COUNT(*) AS physical, 1 AS side FROM " + table + " GROUP BY " + strings.Join(positions, ", ") + ") " +
		"SELECT * FROM expected UNION ALL SELECT * FROM stored ORDER BY " + strings.Join(positions, ", ") + ", " + strconv.Itoa(len(keys)+4)
}
