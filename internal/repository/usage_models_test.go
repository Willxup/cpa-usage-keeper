package repository

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func TestAggregateUsageModelsAggregatesIncrementallyAndIdempotently(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	insertUsageModelAggregationEvents(t, db, []entities.UsageEvent{
		{EventKey: "model-1", Model: " claude-opus ", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)},
		{EventKey: "model-2", Model: "claude-opus", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC)},
		{EventKey: "model-3", Model: "claude-opus", AuthType: "oauth", AuthIndex: "auth-b", Timestamp: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)},
		{EventKey: "model-blank", Model: " ", AuthType: "apikey", AuthIndex: "auth-c", Timestamp: time.Date(2026, 5, 21, 13, 0, 0, 0, time.UTC)},
	})

	pending, err := HasPendingUsageModelAggregation(context.Background(), db)
	if err != nil {
		t.Fatalf("HasPendingUsageModelAggregation returned error: %v", err)
	}
	if !pending {
		t.Fatal("expected usage model aggregation to be pending")
	}
	if err := AggregateUsageModels(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageModels returned error: %v", err)
	}
	assertUsageModelRow(t, db, "claude-opus", "apikey", "auth-a", 2, 2)
	assertUsageModelRow(t, db, "claude-opus", "oauth", "auth-b", 1, 3)
	assertUsageModelCheckpoint(t, db, 4)

	if err := AggregateUsageModels(context.Background(), db, now.Add(time.Minute)); err != nil {
		t.Fatalf("second AggregateUsageModels returned error: %v", err)
	}
	assertUsageModelRow(t, db, "claude-opus", "apikey", "auth-a", 2, 2)
	pending, err = HasPendingUsageModelAggregation(context.Background(), db)
	if err != nil {
		t.Fatalf("second HasPendingUsageModelAggregation returned error: %v", err)
	}
	if pending {
		t.Fatal("expected usage model aggregation not to be pending")
	}

	insertUsageModelAggregationEvents(t, db, []entities.UsageEvent{
		{EventKey: "model-4", Model: "claude-opus", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC)},
	})
	if err := AggregateUsageModels(context.Background(), db, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("delta AggregateUsageModels returned error: %v", err)
	}
	assertUsageModelRow(t, db, "claude-opus", "apikey", "auth-a", 3, 5)
	assertUsageModelCheckpoint(t, db, 5)
}

func TestAggregateUsageModelsInitializesCheckpointFromExistingRows(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	insertUsageModelAggregationEvents(t, db, []entities.UsageEvent{
		{EventKey: "legacy-model-1", Model: "claude-opus", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)},
		{EventKey: "legacy-model-2", Model: "claude-opus", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC)},
	})
	if err := db.Create(&entities.UsageModel{
		Model:            "claude-opus",
		AuthType:         "apikey",
		AuthIndex:        "auth-a",
		RequestCount:     2,
		LastUsageEventID: 2,
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error; err != nil {
		t.Fatalf("seed legacy usage model row: %v", err)
	}

	if err := AggregateUsageModels(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageModels returned error: %v", err)
	}
	assertUsageModelRow(t, db, "claude-opus", "apikey", "auth-a", 2, 2)
	assertUsageModelCheckpoint(t, db, 2)

	insertUsageModelAggregationEvents(t, db, []entities.UsageEvent{
		{EventKey: "legacy-model-3", Model: "claude-opus", AuthType: "apikey", AuthIndex: "auth-a", Timestamp: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)},
	})
	if err := AggregateUsageModels(context.Background(), db, now.Add(time.Minute)); err != nil {
		t.Fatalf("delta AggregateUsageModels returned error: %v", err)
	}
	assertUsageModelRow(t, db, "claude-opus", "apikey", "auth-a", 3, 3)
	assertUsageModelCheckpoint(t, db, 3)
}

func insertUsageModelAggregationEvents(t *testing.T, db *gorm.DB, events []entities.UsageEvent) {
	t.Helper()
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("seed usage events: %v", err)
	}
}

func assertUsageModelRow(t *testing.T, db *gorm.DB, model, authType, authIndex string, wantRequestCount, wantLastUsageEventID int64) {
	t.Helper()
	var row entities.UsageModel
	if err := db.Where("model = ? AND auth_type = ? AND auth_index = ?", model, authType, authIndex).First(&row).Error; err != nil {
		t.Fatalf("load usage model %s/%s/%s: %v", model, authType, authIndex, err)
	}
	if row.RequestCount != wantRequestCount || row.LastUsageEventID != wantLastUsageEventID {
		t.Fatalf("unexpected usage model row: %+v", row)
	}
}

func assertUsageModelCheckpoint(t *testing.T, db *gorm.DB, wantLastID int64) {
	t.Helper()
	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", usageModelAggregationCheckpointName).First(&checkpoint).Error; err != nil {
		t.Fatalf("load usage model checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != wantLastID || checkpoint.StatsUpdatedAt == nil {
		t.Fatalf("unexpected usage model checkpoint: %+v", checkpoint)
	}
}
