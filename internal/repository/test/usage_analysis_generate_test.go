package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildAnalysisLatencyDiagnosticsExcludesPrewarm(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	normalTTFT := int64(120)
	prewarmTTFT := int64(25)
	generate := true
	prewarm := false
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "normal-latency", Generate: &generate, Timestamp: start.Add(10 * time.Minute), LatencyMS: 900, TTFTMS: &normalTTFT},
		{EventKey: "prewarm-latency", Generate: &prewarm, Timestamp: start.Add(20 * time.Minute), LatencyMS: 80, TTFTMS: &prewarmTTFT},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	diagnostics, err := repository.BuildAnalysisLatencyDiagnosticsWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisLatencyDiagnosticsWithFilter returned error: %v", err)
	}
	if diagnostics.TotalPoints != 1 || len(diagnostics.Points) != 1 {
		t.Fatalf("expected one normal latency point, got %+v", diagnostics)
	}
	if diagnostics.Points[0].TTFTMS != normalTTFT || diagnostics.Points[0].LatencyMS != 900 {
		t.Fatalf("expected normal latency point to remain, got %+v", diagnostics.Points[0])
	}
}
