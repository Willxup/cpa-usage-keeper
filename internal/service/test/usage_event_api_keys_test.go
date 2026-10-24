package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageEventAPIKeyCandidatesUseCurrentMatchingUsageOnEveryCall(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	keys := []entities.CPAAPIKey{{APIKey: "first-key"}, {APIKey: "new-key"}, {APIKey: "wrong-model"}, {APIKey: "wrong-source"}, {APIKey: "wrong-result"}, {APIKey: "outside-window"}}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	events := []entities.UsageEvent{
		{EventKey: "first", APIGroupKey: "first-key", Timestamp: start, Model: "gpt-5", AuthIndex: "auth-1", Failed: true},
		{EventKey: "model", APIGroupKey: "wrong-model", Timestamp: start, Model: "other", AuthIndex: "auth-1", Failed: true},
		{EventKey: "source", APIGroupKey: "wrong-source", Timestamp: start, Model: "gpt-5", AuthIndex: "other", Failed: true},
		{EventKey: "result", APIGroupKey: "wrong-result", Timestamp: start, Model: "gpt-5", AuthIndex: "auth-1"},
		{EventKey: "window", APIGroupKey: "outside-window", Timestamp: end, Model: "gpt-5", AuthIndex: "auth-1", Failed: true},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	provider := service.NewUsageService(db, emptyPricingCatalogForTest()).(service.UsageEventAPIKeyFilterProvider)
	filter := servicedto.UsageFilter{
		Range: "custom", StartTime: &start, EndTime: &end, EndExclusive: true,
		Model: "gpt-5", AuthIndex: "auth-1", Result: "failed", APIKeyID: "invalid-ignored",
		PageSize: 1, Offset: 100, CursorTimestamp: &start,
	}
	rows, err := provider.ListUsageEventAPIKeyFilterOptions(context.Background(), filter)
	if err != nil || len(rows) != 1 || rows[0].ID != keys[0].ID {
		t.Fatalf("expected only first matching key, got %+v, %v", rows, err)
	}
	fresh := events[0]
	fresh.ID, fresh.EventKey, fresh.APIGroupKey = 0, "new", "new-key"
	if err := db.Create(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	rows, err = provider.ListUsageEventAPIKeyFilterOptions(context.Background(), filter)
	if err != nil || len(rows) != 2 || rows[1].ID != keys[1].ID {
		t.Fatalf("expected newly used key on next call, got %+v, %v", rows, err)
	}
}
