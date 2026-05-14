package repository

import (
	"context"
	"cpa-usage-keeper/internal/repository/dto"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func withRepositoryTestLocation(t *testing.T, name string) {
	t.Helper()
	previousLocal := time.Local
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location
}

func buildUsageOverviewFromEventsForTest(events []entities.UsageEvent, filter dto.UsageQueryFilter, pricingByModel map[string]entities.ModelPriceSetting) *dto.UsageOverviewRecord {
	windowMinutes := computeWindowMinutes(filter)
	bucketByDay := shouldBucketUsageOverviewByDay(filter, windowMinutes)
	latestHourlyStart := latestHourlySeriesStart(filter)
	overview := newUsageOverviewRecord(filter, windowMinutes)
	for _, event := range events {
		applyUsageEventToSnapshot(overview.Usage, event, false)
		applyUsageEventToOverview(overview, event, bucketByDay, latestHourlyStart, pricingByModel)
	}
	finalizeUsageOverview(overview, false)
	return overview
}

func TestBuildUsageSnapshotWithFilterAppliesTimeBounds(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-filter.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "2", TotalTokens: 20},
		{EventKey: "event-3", APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), Source: "source-c", AuthIndex: "3", TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 0, time.UTC)
	snapshot, err := BuildUsageSnapshotWithFilter(db, dto.UsageQueryFilter{StartTime: &start, EndTime: &end})
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
	if snapshot.RequestsByHour["2026-04-16T18:00:00+08:00"] != 1 {
		t.Fatalf("expected only 18:00 project-time bucket to remain, got %+v", snapshot.RequestsByHour)
	}
	if _, ok := snapshot.RequestsByHour["2026-04-16T17:00:00+08:00"]; ok {
		t.Fatalf("expected 17:00 project-time bucket to be filtered out, got %+v", snapshot.RequestsByHour)
	}
}

func TestBuildUsageOverviewWithFilterRequiresResolvedTimeRange(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-requires-time-range.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "4h"}); err == nil || !strings.Contains(err.Error(), "requires start_time and end_time") {
		t.Fatalf("expected missing resolved time range error, got %v", err)
	}
}

func TestBuildUsageOverviewWithFilterDoesNotRunAggregationCatchup(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-no-query-catchup.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)
	if _, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end}); err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	var checkpointCount int64
	if err := db.Model(&entities.UsageOverviewAggregationCheckpoint{}).Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count overview checkpoints returned error: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("expected overview query not to create aggregation checkpoint, got %d", checkpointCount)
	}
}

func TestLoadUsageOverviewBoundaryEventsUsesSeparateRangeQueries(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-boundary-sql.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	start := time.Date(2026, 4, 16, 9, 20, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 12, 40, 0, 0, time.UTC)
	fullStart := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	fullEnd := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end}
	var sqls []string
	callbackName := "test:capture_boundary_sql"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		sqls = append(sqls, tx.Statement.SQL.String())
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	if _, err := loadUsageOverviewBoundaryEventsWithFilter(db, filter, fullStart, fullEnd); err != nil {
		t.Fatalf("loadUsageOverviewBoundaryEventsWithFilter returned error: %v", err)
	}
	if len(sqls) != 2 {
		t.Fatalf("expected two boundary range queries, got %d: %+v", len(sqls), sqls)
	}
	for _, sql := range sqls {
		if strings.Contains(strings.ToUpper(sql), " OR ") {
			t.Fatalf("expected boundary event query not to contain OR, got %s", sql)
		}
	}
}

func TestBuildUsageOverviewWithFilterIncludesHealthBoundaryInsideFullHour(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-health-inner-boundary.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	start := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	for start.Truncate(usageOverviewHealthPresetSpan).Equal(start) {
		start = start.Add(time.Hour)
	}
	end := start.Add(2 * time.Hour)
	boundaryEventTime := start.Add(time.Second)
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "health-edge", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: boundaryEventTime, TotalTokens: 10},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, end.Add(time.Hour)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "4h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	blockIndex := usageOverviewHealthBlockIndex(overview.Health.BlockDetails, boundaryEventTime)
	if blockIndex < 0 {
		t.Fatalf("expected boundary event to fall inside health grid")
	}
	block := overview.Health.BlockDetails[blockIndex]
	if block.Success != 1 || block.Rate != 1 {
		t.Fatalf("expected health boundary event inside full hour to update block, got %+v", block)
	}
}

