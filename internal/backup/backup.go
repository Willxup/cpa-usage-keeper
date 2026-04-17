package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Writer struct {
	dir string
}

func NewWriter(dir string) *Writer {
	return &Writer{dir: strings.TrimSpace(dir)}
}

func (w *Writer) Write(snapshotRunID uint, fetchedAt time.Time, payload []byte) (string, error) {
	if w == nil {
		return "", fmt.Errorf("backup writer is nil")
	}
	if w.dir == "" {
		return "", fmt.Errorf("backup directory is required")
	}

	stamp := fetchedAt.UTC()
	if stamp.IsZero() {
		stamp = time.Now().UTC()
	}

	dayDir := filepath.Join(w.dir, stamp.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}

	fileName := fmt.Sprintf("snapshot_%06d_%s.json", snapshotRunID, stamp.Format("20060102T150405.000000000Z"))
	fullPath := filepath.Join(dayDir, fileName)
	if err := os.WriteFile(fullPath, payload, 0o644); err != nil {
		return "", fmt.Errorf("write backup file: %w", err)
	}

	return fullPath, nil
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

	cutoff := now.UTC().AddDate(0, 0, -retentionDays)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		backupDay, err := time.Parse("2006-01-02", entry.Name())
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
		if strings.EqualFold(filepath.Ext(d.Name()), ".json") {
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
