package repository

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/models"
)

func TestBuildUsageSnapshotWithFilterAppliesTimeBounds(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-filter.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 0, time.UTC)
	snapshot, err := BuildUsageSnapshotWithFilter(db, UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageSnapshotWithFilter returned error: %v", err)
	}

	if snapshot.TotalRequests != 1 {
		t.Fatalf("expected one in-range request, got %+v", snapshot)
	}
	if snapshot.TotalTokens != 20 {
		t.Fatalf("expected in-range tokens only, got %+v", snapshot)
	}
	if len(snapshot.APIs) != 1 {
		t.Fatalf("expected one API in filtered snapshot, got %+v", snapshot.APIs)
	}
	if snapshot.RequestsByHour["2026-04-16T10:00:00Z"] != 1 {
		t.Fatalf("expected only 10:00 bucket to remain, got %+v", snapshot.RequestsByHour)
	}
	if _, ok := snapshot.RequestsByHour["2026-04-16T09:00:00Z"]; ok {
		t.Fatalf("expected 09:00 bucket to be filtered out, got %+v", snapshot.RequestsByHour)
	}
}

func TestBuildUsageOverviewWithFilterComputesSummaryAndSeries(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	if _, err := UpsertModelPriceSetting(db, ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []models.UsageEvent{
		{
			EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 9, 15, 0, 0, time.UTC), Failed: false,
			InputTokens: 1000, OutputTokens: 500, ReasoningTokens: 100, CachedTokens: 200, TotalTokens: 1800,
		},
		{
			EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 10, 45, 0, 0, time.UTC), Failed: true,
			InputTokens: 2000, OutputTokens: 1000, ReasoningTokens: 50, CachedTokens: 100, TotalTokens: 3150,
		},
		{
			EventKey: "event-3", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 17, 11, 5, 0, 0, time.UTC), Failed: false,
			InputTokens: 500, OutputTokens: 250, ReasoningTokens: 25, CachedTokens: 50, TotalTokens: 825,
		},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 17, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, UsageQueryFilter{Range: "7d", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Summary.RequestCount != 3 || overview.Summary.TokenCount != 5775 {
		t.Fatalf("unexpected summary counts: %+v", overview.Summary)
	}
	if overview.Summary.CachedTokens != 350 || overview.Summary.ReasoningTokens != 175 {
		t.Fatalf("unexpected summary token breakdown: %+v", overview.Summary)
	}
	if overview.Summary.WindowMinutes != 2880 {
		t.Fatalf("expected 2880 minute window, got %+v", overview.Summary)
	}
	if overview.Summary.RPM != 3.0/2880.0 || overview.Summary.TPM != 5775.0/2880.0 {
		t.Fatalf("unexpected summary rates: %+v", overview.Summary)
	}
	if overview.Summary.TotalCost != 0.035805 {
		t.Fatalf("unexpected summary cost: %+v", overview.Summary)
	}
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected summary cost to be available, got %+v", overview.Summary)
	}

	if len(overview.Series.Requests) != 2 || overview.Series.Requests["2026-04-16"] != 2 || overview.Series.Requests["2026-04-17"] != 1 {
		t.Fatalf("unexpected request series: %+v", overview.Series.Requests)
	}
	if overview.Series.Tokens["2026-04-16"] != 4950 || overview.Series.Tokens["2026-04-17"] != 825 {
		t.Fatalf("unexpected token series: %+v", overview.Series.Tokens)
	}
	if overview.Series.RPM["2026-04-16"] != 2.0/1440.0 || overview.Series.RPM["2026-04-17"] != 1.0/1440.0 {
		t.Fatalf("unexpected rpm series: %+v", overview.Series.RPM)
	}
	if overview.Series.TPM["2026-04-16"] != 4950.0/1440.0 || overview.Series.TPM["2026-04-17"] != 825.0/1440.0 {
		t.Fatalf("unexpected tpm series: %+v", overview.Series.TPM)
	}
	if overview.Series.Cost["2026-04-16"] != 0.03069 || overview.Series.Cost["2026-04-17"] != 0.005115000000000001 {
		t.Fatalf("unexpected cost series: %+v", overview.Series.Cost)
	}
	if !reflect.DeepEqual(overview.Series.InputTokens, map[string]int64{"2026-04-16": 3000, "2026-04-17": 500}) {
		t.Fatalf("unexpected input token series: %+v", overview.Series.InputTokens)
	}
	if !reflect.DeepEqual(overview.Series.OutputTokens, map[string]int64{"2026-04-16": 1500, "2026-04-17": 250}) {
		t.Fatalf("unexpected output token series: %+v", overview.Series.OutputTokens)
	}
	if !reflect.DeepEqual(overview.Series.CachedTokens, map[string]int64{"2026-04-16": 300, "2026-04-17": 50}) {
		t.Fatalf("unexpected cached token series: %+v", overview.Series.CachedTokens)
	}
	if !reflect.DeepEqual(overview.Series.ReasoningTokens, map[string]int64{"2026-04-16": 150, "2026-04-17": 25}) {
		t.Fatalf("unexpected reasoning token series: %+v", overview.Series.ReasoningTokens)
	}
	if overview.Health.TotalSuccess != 2 || overview.Health.TotalFailure != 1 {
		t.Fatalf("unexpected overview health totals: %+v", overview.Health)
	}
	expectedSuccessRate := (2.0 / 3.0) * 100.0
	if diff := overview.Health.SuccessRate - expectedSuccessRate; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("unexpected overview health success rate: %+v", overview.Health)
	}
	if overview.Health.Rows != 7 || overview.Health.Columns != 96 || overview.Health.BucketSeconds != 15*60 {
		t.Fatalf("unexpected service health grid metadata: %+v", overview.Health)
	}
	if overview.Health.WindowStart != time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC) ||
		overview.Health.WindowEnd != time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected service health window: %+v", overview.Health)
	}
	if len(overview.Health.BlockDetails) != overview.Health.Rows*overview.Health.Columns {
		t.Fatalf("expected full service health grid, got %d blocks", len(overview.Health.BlockDetails))
	}
	firstBlock := overview.Health.BlockDetails[0]
	if firstBlock.StartTime != time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC) ||
		firstBlock.EndTime != time.Date(2026, 4, 11, 0, 15, 0, 0, time.UTC) ||
		firstBlock.Success != 0 || firstBlock.Failure != 0 || firstBlock.Rate != -1 {
		t.Fatalf("unexpected first health block: %+v", firstBlock)
	}
	populatedBlock := overview.Health.BlockDetails[517]
	if populatedBlock.StartTime != time.Date(2026, 4, 16, 9, 15, 0, 0, time.UTC) ||
		populatedBlock.EndTime != time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC) ||
		populatedBlock.Success != 1 || populatedBlock.Failure != 0 || populatedBlock.Rate != 1 {
		t.Fatalf("unexpected populated health block: %+v", populatedBlock)
	}
	failedBlock := overview.Health.BlockDetails[523]
	if failedBlock.StartTime != time.Date(2026, 4, 16, 10, 45, 0, 0, time.UTC) ||
		failedBlock.EndTime != time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC) ||
		failedBlock.Success != 0 || failedBlock.Failure != 1 || failedBlock.Rate != 0 {
		t.Fatalf("unexpected failed health block: %+v", failedBlock)
	}
	latestPopulatedBlock := overview.Health.BlockDetails[620]
	if latestPopulatedBlock.StartTime != time.Date(2026, 4, 17, 11, 0, 0, 0, time.UTC) ||
		latestPopulatedBlock.EndTime != time.Date(2026, 4, 17, 11, 15, 0, 0, time.UTC) ||
		latestPopulatedBlock.Success != 1 || latestPopulatedBlock.Failure != 0 || latestPopulatedBlock.Rate != 1 {
		t.Fatalf("unexpected latest populated health block: %+v", latestPopulatedBlock)
	}
}

