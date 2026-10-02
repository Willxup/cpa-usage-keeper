package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/backup"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const pricingLegacyBaselineSchemaVersion = 1

var pricingCoverageFields = []string{
	"success_count", "failure_count", "input_tokens", "output_tokens", "reasoning_tokens",
	"cached_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens",
}

// PricingLegacyEventEvidence 是备份时热/冷明细的 ID 范围及 Overview 水位内覆盖量。
type PricingLegacyEventEvidence struct {
	Count   int64            `json:"count"`
	MinID   int64            `json:"min_id"`
	MaxID   int64            `json:"max_id"`
	Covered map[string]int64 `json:"covered"`
}

// PricingLegacyOverviewEvidence 记录旧水位和小时/日已有统计的总量；分组在 M1 从备份另行核对。
type PricingLegacyOverviewEvidence struct {
	CheckpointTable string           `json:"checkpoint_table"`
	Cursor          int64            `json:"cursor"`
	Hourly          map[string]int64 `json:"hourly"`
	Daily           map[string]int64 `json:"daily"`
}

// PricingLegacyBaseline 只保存可复算的小型元数据；逐事件原文及旧分组行留在唯一备份文件。
type PricingLegacyBaseline struct {
	SchemaVersion      int                           `json:"schema_version"`
	SchemaMigrations   []string                      `json:"schema_migrations"`
	SchemaColumns      map[string][]string           `json:"schema_columns"`
	ModelPriceSettings []map[string]any              `json:"model_price_settings"`
	ModelPriceRules    []map[string]any              `json:"model_price_rules"`
	InboxMaxID         int64                         `json:"inbox_max_id"`
	Hot                PricingLegacyEventEvidence    `json:"hot"`
	Archive            PricingLegacyEventEvidence    `json:"archive"`
	Overview           PricingLegacyOverviewEvidence `json:"overview"`
	// Fixed 在 M2 完成后保存实际 C、冷热 H 和等价旧价；M1 原证据始终保留。
	Fixed *PricingMigrationFixedBaseline `json:"fixed,omitempty"`
}

// PricingMigrationFixedBaseline 是 M3 一次固定的费用输入，恢复时不重新读取已变化的现库价格与上限。
type PricingMigrationFixedBaseline struct {
	OverviewCursor int64                        `json:"overview_cursor"`
	HotMaxID       int64                        `json:"hot_max_id"`
	ArchiveMaxID   int64                        `json:"archive_max_id"`
	Configs        []pricing.ModelPricingConfig `json:"configs"`
}

