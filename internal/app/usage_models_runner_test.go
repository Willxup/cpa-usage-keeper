package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestUsageModelAggregationRunnerRunsImmediatelyThenAtInterval(t *testing.T) {
	db := openAppTestDatabase(t)
	runner := NewUsageModelAggregationRunner(db)
	runner.interval = time.Minute
	var delays []time.Duration
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		delays = append(delays, d)
		return len(delays) < 3
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	expected := []time.Duration{0, time.Minute, time.Minute}
	if len(delays) != len(expected) {
		t.Fatalf("expected delays %+v, got %+v", expected, delays)
	}
	for index, want := range expected {
		if delays[index] != want {
			t.Fatalf("expected delay %d to be %s, got %s", index, want, delays[index])
		}
	}
}

func TestUsageModelAggregationRunnerValidatesConfig(t *testing.T) {
	if err := NewUsageModelAggregationRunner(nil).Run(context.Background()); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("expected nil database validation error, got %v", err)
	}
	db := openAppTestDatabase(t)
	runner := NewUsageModelAggregationRunner(db)
	runner.interval = 0
	if err := runner.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "interval") {
		t.Fatalf("expected non-positive interval validation error, got %v", err)
	}
}

func openAppTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: t.TempDir() + "/app.db"})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}
