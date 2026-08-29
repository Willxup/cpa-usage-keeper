package test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageServiceAnalysisLatencyResolvesAPIKeyIDBeforeFiltering(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-analysis-latency-filter.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load database pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-target-key", "sk-other-key"}, now); err != nil {
		t.Fatalf("SyncCPAAPIKeys returned error: %v", err)
	}
	activeKeys, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	var targetID string
	for _, key := range activeKeys {
		if key.APIKey == "sk-target-key" {
			targetID = strconv.FormatInt(key.ID, 10)
		}
	}
	if targetID == "" {
		t.Fatal("expected target API key")
	}

	generated := true
	targetTTFT := int64(1010)
	otherTTFT := int64(202)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "latency-target", APIGroupKey: "sk-target-key", Timestamp: now.Add(-90 * time.Minute), Generate: &generated, TTFTMS: &targetTTFT, LatencyMS: 10010},
		{EventKey: "latency-other", APIGroupKey: "sk-other-key", Timestamp: now.Add(-90 * time.Minute), Generate: &generated, TTFTMS: &otherTTFT, LatencyMS: 2002},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := repository.AggregateUsageLatencyStats(context.Background(), db, now); err != nil {
		t.Fatalf("AggregateUsageLatencyStats returned error: %v", err)
	}

	start := now.Add(-2 * time.Hour)
	end := now
	diagnostics, err := service.NewUsageService(db, emptyPricingCatalogForTest()).GetAnalysisLatency(context.Background(), servicedto.UsageFilter{
		APIKeyID: targetID, Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatalf("GetAnalysisLatency returned error: %v", err)
	}
	if diagnostics.TotalPoints != 1 || len(diagnostics.Points) != 1 || diagnostics.Points[0].TTFTMS != targetTTFT || diagnostics.Points[0].LatencyMS != 10010 {
		t.Fatalf("expected only target API key latency, got %+v", diagnostics)
	}
}