func TestBuildUsageOverviewFromEventsBuildsSnapshotAndOverviewInOnePass(t *testing.T) {
	events := []models.UsageEvent{
		{
			EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 9, 15, 0, 0, time.UTC), Failed: false,
			InputTokens: 1000, OutputTokens: 500, ReasoningTokens: 100, CachedTokens: 200, TotalTokens: 1800,
			Source: "source-a", AuthIndex: "1", LatencyMS: 120,
		},
		{
			EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "", Model: "",
			Timestamp: time.Date(2026, 4, 16, 10, 45, 0, 0, time.UTC), Failed: true,
			InputTokens: 2000, OutputTokens: 1000, ReasoningTokens: 50, CachedTokens: 100, TotalTokens: 3150,
			Source: " source-b ", AuthIndex: " 2 ", LatencyMS: 250,
		},
	}
	filterStart := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	filterEnd := time.Date(2026, 4, 16, 23, 59, 59, 999000000, time.UTC)
	filter := UsageQueryFilter{Range: "24h", StartTime: &filterStart, EndTime: &filterEnd}
	pricingByModel := map[string]models.ModelPriceSetting{
		"claude-sonnet": {
			Model:                "claude-sonnet",
			PromptPricePer1M:     3,
			CompletionPricePer1M: 15,
			CachePricePer1M:      0.3,
		},
	}

	overview := buildUsageOverviewFromEvents(events, filter, pricingByModel)

	if overview.Usage == nil {
		t.Fatal("expected usage snapshot to be populated")
	}
	if overview.Usage.TotalRequests != 2 || overview.Usage.TotalTokens != 4950 {
		t.Fatalf("unexpected usage snapshot totals: %+v", overview.Usage)
	}
	if overview.Usage.SuccessCount != 1 || overview.Usage.FailureCount != 1 {
		t.Fatalf("unexpected usage snapshot success/failure counts: %+v", overview.Usage)
	}
	if overview.Usage.APIs["provider-a"].Models["claude-sonnet"].TotalRequests != 1 {
		t.Fatalf("expected provider/model snapshot to be populated, got %+v", overview.Usage.APIs)
	}
	if overview.Usage.APIs["unknown"].Models["unknown"].TotalRequests != 1 {
		t.Fatalf("expected unknown provider/model snapshot to be populated, got %+v", overview.Usage.APIs)
	}
	if details := overview.Usage.APIs["provider-a"].Models["claude-sonnet"].Details; len(details) != 0 {
		t.Fatalf("expected overview snapshot to skip unreturned details, got %+v", details)
	}
	if details := overview.Usage.APIs["unknown"].Models["unknown"].Details; len(details) != 0 {
		t.Fatalf("expected overview snapshot to skip unreturned unknown-model details, got %+v", details)
	}
	if overview.Summary.RequestCount != 2 || overview.Summary.TokenCount != 4950 {
		t.Fatalf("unexpected summary totals: %+v", overview.Summary)
	}
	if overview.Summary.CachedTokens != 300 || overview.Summary.ReasoningTokens != 150 {
		t.Fatalf("unexpected summary token breakdown: %+v", overview.Summary)
	}
	if overview.Summary.CostAvailable {
		t.Fatalf("expected cost to be unavailable when any event model is unpriced, got %+v", overview.Summary)
	}
	if overview.Series.Requests["2026-04-16T09:00:00Z"] != 1 || overview.Series.Requests["2026-04-16T10:00:00Z"] != 1 {
		t.Fatalf("unexpected hourly request series: %+v", overview.Series.Requests)
	}
	if overview.HourlySeries.Models["claude-sonnet"].Requests["2026-04-16T09:00:00Z"] != 1 {
		t.Fatalf("expected claude-sonnet hourly model series, got %+v", overview.HourlySeries.Models)
	}
	if overview.HourlySeries.Models["unknown"].Tokens["2026-04-16T10:00:00Z"] != 3150 {
		t.Fatalf("expected unknown hourly model token series, got %+v", overview.HourlySeries.Models)
	}
	if overview.DailySeries.Models["claude-sonnet"].Requests["2026-04-16"] != 1 {
		t.Fatalf("expected claude-sonnet daily model series, got %+v", overview.DailySeries.Models)
	}
	if overview.Health.TotalSuccess != 1 || overview.Health.TotalFailure != 1 {
		t.Fatalf("unexpected health totals: %+v", overview.Health)
	}
	if overview.Health.SuccessRate != 50 {
		t.Fatalf("expected 50%% success rate, got %+v", overview.Health)
	}
}

