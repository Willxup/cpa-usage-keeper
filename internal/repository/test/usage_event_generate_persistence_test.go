package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestInsertUsageEventsPersistsNormalizedGenerateValues(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	generate := true
	prewarm := false
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "prewarm", Generate: &prewarm, Timestamp: now},
		{EventKey: "generate", Generate: &generate, Timestamp: now.Add(time.Second)},
		{EventKey: "default-generate", Timestamp: now.Add(2 * time.Second)},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	type generateRow struct {
		EventKey string
		Generate bool
	}
	var rows []generateRow
	if err := db.Table("usage_events").Select("event_key, generate").Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load persisted generate values: %v", err)
	}
	want := []bool{false, true, true}
	if len(rows) != len(want) {
		t.Fatalf("expected %d rows, got %d", len(want), len(rows))
	}
	for index, row := range rows {
		if row.Generate != want[index] {
			t.Errorf("event %q generate=%v, want %v", row.EventKey, row.Generate, want[index])
		}
	}
}