func TestBuildUsageOverviewWithFilterReusesBoundaryEventsForHealth(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-reuse-boundaries.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	start := time.Date(2026, 4, 16, 9, 20, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 12, 40, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end}
	var usageEventQueries []string
	callbackName := "test:capture_overview_usage_event_sql"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if strings.Contains(sql, "FROM `usage_events`") || strings.Contains(sql, "FROM \"usage_events\"") {
			usageEventQueries = append(usageEventQueries, sql)
		}
	}); err != nil {
		t.Fatalf("register query callback returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	if _, err := BuildUsageOverviewWithFilter(db, filter); err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	if len(usageEventQueries) != 2 {
		t.Fatalf("expected only the two main boundary usage_events queries, got %d: %+v", len(usageEventQueries), usageEventQueries)
	}
	for _, sql := range usageEventQueries {
		if strings.Contains(strings.ToUpper(sql), " OR ") {
			t.Fatalf("expected reused boundary event query not to contain OR, got %s", sql)
		}
	}
}

func TestBuildUsageOverviewWithFilterKeepsRawEventQueriesAtBoundaries(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	end := time.Date(2026, 5, 15, 1, 40, 17, 271676179, time.Local)
	cases := []struct {
		name       string
		rangeValue string
		start      time.Time
		end        time.Time
	}{
		{name: "4h", rangeValue: "4h", start: end.Add(-4 * time.Hour), end: end},
		{name: "8h", rangeValue: "8h", start: end.Add(-8 * time.Hour), end: end},
		{name: "12h", rangeValue: "12h", start: end.Add(-12 * time.Hour), end: end},
		{name: "24h", rangeValue: "24h", start: end.Add(-24 * time.Hour), end: end},
		{name: "today", rangeValue: "today", start: time.Date(2026, 5, 15, 0, 0, 0, 0, time.Local), end: end},
		{name: "yesterday", rangeValue: "yesterday", start: time.Date(2026, 5, 14, 0, 0, 0, 0, time.Local), end: time.Date(2026, 5, 14, 23, 59, 59, 0, time.Local)},
		{name: "7d", rangeValue: "7d", start: end.AddDate(0, 0, -7), end: end},
		{name: "30d", rangeValue: "30d", start: end.AddDate(0, 0, -30), end: end},
		{name: "custom-long", rangeValue: "custom", start: end.AddDate(0, 0, -30), end: end},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-boundary-sql.db")})
			if err != nil {
				t.Fatalf("OpenDatabase returned error: %v", err)
			}
			closeTestDatabase(t, db)

			filter := dto.UsageQueryFilter{Range: tc.rangeValue, StartTime: &tc.start, EndTime: &tc.end}
			var ranges []usageEventQueryRange
			callbackName := "test:capture_overview_usage_event_sql_" + tc.name
			if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
				sql := tx.Statement.SQL.String()
				if strings.Contains(sql, "FROM `usage_events`") || strings.Contains(sql, "FROM \"usage_events\"") {
					ranges = append(ranges, usageEventQueryRangesFromVars(t, tx.Statement.Vars)...)
				}
			}); err != nil {
				t.Fatalf("register query callback returned error: %v", err)
			}
			t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

			if _, err := BuildUsageOverviewWithFilter(db, filter); err != nil {
				t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
			}
			if len(ranges) == 0 {
				t.Fatalf("expected raw usage_events boundary queries to be captured")
			}
			for _, queryRange := range ranges {
				if queryRange.end.Sub(queryRange.start) > time.Hour {
					t.Fatalf("expected overview raw usage_events query to stay near bucket boundaries, got %s to %s", queryRange.start, queryRange.end)
				}
			}
		})
	}
}

type usageEventQueryRange struct {
	start time.Time
	end   time.Time
}

