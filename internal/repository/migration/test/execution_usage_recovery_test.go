package test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const executionRecoveryVersion = "20261018_recover_execution_usage_events"
const missingRequestIDError = "decode redis usage message: request_id is required"

// 文件数据库放在 worktree/data；只有编译及工具缓存使用 TMPDIR。
func executionRecoveryDatabasePath(tb testing.TB) string {
	tb.Helper()
	root, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			tb.Fatal("repository root not found")
		}
		root = parent
	}
	dir := filepath.Join(root, "data", "issue-643-tests")
	if err := os.MkdirAll(dir, 0755); err != nil {
		tb.Fatal(err)
	}
	dir, err = os.MkdirTemp(dir, "database-")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "recovery.db")
}

func openExecutionRecoveryDatabase(tb testing.TB, path string) *gorm.DB {
	tb.Helper()
	db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=5000&_foreign_keys=on"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		tb.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	tb.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		tb.Fatal(err)
	}
	return db
}

func createExecutionRecoverySchema(tb testing.TB, db *gorm.DB) {
	tb.Helper()
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, event_key TEXT, request_id TEXT, execution_id TEXT, timestamp TEXT, total_tokens INTEGER, cost_usd REAL)").Error; err != nil {
			tb.Fatal(err)
		}
	}
	if err := db.AutoMigrate(&entities.RedisUsageInbox{}); err != nil {
		tb.Fatal(err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		tb.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", executionRecoveryVersion).Error; err != nil {
		tb.Fatal(err)
	}
}

type recoveredEventRow struct {
	ID                     int64
	EventKey, RequestID    *string
	ExecutionID, Timestamp *string
	TotalTokens            int64
	CostUSD                float64
}

func executionRecoveryPointer(value string) *string { return &value }

func TestExecutionRecoveryMigrationPreservesDataAndSchema(t *testing.T) {
	db := openExecutionRecoveryDatabase(t, executionRecoveryDatabasePath(t))
	createExecutionRecoverySchema(t, db)
	rows := []recoveredEventRow{
		{ID: 1, EventKey: executionRecoveryPointer(""), ExecutionID: executionRecoveryPointer("execution-1")},
		{ID: 3, ExecutionID: executionRecoveryPointer("execution-3")},
		{ID: 5, EventKey: executionRecoveryPointer("real-request"), ExecutionID: executionRecoveryPointer("execution-5")},
		{ID: 7, EventKey: executionRecoveryPointer("")},
		{ID: 8, ExecutionID: executionRecoveryPointer("")},
		{ID: 10, EventKey: executionRecoveryPointer(" "), ExecutionID: executionRecoveryPointer("execution-10")},
		{ID: 11, EventKey: executionRecoveryPointer(""), ExecutionID: executionRecoveryPointer(" \t ")},
		{ID: 13, EventKey: executionRecoveryPointer(""), RequestID: executionRecoveryPointer("saved-request"), ExecutionID: executionRecoveryPointer("execution-13")},
	}
	for i := range rows {
		rows[i].Timestamp = executionRecoveryPointer("2026-09-01T03:04:05+08:00")
		rows[i].TotalTokens = int64(i + 100)
		rows[i].CostUSD = 0.125
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Table(table).Create(&rows).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("CREATE TABLE recovery_checkpoint (id INTEGER PRIMARY KEY, cursor INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO recovery_checkpoint VALUES (1, 13)").Error; err != nil {
		t.Fatal(err)
	}
	var schemaBefore []string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY name").Scan(&schemaBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatal(err)
	}
	want := append([]recoveredEventRow(nil), rows...)
	for _, index := range []int{0, 1, 6, 7} {
		want[index].EventKey = want[index].ExecutionID
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var got []recoveredEventRow
		if err := db.Table(table).Order("id").Find(&got).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s changed values beyond eligible event_key: got=%+v want=%+v", table, got, want)
		}
	}
	var schemaAfter []string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY name").Scan(&schemaAfter).Error; err != nil || !reflect.DeepEqual(schemaAfter, schemaBefore) {
		t.Fatalf("schema or indexes changed: before=%v after=%v error=%v", schemaBefore, schemaAfter, err)
	}
	var cursor int64
	if err := db.Raw("SELECT cursor FROM recovery_checkpoint WHERE id = 1").Scan(&cursor).Error; err != nil || cursor != 13 {
		t.Fatalf("checkpoint changed: cursor=%d error=%v", cursor, err)
	}
	assertExecutionRecoveryVersion(t, db, true)
	if err := migration.Run(db); err != nil {
		t.Fatalf("second startup: %v", err)
	}
}

