package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// PricingLegacyEventEvidence 是备份时仍存在的热/冷明细数量与 ID 范围。
type PricingLegacyEventEvidence struct {
	Count int64 `json:"count"`
	MinID int64 `json:"min_id"`
	MaxID int64 `json:"max_id"`
}

// PricingLegacyOverviewEvidence 记录原聚合水位；旧统计保存在备份内，M5 按现存明细重建。
type PricingLegacyOverviewEvidence struct {
	CheckpointTable string `json:"checkpoint_table"`
	Cursor          int64  `json:"cursor"`
}

// PricingLegacyBaseline 只保存可复算的小型元数据；逐事件原文及旧分组行留在唯一备份文件。
type PricingLegacyBaseline struct {
	SchemaVersion      int                           `json:"schema_version"`
	BackupSHA256       string                        `json:"backup_sha256,omitempty"`
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
// 只从备份读取原 schema、价格、现存明细范围和 inbox 边界；最后同事务提交路径与基线才允许后续阶段使用。
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
		path, err := resolvePricingLegacyBackup(*state.BackupPath, backupDir, stored.BackupSHA256)
		if err != nil {
			return PricingLegacyBaseline{}, err
		}
		fingerprint, err := backup.PricingBackupSHA256(ctx, path)
		if err != nil {
			return PricingLegacyBaseline{}, err
		}
		if stored.BackupSHA256 != "" && stored.BackupSHA256 != fingerprint {
			return PricingLegacyBaseline{}, fmt.Errorf("recorded M1 backup SHA-256 differs from backup")
		}
		backupDB, closeBackup, err := openVerifiedPricingBackup(ctx, path)
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
		originalEvidence.BackupSHA256 = ""
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
		if stored.BackupSHA256 == "" || path != *state.BackupPath {
			// 旧状态只在原文件复验后补指纹；搬迁只接受已知指纹的精确候选，验完再 CAS 改路径。
			stored.BackupSHA256 = fingerprint
			encoded, err := json.Marshal(stored)
			if err != nil {
				return PricingLegacyBaseline{}, fmt.Errorf("encode verified M1 identity: %w", err)
			}
			result := writer.Clauses(dbresolver.Write).WithContext(ctx).Model(&entities.PricingMigrationState{}).
				Where("id = ? AND backup_path = ? AND baseline_json = ?", 1, *state.BackupPath, *state.BaselineJSON).
				Updates(map[string]any{"backup_path": path, "baseline_json": string(encoded)})
			if result.Error != nil {
				return PricingLegacyBaseline{}, fmt.Errorf("persist verified M1 identity: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return PricingLegacyBaseline{}, fmt.Errorf("M1 protection state changed before identity update")
			}
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
	baseline.BackupSHA256, err = backup.PricingBackupSHA256(ctx, path)
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

// resolvePricingLegacyBackup 只允许已持久指纹且原路径缺失时，在配置目录按原日期/文件名定点定位。
// 权限错误或原文件存在时不搜索替代文件；无指纹的旧状态不能据弱元数据认领搬迁后的库。
func resolvePricingLegacyBackup(recordedPath, backupDir, fingerprint string) (string, error) {
	_, err := os.Stat(recordedPath)
	if err == nil {
		return recordedPath, nil
	}
	if !os.IsNotExist(err) || fingerprint == "" || strings.TrimSpace(backupDir) == "" {
		return "", fmt.Errorf("original M1 backup is unavailable; automatic relocation requires a recorded SHA-256 and the original date/file name under the configured backup directory: %w", err)
	}
	day := filepath.Base(filepath.Dir(recordedPath))
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return "", fmt.Errorf("recorded M1 backup has no valid date directory")
	}
	path, err := filepath.Abs(filepath.Join(backupDir, day, filepath.Base(recordedPath)))
	if err != nil {
		return "", fmt.Errorf("resolve relocated M1 backup: %w", err)
	}
	return path, nil
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

// collectPricingLegacyBaseline 从已验证备份收集现存明细与配置的边界，不要求旧聚合能由保留明细解释。
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
	baseline.Hot, err = pricingLegacyEventEvidence(ctx, db, baseline, "usage_events")
	if err != nil {
		return baseline, err
	}
	baseline.Archive, err = pricingLegacyEventEvidence(ctx, db, baseline, "usage_events_archive")
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
	if cursor < 0 {
		return 0, "", fmt.Errorf("negative original overview cursor")
	}
	return cursor, source, nil
}

// pricingLegacyEventEvidence 统计备份内现存明细范围；不依据 ID 空洞或旧聚合推断丢失。
func pricingLegacyEventEvidence(ctx context.Context, db *gorm.DB, b PricingLegacyBaseline, table string) (PricingLegacyEventEvidence, error) {
	evidence := PricingLegacyEventEvidence{}
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
	return evidence, nil
}
