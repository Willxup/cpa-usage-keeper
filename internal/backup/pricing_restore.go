package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cpa-usage-keeper/internal/helper"
)

// PricingRestoreResult 是离线恢复后可对账的 inbox 边界与补回条数。
type PricingRestoreResult struct {
	BackupInboxMaxID int64
	FaultInboxMaxID  int64
	ReplayedRows     int64
}

type pricingRestoreBaseline struct {
	SchemaVersion int   `json:"schema_version"`
	InboxMaxID    int64 `json:"inbox_max_id"`
}

// RestorePricingBackup 从 M1 唯一备份生成新数据库，并按故障库 inbox 行 ID 补回之后收到的消息。
// 调用前必须停稳所有数据库写入；故障库和备份均以只读方式打开，结果只在全部检查通过后发布到不存在的 outputPath。
func RestorePricingBackup(ctx context.Context, backupPath, faultPath, outputPath string) (PricingRestoreResult, error) {
	var result PricingRestoreResult
	backupPath, faultPath, outputPath, err := pricingRestorePaths(backupPath, faultPath, outputPath)
	if err != nil {
		return result, err
	}
	backupDB, err := openPricingRestoreReadOnly(backupPath)
	if err != nil {
		return result, fmt.Errorf("open M1 backup: %w", err)
	}
	defer backupDB.Close()
	faultDB, err := openPricingRestoreReadOnly(faultPath)
	if err != nil {
		return result, fmt.Errorf("open fault database: %w", err)
	}
	defer faultDB.Close()
	if err := verifyPricingRestoreDatabase(ctx, backupDB, "M1 backup"); err != nil {
		return result, err
	}
	if err := verifyPricingRestoreDatabase(ctx, faultDB, "fault database"); err != nil {
		return result, err
	}
	backupColumns, err := pricingRestoreInboxColumns(ctx, backupDB)
	if err != nil {
		return result, fmt.Errorf("inspect M1 backup inbox: %w", err)
	}
	faultColumns, err := pricingRestoreInboxColumns(ctx, faultDB)
	if err != nil {
		return result, fmt.Errorf("inspect fault inbox: %w", err)
	}
	if err := validatePricingRestoreInboxColumns(backupColumns); err != nil {
		return result, fmt.Errorf("M1 backup inbox: %w", err)
	}
	if err := validatePricingRestoreInboxColumns(faultColumns); err != nil {
		return result, fmt.Errorf("fault inbox: %w", err)
	}
	if err := backupDB.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM redis_usage_inboxes").Scan(&result.BackupInboxMaxID); err != nil {
		return result, fmt.Errorf("read M1 backup inbox boundary: %w", err)
	}
	if err := verifyPricingRestoreM1State(ctx, faultDB, backupPath, result.BackupInboxMaxID); err != nil {
		return result, err
	}

	// 先在输出目录创建私有临时库；任何失败都只清理本次临时文件，绝不覆盖备份、故障库或已有结果。
	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".pricing-restore-*.db")
	if err != nil {
		return result, fmt.Errorf("create restore staging database: %w", err)
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return result, fmt.Errorf("close restore staging database: %w", err)
	}
	defer func() {
		_ = os.Remove(tempPath)
		_ = os.Remove(tempPath + "-journal")
		_ = os.Remove(tempPath + "-wal")
		_ = os.Remove(tempPath + "-shm")
	}()
	if err := copySQLiteDatabase(ctx, backupDB, tempPath); err != nil {
		return result, fmt.Errorf("copy M1 backup: %w", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return result, fmt.Errorf("restrict restore database permissions: %w", err)
	}
	if err := replayPricingRestoreInbox(ctx, tempPath, faultPath, backupColumns, faultColumns, &result); err != nil {
		return result, err
	}
	stagedDB, err := openPricingRestoreReadOnly(tempPath)
	if err != nil {
		return result, fmt.Errorf("open staged restore: %w", err)
	}
	checkErr := verifyPricingRestoreDatabase(ctx, stagedDB, "staged restore")
	closeErr := stagedDB.Close()
	if err := errors.Join(checkErr, closeErr); err != nil {
		return result, err
	}
	// 只发布已经完整收拢到主文件的 SQLite 数据；遗漏 WAL 会让补回消息在结果文件中消失。
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(tempPath + suffix); err == nil {
			return result, fmt.Errorf("staged restore has uncheckpointed SQLite sidecar %s", suffix)
		} else if !os.IsNotExist(err) {
			return result, fmt.Errorf("check staged SQLite sidecar %s: %w", suffix, err)
		}
	}
	// 同目录硬链接提供原子且不可覆盖的发布；再次运行会因 outputPath 已存在而失败，不会重复补回。
	if err := os.Link(tempPath, outputPath); err != nil {
		return result, fmt.Errorf("publish new restore database (output must not exist): %w", err)
	}
	return result, nil
}