// ProtectPricingLegacyMigration 在任何旧业务 migration 之前固定一份可验证的原库备份。
// 只从备份读取原 schema、价格、明细覆盖和 inbox 边界；最后同事务提交路径与基线才允许后续阶段使用。
// 接收器仍可向 live inbox 写入，reader 由调用方提供，不占用唯一 writer 连接做长时间复制。
func ProtectPricingLegacyMigration(ctx context.Context, writer, reader *gorm.DB, backupDir string, now time.Time) (PricingLegacyBaseline, error) {
	if writer == nil || reader == nil {
		return PricingLegacyBaseline{}, fmt.Errorf("pricing protection database is missing")
	}
	var state entities.PricingMigrationState
	if err := writer.Clauses(dbresolver.Write).WithContext(ctx).Where("id = ?", 1).Take(&state).Error; err != nil {
		return PricingLegacyBaseline{}, fmt.Errorf("load pricing initialization state: %w", err)
	}
	if state.InitKind != PricingInitKindLegacy {
		return PricingLegacyBaseline{}, fmt.Errorf("pricing legacy protection requires legacy identity")
	}
	if state.BackupPath != nil || state.BaselineJSON != nil {
		if state.BackupPath == nil || state.BaselineJSON == nil {
			return PricingLegacyBaseline{}, fmt.Errorf("pricing backup and baseline are incomplete")
		}
		stored, err := decodePricingLegacyBaseline(*state.BaselineJSON)
		if err != nil {
			return PricingLegacyBaseline{}, err
		}
		backupDB, closeBackup, err := openVerifiedPricingBackup(ctx, *state.BackupPath)
		if err != nil {
			return PricingLegacyBaseline{}, err
		}
		defer closeBackup()
		current, err := collectPricingLegacyBaseline(ctx, backupDB)
		if err != nil {
			return PricingLegacyBaseline{}, err
		}
		// M3 只能追加 fixed 子对象；复验 M1 唯一备份时不把该合法扩展与原备份误比。
		originalEvidence := stored
		originalEvidence.Fixed = nil
		storedJSON, err := json.Marshal(originalEvidence)
		if err != nil {
			return PricingLegacyBaseline{}, fmt.Errorf("encode recorded pricing baseline: %w", err)
		}
		currentJSON, err := json.Marshal(current)
		if err != nil {
			return PricingLegacyBaseline{}, fmt.Errorf("encode backup pricing baseline: %w", err)
		}
		if !bytes.Equal(storedJSON, currentJSON) {
			return PricingLegacyBaseline{}, fmt.Errorf("recorded pricing baseline differs from backup")
		}
		return stored, nil
	}
	if strings.TrimSpace(backupDir) == "" {
		return PricingLegacyBaseline{}, fmt.Errorf("pricing backup directory is required")
	}
	absoluteBackupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return PricingLegacyBaseline{}, fmt.Errorf("resolve pricing backup directory: %w", err)
	}
	readSQL, err := reader.DB()
	if err != nil {
		return PricingLegacyBaseline{}, fmt.Errorf("open pricing backup reader: %w", err)
	}
	path, err := backup.NewWriter(absoluteBackupDir).WriteDatabase(ctx, readSQL, now)
	if err != nil {
		return PricingLegacyBaseline{}, fmt.Errorf("write pricing legacy backup: %w", err)
	}
	backupDB, closeBackup, err := openVerifiedPricingBackup(ctx, path)
	if err != nil {
		return PricingLegacyBaseline{}, err
	}
	defer closeBackup()
	baseline, err := collectPricingLegacyBaseline(ctx, backupDB)
	if err != nil {
		return PricingLegacyBaseline{}, err
	}
	encoded, err := json.Marshal(baseline)
	if err != nil {
		return PricingLegacyBaseline{}, fmt.Errorf("encode pricing legacy baseline: %w", err)
	}
	if err := writer.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.PricingMigrationState{}).
			Where("id = ? AND init_kind = ? AND backup_path IS NULL AND baseline_json IS NULL", 1, PricingInitKindLegacy).
			Updates(map[string]any{"backup_path": path, "baseline_json": string(encoded), "phase": "protected"})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("pricing legacy protection state changed before commit")
		}
		return nil
	}); err != nil {
		return PricingLegacyBaseline{}, fmt.Errorf("commit pricing legacy protection: %w", err)
	}
	return baseline, nil
}

// decodePricingLegacyBaseline 只接受当前持久基线版本，避免重启后把未知格式当作保护完成。
func decodePricingLegacyBaseline(value string) (PricingLegacyBaseline, error) {
	var baseline PricingLegacyBaseline
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&baseline); err != nil || baseline.SchemaVersion != pricingLegacyBaselineSchemaVersion {
		return PricingLegacyBaseline{}, fmt.Errorf("invalid recorded pricing legacy baseline")
	}
	return baseline, nil
}

// openVerifiedPricingBackup 以只读模式打开归档并执行 SQLite quick_check，失败时不授予升级许可。
func openVerifiedPricingBackup(ctx context.Context, path string) (*gorm.DB, func(), error) {
	db, closeDB, err := openPricingBackupReadOnly(path)
	if err != nil {
		return nil, nil, err
	}
	var result string
	if err := db.WithContext(ctx).Raw("PRAGMA quick_check").Scan(&result).Error; err != nil || result != "ok" {
		closeDB()
		return nil, nil, fmt.Errorf("verify pricing backup integrity: %v (%s)", err, result)
	}
	return db, closeDB, nil
}

// openPricingBackupReadOnly 供已完成 M1 quick_check 的同次升级读取原备份，不重复全文件校验。
func openPricingBackupReadOnly(path string) (*gorm.DB, func(), error) {
	dsn := helper.BuildSQLiteFileURI(path) + "?mode=ro&_query_only=on"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("open pricing backup: %w", err)
	}
	closeDB := func() { closeDatabasePool(db) }
	return db, closeDB, nil
}