func usageEventQueryRangesFromVars(t *testing.T, vars []any) []usageEventQueryRange {
	t.Helper()
	ranges := make([]usageEventQueryRange, 0)
	for i := 0; i+1 < len(vars); i++ {
		start, startOK := usageEventQueryTime(vars[i])
		end, endOK := usageEventQueryTime(vars[i+1])
		if startOK && endOK && end.After(start) {
			ranges = append(ranges, usageEventQueryRange{start: start, end: end})
		}
	}
	return ranges
}

func usageEventQueryTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, typed)
		return parsed, err == nil
	default:
		return time.Time{}, false
	}
}

func TestBuildUsageOverviewWithFilterUsesStatsForFullHoursAndRawEventsForBoundaries(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-stats-backed.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     0,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []entities.UsageEvent{
		{EventKey: "outside-before", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 10, 0, 0, time.UTC), InputTokens: 99, OutputTokens: 99, TotalTokens: 198},
		{EventKey: "start-boundary", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 25, 0, 0, time.UTC), InputTokens: 1000, OutputTokens: 500, ReasoningTokens: 100, CachedTokens: 200, TotalTokens: 1800},
		{EventKey: "full-hour-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), InputTokens: 2000, OutputTokens: 1000, ReasoningTokens: 50, CachedTokens: 100, TotalTokens: 3150},
		{EventKey: "full-hour-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 50, 0, 0, time.UTC), Failed: true, InputTokens: 500, OutputTokens: 250, ReasoningTokens: 25, CachedTokens: 50, TotalTokens: 825},
		{EventKey: "full-hour-3", APIGroupKey: "provider-b", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 11, 30, 0, 0, time.UTC), InputTokens: 700, OutputTokens: 300, ReasoningTokens: 30, CachedTokens: 70, TotalTokens: 1100},
		{EventKey: "end-boundary", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 12, 35, 0, 0, time.UTC), InputTokens: 400, OutputTokens: 200, ReasoningTokens: 20, CachedTokens: 40, TotalTokens: 660},
		{EventKey: "outside-after", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 12, 45, 0, 0, time.UTC), InputTokens: 88, OutputTokens: 88, TotalTokens: 176},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 16, 13, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 20, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 12, 40, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end}
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		t.Fatalf("loadPriceSettingsByModel returned error: %v", err)
	}
	oracleEvents, err := loadUsageOverviewEventsWithFilter(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewEventsWithFilter returned error: %v", err)
	}
	oracle := buildUsageOverviewFromEventsForTest(oracleEvents, filter, pricingByModel)

	fullHourStart := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	fullHourEnd := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	if err := db.Where("timestamp >= ? AND timestamp < ?", timeutil.FormatStorageTime(fullHourStart), timeutil.FormatStorageTime(fullHourEnd)).Delete(&entities.UsageEvent{}).Error; err != nil {
		t.Fatalf("delete full-hour usage_events returned error: %v", err)
	}

	overview, err := BuildUsageOverviewWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if !reflect.DeepEqual(overview.Summary, oracle.Summary) {
		t.Fatalf("summary mismatch after full-hour raw events were removed\ngot:  %+v\nwant: %+v", overview.Summary, oracle.Summary)
	}
	if !reflect.DeepEqual(overview.Usage, oracle.Usage) {
		t.Fatalf("usage snapshot mismatch after full-hour raw events were removed\ngot:  %+v\nwant: %+v", overview.Usage, oracle.Usage)
	}
	if !reflect.DeepEqual(overview.Series, oracle.Series) {
		t.Fatalf("series mismatch after full-hour raw events were removed\ngot:  %+v\nwant: %+v", overview.Series, oracle.Series)
	}
	if !reflect.DeepEqual(overview.HourlySeries, oracle.HourlySeries) {
		t.Fatalf("hourly series mismatch after full-hour raw events were removed\ngot:  %+v\nwant: %+v", overview.HourlySeries, oracle.HourlySeries)
	}
	if !reflect.DeepEqual(overview.DailySeries, oracle.DailySeries) {
		t.Fatalf("daily series mismatch after full-hour raw events were removed\ngot:  %+v\nwant: %+v", overview.DailySeries, oracle.DailySeries)
	}
	if !reflect.DeepEqual(overview.Health, oracle.Health) {
		t.Fatalf("health mismatch after full-hour raw events were removed\ngot:  %+v\nwant: %+v", overview.Health, oracle.Health)
	}
}

