package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageServiceAnalysisLatencyResolvesAPIKeyIDBeforeFiltering(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	targetID := seedUsageFilterAPIKeys(t, db)

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
