package test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/backup"

	_ "github.com/mattn/go-sqlite3"
)

func TestWriterWriteDatabaseBacksUpSQLiteDatabase(t *testing.T) {
	root := t.TempDir()
	source := newSourceDatabase(t)
	source.SetMaxOpenConns(1) // 旧调用方仍可传入唯一 writer 池。
	if _, err := source.Exec(`INSERT INTO records (name) VALUES (?)`, "saved"); err != nil {
		t.Fatalf("insert row: %v", err)
	}
	writer := backup.NewWriter(root)
	backupAt := time.Date(2026, 4, 16, 12, 34, 56, 123456789, time.UTC)

	path, err := writer.WriteDatabase(context.Background(), source, backupAt)
	if err != nil {
		t.Fatalf("WriteDatabase returned error: %v", err)
	}

	if filepath.Dir(path) != filepath.Join(root, "2026-04-16") {
		t.Fatalf("unexpected backup directory: %s", path)
	}
	if filepath.Ext(path) != ".db" || !strings.HasPrefix(filepath.Base(path), "database_") {
		t.Fatalf("unexpected backup file name: %s", filepath.Base(path))
	}
	backupDB := openTestSQLiteDB(t, path)
	var name string
	if err := backupDB.QueryRow(`SELECT name FROM records WHERE id = 1`).Scan(&name); err != nil {
		t.Fatalf("query backup row: %v", err)
	}
	if name != "saved" {
		t.Fatalf("expected backed up row name saved, got %q", name)
	}
}

func TestWriterWriteDatabaseUsesLocalDateDirectory(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("Test/Local", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocal })
	root := t.TempDir()
	source := newSourceDatabase(t)
	writer := backup.NewWriter(root)
	backupAt := time.Date(2026, 4, 15, 20, 0, 0, 0, time.UTC)

	path, err := writer.WriteDatabase(context.Background(), source, backupAt)
	if err != nil {
		t.Fatalf("WriteDatabase returned error: %v", err)
	}
	if filepath.Dir(path) != filepath.Join(root, "2026-04-16") {
		t.Fatalf("expected local backup date directory 2026-04-16, got %s", path)
	}
}

func TestWriterWriteDatabaseRestrictsBackupPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	root := t.TempDir()
	source := newSourceDatabase(t)
	writer := backup.NewWriter(root)
	backupAt := time.Date(2026, 4, 16, 12, 34, 56, 0, time.UTC)

	path, err := writer.WriteDatabase(context.Background(), source, backupAt)
	if err != nil {
		t.Fatalf("WriteDatabase returned error: %v", err)
	}

	dayDirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat backup day directory: %v", err)
	}
	if mode := dayDirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("expected backup day directory mode 0700, got %o", mode)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat backup file: %v", err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected backup file mode 0600, got %o", mode)
	}
}

func TestWriterWriteDatabaseHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	source := newSourceDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := backup.NewWriter(root).WriteDatabase(ctx, source, time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected canceled context error")
	}
	files, listErr := backup.ListFiles(root)
	if listErr != nil {
		t.Fatalf("ListFiles returned error: %v", listErr)
	}
	if len(files) != 0 {
		t.Fatalf("expected no finalized backup files after cancellation, got %+v", files)
	}
}

