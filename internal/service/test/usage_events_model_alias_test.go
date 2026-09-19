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

func TestUsageServicePreservesEventMetadataForListAndStream(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	modelAlias := " sonnet-business "
	responseModel := " gpt-5.6-luna "
	statusCode := 200
	stream := true
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:            "model-alias-event",
		APIGroupKey:         "provider-a",
		Model:               "claude-sonnet",
		ModelAlias:          &modelAlias,
		ResponseModel:       responseModel,
		ServiceTier:         "auto",
		ResponseServiceTier: "default",
		StatusCode:          &statusCode,
		Stream:              &stream,
		Timestamp:           time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		InputTokens:         10,
		TotalTokens:         10,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	page, err := provider.ListUsageEvents(context.Background(), servicedto.UsageFilter{Page: 1, PageSize: 10, Limit: 10})
	if err != nil {
		t.Fatalf("ListUsageEvents returned error: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].ModelAlias != "sonnet-business" || page.Events[0].ResponseModel != "gpt-5.6-luna" || page.Events[0].ServiceTier != "auto" || page.Events[0].ResponseServiceTier != "default" || page.Events[0].StatusCode == nil || *page.Events[0].StatusCode != statusCode || page.Events[0].Stream == nil || !*page.Events[0].Stream {
		t.Fatalf("expected list result to preserve event metadata, got %+v", page.Events)
	}

	var streamed []servicedto.UsageEventRecord
	if err := provider.StreamUsageEvents(context.Background(), servicedto.UsageFilter{}, func(event servicedto.UsageEventRecord) error {
		streamed = append(streamed, event)
		return nil
	}); err != nil {
		t.Fatalf("StreamUsageEvents returned error: %v", err)
	}
	if len(streamed) != 1 || streamed[0].ModelAlias != "sonnet-business" || streamed[0].ResponseModel != "gpt-5.6-luna" || streamed[0].ServiceTier != "auto" || streamed[0].ResponseServiceTier != "default" || streamed[0].StatusCode == nil || *streamed[0].StatusCode != statusCode || streamed[0].Stream == nil || !*streamed[0].Stream {
		t.Fatalf("expected stream result to preserve event metadata, got %+v", streamed)
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
