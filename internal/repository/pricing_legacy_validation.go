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
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

const (
	pricingLegacyValidationPageSize = 1000
	// 金额和速度 sum 使用 1e-9 绝对 + 1e-12 相对容差；所有计数及水位仍须精确相等。
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
	if state.Phase != "overview_rebuilding" || state.DataComplete {
		return fmt.Errorf("pricing M6 requires completed overview rebuild before data completion")
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
	if err := reader.WithContext(ctx).Connection(func(db *gorm.DB) error {
		// SQLite 分组使用 Go 的存储时间解析与 Overview 分桶，避免 SQL 日期函数丢失项目时区或纳秒精度。
		// 函数与查询固定在同一只读连接；不注册全局 driver，也不改写原始时间。
		conn, ok := db.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return fmt.Errorf("pricing M6 requires a pinned SQL connection")
		}
		if err := conn.Raw(func(raw any) error {
			sqliteConn, ok := raw.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("pricing M6 requires a SQLite connection")
			}
			return sqliteConn.RegisterFunc("pricing_overview_bucket", func(value string, daily bool) (string, error) {
				timestamp, err := timeutil.ParseStorageTime(value)
				if err != nil {
					return "", err
				}
				hour, day := overview.BucketKeysForEvent(entities.UsageEvent{Timestamp: timestamp})
				bucket := hour.BucketStart
				if daily {
					bucket = day.BucketStart
				}
				return strconv.FormatInt(bucket.Unix(), 10), nil
			}, true)
		}); err != nil {
			return fmt.Errorf("register pricing M6 bucket function: %w", err)
		}
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			if err := verifyPricingOverviewCounts(ctx, db, liveSchema, table, fixed.OverviewCursor); err != nil {
				return fmt.Errorf("verify rebuilt %s: %w", table, err)
			}
			if err := verifyPricingLegacyCostGroups(ctx, db, table, fixed.OverviewCursor); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// M1 已完成 quick_check 并固定备份；此处复用同一只读文件，不再建镜像或重算价格。
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
	speed       sql.NullFloat64
	speedCount  sql.NullInt64
	decodeSpeed sql.NullFloat64
	decodeCount sql.NullInt64
	physical    int64
	side        int64
}

// verifyPricingLegacyCostGroups 同轮核对费用与两种速度，复用原分组扫描，Go 仅保留当前一组。
func verifyPricingLegacyCostGroups(ctx context.Context, reader *gorm.DB, table string, cursor int64) error {
	query := pricingLegacyCostGroupsSQL(table)
	rows, err := reader.WithContext(ctx).Raw(query, cursor, cursor).Rows()
	if err != nil {
		return fmt.Errorf("read %s M6 cost groups: %w", table, err)
	}
	defer rows.Close()
	var pending *pricingLegacyCostGroup
	for rows.Next() {
		group := pricingLegacyCostGroup{key: make([]string, len(pricingOverviewDimensions)+1)}
		dest := make([]any, 0, len(group.key)+8)
		for i := range group.key {
			dest = append(dest, &group.key[i])
		}
		dest = append(dest, &group.cost, &group.unavailable, &group.speed, &group.speedCount, &group.decodeSpeed, &group.decodeCount, &group.physical, &group.side)
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("scan %s M6 cost group: %w", table, err)
		}
		if !group.cost.Valid || math.IsNaN(group.cost.Float64) || math.IsInf(group.cost.Float64, 0) || !group.unavailable.Valid {
			return fmt.Errorf("%s group %v cost_usd or unavailable_cost_count is NULL or invalid", table, group.key)
		}
		if !group.speed.Valid || !group.decodeSpeed.Valid || !group.speedCount.Valid || !group.decodeCount.Valid ||
			math.IsNaN(group.speed.Float64) || math.IsInf(group.speed.Float64, 0) ||
			math.IsNaN(group.decodeSpeed.Float64) || math.IsInf(group.decodeSpeed.Float64, 0) ||
			group.speed.Float64 < 0 || group.decodeSpeed.Float64 < 0 || group.speedCount.Int64 < 0 || group.decodeCount.Int64 < 0 {
			return fmt.Errorf("%s group %v speed statistics are NULL or invalid", table, group.key)
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
		if pending.speedCount.Int64 != group.speedCount.Int64 || pending.decodeCount.Int64 != group.decodeCount.Int64 ||
			!pricingLegacyCloseCost(pending.speed.Float64, group.speed.Float64) || !pricingLegacyCloseCost(pending.decodeSpeed.Float64, group.decodeSpeed.Float64) {
			return fmt.Errorf("%s group %v speed statistics differ: events %.12g/%d %.12g/%d, stored %.12g/%d %.12g/%d", table, group.key,
				pending.speed.Float64, pending.speedCount.Int64, pending.decodeSpeed.Float64, pending.decodeCount.Int64,
				group.speed.Float64, group.speedCount.Int64, group.decodeSpeed.Float64, group.decodeCount.Int64)
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

// pricingLegacyCostGroupsSQL 将固定 C 内冷热事件及重建小时/日桶投影为同一键序列，数据库分组后供流式逐组比较。
func pricingLegacyCostGroupsSQL(table string) string {
	keys := []string{"bucket_key"}
	selects := []string{pricingOverviewBucketExpr("timestamp", table) + " AS bucket_key"}
	statSelects := []string{pricingOverviewBucketExpr("bucket_start", table) + " AS bucket_key"}
	for _, field := range pricingOverviewDimensions {
		keys = append(keys, field)
		selects = append(selects, pricingOverviewDimensionExpr(field)+" AS "+field)
		statSelects = append(statSelects, pricingOverviewDimensionExpr(field)+" AS "+field)
	}
	selects = append(selects, "cost_usd", "CASE WHEN cost_available = 0 THEN 1 ELSE 0 END AS unavailable")
	// SQL 独立重算逐请求速度作为校验 oracle；不能改成 SUM(output_tokens)/SUM(latency_ms)。
	const speedValid = "output_tokens > 0 AND latency_ms > 0"
	const decodeValid = speedValid + " AND ttft_ms > 0 AND ttft_ms < latency_ms"
	selects = append(selects,
		"CASE WHEN "+speedValid+" THEN CAST(output_tokens AS REAL) * 1000.0 / latency_ms ELSE 0.0 END AS speed_tps_sum",
		"CASE WHEN "+speedValid+" THEN 1 ELSE 0 END AS speed_sample_count",
		"CASE WHEN "+decodeValid+" THEN CAST(output_tokens AS REAL) * 1000.0 / (latency_ms - ttft_ms) ELSE 0.0 END AS decode_speed_tps_sum",
		"CASE WHEN "+decodeValid+" THEN 1 ELSE 0 END AS decode_speed_sample_count",
	)
	sources := []string{}
	for _, source := range []string{"usage_events", "usage_events_archive"} {
		sources = append(sources, "SELECT "+strings.Join(selects, ", ")+" FROM "+source+" WHERE id <= ?")
	}
	positions := make([]string, len(keys))
	for i := range keys {
		positions[i] = strconv.Itoa(i + 1)
	}
	const speedSums = ", SUM(speed_tps_sum), SUM(speed_sample_count), SUM(decode_speed_tps_sum), SUM(decode_speed_sample_count)"
	return "WITH covered AS (" + strings.Join(sources, " UNION ALL ") + "), " +
		"expected AS (SELECT " + strings.Join(keys, ", ") + ", SUM(cost_usd) AS cost_usd, SUM(unavailable) AS unavailable_count" + speedSums + ", 1 AS physical, 0 AS side FROM covered GROUP BY " + strings.Join(keys, ", ") + "), " +
		"stored AS (SELECT " + strings.Join(statSelects, ", ") + ", SUM(cost_usd) AS cost_usd, SUM(unavailable_cost_count) AS unavailable_count" + speedSums + ", COUNT(*) AS physical, 1 AS side FROM " + table + " GROUP BY " + strings.Join(positions, ", ") + ") " +
		"SELECT * FROM expected UNION ALL SELECT * FROM stored ORDER BY " + strings.Join(positions, ", ") + ", " + strconv.Itoa(len(keys)+8)
}

var pricingOverviewDimensions = []string{
	"api_group_key", "model", "auth_index", "model_alias",
	"service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type",
}

var pricingOverviewCounts = []string{
	"request_count", "success_count", "failure_count", "input_tokens", "output_tokens",
	"reasoning_tokens", "cached_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens",
}

type pricingOverviewGroupRow struct {
	Key    []string
	Counts []int64
}

// verifyPricingOverviewCounts 在 M5 重建后按固定水位核对现存事件与每个小时/日分组的请求和 Token。
func verifyPricingOverviewCounts(ctx context.Context, db *gorm.DB, schema PricingLegacyBaseline, table string, cursor int64) error {
	if !hasPricingBaselineTable(schema, table) {
		if cursor != 0 {
			return fmt.Errorf("%s missing with nonzero cursor %d", table, cursor)
		}
		return nil
	}
	if !hasPricingBaselineColumn(schema, table, "bucket_start") || !hasPricingBaselineColumn(schema, table, "request_count") {
		return fmt.Errorf("%s lacks grouping or count columns", table)
	}
	if !hasPricingBaselineTable(schema, "usage_events") {
		return fmt.Errorf("usage_events missing for %s verification", table)
	}
	dims := []string{}
	for _, field := range pricingOverviewDimensions {
		if hasPricingBaselineColumn(schema, table, field) {
			dims = append(dims, field)
		}
	}
	counts := []string{}
	for _, field := range pricingOverviewCounts {
		if hasPricingBaselineColumn(schema, table, field) {
			counts = append(counts, field)
		}
	}
	// 重建后的汇总列必须对应现存明细字段，不能用零值掩盖结构缺失。
	sources := []string{}
	for _, source := range []string{"usage_events", "usage_events_archive"} {
		if !hasPricingBaselineTable(schema, source) {
			continue
		}
		var covered int64
		if err := db.WithContext(ctx).Table(source).Where("id <= ?", cursor).Count(&covered).Error; err != nil {
			return fmt.Errorf("count %s at cursor %d: %w", source, cursor, err)
		}
		if covered == 0 {
			continue
		}
		sources = append(sources, source)
		for _, field := range dims {
			if !hasPricingBaselineColumn(schema, source, field) {
				return fmt.Errorf("%s.%s cannot prove %s", source, field, table)
			}
		}
		for _, field := range counts {
			if field == "request_count" || field == "success_count" || field == "failure_count" {
				if field != "request_count" && !hasPricingBaselineColumn(schema, source, "failed") {
					return fmt.Errorf("%s.failed cannot prove %s", source, table)
				}
				continue
			}
			if !hasPricingBaselineColumn(schema, source, field) {
				return fmt.Errorf("%s.%s cannot prove %s", source, field, table)
			}
		}
	}
	if len(sources) == 0 {
		var rows int64
		if err := db.WithContext(ctx).Table(table).Count(&rows).Error; err != nil {
			return err
		}
		if rows != 0 {
			return fmt.Errorf("%s has %d rows without cursor-covered events", table, rows)
		}
		return nil
	}
	eventQuery := pricingOverviewEventGroupsSQL(table, dims, counts, sources)
	statQuery := pricingOverviewStatGroupsSQL(table, dims, counts)
	args := make([]any, len(sources))
	for i := range args {
		args[i] = cursor
	}
	query := "WITH event_groups AS (" + eventQuery + "), stored_groups AS (" + statQuery + "), " +
		"missing AS (SELECT * FROM event_groups EXCEPT SELECT * FROM stored_groups), " +
		"extra AS (SELECT * FROM stored_groups EXCEPT SELECT * FROM event_groups) " +
		"SELECT 'event' AS side, * FROM missing UNION ALL SELECT 'stored' AS side, * FROM extra LIMIT 1"
	rows, err := db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return fmt.Errorf("compare %s event and stored groups: %w", table, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return rows.Err()
	}
	var side string
	group := pricingOverviewGroupRow{Key: make([]string, len(dims)+1), Counts: make([]int64, len(counts)+1)}
	dest := []any{&side}
	for i := range group.Key {
		dest = append(dest, &group.Key[i])
	}
	for i := range group.Counts {
		dest = append(dest, &group.Counts[i])
	}
	if err := rows.Scan(dest...); err != nil {
		return fmt.Errorf("scan %s group difference: %w", table, err)
	}
	return fmt.Errorf("%s %s group %v differs in fields %v: %v", table, side, group.Key, counts, group.Counts)
}

func pricingOverviewBucketExpr(column, table string) string {
	daily := "0"
	if strings.Contains(table, "daily") {
		daily = "1"
	}
	return "pricing_overview_bucket(CAST(" + column + " AS TEXT), " + daily + ")"
}

func pricingOverviewDimensionExpr(field string) string {
	value := "TRIM(COALESCE(" + field + ", ''))"
	if field == "api_group_key" || field == "model" {
		return "CASE WHEN " + value + " = '' THEN 'unknown' ELSE " + value + " END"
	}
	return value
}

func pricingOverviewEventGroupsSQL(table string, dims, counts, sources []string) string {
	selects := []string{pricingOverviewBucketExpr("timestamp", table) + " AS bucket_key"}
	for _, field := range dims {
		selects = append(selects, pricingOverviewDimensionExpr(field)+" AS "+field)
	}
	selects = append(selects, "COALESCE(failed, 0) AS failed")
	for _, field := range counts {
		if field == "request_count" || field == "success_count" || field == "failure_count" {
			continue
		}
		selects = append(selects, "COALESCE("+field+", 0) AS "+field)
	}
	source := func(name string) string {
		return "SELECT " + strings.Join(selects, ", ") + " FROM " + name + " WHERE id <= ?"
	}
	parts := make([]string, 0, len(sources))
	for _, name := range sources {
		parts = append(parts, source(name))
	}
	union := strings.Join(parts, " UNION ALL ")
	keys := []string{"bucket_key"}
	keys = append(keys, dims...)
	aggregates := []string{}
	for _, field := range counts {
		switch field {
		case "request_count":
			aggregates = append(aggregates, "COUNT(*)")
		case "success_count":
			aggregates = append(aggregates, "SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END)")
		case "failure_count":
			aggregates = append(aggregates, "SUM(CASE WHEN failed <> 0 THEN 1 ELSE 0 END)")
		default:
			aggregates = append(aggregates, "SUM("+field+")")
		}
	}
	aggregates = append(aggregates, "1 AS row_count")
	return "SELECT " + strings.Join(append(keys, aggregates...), ", ") + " FROM (" + union + ") GROUP BY " + strings.Join(keys, ", ")
}

func pricingOverviewStatGroupsSQL(table string, dims, counts []string) string {
	keys := []string{pricingOverviewBucketExpr("bucket_start", table) + " AS bucket_key"}
	for _, field := range dims {
		keys = append(keys, pricingOverviewDimensionExpr(field)+" AS "+field)
	}
	aggregates := []string{}
	for _, field := range counts {
		aggregates = append(aggregates, "SUM("+field+")")
	}
	aggregates = append(aggregates, "COUNT(*)")
	positions := []string{}
	for i := range keys {
		positions = append(positions, strconv.Itoa(i+1))
	}
	return "SELECT " + strings.Join(append(keys, aggregates...), ", ") + " FROM " + table + " GROUP BY " + strings.Join(positions, ", ")
}
