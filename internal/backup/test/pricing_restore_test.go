package test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/backup"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	_ "github.com/mattn/go-sqlite3"
)

const pricingRestoreInboxDDL = `CREATE TABLE redis_usage_inboxes (
 id INTEGER PRIMARY KEY AUTOINCREMENT, %s TEXT NOT NULL,
 message_hash TEXT NOT NULL, raw_message TEXT NOT NULL, status TEXT NOT NULL,
 attempt_count INTEGER NOT NULL DEFAULT 0, last_error TEXT, usage_event_key TEXT,
 popped_at DATETIME NOT NULL, processed_at DATETIME, created_at DATETIME, updated_at DATETIME
)`

type restoreFixture struct {
	backupPath string
	faultPath  string
	fault      *sql.DB
}

func makeRestoreFixture(t *testing.T, oldBackup bool) restoreFixture {
	t.Helper()
	// 文件名中的空格与 # 必须在读取备份和 ATTACH 故障库时保持原义。
	dir := filepath.Join(t.TempDir(), "restore # files")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	faultPath := filepath.Join(dir, "fault.db")
	fault := openRestoreTestDB(t, faultPath)
	column := "source"
	if oldBackup {
		column = "queue_key"
	}
	mustRestoreExec(t, fault, fmt.Sprintf(pricingRestoreInboxDDL, column))
	mustRestoreExec(t, fault, `CREATE TABLE pricing_migration_state (
 id INTEGER PRIMARY KEY, init_kind TEXT NOT NULL, backup_path TEXT, baseline_json TEXT
)`)
	mustRestoreExec(t, fault, "INSERT INTO pricing_migration_state(id,init_kind) VALUES(1,'legacy')")
	insertRestoreInbox(t, fault, column, 1, "before-one", "pending", "2026-09-22 10:00:00.123456789", "2026-09-22 10:00:01", "2026-09-22 10:00:02")
	insertRestoreInbox(t, fault, column, 5, "before-five", "pending", "2026-09-22 10:01:00", "2026-09-22 10:01:01", "2026-09-22 10:01:02")
	backupPath, err := backup.NewWriter(filepath.Join(dir, "backups")).WriteDatabase(context.Background(), fault, time.Date(2026, 9, 22, 10, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := json.Marshal(map[string]any{"schema_version": 1, "inbox_max_id": 5})
	if err != nil {
		t.Fatal(err)
	}
	mustRestoreExec(t, fault, "UPDATE pricing_migration_state SET backup_path=?, baseline_json=? WHERE id=1", backupPath, string(baseline))
	return restoreFixture{backupPath: backupPath, faultPath: faultPath, fault: fault}
}

func openRestoreTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustRestoreExec(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatal(err)
	}
}

func insertRestoreInbox(t *testing.T, db *sql.DB, sourceColumn string, id int64, raw, status, popped, created, updated string) {
	t.Helper()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	statement := fmt.Sprintf(`INSERT INTO redis_usage_inboxes
 (id,%s,message_hash,raw_message,status,attempt_count,last_error,usage_event_key,popped_at,processed_at,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, sourceColumn)
	source := "redis_pull:usage"
	if sourceColumn == "queue_key" {
		source = "queue"
	}
	var processed any
	var eventKey any
	attempts := 0
	if status == "processed" {
		processed, eventKey, attempts = "2026-09-22 11:00:00", "event-after-backup", 1
	}
	mustRestoreExec(t, db, statement, id, source, hash, raw, status, attempts, "prior error", eventKey, popped, processed, created, updated)
}

func TestRestorePricingBackupReplaysOriginalRowsIntoOldInbox(t *testing.T) {
	fixture := makeRestoreFixture(t, true)
	// 故障库已经切到新列。备份复制完成后收到的第一条消息 ID=6；不能使用故障库当前 MAX=9 当边界。
	mustRestoreExec(t, fixture.fault, "ALTER TABLE redis_usage_inboxes DROP COLUMN queue_key")
	mustRestoreExec(t, fixture.fault, "ALTER TABLE redis_usage_inboxes ADD COLUMN source TEXT NOT NULL DEFAULT 'unknown'")
	insertRestoreInbox(t, fixture.fault, "source", 6, "same-content", "pending", "2026-09-22 10:02:00.111", "2026-09-22 10:02:01", "2026-09-22 10:02:02")
	insertRestoreInbox(t, fixture.fault, "source", 7, "same-content", "processed", "2026-09-22 10:03:00.222", "2026-09-22 10:03:01", "2026-09-22 10:03:02")
	insertRestoreInbox(t, fixture.fault, "source", 9, "last-content", "process_failed", "2026-09-22 10:04:00.333", "2026-09-22 10:04:01", "2026-09-22 10:04:02")
	output := filepath.Join(filepath.Dir(fixture.faultPath), "restored.db")
	result, err := backup.RestorePricingBackup(context.Background(), fixture.backupPath, fixture.faultPath, output)
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupInboxMaxID != 5 || result.FaultInboxMaxID != 9 || result.ReplayedRows != 3 {
		t.Fatalf("restore result = %+v", result)
	}
	restored := openRestoreTestDB(t, output)
	rows, err := restored.Query(`SELECT id, queue_key, message_hash, raw_message, status, attempt_count,
 last_error, usage_event_key, CAST(popped_at AS TEXT), processed_at, CAST(created_at AS TEXT), CAST(updated_at AS TEXT)
 FROM redis_usage_inboxes ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var queue, hash, raw, status, popped string
		var attempt int
		var lastError, eventKey, processed, created, updated sql.NullString
		if err := rows.Scan(&id, &queue, &hash, &raw, &status, &attempt, &lastError, &eventKey, &popped, &processed, &created, &updated); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if id <= 5 {
			continue
		}
		if queue != "queue" || status != "pending" || attempt != 0 || lastError.Valid || eventKey.Valid || processed.Valid {
			t.Fatalf("row %d was not reset for replay: queue=%q status=%q attempt=%d error=%v event=%v processed=%v", id, queue, status, attempt, lastError, eventKey, processed)
		}
		expectedRaw := "same-content"
		if id == 9 {
			expectedRaw = "last-content"
		}
		if raw != expectedRaw || hash != fmt.Sprintf("%x", sha256.Sum256([]byte(expectedRaw))) {
			t.Fatalf("row %d lost original content/hash", id)
		}
		expectedPopped := map[int64]string{
			6: "2026-09-22 10:02:00.111",
			7: "2026-09-22 10:03:00.222",
			9: "2026-09-22 10:04:00.333",
		}[id]
		if popped != expectedPopped {
			t.Fatalf("row %d popped_at changed: %q", id, popped)
		}
		expectedCreated := map[int64]string{6: "2026-09-22 10:02:01", 7: "2026-09-22 10:03:01", 9: "2026-09-22 10:04:01"}[id]
		expectedUpdated := map[int64]string{6: "2026-09-22 10:02:02", 7: "2026-09-22 10:03:02", 9: "2026-09-22 10:04:02"}[id]
		if !created.Valid || !updated.Valid || created.String != expectedCreated || updated.String != expectedUpdated {
			t.Fatalf("row %d receipt timestamps changed: created=%v updated=%v", id, created, updated)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[1 5 6 7 9]" {
		t.Fatalf("restored IDs = %v", ids)
	}
	// 旧列输出仍把同原文的各条原始消息保留为独立待处理行。
	var processable int
	if err := restored.QueryRow("SELECT COUNT(*) FROM redis_usage_inboxes WHERE id > 5 AND status = 'pending'").Scan(&processable); err != nil || processable != 3 {
		t.Fatalf("processable restored rows = %d, %v", processable, err)
	}
	var faultStatus string
	if err := fixture.fault.QueryRow("SELECT status FROM redis_usage_inboxes WHERE id=7").Scan(&faultStatus); err != nil || faultStatus != "processed" {
		t.Fatalf("fault database changed: %q, %v", faultStatus, err)
	}
	var backupMax int64
	backupDB := openRestoreTestDB(t, fixture.backupPath)
	if err := backupDB.QueryRow("SELECT MAX(id) FROM redis_usage_inboxes").Scan(&backupMax); err != nil || backupMax != 5 {
		t.Fatalf("backup changed: max=%d, %v", backupMax, err)
	}
	if _, err := backup.RestorePricingBackup(context.Background(), fixture.backupPath, fixture.faultPath, output); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("repeat restore should refuse existing output: %v", err)
	}
}

func TestRestorePricingBackupMapsQueueKeyToUnknownSource(t *testing.T) {
	fixture := makeRestoreFixture(t, false)
	mustRestoreExec(t, fixture.fault, "ALTER TABLE redis_usage_inboxes DROP COLUMN source")
	mustRestoreExec(t, fixture.fault, "ALTER TABLE redis_usage_inboxes ADD COLUMN queue_key TEXT NOT NULL DEFAULT 'queue'")
	insertRestoreInbox(t, fixture.fault, "queue_key", 6, "postbackup", "processed", "2026-09-22 10:03:00", "2026-09-22 10:03:01", "2026-09-22 10:03:02")
	output := filepath.Join(filepath.Dir(fixture.faultPath), "restored.db")
	result, err := backup.RestorePricingBackup(context.Background(), fixture.backupPath, fixture.faultPath, output)
	if err != nil || result.ReplayedRows != 1 {
		t.Fatalf("restore = %+v, %v", result, err)
	}
	restored := openRestoreTestDB(t, output)
	var source, status string
	if err := restored.QueryRow("SELECT source, status FROM redis_usage_inboxes WHERE id=6").Scan(&source, &status); err != nil || source != "unknown" || status != "pending" {
		t.Fatalf("restored source/status = %q/%q, %v", source, status, err)
	}
}

func TestRestorePricingBackupRefusesUnverifiedOrExistingOutput(t *testing.T) {
	fixture := makeRestoreFixture(t, true)
	output := filepath.Join(filepath.Dir(fixture.faultPath), "restored.db")
	mustRestoreExec(t, fixture.fault, "UPDATE pricing_migration_state SET baseline_json=? WHERE id=1", `{"schema_version":1,"inbox_max_id":6}`)
	if _, err := backup.RestorePricingBackup(context.Background(), fixture.backupPath, fixture.faultPath, output); err == nil || !strings.Contains(err.Error(), "boundary") {
		t.Fatalf("mismatched M1 boundary accepted: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed restore created output: %v", err)
	}
	mustRestoreExec(t, fixture.fault, "UPDATE pricing_migration_state SET baseline_json=? WHERE id=1", `{"schema_version":1,"inbox_max_id":5}`)
	if err := os.WriteFile(output, []byte("leave intact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestorePricingBackup(context.Background(), fixture.backupPath, fixture.faultPath, output); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output accepted: %v", err)
	}
	content, err := os.ReadFile(output)
	if err != nil || string(content) != "leave intact" {
		t.Fatalf("existing output modified: %q, %v", content, err)
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sidecar := output + suffix
		if err := os.WriteFile(sidecar, []byte("leave sidecar intact"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := backup.RestorePricingBackup(context.Background(), fixture.backupPath, fixture.faultPath, output); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("orphan sidecar %s accepted: %v", suffix, err)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("orphan sidecar %s allowed output creation: %v", suffix, err)
		}
		content, err := os.ReadFile(sidecar)
		if err != nil || string(content) != "leave sidecar intact" {
			t.Fatalf("orphan sidecar %s changed: %q, %v", suffix, content, err)
		}
		if err := os.Remove(sidecar); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRestorePricingBackupProcessesDuplicateContentOncePerOriginalID(t *testing.T) {
	dir := t.TempDir()
	faultPath := filepath.Join(dir, "fault.db")
	fault, err := repository.OpenDatabase(config.Config{SQLitePath: faultPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := fault.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := fault.Exec("UPDATE pricing_migration_state SET init_kind='legacy' WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	faultSQL, err := fault.DB()
	if err != nil {
		t.Fatal(err)
	}
	backupPath, err := backup.NewWriter(filepath.Join(dir, "backups")).WriteDatabase(context.Background(), faultSQL, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := fault.Exec("UPDATE pricing_migration_state SET backup_path=?, baseline_json=? WHERE id=1", backupPath, `{"schema_version":1,"inbox_max_id":0}`).Error; err != nil {
		t.Fatal(err)
	}
	message := `{"timestamp":"2026-09-22T08:00:00Z","provider":"claude","model":"sonnet","request_id":"restore-duplicate","tokens":{"input_tokens":1,"output_tokens":2}}`
	catalog := pricing.NewCatalog(pricing.EmptySnapshot())
	syncer := service.NewSyncServiceWithOptions(fault, service.SyncServiceOptions{PricingCatalog: catalog, BaseURL: "https://cpa.example.com"})
	firstRows, err := repository.InsertRedisUsageInboxRawMessages(fault, "redis_pull:usage", []string{message}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := syncer.ProcessRedisUsageInbox(context.Background())
	if err != nil || firstResult.InsertedEvents != 1 {
		t.Fatalf("fault database first processing = %+v, %v", firstResult, err)
	}
	secondRows, err := repository.InsertRedisUsageInboxRawMessages(fault, "redis_pull:usage", []string{message}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if firstRows[0].ID == secondRows[0].ID {
		t.Fatal("duplicate messages have same inbox ID")
	}
	if err := faultSQL.Close(); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "restored.db")
	result, err := backup.RestorePricingBackup(context.Background(), backupPath, faultPath, output)
	if err != nil || result.BackupInboxMaxID != 0 || result.ReplayedRows != 2 {
		t.Fatalf("restore postbackup messages = %+v, %v", result, err)
	}
	restored, err := repository.OpenDatabaseConnection(config.Config{SQLitePath: output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := restored.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	restoreSyncer := service.NewSyncServiceWithOptions(restored, service.SyncServiceOptions{PricingCatalog: catalog, BaseURL: "https://cpa.example.com"})
	firstPass, err := restoreSyncer.ProcessRedisUsageInbox(context.Background())
	if err != nil || firstPass.InsertedEvents != 2 || firstPass.ProcessedRows != 2 {
		t.Fatalf("restored first pass = %+v, %v", firstPass, err)
	}
	secondPass, err := restoreSyncer.ProcessRedisUsageInbox(context.Background())
	if err != nil || secondPass.InsertedEvents != 0 || secondPass.ProcessedRows != 0 {
		t.Fatalf("restored second pass = %+v, %v", secondPass, err)
	}
	var eventCount, processedCount int64
	if err := restored.Model(&entities.UsageEvent{}).Where("request_id = ?", "restore-duplicate").Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := restored.Model(&entities.RedisUsageInbox{}).Where("status = ?", repository.RedisUsageInboxStatusProcessed).Count(&processedCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 || processedCount != 2 {
		t.Fatalf("same-content replay produced events=%d processed-inbox=%d", eventCount, processedCount)
	}
}

func TestPricingRestoreCLIProducesNewDatabase(t *testing.T) {
	fixture := makeRestoreFixture(t, true)
	insertRestoreInbox(t, fixture.fault, "queue_key", 6, "cli-postbackup", "pending", "2026-09-22 10:05:00", "2026-09-22 10:05:01", "2026-09-22 10:05:02")
	output := filepath.Join(filepath.Dir(fixture.faultPath), "from-cli.db")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "../../../cmd/pricing-restore", "-backup", fixture.backupPath, "-fault", fixture.faultPath, "-out", output)
	command.Env = os.Environ()
	printed, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(printed), "replayed inbox rows: 1") {
		t.Fatalf("CLI result: %v (context: %v), %s", err, ctx.Err(), printed)
	}
	restored := openRestoreTestDB(t, output)
	var raw string
	if err := restored.QueryRow("SELECT raw_message FROM redis_usage_inboxes WHERE id=6").Scan(&raw); err != nil || raw != "cli-postbackup" {
		t.Fatalf("CLI restored message = %q, %v", raw, err)
	}
}