func TestBuildUsageOverviewWithFilterKeepsHealthWindowExactAtStatsBoundaries(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-health-boundary.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "outside-health-bucket", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 19, 30, 0, time.UTC), Failed: true, TotalTokens: 10},
		{EventKey: "inside-health-bucket", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 9, 20, 30, 0, time.UTC), Failed: false, TotalTokens: 20},
		{EventKey: "full-hour", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), Failed: false, TotalTokens: 30},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 9, 20, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 10, 30, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end}
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		t.Fatalf("loadPriceSettingsByModel returned error: %v", err)
	}
	oracleEvents, err := loadUsageOverviewEventsWithFilter(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewEventsWithFilter returned error: %v", err)
	}
	oracle := buildUsageOverviewFromEventsForTest(oracleEvents, filter, pricingByModel)

	overview, err := BuildUsageOverviewWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if !reflect.DeepEqual(overview.Health, oracle.Health) {
		t.Fatalf("health mismatch for non-aligned stats window\ngot:  %+v\nwant: %+v", overview.Health, oracle.Health)
	}
}

func TestBuildUsageOverviewWithFilterKeepsHourlyBucketsWhenShortWindowContainsCompleteDay(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-short-complete-day.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "hour-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 2, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "hour-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC), TotalTokens: 20},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 15, 15, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 16, 30, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end}
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		t.Fatalf("loadPriceSettingsByModel returned error: %v", err)
	}
	oracleEvents, err := loadUsageOverviewEventsWithFilter(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewEventsWithFilter returned error: %v", err)
	}
	oracle := buildUsageOverviewFromEventsForTest(oracleEvents, filter, pricingByModel)

	overview, err := BuildUsageOverviewWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if !reflect.DeepEqual(overview.Series, oracle.Series) {
		t.Fatalf("series mismatch for short window with complete day\ngot:  %+v\nwant: %+v", overview.Series, oracle.Series)
	}
	if !reflect.DeepEqual(overview.Usage.RequestsByHour, oracle.Usage.RequestsByHour) {
		t.Fatalf("snapshot hourly requests mismatch\ngot:  %+v\nwant: %+v", overview.Usage.RequestsByHour, oracle.Usage.RequestsByHour)
	}
}

func TestBuildUsageOverviewWithFilterKeepsHealthTotalsForFullQueryWindow(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-health-totals.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "old-success", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "recent-failure", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC), Failed: true, TotalTokens: 20},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{Range: "30d", StartTime: &start, EndTime: &end}
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		t.Fatalf("loadPriceSettingsByModel returned error: %v", err)
	}
	oracleEvents, err := loadUsageOverviewEventsWithFilter(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewEventsWithFilter returned error: %v", err)
	}
	oracle := buildUsageOverviewFromEventsForTest(oracleEvents, filter, pricingByModel)

	overview, err := BuildUsageOverviewWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Health.TotalSuccess != oracle.Health.TotalSuccess || overview.Health.TotalFailure != oracle.Health.TotalFailure || overview.Health.SuccessRate != oracle.Health.SuccessRate {
		t.Fatalf("health totals mismatch for full query window\ngot:  %+v\nwant: %+v", overview.Health, oracle.Health)
	}
}