// collectPricingLegacyBaseline 从已验证备份收集小型元数据，并在破坏性旧迁移前核对原分组。
// 逐事件与旧桶行只留在备份文件，不复制到控制行。
func collectPricingLegacyBaseline(ctx context.Context, db *gorm.DB) (PricingLegacyBaseline, error) {
	baseline := PricingLegacyBaseline{SchemaVersion: pricingLegacyBaselineSchemaVersion, SchemaMigrations: []string{}, SchemaColumns: map[string][]string{}, ModelPriceSettings: []map[string]any{}, ModelPriceRules: []map[string]any{}}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return baseline, fmt.Errorf("list pricing backup tables: %w", err)
	}
	sort.Strings(tables)
	for _, table := range tables {
		if strings.HasPrefix(table, "sqlite_") {
			continue
		}
		columnTypes, err := db.Migrator().ColumnTypes(table)
		if err != nil {
			return baseline, fmt.Errorf("inspect pricing backup table %s: %w", table, err)
		}
		columns := make([]string, 0, len(columnTypes))
		for _, column := range columnTypes {
			columns = append(columns, column.Name())
		}
		sort.Strings(columns)
		baseline.SchemaColumns[table] = columns
	}
	if hasPricingBaselineTable(baseline, "schema_migrations") {
		if !hasPricingBaselineColumn(baseline, "schema_migrations", "version") {
			return baseline, fmt.Errorf("schema_migrations.version is missing")
		}
		if err := db.WithContext(ctx).Table("schema_migrations").Order("version").Pluck("version", &baseline.SchemaMigrations).Error; err != nil {
			return baseline, err
		}
	}
	for _, target := range []struct {
		table string
		rows  *[]map[string]any
	}{{"model_price_settings", &baseline.ModelPriceSettings}, {"model_price_rules", &baseline.ModelPriceRules}} {
		if !hasPricingBaselineTable(baseline, target.table) {
			continue
		}
		if !hasPricingBaselineColumn(baseline, target.table, "id") {
			return baseline, fmt.Errorf("%s.id is missing", target.table)
		}
		if err := db.WithContext(ctx).Table(target.table).Order("id").Find(target.rows).Error; err != nil {
			return baseline, fmt.Errorf("read original %s: %w", target.table, err)
		}
	}
	if !hasPricingBaselineTable(baseline, "redis_usage_inboxes") || !hasPricingBaselineColumn(baseline, "redis_usage_inboxes", "id") {
		return baseline, fmt.Errorf("pricing backup lacks durable inbox")
	}
	if err := db.WithContext(ctx).Table("redis_usage_inboxes").Select("COALESCE(MAX(id), 0)").Scan(&baseline.InboxMaxID).Error; err != nil {
		return baseline, fmt.Errorf("read backup inbox boundary: %w", err)
	}
	cursor, checkpointTable, err := pricingLegacyOverviewCursor(ctx, db, baseline)
	if err != nil {
		return baseline, err
	}
	baseline.Overview.Cursor, baseline.Overview.CheckpointTable = cursor, checkpointTable
	baseline.Hot, err = pricingLegacyEventEvidence(ctx, db, baseline, "usage_events", cursor)
	if err != nil {
		return baseline, err
	}
	baseline.Archive, err = pricingLegacyEventEvidence(ctx, db, baseline, "usage_events_archive", cursor)
	if err != nil {
		return baseline, err
	}
	if baseline.Hot.Count > 0 && baseline.Archive.Count > 0 {
		var overlap int64
		if err := db.WithContext(ctx).Raw("SELECT EXISTS(SELECT 1 FROM usage_events h JOIN usage_events_archive a ON h.id = a.id LIMIT 1)").Scan(&overlap).Error; err != nil {
			return baseline, fmt.Errorf("check hot/archive ID overlap: %w", err)
		}
		if overlap > 0 {
			return baseline, fmt.Errorf("hot/archive event IDs overlap")
		}
	}
	baseline.Overview.Hourly, err = pricingLegacyStatEvidence(ctx, db, baseline, "usage_overview_hourly_stats")
	if err != nil {
		return baseline, err
	}
	baseline.Overview.Daily, err = pricingLegacyStatEvidence(ctx, db, baseline, "usage_overview_daily_stats")
	if err != nil {
		return baseline, err
	}
	if err := verifyPricingLegacyCoverage(baseline); err != nil {
		return baseline, err
	}
	// 总量相同仍可能把请求移到错误小时/自然日；旧五维迁移清表前必须确认原桶可由明细解释。
	for _, period := range []string{"hourly", "daily"} {
		table := "usage_overview_" + period + "_stats"
		if err := verifyPricingM2Rollup(ctx, db, baseline, table, baseline.Overview.Cursor); err != nil {
			return baseline, fmt.Errorf("verify original %s before migration: %w", table, err)
		}
	}
	return baseline, nil
}

