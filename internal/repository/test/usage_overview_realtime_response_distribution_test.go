package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildUsageOverviewRealtimeWithFilterRequiresValidTTFTAndLatencyForBothDistributions(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	validTTFT := int64(100)
	zeroTTFT := int64(0)
	ttftWithoutLatency := int64(200)

	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "valid-response", Timestamp: now.Add(-4 * time.Minute), LatencyMS: 500, TTFTMS: &validTTFT},
		{EventKey: "zero-ttft", Timestamp: now.Add(-3 * time.Minute), LatencyMS: 600, TTFTMS: &zeroTTFT},
		{EventKey: "zero-latency", Timestamp: now.Add(-2 * time.Minute), LatencyMS: 0, TTFTMS: &ttftWithoutLatency},
		{EventKey: "missing-ttft", Timestamp: now.Add(-time.Minute), LatencyMS: 700},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilter returned error: %v", err)
	}

	assertRealtimeResponseDistributionUsesOnlyValidPairs(t, realtime.ResponseDistribution.TTFT, 100)
	assertRealtimeResponseDistributionUsesOnlyValidPairs(t, realtime.ResponseDistribution.Latency, 500)
}

func assertRealtimeResponseDistributionUsesOnlyValidPairs(t *testing.T, series repodto.RealtimeResponseDistributionSeriesRecord, wantMS int64) {
	t.Helper()
	if series.TotalParticles != 1 || len(series.Particles) != 1 {
		t.Fatalf("expected one valid response sample, got total=%d particles=%+v", series.TotalParticles, series.Particles)
	}
	if series.Particles[0].MS != wantMS {
		t.Fatalf("expected valid response sample %dms, got %+v", wantMS, series.Particles[0])
	}

	nonEmptyAveragePoints := 0
	for _, point := range series.AverageLine {
		if point.AvgMS == nil {
			continue
		}
		nonEmptyAveragePoints++
		if *point.AvgMS != float64(wantMS) {
			t.Fatalf("expected average line to use only %dms samples, got %+v", wantMS, point)
		}
	}
	if nonEmptyAveragePoints == 0 {
		t.Fatal("expected average line to contain the valid response sample")
	}
}