func TestBuildUsageOverviewWithFilterBuilds24hHealthGridFor24hRange(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-health-24h.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "event-success", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 17, 9, 31, 0, 0, time.UTC), Failed: false, TotalTokens: 10},
		{EventKey: "event-failed", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 17, 23, 59, 0, 0, time.UTC), Failed: true, TotalTokens: 20},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 17, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, UsageQueryFilter{Range: "24h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Health.Rows != 7 || overview.Health.Columns != 96 || overview.Health.BucketSeconds != 129 {
		t.Fatalf("unexpected service health grid metadata: rows=%d columns=%d bucket_seconds=%d", overview.Health.Rows, overview.Health.Columns, overview.Health.BucketSeconds)
	}
	if overview.Health.WindowStart.Before(end.Add(-24*time.Hour)) || overview.Health.WindowStart.After(end.Add(-24*time.Hour).Add(time.Second)) ||
		overview.Health.WindowEnd.Before(end) || overview.Health.WindowEnd.After(end.Add(time.Second)) {
		t.Fatalf("unexpected service health window: %+v", overview.Health)
	}
	if len(overview.Health.BlockDetails) != 7*96 {
		t.Fatalf("expected 24h service health grid, got %d blocks", len(overview.Health.BlockDetails))
	}

	var successBlock *UsageOverviewHealthBlockRecord
	var failedBlock *UsageOverviewHealthBlockRecord
	for index := range overview.Health.BlockDetails {
		block := &overview.Health.BlockDetails[index]
		if block.Success == 1 {
			successBlock = block
		}
		if block.Failure == 1 {
			failedBlock = block
		}
	}
	if successBlock == nil || successBlock.StartTime.After(events[0].Timestamp) || !successBlock.EndTime.After(events[0].Timestamp) || successBlock.Rate != 1 {
		t.Fatalf("unexpected success health block: %+v", successBlock)
	}
	if failedBlock == nil || failedBlock.StartTime.After(events[1].Timestamp) || !failedBlock.EndTime.After(events[1].Timestamp) || failedBlock.Rate != 0 {
		t.Fatalf("unexpected failed health block: %+v", failedBlock)
	}
}