func TestBuildUsageOverviewWithFilterUsesDailyStatsForCompleteDays(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-daily-stats-backed.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     0,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []entities.UsageEvent{
		{EventKey: "start-boundary", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 15, 15, 40, 0, 0, time.UTC), InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		{EventKey: "full-day-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 2, 0, 0, 0, time.UTC), InputTokens: 200, OutputTokens: 100, CachedTokens: 25, TotalTokens: 325},
		{EventKey: "full-day-2", APIGroupKey: "provider-b", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC), Failed: true, InputTokens: 300, OutputTokens: 150, ReasoningTokens: 40, TotalTokens: 490},
		{EventKey: "end-boundary", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 24, 16, 30, 0, 0, time.UTC), InputTokens: 400, OutputTokens: 200, TotalTokens: 600},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 16, 18, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 15, 15, 30, 0, 0, time.UTC)
	end := time.Date(2026, 4, 24, 17, 30, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end}
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		t.Fatalf("loadPriceSettingsByModel returned error: %v", err)
	}
	oracleEvents, err := loadUsageOverviewEventsWithFilter(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewEventsWithFilter returned error: %v", err)
	}
	oracle := buildUsageOverviewFromEventsForTest(oracleEvents, filter, pricingByModel)

	fullDayStart := time.Date(2026, 4, 16, 0, 0, 0, 0, time.Local)
	fullDayEnd := fullDayStart.Add(24 * time.Hour)
	if err := db.Where("timestamp >= ? AND timestamp < ?", timeutil.FormatStorageTime(fullDayStart), timeutil.FormatStorageTime(fullDayEnd)).Delete(&entities.UsageEvent{}).Error; err != nil {
		t.Fatalf("delete full-day usage_events returned error: %v", err)
	}
	overview, err := BuildUsageOverviewWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if !reflect.DeepEqual(overview.Summary, oracle.Summary) {
		t.Fatalf("summary mismatch after full-day hourly/raw data were removed\ngot:  %+v\nwant: %+v", overview.Summary, oracle.Summary)
	}
	if !reflect.DeepEqual(overview.Series, oracle.Series) {
		t.Fatalf("series mismatch after full-day hourly/raw data were removed\ngot:  %+v\nwant: %+v", overview.Series, oracle.Series)
	}
	if !reflect.DeepEqual(overview.DailySeries, oracle.DailySeries) {
		t.Fatalf("daily series mismatch after full-day hourly/raw data were removed\ngot:  %+v\nwant: %+v", overview.DailySeries, oracle.DailySeries)
	}
	if !reflect.DeepEqual(overview.Usage.RequestsByHour, oracle.Usage.RequestsByHour) || !reflect.DeepEqual(overview.Usage.TokensByHour, oracle.Usage.TokensByHour) {
		t.Fatalf("snapshot hourly maps mismatch after full-day raw data were removed\ngot:  %+v/%+v\nwant: %+v/%+v", overview.Usage.RequestsByHour, overview.Usage.TokensByHour, oracle.Usage.RequestsByHour, oracle.Usage.TokensByHour)
	}
}

