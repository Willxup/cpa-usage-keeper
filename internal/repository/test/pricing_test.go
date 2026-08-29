package test

import (
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestListUsedModelsNormalizesDirtyModelValuesInMemory(t *testing.T) {
	db := openTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "model-alpha", Model: "alpha", Timestamp: time.Unix(1, 0)},
		{EventKey: "model-beta-spaced", Model: " beta ", Timestamp: time.Unix(2, 0)},
		{EventKey: "model-beta", Model: "beta", Timestamp: time.Unix(3, 0)},
		{EventKey: "model-empty", Model: "", Timestamp: time.Unix(4, 0)},
		{EventKey: "model-blank", Model: " ", Timestamp: time.Unix(5, 0)},
	}); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}
	if err := db.Exec("INSERT INTO usage_events (event_key, model) VALUES (?, NULL)", "model-null").Error; err != nil {
		t.Fatalf("insert null model usage event: %v", err)
	}

	models, err := repository.ListUsedModels(db)
	if err != nil {
		t.Fatalf("ListUsedModels returned error: %v", err)
	}

	expected := []string{"alpha", "beta"}
	if strings.Join(models, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected normalized models %#v, got %#v", expected, models)
	}
}
