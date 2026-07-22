package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestLongCustomOverviewWithoutFactsReturnsEmptySeries(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider := service.NewUsageService(db)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 31)

	overview, err := provider.GetUsageOverview(context.Background(), servicedto.UsageFilter{
		Range: "custom", CustomUnit: "day", RangeCount: 31,
		StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatalf("GetUsageOverview returned error: %v", err)
	}
	if len(overview.Series.Buckets) != 0 || len(overview.Series.Requests) != 0 || len(overview.Series.Tokens) != 0 ||
		len(overview.Series.RPM) != 0 || len(overview.Series.TPM) != 0 || len(overview.Series.Cost) != 0 ||
		len(overview.Series.CacheReadRate) != 0 {
		t.Fatalf("expected an empty series for an empty range, got %+v", overview.Series)
	}
}
