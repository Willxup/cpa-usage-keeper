package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildUsageOverviewDoesNotDependOnActivityTable(t *testing.T) {
	db := openTestDatabase(t)
	end := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	start := end.Add(-2 * time.Hour)
	event := entities.UsageEvent{
		EventKey:    "overview-without-activity-table",
		APIGroupKey: "provider-a",
		Model:       "model-a",
		Timestamp:   end.Add(-time.Hour),
		InputTokens: 10,
		TotalTokens: 10,
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
		t.Fatalf("insert overview event: %v", err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatalf("aggregate overview stats: %v", err)
	}
	if err := db.Migrator().DropTable("usage_activity_stats"); err != nil {
		t.Fatalf("drop Activity table: %v", err)
	}

	overview, err := repository.BuildUsageOverviewWithFilter(db, repositorydto.UsageQueryFilter{
		Range: "2h", StartTime: &start, EndTime: &end, QueryNow: &end,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter should not query Activity: %v", err)
	}
	if overview.Usage.TotalRequests != 1 || overview.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected overview usage without Activity table: %+v", overview.Usage)
	}
	if overview.Summary.RequestCount != 1 || overview.Summary.TokenCount != 10 {
		t.Fatalf("unexpected overview summary without Activity table: %+v", overview.Summary)
	}
	var seriesRequests int64
	for _, requests := range overview.Series.Requests {
		seriesRequests += requests
	}
	if seriesRequests != 1 {
		t.Fatalf("unexpected overview series without Activity table: %+v", overview.Series.Requests)
	}
}
