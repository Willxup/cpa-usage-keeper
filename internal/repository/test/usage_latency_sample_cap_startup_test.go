package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
)

func TestFreshDatabaseMarksLatencySampleCapMigrationApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	db, err := repository.OpenDatabase(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatalf("open fresh database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260925_limit_latency_sample_points").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("fresh schema has %d latency sample cap versions, want 1", count)
	}
}
