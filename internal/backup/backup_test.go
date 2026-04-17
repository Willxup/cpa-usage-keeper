package backup

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWriterWritePersistsPayload(t *testing.T) {
	root := t.TempDir()
	writer := NewWriter(root)
	payload := []byte(`{"version":1}`)
	fetchedAt := time.Date(2026, 4, 16, 12, 34, 56, 123456789, time.UTC)

	path, err := writer.Write(7, fetchedAt, payload)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if filepath.Dir(path) != filepath.Join(root, "2026-04-16") {
		t.Fatalf("unexpected backup directory: %s", path)
	}

	files, err := ListFiles(root)
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0] != path {
		t.Fatalf("unexpected backup files: %+v", files)
	}
}

func TestCleanupRemovesExpiredBackupDirectories(t *testing.T) {
	root := t.TempDir()
	writer := NewWriter(root)
	_, _ = writer.Write(1, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC), []byte(`old`))
	_, _ = writer.Write(2, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC), []byte(`keep`))

	removed, err := Cleanup(root, 3, time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed directory, got %d", removed)
	}

	files, err := ListFiles(root)
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if len(files) != 1 || filepath.Base(filepath.Dir(files[0])) != "2026-04-15" {
		t.Fatalf("unexpected remaining files: %+v", files)
	}
}

func TestCleanupIgnoresMissingDirectory(t *testing.T) {
	removed, err := Cleanup(filepath.Join(t.TempDir(), "missing"), 30, time.Now())
	if err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 removed directories, got %d", removed)
	}
}