func TestBuildUsageOverviewWithFilterComputesSummaryAndSeries(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []entities.UsageEvent{
		{
			EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 9, 15, 0, 0, time.UTC), Failed: false,
			InputTokens: 1000, OutputTokens: 500, ReasoningTokens: 100, CachedTokens: 200, TotalTokens: 1800,
		},
		{
			EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 10, 45, 0, 0, time.UTC), Failed: true,
			InputTokens: 2000, OutputTokens: 1000, ReasoningTokens: 50, CachedTokens: 100, TotalTokens: 3150,
		},
		{
			EventKey: "event-3", APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 17, 11, 5, 0, 0, time.UTC), Failed: false,
			InputTokens: 500, OutputTokens: 250, ReasoningTokens: 25, CachedTokens: 50, TotalTokens: 825,
		},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 17, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "7d", StartTime: &start, EndTime: &end})
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
	if math.Abs(overview.Summary.TotalCost-0.035805) > 0.000000001 {
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
	if math.Abs(overview.Series.Cost["2026-04-16"]-0.03069) > 0.000000001 || math.Abs(overview.Series.Cost["2026-04-17"]-0.005115) > 0.000000001 {
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
	location := time.Local
	if overview.Health.WindowStart != time.Date(2026, 4, 11, 8, 0, 0, 0, location) ||
		overview.Health.WindowEnd != time.Date(2026, 4, 18, 8, 0, 0, 0, location) {
		t.Fatalf("unexpected service health window: %+v", overview.Health)
	}
	if len(overview.Health.BlockDetails) != overview.Health.Rows*overview.Health.Columns {
		t.Fatalf("expected full service health grid, got %d blocks", len(overview.Health.BlockDetails))
	}
	firstBlock := overview.Health.BlockDetails[0]
	if firstBlock.StartTime != time.Date(2026, 4, 11, 8, 0, 0, 0, location) ||
		firstBlock.EndTime != time.Date(2026, 4, 11, 8, 15, 0, 0, location) ||
		firstBlock.Success != 0 || firstBlock.Failure != 0 || firstBlock.Rate != -1 {
		t.Fatalf("unexpected first health block: %+v", firstBlock)
	}
	populatedBlock := overview.Health.BlockDetails[517]
	if populatedBlock.StartTime != time.Date(2026, 4, 16, 17, 15, 0, 0, location) ||
		populatedBlock.EndTime != time.Date(2026, 4, 16, 17, 30, 0, 0, location) ||
		populatedBlock.Success != 1 || populatedBlock.Failure != 0 || populatedBlock.Rate != 1 {
		t.Fatalf("unexpected populated health block: %+v", populatedBlock)
	}
	failedBlock := overview.Health.BlockDetails[523]
	if failedBlock.StartTime != time.Date(2026, 4, 16, 18, 45, 0, 0, location) ||
		failedBlock.EndTime != time.Date(2026, 4, 16, 19, 0, 0, 0, location) ||
		failedBlock.Success != 0 || failedBlock.Failure != 1 || failedBlock.Rate != 0 {
		t.Fatalf("unexpected failed health block: %+v", failedBlock)
	}
	latestPopulatedBlock := overview.Health.BlockDetails[620]
	if latestPopulatedBlock.StartTime != time.Date(2026, 4, 17, 19, 0, 0, 0, location) ||
		latestPopulatedBlock.EndTime != time.Date(2026, 4, 17, 19, 15, 0, 0, location) ||
		latestPopulatedBlock.Success != 1 || latestPopulatedBlock.Failure != 0 || latestPopulatedBlock.Rate != 1 {
		t.Fatalf("unexpected latest populated health block: %+v", latestPopulatedBlock)
	}
}

func TestBuildUsageOverviewFromEventsBuildsSnapshotAndOverviewInOnePass(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	events := []entities.UsageEvent{
		{
			EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet",
			Timestamp: time.Date(2026, 4, 16, 9, 15, 0, 0, time.UTC), Failed: false,
			InputTokens: 1000, OutputTokens: 500, ReasoningTokens: 100, CachedTokens: 200, TotalTokens: 1800,
			Source: "source-a", AuthIndex: "1", LatencyMS: 120,
		},
		{
			EventKey: "event-2", APIGroupKey: "", Model: "",
			Timestamp: time.Date(2026, 4, 16, 10, 45, 0, 0, time.UTC), Failed: true,
			InputTokens: 2000, OutputTokens: 1000, ReasoningTokens: 50, CachedTokens: 100, TotalTokens: 3150,
			Source: " source-b ", AuthIndex: " 2 ", LatencyMS: 250,
		},
	}
	filterStart := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	filterEnd := time.Date(2026, 4, 16, 23, 59, 59, 999000000, time.UTC)
	filter := dto.UsageQueryFilter{Range: "24h", StartTime: &filterStart, EndTime: &filterEnd}
	pricingByModel := map[string]entities.ModelPriceSetting{
		"claude-sonnet": {
			Model:                "claude-sonnet",
			PromptPricePer1M:     3,
			CompletionPricePer1M: 15,
			CachePricePer1M:      0.3,
		},
	}

	overview := buildUsageOverviewFromEventsForTest(events, filter, pricingByModel)

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
		t.Fatalf("expected cost to be unavailable when any event model with billable tokens is unpriced, got %+v", overview.Summary)
	}
	if overview.Series.Requests["2026-04-16T17:00:00+08:00"] != 1 || overview.Series.Requests["2026-04-16T18:00:00+08:00"] != 1 {
		t.Fatalf("unexpected hourly request series: %+v", overview.Series.Requests)
	}
	if overview.HourlySeries.Models["claude-sonnet"].Requests["2026-04-16T17:00:00+08:00"] != 1 {
		t.Fatalf("expected claude-sonnet hourly model series, got %+v", overview.HourlySeries.Models)
	}
	if overview.HourlySeries.Models["unknown"].Tokens["2026-04-16T18:00:00+08:00"] != 3150 {
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
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "event-success", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 17, 9, 31, 0, 0, time.UTC), Failed: false, TotalTokens: 10},
		{EventKey: "event-failed", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 17, 23, 59, 0, 0, time.UTC), Failed: true, TotalTokens: 20},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 17, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "24h", StartTime: &start, EndTime: &end})
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

	var successBlock *dto.UsageOverviewHealthBlockRecord
	var failedBlock *dto.UsageOverviewHealthBlockRecord
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

