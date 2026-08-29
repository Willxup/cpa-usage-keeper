package poller_test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func openUsageAggregationRunnerDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "runner.db")})
	if err != nil {
		t.Fatalf("open usage aggregation runner database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load usage aggregation runner sql db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close usage aggregation runner database: %v", err)
		}
	})
	return db
}

func assertUsageAggregationCheckpoint(t *testing.T, db *gorm.DB, name string, want int64) {
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", name).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load usage aggregation checkpoint %s: %v", name, err)
	}
	if checkpoint.LastAggregatedUsageEventID != want {
		t.Fatalf("expected usage aggregation checkpoint %s=%d, got %+v", name, want, checkpoint)
	}
}