func TestBuildUsageOverviewWithFilterReturnsUnavailableCostForPartialPricing(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-partial-pricing.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	if _, err := UpsertModelPriceSetting(db, ModelPriceSettingInput{
		Model:                "priced-model",
		PromptPricePer1M:     1,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []models.UsageEvent{
		{
			EventKey: "event-priced", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "priced-model",
			Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 1_000_000,
			InputTokens: 1_000_000,
		},
		{
			EventKey: "event-unpriced", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "unpriced-model",
			Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 1_000_000,
			InputTokens: 1_000_000,
		},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, UsageQueryFilter{Range: "24h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Summary.CostAvailable {
		t.Fatalf("expected cost to be unavailable when any in-range model is unpriced, got %+v", overview.Summary)
	}
	if overview.Summary.TotalCost != 1 {
		t.Fatalf("expected priced portion to remain in total cost, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterReturnsUnavailableCostWithoutPricing(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-no-pricing.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	events := []models.UsageEvent{{
		EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet",
		Timestamp: time.Date(2026, 4, 16, 9, 15, 0, 0, time.UTC), TotalTokens: 1800,
		InputTokens: 1000, OutputTokens: 500, CachedTokens: 200,
	}}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, UsageQueryFilter{Range: "24h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Summary.CostAvailable {
		t.Fatalf("expected summary cost to be unavailable, got %+v", overview.Summary)
	}
	if overview.Summary.TotalCost != 0 {
		t.Fatalf("expected zero summary cost without pricing, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterUsesExactPresetWindowMinutes(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-preset-window.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	cases := []struct {
		name            string
		rangeName       string
		start           time.Time
		end             time.Time
		expectMinutes   int64
		expectBucketKey string
	}{
		{
			name:            "24h stays hourly with 1440 minute window",
			rangeName:       "24h",
			start:           time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC),
			end:             time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			expectMinutes:   1440,
			expectBucketKey: "2026-04-17T12:00:00Z",
		},
		{
			name:            "7d uses daily buckets with 10080 minute window",
			rangeName:       "7d",
			start:           time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
			end:             time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC),
			expectMinutes:   10080,
			expectBucketKey: "2026-04-17",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := models.UsageEvent{
				EventKey:        "event-" + tc.rangeName,
				SnapshotRunID:   1,
				APIGroupKey:     "provider-a",
				Model:           "claude-sonnet",
				Timestamp:       tc.end,
				TotalTokens:     25,
				InputTokens:     10,
				OutputTokens:    15,
				ReasoningTokens: 0,
			}
			if _, _, err := InsertUsageEvents(db, []models.UsageEvent{event}); err != nil {
				t.Fatalf("InsertUsageEvents returned error: %v", err)
			}

			overview, err := BuildUsageOverviewWithFilter(db, UsageQueryFilter{Range: tc.rangeName, StartTime: &tc.start, EndTime: &tc.end})
			if err != nil {
				t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
			}

			if overview.Summary.WindowMinutes != tc.expectMinutes {
				t.Fatalf("expected %d minute window, got %+v", tc.expectMinutes, overview.Summary)
			}
			if len(overview.Series.Requests) != 1 || overview.Series.Requests[tc.expectBucketKey] != 1 {
				t.Fatalf("unexpected request series for %s: %+v", tc.rangeName, overview.Series.Requests)
			}
		})
		if err := db.Exec("DELETE FROM usage_events").Error; err != nil {
			t.Fatalf("DELETE usage_events returned error: %v", err)
		}
	}
}

func TestBuildUsageOverviewWithFilterBuildsLatestHourlySeriesForLongRanges(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-hourly-series.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	if _, err := UpsertModelPriceSetting(db, ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     1,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "event-old", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 17, 8, 0, 0, 0, time.UTC), TotalTokens: 1_000_000, InputTokens: 1_000_000},
		{EventKey: "event-latest-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 23, 22, 15, 0, 0, time.UTC), TotalTokens: 2_000_000, InputTokens: 2_000_000, OutputTokens: 5, CachedTokens: 7, ReasoningTokens: 11},
		{EventKey: "event-latest-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 23, 23, 45, 0, 0, time.UTC), TotalTokens: 3_000_000, InputTokens: 3_000_000, OutputTokens: 13, CachedTokens: 17, ReasoningTokens: 19},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 23, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, UsageQueryFilter{Range: "7d", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if len(overview.Series.Requests) != 2 || overview.Series.Requests["2026-04-17"] != 1 || overview.Series.Requests["2026-04-23"] != 2 {
		t.Fatalf("expected main overview series to remain daily for 7d, got %+v", overview.Series.Requests)
	}
	if _, ok := overview.HourlySeries.Requests["2026-04-17T08:00:00Z"]; ok {
		t.Fatalf("expected latest hourly series to exclude buckets before the latest 24 hours, got %+v", overview.HourlySeries.Requests)
	}
	if overview.HourlySeries.Requests["2026-04-23T22:00:00Z"] != 1 || overview.HourlySeries.Requests["2026-04-23T23:00:00Z"] != 1 {
		t.Fatalf("unexpected latest hourly request series: %+v", overview.HourlySeries.Requests)
	}
	if overview.HourlySeries.Cost["2026-04-23T22:00:00Z"] != 1.999993 || overview.HourlySeries.Cost["2026-04-23T23:00:00Z"] != 2.999983 {
		t.Fatalf("unexpected latest hourly cost series: %+v", overview.HourlySeries.Cost)
	}
	if overview.HourlySeries.InputTokens["2026-04-23T22:00:00Z"] != 2_000_000 || overview.HourlySeries.OutputTokens["2026-04-23T23:00:00Z"] != 13 {
		t.Fatalf("unexpected latest hourly token category series: %+v", overview.HourlySeries)
	}
	if overview.DailySeries.Requests["2026-04-17"] != 1 || overview.DailySeries.Requests["2026-04-23"] != 2 {
		t.Fatalf("unexpected daily request series: %+v", overview.DailySeries.Requests)
	}
}

func TestBuildUsageOverviewWithFilterUsesDailyBucketsForLongCustomRanges(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-custom-buckets.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}

	events := []models.UsageEvent{
		{EventKey: "event-1", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 20, 8, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-2", SnapshotRunID: 1, APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 26, 18, 0, 0, 0, time.UTC), TotalTokens: 20},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 26, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Summary.WindowMinutes != 10080 {
		t.Fatalf("expected 10080 minute window, got %+v", overview.Summary)
	}
	if len(overview.Series.Requests) != 2 {
		t.Fatalf("expected daily buckets for long custom range, got %+v", overview.Series.Requests)
	}
	if overview.Series.Requests["2026-04-20"] != 1 || overview.Series.Requests["2026-04-26"] != 1 {
		t.Fatalf("expected daily request buckets, got %+v", overview.Series.Requests)
	}
	if _, ok := overview.Series.Requests["2026-04-20T08:00:00Z"]; ok {
		t.Fatalf("expected long custom range not to keep hourly buckets, got %+v", overview.Series.Requests)
	}
}