// pricingRestorePaths 只接受真实的输入文件与完全空闲的输出文件名，包括 SQLite sidecar 名称。
func pricingRestorePaths(backupPath, faultPath, outputPath string) (string, string, string, error) {
	paths := []*string{&backupPath, &faultPath, &outputPath}
	for _, path := range paths {
		if strings.TrimSpace(*path) == "" {
			return "", "", "", fmt.Errorf("backup, fault and output paths are required")
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return "", "", "", fmt.Errorf("resolve database path: %w", err)
		}
		*path = absolute
	}
	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return "", "", "", fmt.Errorf("stat M1 backup: %w", err)
	}
	if !backupInfo.Mode().IsRegular() {
		return "", "", "", fmt.Errorf("M1 backup must be a regular file")
	}
	faultInfo, err := os.Stat(faultPath)
	if err != nil {
		return "", "", "", fmt.Errorf("stat fault database: %w", err)
	}
	if !faultInfo.Mode().IsRegular() {
		return "", "", "", fmt.Errorf("fault database must be a regular file")
	}
	if os.SameFile(backupInfo, faultInfo) {
		return "", "", "", fmt.Errorf("M1 backup and fault database must be separate files")
	}
	for _, path := range []string{outputPath, outputPath + "-wal", outputPath + "-shm", outputPath + "-journal"} {
		if _, err := os.Lstat(path); err == nil {
			return "", "", "", fmt.Errorf("restore output path already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return "", "", "", fmt.Errorf("check restore output path %s: %w", path, err)
		}
	}
	return backupPath, faultPath, outputPath, nil
}