func recoveryInbox(id int64, raw string) entities.RedisUsageInbox {
	stamp := time.Date(2026, 9, 18, 23, 45, 0, 123456000, time.Local)
	processed := stamp.Add(time.Hour)
	return entities.RedisUsageInbox{ID: id, Source: "redis_subscribe:usage", MessageHash: fmt.Sprintf("hash-%d", id), RawMessage: raw, Status: repository.RedisUsageInboxStatusDecodeFailed, LastError: missingRequestIDError, AttemptCount: 4, UsageEventKey: "saved-link", PoppedAt: stamp, CreatedAt: stamp.Add(-time.Hour), UpdatedAt: stamp.Add(2 * time.Hour), ProcessedAt: &processed}
}

func TestExecutionRecoveryMigrationTargetsInboxAndPreservesRawTimes(t *testing.T) {
	db := openExecutionRecoveryDatabase(t, executionRecoveryDatabasePath(t))
	createExecutionRecoverySchema(t, db)
	rows := []entities.RedisUsageInbox{
		recoveryInbox(1, `{"execution_id":"execution-1"}`),
		recoveryInbox(3, `{"request_id":null,"execution_id":"execution-3"}`),
		recoveryInbox(5, `{"request_id":" \t ","execution_id":" \t "}`),
		recoveryInbox(7, `{"request_id":"real-request","execution_id":"execution-7"}`),
		recoveryInbox(9, `{bad-json}`),
		recoveryInbox(11, `{"execution_id":null}`),
		recoveryInbox(13, `{"execution_id":""}`),
		recoveryInbox(15, `{"request_id":123,"execution_id":"execution-15"}`),
		recoveryInbox(17, `{"execution_id":123}`),
		recoveryInbox(19, `{"request_id":""}`),
	}
	for i, status := range []string{repository.RedisUsageInboxStatusPending, repository.RedisUsageInboxStatusProcessFailed, repository.RedisUsageInboxStatusDiscarded, repository.RedisUsageInboxStatusProcessed} {
		row := recoveryInbox(int64(21+i), `{"execution_id":"keep-status"}`)
		row.Status = status
		rows = append(rows, row)
	}
	for i, lastError := range []string{"decode redis usage message: execution_id is required", missingRequestIDError + " ", "decode redis usage message: invalid character"} {
		row := recoveryInbox(int64(31+i), `{"execution_id":"keep-error"}`)
		row.LastError = lastError
		rows = append(rows, row)
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var before []entities.RedisUsageInbox
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatal(err)
	}
	var after []entities.RedisUsageInbox
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		before[i].Status = repository.RedisUsageInboxStatusPending
		before[i].LastError = ""
		before[i].AttemptCount = 0
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("inbox must only reset eligible status/error/attempts, preserving raw/time/link columns: got=%+v want=%+v", after, before)
	}
}

