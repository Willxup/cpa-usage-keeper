package repository

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"gorm.io/gorm"
)

func TestReplaceAuthFilesUpsertsSoftDeletesAndRestoresRows(t *testing.T) {
	db := openAuthFilesTestDatabase(t)
	if err := ReplaceAuthFiles(db, []AuthFileInput{{
		AuthIndex: "1",
		Name:      "First",
		Email:     "first@example.com",
		Type:      "claude",
	}, {
		AuthIndex: "2",
		Name:      "Second",
		Email:     "second@example.com",
		Type:      "gemini",
	}}); err != nil {
		t.Fatalf("ReplaceAuthFiles returned error: %v", err)
	}

	if err := ReplaceAuthFiles(db, []AuthFileInput{{
		AuthIndex: "2",
		Name:      "Second Updated",
		Email:     "updated@example.com",
		Type:      "vertex",
	}}); err != nil {
		t.Fatalf("ReplaceAuthFiles returned error: %v", err)
	}

	files, err := ListAuthFiles(db)
	if err != nil {
		t.Fatalf("ListAuthFiles returned error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 auth file after replacement, got %d", len(files))
	}
	if files[0].AuthIndex != "2" || files[0].Email != "updated@example.com" || files[0].Type != "vertex" {
		t.Fatalf("unexpected auth file after replacement: %+v", files[0])
	}

	if err := ReplaceAuthFiles(db, []AuthFileInput{{
		AuthIndex: "1",
		Name:      "First Restored",
		Email:     "first-restored@example.com",
		Type:      "claude",
	}}); err != nil {
		t.Fatalf("ReplaceAuthFiles restore returned error: %v", err)
	}

	files, err = ListAuthFiles(db)
	if err != nil {
		t.Fatalf("ListAuthFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0].AuthIndex != "1" || files[0].Email != "first-restored@example.com" {
		t.Fatalf("unexpected restored auth file: %+v", files)
	}
}

func openAuthFilesTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "auth_files.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}
