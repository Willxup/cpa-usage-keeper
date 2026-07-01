package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

func TestUsageServicePreservesModelAliasForListAndStream(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	modelAlias := " sonnet-business "
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "model-alias-event",
		APIGroupKey: "provider-a",
		Model:       "claude-sonnet",
		ModelAlias:  &modelAlias,
		Timestamp:   time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		InputTokens: 10,
		TotalTokens: 10,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	provider := service.NewUsageService(db)
	page, err := provider.ListUsageEvents(context.Background(), servicedto.UsageFilter{Page: 1, PageSize: 10, Limit: 10})
	if err != nil {
		t.Fatalf("ListUsageEvents returned error: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].ModelAlias != "sonnet-business" {
		t.Fatalf("expected list result to preserve model alias, got %+v", page.Events)
	}

	var streamed []servicedto.UsageEventRecord
	if err := provider.StreamUsageEvents(context.Background(), servicedto.UsageFilter{}, func(event servicedto.UsageEventRecord) error {
		streamed = append(streamed, event)
		return nil
	}); err != nil {
		t.Fatalf("StreamUsageEvents returned error: %v", err)
	}
	if len(streamed) != 1 || streamed[0].ModelAlias != "sonnet-business" {
		t.Fatalf("expected stream result to preserve model alias, got %+v", streamed)
	}
}

func openUsageServiceTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-service-test.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}