// hasPricingBaselineTable 使用备份内实际存在的表，不能根据当前实体猜旧结构。
func hasPricingBaselineTable(b PricingLegacyBaseline, table string) bool {
	_, ok := b.SchemaColumns[table]
	return ok
}

// hasPricingBaselineColumn 使用备份物理列判断旧版可读字段。
func hasPricingBaselineColumn(b PricingLegacyBaseline, table, column string) bool {
	for _, name := range b.SchemaColumns[table] {
		if name == column {
			return true
		}
	}
	return false
}

// pricingLegacyOverviewCursor 兼容已发布的单独/共享水位表，不用未来实体查询旧 schema。
func pricingLegacyOverviewCursor(ctx context.Context, db *gorm.DB, b PricingLegacyBaseline) (int64, string, error) {
	var found bool
	var cursor int64
	var source string
	for _, table := range []string{"usage_aggregation_checkpoints", "usage_overview_aggregation_checkpoints"} {
		if !hasPricingBaselineTable(b, table) {
			continue
		}
		if !hasPricingBaselineColumn(b, table, "name") || !hasPricingBaselineColumn(b, table, "last_aggregated_usage_event_id") {
			return 0, "", fmt.Errorf("%s lacks overview cursor columns", table)
		}
		var row struct {
			Cursor int64 `gorm:"column:cursor"`
		}
		result := db.WithContext(ctx).Table(table).Select("last_aggregated_usage_event_id AS cursor").Where("name = ?", "overview").Limit(1).Scan(&row)
		if result.Error != nil {
			return 0, "", fmt.Errorf("read %s overview cursor: %w", table, result.Error)
		}
		if result.RowsAffected > 0 {
			if found && cursor != row.Cursor {
				return 0, "", fmt.Errorf("overview checkpoint tables disagree")
			}
			if !found {
				found, cursor, source = true, row.Cursor, table
			}
		}
	}
	return cursor, source, nil
}

// pricingLegacyEventEvidence 统计水位内真实明细总量；ID 空洞不被误判为明细丢失。
func pricingLegacyEventEvidence(ctx context.Context, db *gorm.DB, b PricingLegacyBaseline, table string, cursor int64) (PricingLegacyEventEvidence, error) {
	evidence := PricingLegacyEventEvidence{Covered: map[string]int64{}}
	if !hasPricingBaselineTable(b, table) {
		return evidence, nil
	}
	if !hasPricingBaselineColumn(b, table, "id") {
		return evidence, fmt.Errorf("%s.id is missing", table)
	}
	var bounds struct{ Count, MinID, MaxID int64 }
	if err := db.WithContext(ctx).Table(table).Select("COUNT(*) AS count, COALESCE(MIN(id), 0) AS min_id, COALESCE(MAX(id), 0) AS max_id").Scan(&bounds).Error; err != nil {
		return evidence, fmt.Errorf("read %s range: %w", table, err)
	}
	evidence.Count, evidence.MinID, evidence.MaxID = bounds.Count, bounds.MinID, bounds.MaxID
	selects := []string{"COUNT(*) AS request_count"}
	for _, field := range pricingCoverageFields {
		switch field {
		case "success_count", "failure_count":
			if hasPricingBaselineColumn(b, table, "failed") {
				if field == "success_count" {
					selects = append(selects, "COALESCE(SUM(CASE WHEN COALESCE(failed, 0) = 0 THEN 1 ELSE 0 END), 0) AS success_count")
				} else {
					selects = append(selects, "COALESCE(SUM(CASE WHEN failed <> 0 THEN 1 ELSE 0 END), 0) AS failure_count")
				}
			}
		default:
			if hasPricingBaselineColumn(b, table, field) {
				selects = append(selects, "COALESCE(SUM("+field+"), 0) AS "+field)
			}
		}
	}
	covered, err := pricingLegacyIntegerTotals(ctx, db, table, selects, &cursor)
	if err != nil {
		return evidence, fmt.Errorf("decode %s overview coverage: %w", table, err)
	}
	evidence.Covered = covered
	return evidence, nil
}