func TestWriterWriteDatabaseFromReadOnlyPoolFinishesDuringWrites(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	writerDB := openTestSQLiteDB(t, sourcePath)
	if _, err := writerDB.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := writerDB.Exec(`CREATE TABLE records (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := writerDB.Exec(`INSERT INTO records (name) VALUES ('before')`); err != nil {
		t.Fatal(err)
	}
	if _, err := writerDB.Exec(`CREATE TABLE padding (body BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := writerDB.Exec(`INSERT INTO padding (body) VALUES (zeroblob(33554432))`); err != nil {
		t.Fatal(err)
	}
	readerDB := openReadOnlySQLiteDB(t, sourcePath)
	readerDB.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	type result struct {
		path string
		err  error
	}
	backupResult := make(chan result, 1)
	backupDone := make(chan struct{})
	go func() {
		path, err := backup.NewWriter(filepath.Join(root, "backups")).WriteDatabase(ctx, readerDB, time.Now())
		backupResult <- result{path, err}
		close(backupDone)
	}()

	// 等备份实际占用只读池后持续提交小写入，验证复制不会被外部写入反复重启。
	for readerDB.Stats().InUse == 0 {
		select {
		case <-backupDone:
			t.Fatal("backup finished before read-only source connection was observed")
		case <-ctx.Done():
			t.Fatal("backup did not acquire the read-only connection")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	var committed atomic.Int64
	stopWrites := make(chan struct{})
	writesDone := make(chan struct{})
	go func() {
		defer close(writesDone)
		for n := 0; ; n++ {
			select {
			case <-stopWrites:
				return
			default:
			}
			if _, err := writerDB.ExecContext(ctx, `INSERT INTO records (name) VALUES (?)`, fmt.Sprintf("during-%d", n)); err == nil {
				committed.Add(1)
			}
			time.Sleep(250 * time.Microsecond)
		}
	}()
	backupOutcome := <-backupResult
	close(stopWrites)
	<-writesDone
	if backupOutcome.err != nil {
		t.Fatalf("backup did not finish during concurrent writes: %v", backupOutcome.err)
	}
	if committed.Load() == 0 {
		t.Fatal("writer did not commit while backup used the separate reader pool")
	}
	backupDB := openTestSQLiteDB(t, backupOutcome.path)
	var count, paddingLength int
	if err := backupDB.QueryRow(`SELECT count(*) FROM records`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := backupDB.QueryRow(`SELECT length(body) FROM padding`).Scan(&paddingLength); err != nil {
		t.Fatal(err)
	}
	if count < 1 || count > int(committed.Load())+1 || paddingLength != 33554432 {
		t.Fatalf("incomplete backup: count=%d, writes=%d, padding=%d", count, committed.Load(), paddingLength)
	}
	var integrity string
	if err := backupDB.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("backup integrity = %q, %v", integrity, err)
	}
}

func TestWriterWriteDatabaseReleasesReadSnapshotOnFailure(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	writerDB := openTestSQLiteDB(t, sourcePath)
	if _, err := writerDB.Exec(`CREATE TABLE records (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	readerDB := openReadOnlySQLiteDB(t, sourcePath)
	readerDB.SetMaxOpenConns(1)
	stamp := time.Date(2026, 4, 16, 12, 34, 56, 0, time.Local)
	backupRoot := filepath.Join(root, "backups")
	tempPath := filepath.Join(backupRoot, "2026-04-16", "database_20260416T123456.000000000.db.tmp")
	if err := os.MkdirAll(tempPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempPath, "keep"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.NewWriter(backupRoot).WriteDatabase(context.Background(), readerDB, stamp); err == nil {
		t.Fatal("expected destination open failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var count int
	if err := readerDB.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&count); err != nil {
		t.Fatalf("read-only connection was not released: %v", err)
	}
	if _, err := writerDB.ExecContext(ctx, `INSERT INTO records (name) VALUES ('after failure')`); err != nil {
		t.Fatalf("read transaction was not released: %v", err)
	}
}

func TestWriterWriteDatabaseCancellationReleasesReadConnection(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	writerDB := openTestSQLiteDB(t, sourcePath)
	if _, err := writerDB.Exec(`CREATE TABLE records (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	readerDB := openReadOnlySQLiteDB(t, sourcePath)
	readerDB.SetMaxOpenConns(1)
	writerConn, err := writerDB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writerConn.Close()
	if _, err := writerConn.ExecContext(context.Background(), `BEGIN EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	defer writerConn.ExecContext(context.Background(), `ROLLBACK`)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	backupRoot := filepath.Join(root, "backups")
	go func() {
		_, err := backup.NewWriter(backupRoot).WriteDatabase(ctx, readerDB, time.Now())
		finished <- err
	}()
	deadline := time.After(2 * time.Second)
	for readerDB.Stats().InUse == 0 {
		select {
		case <-deadline:
			t.Fatal("backup did not acquire read-only connection")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if _, err := writerConn.ExecContext(context.Background(), `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("backup cancellation = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backup did not stop after cancellation")
	}
	checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
	defer checkCancel()
	var count int
	if err := readerDB.QueryRowContext(checkCtx, `SELECT count(*) FROM records`).Scan(&count); err != nil {
		t.Fatalf("read-only pool remained occupied after cancellation: %v", err)
	}
}

func TestWriterWriteDatabaseValidatesInputs(t *testing.T) {
	if _, err := backup.NewWriter("").WriteDatabase(context.Background(), openTestSQLiteDB(t, filepath.Join(t.TempDir(), "source.db")), time.Now()); err == nil || !strings.Contains(err.Error(), "backup directory is required") {
		t.Fatalf("expected backup directory error, got %v", err)
	}
	if _, err := backup.NewWriter(t.TempDir()).WriteDatabase(context.Background(), nil, time.Now()); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("expected database required error, got %v", err)
	}
}

func TestCleanupRetention(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("Test/Local", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	for _, tc := range []struct {
		name          string
		retention     int
		now           time.Time
		expired, keep string
	}{
		{"expired directory", 3, time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), "2026-04-10", "2026-04-15"},
		{"local day boundary", 1, time.Date(2026, 4, 16, 0, 30, 0, 0, time.Local), "2026-04-15", "2026-04-16"},
		{"inclusive retention count", 7, time.Date(2026, 4, 16, 4, 30, 0, 0, time.Local), "2026-04-09", "2026-04-10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, day := range []string{tc.expired, tc.keep} {
				dir := filepath.Join(root, day)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "database.db"), []byte("backup"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			removed, err := backup.Cleanup(root, tc.retention, tc.now)
			if err != nil || removed != 1 {
				t.Fatalf("Cleanup = %d, %v; want one removed directory", removed, err)
			}
			files, err := backup.ListFiles(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || filepath.Base(filepath.Dir(files[0])) != tc.keep {
				t.Fatalf("remaining files = %v, want only %s", files, tc.keep)
			}
		})
	}
}

func TestListFilesReturnsDatabaseBackups(t *testing.T) {
	root := t.TempDir()
	dayDir := filepath.Join(root, "2026-04-16")
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		t.Fatalf("create day dir: %v", err)
	}
	databasePath := filepath.Join(dayDir, "database.db")
	if err := os.WriteFile(databasePath, []byte("db"), 0o600); err != nil {
		t.Fatalf("write db backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dayDir, "snapshot.json"), []byte("json"), 0o600); err != nil {
		t.Fatalf("write json backup: %v", err)
	}

	files, err := backup.ListFiles(root)
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0] != databasePath {
		t.Fatalf("expected only database backup, got %+v", files)
	}
}

func TestCleanupIgnoresMissingDirectory(t *testing.T) {
	removed, err := backup.Cleanup(filepath.Join(t.TempDir(), "missing"), 30, time.Now())
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 removed directories, got %d", removed)
	}
}

func openTestSQLiteDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newSourceDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestSQLiteDB(t, filepath.Join(t.TempDir(), "source.db"))
	if _, err := db.Exec(`CREATE TABLE records (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func openReadOnlySQLiteDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	return openTestSQLiteDB(t, "file:"+path+"?mode=ro&_query_only=1&_busy_timeout=5000")
}