// openPricingRestoreReadOnly 不允许 SQLite 自动创建缺失的备份或故障文件。
func openPricingRestoreReadOnly(path string) (*sql.DB, error) {
	uri := helper.BuildSQLiteFileURI(path) + "?mode=ro&_query_only=on"
	db, err := sql.Open("sqlite3", uri)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// verifyPricingRestoreDatabase 在读取或发布前执行 SQLite 文件完整性检查。
func verifyPricingRestoreDatabase(ctx context.Context, db *sql.DB, label string) error {
	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil {
		return fmt.Errorf("check %s integrity: %w", label, err)
	}
	if check != "ok" {
		return fmt.Errorf("%s failed SQLite quick_check: %s", label, check)
	}
	return nil
}

// verifyPricingRestoreM1State 核对故障库记录的唯一 M1 文件身份及备份本身的 inbox ID 边界。
func verifyPricingRestoreM1State(ctx context.Context, faultDB *sql.DB, backupPath string, maxID int64) error {
	var kind string
	var recordedPath, baselineJSON sql.NullString
	if err := faultDB.QueryRowContext(ctx, "SELECT init_kind, backup_path, baseline_json FROM pricing_migration_state WHERE id = 1").Scan(&kind, &recordedPath, &baselineJSON); err != nil {
		return fmt.Errorf("read fault M1 protection state: %w", err)
	}
	if kind != "legacy" || !recordedPath.Valid || !baselineJSON.Valid {
		return fmt.Errorf("fault database has no complete legacy M1 protection state")
	}
	recordedInfo, err := os.Stat(recordedPath.String)
	if err != nil {
		return fmt.Errorf("read recorded M1 backup path: %w", err)
	}
	actualInfo, err := os.Stat(backupPath)
	if err != nil {
		return fmt.Errorf("stat selected M1 backup: %w", err)
	}
	if !os.SameFile(recordedInfo, actualInfo) {
		return fmt.Errorf("selected M1 backup differs from fault protection state")
	}
	var baseline pricingRestoreBaseline
	if err := json.Unmarshal([]byte(baselineJSON.String), &baseline); err != nil {
		return fmt.Errorf("decode fault M1 baseline: %w", err)
	}
	if baseline.SchemaVersion != 1 || baseline.InboxMaxID < 0 || baseline.InboxMaxID != maxID {
		return fmt.Errorf("recorded M1 inbox boundary differs from verified backup")
	}
	return nil
}

// pricingRestoreInboxColumns 只读实际表列，供后续选择旧 queue_key 或新 source 投影。
func pricingRestoreInboxColumns(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(redis_usage_inboxes)")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var index, notNull, primaryKey int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&index, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

// validatePricingRestoreInboxColumns 拒绝无法保留原始行与重置处理状态的未知 schema。
func validatePricingRestoreInboxColumns(columns map[string]bool) error {
	for _, name := range []string{"id", "message_hash", "raw_message", "status", "attempt_count", "last_error", "usage_event_key", "popped_at", "processed_at", "created_at", "updated_at"} {
		if !columns[name] {
			return fmt.Errorf("required column %s is missing", name)
		}
	}
	if !columns["source"] && !columns["queue_key"] {
		return fmt.Errorf("source and queue_key columns are both missing")
	}
	return nil
}

// replayPricingRestoreInbox 在新库的一次事务内按原 ID 插入固定边界之后的每一行，不按内容去重。
func replayPricingRestoreInbox(ctx context.Context, stagedPath, faultPath string, target, source map[string]bool, result *PricingRestoreResult) error {
	db, err := sql.Open("sqlite3", stagedPath)
	if err != nil {
		return fmt.Errorf("open restore staging database: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect restore staging database: %w", err)
	}
	defer conn.Close()
	faultURI := helper.BuildSQLiteFileURI(faultPath) + "?mode=ro"
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS fault", faultURI); err != nil {
		return fmt.Errorf("attach read-only fault database: %w", err)
	}
	attached := true
	defer func() {
		if attached {
			_, _ = conn.ExecContext(context.Background(), "DETACH DATABASE fault")
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin restore replay: %w", err)
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM fault.redis_usage_inboxes").Scan(&result.FaultInboxMaxID); err != nil {
		return fmt.Errorf("read fault inbox upper boundary: %w", err)
	}
	var expected int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM fault.redis_usage_inboxes WHERE id > ? AND id <= ?", result.BackupInboxMaxID, result.FaultInboxMaxID).Scan(&expected); err != nil {
		return fmt.Errorf("count postbackup inbox rows: %w", err)
	}
	columns := []string{"id", "message_hash", "raw_message", "status", "attempt_count", "last_error", "usage_event_key", "popped_at", "processed_at", "created_at", "updated_at"}
	values := []string{"id", "message_hash", "raw_message", "'pending'", "0", "NULL", "NULL", "popped_at", "NULL", "created_at", "updated_at"}
	if target["source"] {
		columns = append(columns, "source")
		if source["source"] {
			values = append(values, "source")
		} else {
			// 旧 queue_key 无法还原完整来源；沿用历史列迁移的 unknown 值。
			values = append(values, "'unknown'")
		}
	}
	if target["queue_key"] {
		columns = append(columns, "queue_key")
		if source["queue_key"] {
			values = append(values, "queue_key")
		} else {
			// 旧版消费只按状态和 ID 取行；queue_key 是诊断字段，不能从 source 猜回真实队列名。
			values = append(values, "'queue'")
		}
	}
	statement := fmt.Sprintf("INSERT INTO redis_usage_inboxes (%s) SELECT %s FROM fault.redis_usage_inboxes WHERE id > ? AND id <= ? ORDER BY id", strings.Join(columns, ", "), strings.Join(values, ", "))
	inserted, err := tx.ExecContext(ctx, statement, result.BackupInboxMaxID, result.FaultInboxMaxID)
	if err != nil {
		return fmt.Errorf("replay postbackup inbox rows: %w", err)
	}
	result.ReplayedRows, err = inserted.RowsAffected()
	if err != nil {
		return fmt.Errorf("count inserted postbackup inbox rows: %w", err)
	}
	if result.ReplayedRows != expected {
		return fmt.Errorf("postbackup inbox replay count differs: inserted=%d expected=%d", result.ReplayedRows, expected)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postbackup inbox replay: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "DETACH DATABASE fault"); err != nil {
		return fmt.Errorf("detach fault database after replay: %w", err)
	}
	attached = false
	// M1 原库可能启用了 WAL；发布单文件结果前切回 DELETE，使提交的补回行完整落入主文件。
	var journalMode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode=DELETE").Scan(&journalMode); err != nil {
		return fmt.Errorf("checkpoint staged restore: %w", err)
	}
	if journalMode != "delete" {
		return fmt.Errorf("staged restore journal mode is %s, expected delete", journalMode)
	}
	return nil
}