func TestCalculateUsageEventCostDoesNotDoubleChargeReasoningTokens(t *testing.T) {
	event := entities.UsageEvent{
		InputTokens:     1_000_000,
		OutputTokens:    2_000_000,
		ReasoningTokens: 3_000_000,
		CachedTokens:    400_000,
		TotalTokens:     6_400_000,
	}
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     10,
		CompletionPricePer1M: 20,
		CachePricePer1M:      1,
	}

	cost := calculateUsageEventCost(event, pricing)

	if cost != 46.4 {
		t.Fatalf("expected reasoning tokens not to be added to completion cost, got %f", cost)
	}
}

func TestBuildUsageOverviewWithFilterReturnsUnavailableCostForPartialPricing(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-partial-pricing.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "priced-model",
		PromptPricePer1M:     1,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []entities.UsageEvent{
		{
			EventKey: "event-priced", APIGroupKey: "provider-a", Model: "priced-model",
			Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 1_000_000,
			InputTokens: 1_000_000,
		},
		{
			EventKey: "event-unpriced", APIGroupKey: "provider-a", Model: "unpriced-model",
			Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), TotalTokens: 1_000_000,
			InputTokens: 1_000_000,
		},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "24h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Summary.CostAvailable {
		t.Fatalf("expected cost to be unavailable when any in-range event model with billable tokens is unpriced, got %+v", overview.Summary)
	}
	if overview.Summary.TotalCost != 1 {
		t.Fatalf("expected priced portion to remain in total cost, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterReturnsAvailableCostWhenUnpricedEventsHaveNoBillableTokens(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-zero-token-unpriced.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "priced-model",
		PromptPricePer1M:     1,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []entities.UsageEvent{
		{
			EventKey: "event-priced", APIGroupKey: "provider-a", Model: "priced-model",
			Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), TotalTokens: 1_000_000,
			InputTokens: 1_000_000,
		},
		{
			EventKey: "event-zero-token", APIGroupKey: "provider-a", Model: "unpriced-image-model",
			Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
		},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "24h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if !overview.Summary.CostAvailable {
		t.Fatalf("expected zero-token unpriced model not to make cost unavailable, got %+v", overview.Summary)
	}
	if overview.Summary.TotalCost != 1 {
		t.Fatalf("expected priced event cost to remain available, got %+v", overview.Summary)
	}
}

func TestBuildUsageOverviewWithFilterReturnsUnavailableCostWithoutPricing(t *testing.T) {
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-no-pricing.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{{
		EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet",
		Timestamp: time.Date(2026, 4, 16, 9, 15, 0, 0, time.UTC), TotalTokens: 1800,
		InputTokens: 1000, OutputTokens: 500, CachedTokens: 200,
	}}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "24h", StartTime: &start, EndTime: &end})
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
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-preset-window.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

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
			expectBucketKey: "2026-04-17T20:00:00+08:00",
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
			event := entities.UsageEvent{
				EventKey:        "event-" + tc.rangeName,
				APIGroupKey:     "provider-a",
				Model:           "claude-sonnet",
				Timestamp:       tc.end,
				TotalTokens:     25,
				InputTokens:     10,
				OutputTokens:    15,
				ReasoningTokens: 0,
			}
			if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{event}); err != nil {
				t.Fatalf("InsertUsageEvents returned error: %v", err)
			}
			if err := AggregateUsageOverviewStats(context.Background(), db, tc.end); err != nil {
				t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
			}

			overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: tc.rangeName, StartTime: &tc.start, EndTime: &tc.end})
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
		for _, table := range []string{"usage_events", "usage_overview_hourly_stats", "usage_overview_daily_stats", "usage_overview_health_stats", "usage_overview_aggregation_checkpoints"} {
			if err := db.Exec("DELETE FROM " + table).Error; err != nil {
				t.Fatalf("DELETE %s returned error: %v", table, err)
			}
		}
	}
}