// pricingLegacyStatEvidence 读取旧小时/日既有统计，不用当前费用列或实体零值伪造结果。
func pricingLegacyStatEvidence(ctx context.Context, db *gorm.DB, b PricingLegacyBaseline, table string) (map[string]int64, error) {
	evidence := map[string]int64{"row_count": 0, "request_count": 0}
	if !hasPricingBaselineTable(b, table) {
		return evidence, nil
	}
	if !hasPricingBaselineColumn(b, table, "request_count") {
		return nil, fmt.Errorf("%s.request_count is missing", table)
	}
	selects := []string{"COUNT(*) AS row_count", "COALESCE(SUM(request_count), 0) AS request_count"}
	for _, field := range pricingCoverageFields {
		if hasPricingBaselineColumn(b, table, field) {
			selects = append(selects, "COALESCE(SUM("+field+"), 0) AS "+field)
		}
	}
	return pricingLegacyIntegerTotals(ctx, db, table, selects, nil)
}

// pricingLegacyIntegerTotals 在一次 SQL 聚合中将每列直接扫描到 int64，不经 float64 转换计数。
func pricingLegacyIntegerTotals(ctx context.Context, db *gorm.DB, table string, selects []string, cursor *int64) (map[string]int64, error) {
	query := db.WithContext(ctx).Table(table).Select(strings.Join(selects, ", "))
	if cursor != nil {
		query = query.Where("id <= ?", *cursor)
	}
	rows, err := query.Rows()
	if err != nil {
		return nil, fmt.Errorf("read %s integer totals: %w", table, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read %s total columns: %w", table, err)
	}
	if !rows.Next() {
		return nil, fmt.Errorf("%s integer totals returned no row", table)
	}
	values := make([]int64, len(columns))
	dest := make([]any, len(columns))
	for index := range dest {
		dest[index] = &values[index]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, fmt.Errorf("scan %s integer totals: %w", table, err)
	}
	result := make(map[string]int64, len(columns))
	for index, name := range columns {
		result[name] = values[index]
	}
	return result, rows.Err()
}

// verifyPricingLegacyCoverage 比较旧水位已覆盖的计数/Token；旧维度分组由调用方随后核对。
func verifyPricingLegacyCoverage(b PricingLegacyBaseline) error {
	if b.Overview.Cursor < 0 {
		return fmt.Errorf("negative original overview cursor")
	}
	covered := map[string]int64{}
	for field, amount := range b.Hot.Covered {
		covered[field] += amount
	}
	for field, amount := range b.Archive.Covered {
		covered[field] += amount
	}
	for _, stats := range []map[string]int64{b.Overview.Hourly, b.Overview.Daily} {
		if stats["row_count"] > 0 {
			if _, ok := stats["total_tokens"]; !ok {
				return fmt.Errorf("overview stats lack total_tokens coverage")
			}
		}
		if stats["request_count"] != covered["request_count"] {
			return fmt.Errorf("overview request count lacks matching event detail")
		}
		for _, field := range pricingCoverageFields {
			if value, ok := stats[field]; ok {
				missingColumn := field
				if field == "success_count" || field == "failure_count" {
					missingColumn = "failed"
				}
				if _, available := b.Hot.Covered[field]; b.Hot.Covered["request_count"] > 0 && !available {
					return fmt.Errorf("usage_events.%s is missing for overview coverage", missingColumn)
				}
				if _, available := b.Archive.Covered[field]; b.Archive.Covered["request_count"] > 0 && !available {
					return fmt.Errorf("usage_events_archive.%s is missing for overview coverage", missingColumn)
				}
				if value != covered[field] {
					return fmt.Errorf("overview %s lacks matching event detail", field)
				}
			}
		}
	}
	if b.Overview.CheckpointTable == "" && (b.Overview.Hourly["row_count"] > 0 || b.Overview.Daily["row_count"] > 0) {
		return fmt.Errorf("overview stats exist without a checkpoint")
	}
	if b.Overview.Cursor > 0 && (b.Overview.Hourly["row_count"] == 0 || b.Overview.Daily["row_count"] == 0) {
		return fmt.Errorf("overview checkpoint has no complete hourly/daily stats")
	}
	return nil
}