func TestExecutionRecoveryMigrationBatchesSparseIDsAndAllInboxCandidates(t *testing.T) {
	for _, matchEvery := range []int{0, 137, 1} {
		t.Run(fmt.Sprintf("match-every-%d", matchEvery), func(t *testing.T) {
			db := openExecutionRecoveryDatabase(t, executionRecoveryDatabasePath(t))
			createExecutionRecoverySchema(t, db)
			const count = 1207
			for _, table := range []string{"usage_events", "usage_events_archive"} {
				if err := db.Transaction(func(tx *gorm.DB) error {
					for i := 1; i <= count; i++ {
						key := "existing"
						if matchEvery > 0 && i%matchEvery == 0 {
							key = ""
						}
						if err := tx.Exec("INSERT INTO "+table+" (id,event_key,execution_id) VALUES (?,?,?)", i*3, key, fmt.Sprintf("execution-%d", i)).Error; err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			inboxes := make([]entities.RedisUsageInbox, count)
			for i := range inboxes {
				inboxes[i] = recoveryInbox(int64((i+1)*5), fmt.Sprintf(`{"execution_id":"execution-%d"}`, i))
			}
			if err := db.CreateInBatches(&inboxes, 40).Error; err != nil {
				t.Fatal(err)
			}
			if err := migration.Run(db); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"usage_events", "usage_events_archive"} {
				var fixed int64
				if err := db.Table(table).Where("event_key = execution_id").Count(&fixed).Error; err != nil {
					t.Fatal(err)
				}
				want := 0
				if matchEvery > 0 {
					want = count / matchEvery
				}
				if fixed != int64(want) {
					t.Fatalf("%s repaired %d rows, want %d", table, fixed, want)
				}
			}
			var pending int64
			if err := db.Model(&entities.RedisUsageInbox{}).Where("status = ?", repository.RedisUsageInboxStatusPending).Count(&pending).Error; err != nil || pending != count {
				t.Fatalf("all candidates across SQLite variable limit: pending=%d error=%v", pending, err)
			}
		})
	}
}

func TestExecutionRecoveryMigrationRestartsAfterCommittedBatches(t *testing.T) {
	for _, failureTable := range []string{"usage_events", "usage_events_archive", "redis_usage_inboxes"} {
		t.Run(failureTable, func(t *testing.T) {
			path := executionRecoveryDatabasePath(t)
			db := openExecutionRecoveryDatabase(t, path)
			createExecutionRecoverySchema(t, db)
			for _, table := range []string{"usage_events", "usage_events_archive"} {
				if err := db.Exec("WITH RECURSIVE rows(id) AS (VALUES(1) UNION ALL SELECT id+1 FROM rows WHERE id < 1003) INSERT INTO " + table + " (id,event_key,execution_id) SELECT id,'','execution-' || id FROM rows").Error; err != nil {
					t.Fatal(err)
				}
			}
			inboxes := make([]entities.RedisUsageInbox, 1003)
			for i := range inboxes {
				inboxes[i] = recoveryInbox(int64(i+1), `{"execution_id":"execution-inbox"}`)
			}
			if err := db.CreateInBatches(&inboxes, 40).Error; err != nil {
				t.Fatal(err)
			}
			column := "event_key"
			if failureTable == "redis_usage_inboxes" {
				column = "status"
			}
			if err := db.Exec("CREATE TRIGGER fail_recovery BEFORE UPDATE OF " + column + " ON " + failureTable + " WHEN NEW.id = 750 BEGIN SELECT RAISE(ABORT, 'injected batch failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			if err := migration.Run(db); err == nil || !strings.Contains(err.Error(), "injected batch failure") {
				t.Fatalf("runner must stop on batch failure: %v", err)
			}
			assertExecutionRecoveryVersion(t, db, false)
			var committed int64
			query := db.Table(failureTable).Where("event_key = execution_id")
			if failureTable == "redis_usage_inboxes" {
				query = db.Table(failureTable).Where("status = ?", repository.RedisUsageInboxStatusPending)
			}
			if err := query.Count(&committed).Error; err != nil || committed != 500 {
				t.Fatalf("only first short transaction should survive: committed=%d error=%v", committed, err)
			}
			sqlDB, _ := db.DB()
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}
			db = openExecutionRecoveryDatabase(t, path)
			if err := db.Exec("DROP TRIGGER fail_recovery").Error; err != nil {
				t.Fatal(err)
			}
			if err := migration.Run(db); err != nil {
				t.Fatalf("restart runner: %v", err)
			}
			assertExecutionRecoveryVersion(t, db, true)
			for _, table := range []string{"usage_events", "usage_events_archive"} {
				var fixed int64
				if err := db.Table(table).Where("event_key = execution_id").Count(&fixed).Error; err != nil || fixed != 1003 {
					t.Fatalf("%s restart repair: fixed=%d error=%v", table, fixed, err)
				}
			}
			var pending int64
			if err := db.Model(&entities.RedisUsageInbox{}).Where("status = ?", repository.RedisUsageInboxStatusPending).Count(&pending).Error; err != nil || pending != 1003 {
				t.Fatalf("restart inbox repair: pending=%d error=%v", pending, err)
			}
			if err := migration.Run(db); err != nil {
				t.Fatalf("repeated startup: %v", err)
			}
		})
	}
}

func TestExecutionRecoveryMigrationFreshAndExistingDatabaseStartup(t *testing.T) {
	path := executionRecoveryDatabasePath(t)
	cfg := config.Config{SQLitePath: path}
	db, err := repository.OpenDatabase(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	assertExecutionRecoveryVersion(t, db, true)
	// 新库先标记版本；只有旧库缺版本时才执行数据迁移。
	if err := db.Exec("INSERT INTO usage_events (event_key, request_id, execution_id) VALUES ('','','startup-execution')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", executionRecoveryVersion).Error; err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	for attempt := 0; attempt < 2; attempt++ {
		db, err = repository.OpenDatabase(cfg)
		if err != nil {
			t.Fatalf("existing startup: %v", err)
		}
		sqlDB, _ = db.DB()
		assertExecutionRecoveryVersion(t, db, true)
		var key string
		if err := db.Raw("SELECT event_key FROM usage_events LIMIT 1").Scan(&key).Error; err != nil || key != "startup-execution" {
			t.Fatalf("startup migration key=%q error=%v", key, err)
		}
		_ = sqlDB.Close()
	}
}

func TestExecutionRecoveryFixesUpperBoundsBeforeEachTable(t *testing.T) {
	db := openExecutionRecoveryDatabase(t, executionRecoveryDatabasePath(t))
	createExecutionRecoverySchema(t, db)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if err := db.Exec("INSERT INTO " + table + " (id,event_key,execution_id) VALUES (1,'','original-execution')").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("CREATE TRIGGER append_" + table + " AFTER UPDATE OF event_key ON " + table + " WHEN OLD.id=1 BEGIN INSERT OR IGNORE INTO " + table + " (id,event_key,execution_id) VALUES (10000,'','later-execution'); END").Error; err != nil {
			t.Fatal(err)
		}
	}
	row := recoveryInbox(1, `{"execution_id":"inbox-execution"}`)
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER append_inbox AFTER UPDATE OF status ON redis_usage_inboxes WHEN OLD.id=1 AND NEW.status='pending' BEGIN INSERT OR IGNORE INTO redis_usage_inboxes (id,source,message_hash,raw_message,status,last_error,attempt_count,popped_at,created_at,updated_at) SELECT 10000,source,'new-hash',raw_message,'decode_failed','decode redis usage message: request_id is required',3,popped_at,created_at,updated_at FROM redis_usage_inboxes WHERE id=1; END").Error; err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := migration.Run(db); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"usage_events", "usage_events_archive"} {
			var key string
			if err := db.Table(table).Select("event_key").Where("id = 10000").Scan(&key).Error; err != nil {
				t.Fatal(err)
			}
			want := ""
			if attempt == 1 {
				want = "later-execution"
			}
			if key != want {
				t.Fatalf("%s fixed bound: key=%q want=%q", table, key, want)
			}
		}
		var inbox entities.RedisUsageInbox
		if err := db.First(&inbox, 10000).Error; err != nil {
			t.Fatal(err)
		}
		wantStatus := repository.RedisUsageInboxStatusDecodeFailed
		if attempt == 1 {
			wantStatus = repository.RedisUsageInboxStatusPending
		}
		if inbox.Status != wantStatus {
			t.Fatalf("inbox fixed bound: status=%q want=%q", inbox.Status, wantStatus)
		}
		if attempt == 0 {
			if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", executionRecoveryVersion).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestExecutionRecoveryRechecksInboxStatusAndError(t *testing.T) {
	db := openExecutionRecoveryDatabase(t, executionRecoveryDatabasePath(t))
	createExecutionRecoverySchema(t, db)
	rows := []entities.RedisUsageInbox{recoveryInbox(1, `{"execution_id":"execution-1"}`), recoveryInbox(2, `{"execution_id":"execution-2"}`)}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	changed := false
	const callbackName = "test:execution-recovery-change-candidates"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if changed || tx.Statement.Table != "redis_usage_inboxes" || !strings.Contains(tx.Statement.SQL.String(), "raw_message") {
			return
		}
		changed = true
		if err := db.Model(&entities.RedisUsageInbox{}).Where("id = 1").UpdateColumns(map[string]any{"status": repository.RedisUsageInboxStatusProcessed}).Error; err != nil {
			tx.AddError(err)
		}
		if err := db.Model(&entities.RedisUsageInbox{}).Where("id = 2").UpdateColumns(map[string]any{"last_error": "changed error"}).Error; err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	if err := migration.Run(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if !changed || rows[0].Status != repository.RedisUsageInboxStatusProcessed || rows[0].AttemptCount != 4 || rows[0].LastError != missingRequestIDError || rows[1].Status != repository.RedisUsageInboxStatusDecodeFailed || rows[1].LastError != "changed error" || rows[1].AttemptCount != 4 {
		t.Fatalf("changed candidate state/error must not be overwritten: %+v", rows)
	}
}

func assertExecutionRecoveryVersion(t *testing.T, db *gorm.DB, applied bool) {
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", executionRecoveryVersion).Count(&count).Error; err != nil || (count == 1) != applied {
		t.Fatalf("version marker: count=%d want applied=%t error=%v", count, applied, err)
	}
}
