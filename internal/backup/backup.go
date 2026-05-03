package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

type Writer struct {
	dir string
}

func NewWriter(dir string) *Writer {
	return &Writer{dir: strings.TrimSpace(dir)}
}

func (w *Writer) WriteDatabase(ctx context.Context, db *sql.DB, backupAt time.Time) (string, error) {
	if w == nil {
		return "", fmt.Errorf("backup writer is nil")
	}
	if w.dir == "" {
		return "", fmt.Errorf("backup directory is required")
	}
	if db == nil {
		return "", fmt.Errorf("database is required")
	}

	stamp := backupAt.In(time.Local)
	if stamp.IsZero() {
		stamp = time.Now().In(time.Local)
	}
	dayDir := filepath.Join(w.dir, stamp.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Chmod(dayDir, 0o700); err != nil {
		return "", fmt.Errorf("restrict backup directory permissions: %w", err)
	}

	fileName := fmt.Sprintf("database_%s.db", stamp.Format("20060102T150405.000000000"))
	fullPath := filepath.Join(dayDir, fileName)
	tempPath := fullPath + ".tmp"
	_ = os.Remove(tempPath)
	if err := copySQLiteDatabase(ctx, db, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("restrict backup file permissions: %w", err)
	}
	if err := os.Rename(tempPath, fullPath); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("finalize backup file: %w", err)
	}
	return fullPath, nil
}

func copySQLiteDatabase(ctx context.Context, sourceDB *sql.DB, destPath string) error {
	sourceConn, err := sourceDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open source database connection: %w", err)
	}
	defer sourceConn.Close()

	destDB, err := sql.Open("sqlite3", destPath)
	if err != nil {
		return fmt.Errorf("open backup database: %w", err)
	}
	defer destDB.Close()
	destConn, err := destDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open backup database connection: %w", err)
	}
	defer destConn.Close()

	return sourceConn.Raw(func(sourceDriverConn any) error {
		sourceSQLite, ok := sourceDriverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("source database connection is not sqlite3")
		}
		return destConn.Raw(func(destDriverConn any) error {
			destSQLite, ok := destDriverConn.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("backup database connection is not sqlite3")
			}
			backup, err := destSQLite.Backup("main", sourceSQLite, "main")
			if err != nil {
				return fmt.Errorf("start sqlite backup: %w", err)
			}
			var backupErr error
			for {
				if err := ctx.Err(); err != nil {
					backupErr = err
					break
				}
				done, err := backup.Step(100)
				if err != nil {
					backupErr = fmt.Errorf("copy sqlite backup: %w", err)
					break
				}
				if done {
					break
				}
			}
			if err := backup.Close(); err != nil {
				backupErr = errors.Join(backupErr, fmt.Errorf("close sqlite backup: %w", err))
			}
			return backupErr
		})
	})
}

func (w *Writer) Cleanup(retentionDays int, now time.Time) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("backup writer is nil")
	}
	return Cleanup(w.dir, retentionDays, now)
}

func Cleanup(dir string, retentionDays int, now time.Time) (int, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" || retentionDays <= 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read backup directory: %w", err)
	}

	localNow := now.In(time.Local)
	cutoff := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -retentionDays)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		backupDay, err := time.ParseInLocation("2006-01-02", entry.Name(), time.Local)
		if err != nil {
			continue
		}
		if backupDay.Before(cutoff.Truncate(24 * time.Hour)) {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return removed, fmt.Errorf("remove expired backup directory %s: %w", entry.Name(), err)
			}
			removed++
		}
	}

	return removed, nil
}

func ListFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".db") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