func TestBuildUsageOverviewWithFilterBuildsLatestHourlySeriesForLongRanges(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-hourly-series.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
		Model:                "claude-sonnet",
		PromptPricePer1M:     1,
		CompletionPricePer1M: 0,
		CachePricePer1M:      0,
	}); err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}

	events := []entities.UsageEvent{
		{EventKey: "event-old", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 17, 8, 0, 0, 0, time.UTC), TotalTokens: 1_000_000, InputTokens: 1_000_000},
		{EventKey: "event-latest-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 23, 22, 15, 0, 0, time.UTC), TotalTokens: 2_000_000, InputTokens: 2_000_000, OutputTokens: 5, CachedTokens: 7, ReasoningTokens: 11},
		{EventKey: "event-latest-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 23, 23, 45, 0, 0, time.UTC), TotalTokens: 3_000_000, InputTokens: 3_000_000, OutputTokens: 13, CachedTokens: 17, ReasoningTokens: 19},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 23, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "7d", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if len(overview.Series.Requests) != 2 || overview.Series.Requests["2026-04-17"] != 1 || overview.Series.Requests["2026-04-24"] != 2 {
		t.Fatalf("expected main overview series to remain daily for 7d, got %+v", overview.Series.Requests)
	}
	if _, ok := overview.HourlySeries.Requests["2026-04-17T16:00:00+08:00"]; ok {
		t.Fatalf("expected latest hourly series to exclude buckets before the latest 24 hours, got %+v", overview.HourlySeries.Requests)
	}
	if overview.HourlySeries.Requests["2026-04-24T06:00:00+08:00"] != 1 || overview.HourlySeries.Requests["2026-04-24T07:00:00+08:00"] != 1 {
		t.Fatalf("unexpected latest hourly request series: %+v", overview.HourlySeries.Requests)
	}
	if overview.HourlySeries.Cost["2026-04-24T06:00:00+08:00"] != 1.999993 || overview.HourlySeries.Cost["2026-04-24T07:00:00+08:00"] != 2.999983 {
		t.Fatalf("unexpected latest hourly cost series: %+v", overview.HourlySeries.Cost)
	}
	if overview.HourlySeries.InputTokens["2026-04-24T06:00:00+08:00"] != 2_000_000 || overview.HourlySeries.OutputTokens["2026-04-24T07:00:00+08:00"] != 13 {
		t.Fatalf("unexpected latest hourly token category series: %+v", overview.HourlySeries)
	}
	if overview.DailySeries.Requests["2026-04-17"] != 1 || overview.DailySeries.Requests["2026-04-24"] != 2 {
		t.Fatalf("unexpected daily request series: %+v", overview.DailySeries.Requests)
	}
}

func TestBuildUsageOverviewWithFilterUsesDailyBucketsForLongCustomRanges(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-custom-buckets.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 20, 8, 0, 0, 0, time.UTC), TotalTokens: 10},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 26, 18, 0, 0, 0, time.UTC), TotalTokens: 20},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}

	start := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 26, 23, 59, 59, 999000000, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if overview.Summary.WindowMinutes != 10080 {
		t.Fatalf("expected 10080 minute window, got %+v", overview.Summary)
	}
	if len(overview.Series.Requests) != 2 {
		t.Fatalf("expected daily buckets for long custom range, got %+v", overview.Series.Requests)
	}
	if overview.Series.Requests["2026-04-20"] != 1 || overview.Series.Requests["2026-04-27"] != 1 {
		t.Fatalf("expected daily request buckets, got %+v", overview.Series.Requests)
	}
	if _, ok := overview.Series.Requests["2026-04-20T08:00:00Z"]; ok {
		t.Fatalf("expected long custom range not to keep hourly buckets, got %+v", overview.Series.Requests)
	}
}
