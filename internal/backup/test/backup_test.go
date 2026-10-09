package test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/backup"

	_ "github.com/mattn/go-sqlite3"
)

func TestWriterWriteDatabaseBacksUpSQLiteDatabase(t *testing.T) {
	root := t.TempDir()
	source := newSourceDatabase(t)
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
