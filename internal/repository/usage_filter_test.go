package repository

import (
	"context"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository/dto"
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
	overview := newUsageOverviewRecord(filter, windowMinutes)
	costResolver := &UsageCostResolver{pricesByModel: pricingByModel}
	for _, event := range events {
		applyUsageEventToOverviewSnapshot(overview.Usage, event)
		applyUsageEventToOverview(overview, event, bucketByDay, costResolver)
	}
	finalizeUsageOverview(overview)
	return overview
}

func loadPriceSettingsByModel(db *gorm.DB) (map[string]entities.ModelPriceSetting, error) {
	settings, err := ListModelPriceSettings(db)
	if err != nil {
		return nil, err
	}
	result := make(map[string]entities.ModelPriceSetting, len(settings))
	for _, setting := range settings {
		result[strings.TrimSpace(setting.Model)] = setting
	}
	return result, nil
}

func loadUsageOverviewOracleEventsForTest(db *gorm.DB, filter dto.UsageQueryFilter) ([]entities.UsageEvent, error) {
	query := applyUsageOverviewQuery(db.Model(&entities.UsageEvent{}), filter).Select(usageEventProjectionColumns).Order("timestamp asc")
	var rows []usageEventProjection
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]entities.UsageEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, usageEventProjectionToEntity(row))
	}
	return events, nil
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

func TestLoadUsageOverviewRawEventWindowsUsesSeparateRangeQueries(t *testing.T) {
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
	windows := []usageOverviewRawEventWindow{
		{start: start, end: fullStart},
		{start: fullEnd, end: end, includeEnd: true},
	}
	if _, err := loadUsageOverviewRawEventWindowsWithFilter(db, filter, windows, nil); err != nil {
		t.Fatalf("loadUsageOverviewRawEventWindowsWithFilter returned error: %v", err)
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

func TestBuildUsageOverviewWithFilterIncludesEndBoundaryWhenNoFullHour(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-end-boundary-no-full-hour.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	start := time.Date(2026, 4, 16, 9, 20, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 9, 40, 0, 0, time.UTC)
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "end-boundary", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: end, InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	if overview.Usage.TotalRequests != 1 || overview.Summary.RequestCount != 1 || overview.Usage.TotalTokens != 15 {
		t.Fatalf("expected end boundary event to be included, got usage=%+v summary=%+v", overview.Usage, overview.Summary)
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
		t.Fatalf("expected two main boundary usage_events queries, got %d: %+v", len(usageEventQueries), usageEventQueries)
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
		{EventKey: "full-hour-1", APIGroupKey: "provider-a", Model: "claude-sonnet", ModelAlias: stringPtr("sonnet-alias-a"), AuthIndex: "auth-a", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), InputTokens: 2000, OutputTokens: 1000, ReasoningTokens: 50, CachedTokens: 100, TotalTokens: 3150},
		{EventKey: "full-hour-2", APIGroupKey: "provider-a", Model: "claude-sonnet", ModelAlias: stringPtr("sonnet-alias-b"), AuthIndex: "auth-b", Timestamp: time.Date(2026, 4, 16, 10, 50, 0, 0, time.UTC), Failed: true, InputTokens: 500, OutputTokens: 250, ReasoningTokens: 25, CachedTokens: 50, TotalTokens: 825},
		{EventKey: "full-hour-3", APIGroupKey: "provider-b", Model: "claude-sonnet", ModelAlias: stringPtr("sonnet-alias-a"), AuthIndex: "auth-c", Timestamp: time.Date(2026, 4, 16, 11, 30, 0, 0, time.UTC), InputTokens: 700, OutputTokens: 300, ReasoningTokens: 30, CachedTokens: 70, TotalTokens: 1100},
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
	oracleEvents, err := loadUsageOverviewOracleEventsForTest(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewOracleEventsForTest returned error: %v", err)
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
	oracleEvents, err := loadUsageOverviewOracleEventsForTest(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewOracleEventsForTest returned error: %v", err)
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
	oracleEvents, err := loadUsageOverviewOracleEventsForTest(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewOracleEventsForTest returned error: %v", err)
	}
	oracle := buildUsageOverviewFromEventsForTest(oracleEvents, filter, pricingByModel)

	overview, err := BuildUsageOverviewWithFilter(db, filter)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	if !reflect.DeepEqual(overview.Series, oracle.Series) {
		t.Fatalf("series mismatch for short window with complete day\ngot:  %+v\nwant: %+v", overview.Series, oracle.Series)
	}
	if !reflect.DeepEqual(overview.Usage, oracle.Usage) {
		t.Fatalf("usage totals mismatch for short window with complete day\ngot:  %+v\nwant: %+v", overview.Usage, oracle.Usage)
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
	oracleEvents, err := loadUsageOverviewOracleEventsForTest(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewOracleEventsForTest returned error: %v", err)
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
	oracleEvents, err := loadUsageOverviewOracleEventsForTest(db, filter)
	if err != nil {
		t.Fatalf("loadUsageOverviewOracleEventsForTest returned error: %v", err)
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
	if !reflect.DeepEqual(overview.Usage, oracle.Usage) {
		t.Fatalf("usage totals mismatch after full-day hourly/raw data were removed\ngot:  %+v\nwant: %+v", overview.Usage, oracle.Usage)
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
	if overview.Summary.InputTokens != 3500 || overview.Summary.CachedTokens != 350 || overview.Summary.ReasoningTokens != 175 {
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
	if overview.Series.CacheRate["2026-04-16"] == nil || math.Abs(*overview.Series.CacheRate["2026-04-16"]-10) > 0.000000001 ||
		overview.Series.CacheRate["2026-04-17"] == nil || math.Abs(*overview.Series.CacheRate["2026-04-17"]-10) > 0.000000001 {
		t.Fatalf("unexpected cache-rate series: %+v", overview.Series.CacheRate)
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
	if overview.Summary.RequestCount != 2 || overview.Summary.TokenCount != 4950 {
		t.Fatalf("unexpected summary totals: %+v", overview.Summary)
	}
	if overview.Summary.InputTokens != 3000 || overview.Summary.CachedTokens != 300 || overview.Summary.ReasoningTokens != 150 {
		t.Fatalf("unexpected summary token breakdown: %+v", overview.Summary)
	}
	if overview.Summary.CostAvailable {
		t.Fatalf("expected cost to be unavailable when any event model with billable tokens is unpriced, got %+v", overview.Summary)
	}
	if overview.Series.Requests["2026-04-16T17:00:00+08:00"] != 1 || overview.Series.Requests["2026-04-16T18:00:00+08:00"] != 1 {
		t.Fatalf("unexpected hourly request series: %+v", overview.Series.Requests)
	}
	if overview.Series.CacheRate["2026-04-16T17:00:00+08:00"] == nil || math.Abs(*overview.Series.CacheRate["2026-04-16T17:00:00+08:00"]-20) > 0.000000001 ||
		overview.Series.CacheRate["2026-04-16T18:00:00+08:00"] == nil || math.Abs(*overview.Series.CacheRate["2026-04-16T18:00:00+08:00"]-5) > 0.000000001 {
		t.Fatalf("unexpected hourly cache-rate series: %+v", overview.Series.CacheRate)
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

func TestBuildUsageOverviewWithFilterKeepsCalendarDayHealthWindow(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")

	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-health-calendar-day.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	location := time.Local
	queryNow := time.Date(2026, 6, 22, 15, 30, 0, 0, location)
	todayStart := time.Date(2026, 6, 22, 0, 0, 0, 0, location)
	todayEnd := todayStart.AddDate(0, 0, 1).Add(-time.Nanosecond)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	yesterdayEnd := todayStart.Add(-time.Nanosecond)

	for _, tc := range []struct {
		name        string
		rangeName   string
		start       time.Time
		end         time.Time
		wantStart   time.Time
		wantEnd     time.Time
		wantMinutes int64
	}{
		{
			name:        "today keeps midnight to next midnight after future end clamp",
			rangeName:   "today",
			start:       todayStart,
			end:         todayEnd,
			wantStart:   todayStart,
			wantEnd:     todayStart.AddDate(0, 0, 1),
			wantMinutes: int64(queryNow.Sub(todayStart) / time.Minute),
		},
		{
			name:        "yesterday keeps previous midnight to current midnight",
			rangeName:   "yesterday",
			start:       yesterdayStart,
			end:         yesterdayEnd,
			wantStart:   yesterdayStart,
			wantEnd:     todayStart,
			wantMinutes: 24 * 60,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overview, err := BuildUsageOverviewWithFilterAndRecentCache(db, dto.UsageQueryFilter{
				Range:     tc.rangeName,
				StartTime: &tc.start,
				EndTime:   &tc.end,
				QueryNow:  &queryNow,
			}, nil)
			if err != nil {
				t.Fatalf("BuildUsageOverviewWithFilterAndRecentCache returned error: %v", err)
			}
			if !overview.Health.WindowStart.Equal(tc.wantStart) || !overview.Health.WindowEnd.Equal(tc.wantEnd) {
				t.Fatalf("expected %s health window %s - %s, got %s - %s", tc.rangeName, tc.wantStart, tc.wantEnd, overview.Health.WindowStart, overview.Health.WindowEnd)
			}
			if overview.Summary.WindowMinutes != tc.wantMinutes {
				t.Fatalf("expected %s query window minutes %d, got %+v", tc.rangeName, tc.wantMinutes, overview.Summary)
			}
		})
	}
}

func TestCalculateUsageTokenCostBreakdownDoesNotDoubleChargeReasoningTokens(t *testing.T) {
	input := helper.UsageTokenCostInput{
		InputTokens:  1_000_000,
		OutputTokens: 2_000_000,
		CachedTokens: 400_000,
	}
	pricing := entities.ModelPriceSetting{
		PromptPricePer1M:     10,
		CompletionPricePer1M: 20,
		CachePricePer1M:      1,
	}

	cost := helper.CalculateUsageTokenCostBreakdown(input, pricing).TotalCostUSD

	if math.Abs(cost-46.4) > 0.000000001 {
		t.Fatalf("expected reasoning tokens not to be added to completion cost, got %f", cost)
	}
}

func TestBuildUsageOverviewCalculatesClaudeCacheReadAndCreationCost(t *testing.T) {
	start := time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	filter := dto.UsageQueryFilter{
		StartTime: &start,
		EndTime:   &end,
	}
	event := entities.UsageEvent{
		EventKey:            "claude-cache-cost",
		APIGroupKey:         "provider-a",
		Model:               "claude-sonnet",
		Timestamp:           time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC),
		InputTokens:         1_300_000,
		OutputTokens:        500_000,
		CachedTokens:        200_000,
		CacheReadTokens:     200_000,
		CacheCreationTokens: 100_000,
		TotalTokens:         1_800_000,
	}
	overview := buildUsageOverviewFromEventsForTest([]entities.UsageEvent{event}, filter, map[string]entities.ModelPriceSetting{
		"claude-sonnet": {
			PricingStyle:            entities.ModelPricingStyleClaude,
			PromptPricePer1M:        10,
			CompletionPricePer1M:    20,
			CachePricePer1M:         1,
			CacheCreationPricePer1M: 12.5,
		},
	})

	wantCost := 1.0*10 + 0.5*20 + 0.2*1 + 0.1*12.5
	if math.Abs(overview.Summary.TotalCost-wantCost) > 0.000000001 {
		t.Fatalf("expected Claude cache read/write cost %.8f, got %.8f", wantCost, overview.Summary.TotalCost)
	}
	if !overview.Summary.CostAvailable {
		t.Fatalf("expected cost to be available, got %+v", overview.Summary)
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
			if tc.rangeName == "24h" {
				if overview.Summary.DailyAverageRequests != nil || overview.Summary.DailyAverageTokens != nil || overview.Summary.DailyAverageCost != nil || overview.Summary.DailyAverageRangeDays != nil {
					t.Fatalf("expected 24h overview not to expose daily averages, got %+v", overview.Summary)
				}
			}
			if tc.rangeName == "7d" {
				assertFloat64PtrClose(t, overview.Summary.DailyAverageRequests, 1.0/7.0)
				assertFloat64PtrClose(t, overview.Summary.DailyAverageTokens, 25.0/7.0)
				assertFloat64PtrClose(t, overview.Summary.DailyAverageCost, overview.Summary.TotalCost/7.0)
				assertFloat64PtrClose(t, overview.Summary.DailyAverageRangeDays, 7.0)
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

func TestFinalizeUsageOverviewDailyAverageCustomWindowBoundaries(t *testing.T) {
	start := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name          string
		end           time.Time
		expectAverage bool
		expectDays    float64
	}{
		{
			name:          "custom one day hides daily averages",
			end:           start.Add(24 * time.Hour),
			expectAverage: false,
		},
		{
			name:          "custom two days exposes daily averages",
			end:           start.Add(48 * time.Hour),
			expectAverage: true,
			expectDays:    2,
		},
		{
			name:          "custom slightly over one day exposes daily averages",
			end:           start.Add(24*time.Hour + time.Minute),
			expectAverage: true,
			expectDays:    1441.0 / 1440.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			overview := buildUsageOverviewFromEventsForTest([]entities.UsageEvent{{
				EventKey:    "event-" + strings.ReplaceAll(tc.name, " ", "-"),
				APIGroupKey: "provider-a",
				Model:       "claude-sonnet",
				Timestamp:   tc.end,
				TotalTokens: 1440,
				InputTokens: 1440,
			}}, dto.UsageQueryFilter{Range: "custom", StartTime: &start, EndTime: &tc.end}, nil)

			if !tc.expectAverage {
				if overview.Summary.DailyAverageRequests != nil || overview.Summary.DailyAverageTokens != nil || overview.Summary.DailyAverageCost != nil || overview.Summary.DailyAverageRangeDays != nil {
					t.Fatalf("expected custom one-day overview not to expose daily averages, got %+v", overview.Summary)
				}
				return
			}

			assertFloat64PtrClose(t, overview.Summary.DailyAverageRangeDays, tc.expectDays)
			assertFloat64PtrClose(t, overview.Summary.DailyAverageRequests, 1.0/tc.expectDays)
			assertFloat64PtrClose(t, overview.Summary.DailyAverageTokens, 1440.0/tc.expectDays)
			assertFloat64PtrClose(t, overview.Summary.DailyAverageCost, 0)
		})
	}
}

func assertFloat64PtrClose(t *testing.T, actual *float64, expected float64) {
	t.Helper()
	if actual == nil {
		t.Fatalf("expected %.8f, got nil", expected)
	}
	if diff := math.Abs(*actual - expected); diff > 0.0000001 {
		t.Fatalf("expected %.8f, got %.8f", expected, *actual)
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

func TestBuildUsageOverviewRealtimeWithFilterBuildsRealtimeBlockFromRecentCache(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-realtime.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "gpt-5", PromptPricePer1M: 1, CompletionPricePer1M: 1, CachePricePer1M: 0.5}); err != nil {
		t.Fatalf("UpsertModelPriceSetting gpt-5 returned error: %v", err)
	}
	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "claude-sonnet", PromptPricePer1M: 1, CompletionPricePer1M: 1, CachePricePer1M: 0.5}); err != nil {
		t.Fatalf("UpsertModelPriceSetting claude returned error: %v", err)
	}

	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	ttft100 := int64(100)
	ttft200 := int64(200)
	ttftZero := int64(0)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{
		{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", AuthIndex: "auth-file-1", Timestamp: now.Add(-16 * time.Minute), InputTokens: 900, TotalTokens: 900},
		{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", AuthIndex: "auth-file-1", Timestamp: now.Add(-4*time.Minute - 50*time.Second), InputTokens: 100, OutputTokens: 60, CachedTokens: 20, TotalTokens: 120, LatencyMS: 500, TTFTMS: &ttft100},
		{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", AuthIndex: "auth-file-1", Timestamp: now.Add(-4*time.Minute - 45*time.Second), InputTokens: 50, OutputTokens: 40, CachedTokens: 5, TotalTokens: 80, LatencyMS: 700, TTFTMS: &ttft200},
		{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", AuthIndex: "auth-file-1", Timestamp: now.Add(-4*time.Minute - 40*time.Second), InputTokens: 10, OutputTokens: 10, TotalTokens: 20, LatencyMS: 650, TTFTMS: &ttftZero},
		{APIGroupKey: "provider-a", Model: "gpt-5", AuthType: "oauth", AuthIndex: "auth-file-1", Timestamp: now.Add(-4*time.Minute - 30*time.Second), Failed: true, InputTokens: 1000, TotalTokens: 1000, LatencyMS: 900},
		{APIGroupKey: "provider-a", Model: "claude-sonnet", AuthType: "apikey", Provider: "OpenAI Provider", AuthIndex: "provider-1", Timestamp: now.Add(-20 * time.Second), InputTokens: 100, OutputTokens: 25, TotalTokens: 50, LatencyMS: 300},
		{APIGroupKey: "provider-b", Model: "gpt-5", AuthType: "oauth", AuthIndex: "auth-file-2", Timestamp: now.Add(-10 * time.Second), InputTokens: 700, TotalTokens: 700, LatencyMS: 100},
	})
	if err := db.Create([]entities.UsageIdentity{
		{Name: "Claude Account", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-file-1", Type: "claude", Provider: "Claude", CreatedAt: now, UpdatedAt: now},
		{Name: "OpenAI Provider", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "provider-1", Type: "openai", Provider: "OpenAI", CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("create usage identities returned error: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events returned error: %v", err)
	}

	realtime, err := BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		APIGroupKey:     "provider-a",
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	}, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache returned error: %v", err)
	}

	if realtime.Window != "15m" || realtime.BucketSeconds != 30 {
		t.Fatalf("unexpected realtime metadata: %+v", realtime)
	}
	if !realtime.WindowStart.Equal(now.Add(-15*time.Minute)) || !realtime.WindowEnd.Equal(now) {
		t.Fatalf("expected realtime window bounds to match the selected range, got start=%s end=%s", realtime.WindowStart, realtime.WindowEnd)
	}
	if len(realtime.TokenVelocity) != 30 || len(realtime.ResponseLevel) != 30 || len(realtime.RequestLevel) != 30 || len(realtime.CacheLevel) != 30 {
		t.Fatalf("expected 30 realtime buckets, got token=%d response=%d request=%d cache=%d", len(realtime.TokenVelocity), len(realtime.ResponseLevel), len(realtime.RequestLevel), len(realtime.CacheLevel))
	}

	firstUsageBucket := realtime.TokenVelocity[20]
	if firstUsageBucket.Bucket != "2026-06-09T11:55:00Z" || firstUsageBucket.Tokens != 220 || math.Abs(firstUsageBucket.TokensPerMinute-(220.0/3.0)) > 0.000000001 {
		t.Fatalf("unexpected first token velocity bucket: %+v", firstUsageBucket)
	}
	carriedUsageBucket := realtime.TokenVelocity[25]
	if carriedUsageBucket.Tokens != 220 || math.Abs(carriedUsageBucket.TokensPerMinute-(220.0/3.0)) > 0.000000001 {
		t.Fatalf("expected token velocity to carry over the 3m sliding window, got %+v", carriedUsageBucket)
	}
	expiredUsageBucket := realtime.TokenVelocity[26]
	if expiredUsageBucket.Tokens != 0 || expiredUsageBucket.TokensPerMinute != 0 {
		t.Fatalf("expected token velocity to expire after the 3m sliding window, got %+v", expiredUsageBucket)
	}
	if realtime.ResponseLevel[21].LatencyP95MS == nil || *realtime.ResponseLevel[21].LatencyP95MS != 900 ||
		realtime.ResponseLevel[21].TTFTP95MS == nil || *realtime.ResponseLevel[21].TTFTP95MS != 200 {
		t.Fatalf("expected response level to carry over the sliding window, got %+v", realtime.ResponseLevel[21])
	}
	if realtime.ResponseLevel[26].LatencyP95MS == nil || *realtime.ResponseLevel[26].LatencyP95MS != 900 || realtime.ResponseLevel[26].TTFTP95MS != nil {
		t.Fatalf("expected failed request latency to remain visible without token TTFT, got %+v", realtime.ResponseLevel[26])
	}
	if realtime.ResponseLevel[27].LatencyP95MS != nil || realtime.ResponseLevel[27].TTFTP95MS != nil {
		t.Fatalf("expected response level to expire after the sliding window, got %+v", realtime.ResponseLevel[27])
	}
	if len(realtime.ResponseDistribution.TTFT.AverageLine) != 30 || len(realtime.ResponseDistribution.Latency.AverageLine) != 30 {
		t.Fatalf("expected response distribution average lines to use the realtime buckets, got ttft=%d latency=%d", len(realtime.ResponseDistribution.TTFT.AverageLine), len(realtime.ResponseDistribution.Latency.AverageLine))
	}
	if realtime.ResponseDistribution.TTFT.AverageLine[21].AvgMS == nil || math.Abs(*realtime.ResponseDistribution.TTFT.AverageLine[21].AvgMS-150) > 0.000000001 {
		t.Fatalf("expected ttft average line to use sliding samples, got %+v", realtime.ResponseDistribution.TTFT.AverageLine[21])
	}
	if realtime.ResponseDistribution.Latency.AverageLine[21].AvgMS == nil || math.Abs(*realtime.ResponseDistribution.Latency.AverageLine[21].AvgMS-687.5) > 0.000000001 {
		t.Fatalf("expected latency average line to include failed request latency, got %+v", realtime.ResponseDistribution.Latency.AverageLine[21])
	}
	if realtime.ResponseDistribution.TTFT.AverageLine[26].AvgMS != nil ||
		realtime.ResponseDistribution.Latency.AverageLine[26].AvgMS == nil || math.Abs(*realtime.ResponseDistribution.Latency.AverageLine[26].AvgMS-900) > 0.000000001 {
		t.Fatalf("expected failed request latency distribution without ttft after sliding carry, got ttft=%+v latency=%+v", realtime.ResponseDistribution.TTFT.AverageLine[26], realtime.ResponseDistribution.Latency.AverageLine[26])
	}
	if len(realtime.ResponseDistribution.TTFT.Particles) != 2 {
		t.Fatalf("expected response distribution TTFT particles to map one usage event to one point, got %+v", realtime.ResponseDistribution.TTFT.Particles)
	}
	assertRealtimeParticleCore(t, realtime.ResponseDistribution.TTFT.Particles[0], "2026-06-09T11:55:00Z", 100, 1)
	assertRealtimeParticleCore(t, realtime.ResponseDistribution.TTFT.Particles[1], "2026-06-09T11:55:00Z", 200, 1)
	assertRealtimeParticleTimestamp(t, realtime.ResponseDistribution.TTFT.Particles[0], "2026-06-09T11:55:10Z")
	assertRealtimeParticleTimestamp(t, realtime.ResponseDistribution.TTFT.Particles[1], "2026-06-09T11:55:15Z")
	if len(realtime.ResponseDistribution.Latency.Particles) != 5 {
		t.Fatalf("expected response distribution latency particles to map one usage event to one point, got %+v", realtime.ResponseDistribution.Latency.Particles)
	}
	assertRealtimeParticleCore(t, realtime.ResponseDistribution.Latency.Particles[0], "2026-06-09T11:55:00Z", 500, 1)
	assertRealtimeParticleCore(t, realtime.ResponseDistribution.Latency.Particles[1], "2026-06-09T11:55:00Z", 700, 1)
	assertRealtimeParticleCore(t, realtime.ResponseDistribution.Latency.Particles[2], "2026-06-09T11:55:00Z", 650, 1)
	assertRealtimeParticleCore(t, realtime.ResponseDistribution.Latency.Particles[3], "2026-06-09T11:55:30Z", 900, 1)
	assertRealtimeParticleCore(t, realtime.ResponseDistribution.Latency.Particles[4], "2026-06-09T11:59:30Z", 300, 1)
	assertRealtimeParticleTimestamp(t, realtime.ResponseDistribution.Latency.Particles[0], "2026-06-09T11:55:10Z")
	assertRealtimeParticleTimestamp(t, realtime.ResponseDistribution.Latency.Particles[1], "2026-06-09T11:55:15Z")
	assertRealtimeParticleTimestamp(t, realtime.ResponseDistribution.Latency.Particles[2], "2026-06-09T11:55:20Z")
	assertRealtimeParticleTimestamp(t, realtime.ResponseDistribution.Latency.Particles[3], "2026-06-09T11:55:30Z")
	assertRealtimeParticleTimestamp(t, realtime.ResponseDistribution.Latency.Particles[4], "2026-06-09T11:59:40Z")
	if realtime.RequestLevel[21].Requests != 4 || math.Abs(realtime.RequestLevel[21].RequestsPerMinute-(4.0/3.0)) > 0.000000001 {
		t.Fatalf("expected request level to use the 3m sliding window, got %+v", realtime.RequestLevel[21])
	}
	if realtime.RequestLevel[26].Requests != 1 || math.Abs(realtime.RequestLevel[26].RequestsPerMinute-(1.0/3.0)) > 0.000000001 {
		t.Fatalf("expected failed request to remain visible inside the sliding window, got %+v", realtime.RequestLevel[26])
	}
	if realtime.CacheLevel[20].InputTokens != 160 || realtime.CacheLevel[20].CachedTokens != 25 ||
		realtime.CacheLevel[20].CacheRate == nil || math.Abs(*realtime.CacheLevel[20].CacheRate-(25.0/160.0)*100) > 0.000000001 {
		t.Fatalf("unexpected cache level bucket: %+v", realtime.CacheLevel[20])
	}
	if realtime.CacheLevel[25].InputTokens != 160 || realtime.CacheLevel[25].CachedTokens != 25 ||
		realtime.CacheLevel[25].CacheRate == nil || math.Abs(*realtime.CacheLevel[25].CacheRate-(25.0/160.0)*100) > 0.000000001 {
		t.Fatalf("expected cache level to carry over the sliding window, got %+v", realtime.CacheLevel[25])
	}
	if realtime.CacheLevel[26].CacheRate != nil || realtime.CacheLevel[26].InputTokens != 0 || realtime.CacheLevel[26].CachedTokens != 0 {
		t.Fatalf("expected cache level to expire after the sliding window, got %+v", realtime.CacheLevel[26])
	}

	if len(realtime.CurrentUsage.Models) != 2 ||
		realtime.CurrentUsage.Models[0].Key != "gpt-5" ||
		realtime.CurrentUsage.Models[0].Tokens != 220 ||
		math.Abs(realtime.CurrentUsage.Models[0].Share-(220.0/270.0)*100) > 0.000000001 {
		t.Fatalf("unexpected realtime model usage: %+v", realtime.CurrentUsage.Models)
	}
	if len(realtime.CurrentUsage.APIKeys) != 1 ||
		realtime.CurrentUsage.APIKeys[0].Key != "provider-a" ||
		realtime.CurrentUsage.APIKeys[0].Requests != 5 ||
		realtime.CurrentUsage.APIKeys[0].Tokens != 270 {
		t.Fatalf("unexpected realtime api key usage: %+v", realtime.CurrentUsage.APIKeys)
	}
	if len(realtime.CurrentUsage.AuthFiles) != 1 ||
		realtime.CurrentUsage.AuthFiles[0].Label != "Claude Account" ||
		realtime.CurrentUsage.AuthFiles[0].Tokens != 220 {
		t.Fatalf("unexpected realtime auth file usage: %+v", realtime.CurrentUsage.AuthFiles)
	}
	if len(realtime.CurrentUsage.AIProviders) != 1 ||
		realtime.CurrentUsage.AIProviders[0].Label != "OpenAI Provider" ||
		realtime.CurrentUsage.AIProviders[0].Tokens != 50 {
		t.Fatalf("unexpected realtime ai provider usage: %+v", realtime.CurrentUsage.AIProviders)
	}
}

func TestBuildUsageOverviewRealtimeWithFilterCapsResponseDistributionParticles(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-realtime-particle-cap.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-60 * time.Minute)
	const sampleCount = 1205
	events := make([]entities.UsageEvent, 0, sampleCount)
	for index := 0; index < sampleCount; index++ {
		ttft := int64(80 + index%40)
		events = append(events, entities.UsageEvent{
			APIGroupKey: "provider-a",
			Model:       "gpt-5",
			Timestamp:   windowStart.Add(time.Duration(index) * 2 * time.Second),
			LatencyMS:   int64(300 + index%200),
			TTFTMS:      &ttft,
		})
	}
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents(events)
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events returned error: %v", err)
	}

	realtime, err := BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		APIGroupKey:     "provider-a",
		RealtimeWindow:  "60m",
		RealtimeEndTime: &now,
	}, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache returned error: %v", err)
	}

	assertRealtimeDistributionParticleCap(t, realtime.ResponseDistribution.TTFT, sampleCount)
	assertRealtimeDistributionParticleCap(t, realtime.ResponseDistribution.Latency, sampleCount)
}

func TestUsageOverviewRealtimeDistributionParticleRangeUsesWideArithmetic(t *testing.T) {
	start, end := usageOverviewRealtimeDistributionParticleRange(999, 2_200_000, 1000)
	if start != 2_197_800 || end != 2_200_000 {
		t.Fatalf("expected wide particle range arithmetic, got start=%d end=%d", start, end)
	}
}

func assertRealtimeDistributionParticleCap(t *testing.T, series dto.RealtimeResponseDistributionSeriesRecord, totalSamples int64) {
	t.Helper()
	if len(series.Particles) > 1000 {
		t.Fatalf("expected response distribution particles to be capped at 1000, got %d", len(series.Particles))
	}
	if got := sumRealtimeParticleCounts(series.Particles); got != totalSamples {
		t.Fatalf("expected sampled particle counts to preserve %d real samples, got %d", totalSamples, got)
	}
	var merged bool
	for _, particle := range series.Particles {
		if particle.Count > 1 {
			merged = true
			break
		}
	}
	if !merged {
		t.Fatalf("expected capped response distribution to merge at least one particle, got %+v", series.Particles)
	}
	assertRealtimeDistributionSamplingMetadata(t, series, totalSamples, true, 1000)
}

func sumRealtimeParticleCounts(particles []dto.RealtimeResponseParticleRecord) int64 {
	var total int64
	for _, particle := range particles {
		total += particle.Count
	}
	return total
}

func assertRealtimeDistributionSamplingMetadata(t *testing.T, series dto.RealtimeResponseDistributionSeriesRecord, totalParticles int64, sampled bool, maxParticles int64) {
	t.Helper()
	value := reflect.ValueOf(series)
	assertRealtimeDistributionIntField(t, value, "TotalParticles", totalParticles)
	assertRealtimeDistributionBoolField(t, value, "Sampled", sampled)
	assertRealtimeDistributionIntField(t, value, "MaxParticles", maxParticles)
}

func assertRealtimeDistributionIntField(t *testing.T, value reflect.Value, name string, expected int64) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("expected response distribution series to carry %s metadata", name)
	}
	if field.Kind() != reflect.Int && field.Kind() != reflect.Int64 {
		t.Fatalf("expected %s metadata to be integer, got %s", name, field.Kind())
	}
	if got := field.Int(); got != expected {
		t.Fatalf("expected %s metadata %d, got %d", name, expected, got)
	}
}

func assertRealtimeDistributionBoolField(t *testing.T, value reflect.Value, name string, expected bool) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("expected response distribution series to carry %s metadata", name)
	}
	if field.Kind() != reflect.Bool {
		t.Fatalf("expected %s metadata to be bool, got %s", name, field.Kind())
	}
	if got := field.Bool(); got != expected {
		t.Fatalf("expected %s metadata %t, got %t", name, expected, got)
	}
}

func assertRealtimeParticleCore(t *testing.T, particle dto.RealtimeResponseParticleRecord, bucket string, ms, count int64) {
	t.Helper()
	if particle.Bucket != bucket || particle.MS != ms || particle.Count != count {
		t.Fatalf("unexpected response distribution particle core fields: got %+v want bucket=%s ms=%d count=%d", particle, bucket, ms, count)
	}
}

func assertRealtimeParticleTimestamp(t *testing.T, particle dto.RealtimeResponseParticleRecord, expected string) {
	t.Helper()
	field := reflect.ValueOf(particle).FieldByName("Timestamp")
	if !field.IsValid() {
		t.Fatalf("expected response distribution particle to carry the usage event timestamp, got %+v", particle)
	}
	if field.Kind() != reflect.String {
		t.Fatalf("expected response distribution particle timestamp to be string, got %s", field.Kind())
	}
	if got := field.String(); got != expected {
		t.Fatalf("expected response distribution particle timestamp %s, got %s", expected, got)
	}
}

func TestBuildUsageOverviewRealtimeWithFilterUsesWarmupEventsForSlidingBucketsOnly(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-realtime-warmup.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-15 * time.Minute)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{
		{EventKey: "warmup", APIGroupKey: "provider-a", Model: "warmup-model", Timestamp: windowStart.Add(-30 * time.Second), InputTokens: 600, TotalTokens: 600},
		{EventKey: "visible", APIGroupKey: "provider-a", Model: "visible-model", Timestamp: windowStart.Add(10 * time.Second), InputTokens: 60, TotalTokens: 60},
	})
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events returned error: %v", err)
	}

	realtime, err := BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	}, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache returned error: %v", err)
	}

	if len(realtime.TokenVelocity) != 30 {
		t.Fatalf("expected 30 visible token velocity buckets, got %d", len(realtime.TokenVelocity))
	}
	firstBucket := realtime.TokenVelocity[0]
	if firstBucket.Tokens != 660 || math.Abs(firstBucket.TokensPerMinute-220) > 0.000000001 {
		t.Fatalf("expected first visible bucket to include warmup sliding tokens only in chart, got %+v", firstBucket)
	}
	if len(realtime.CurrentUsage.Models) != 1 || realtime.CurrentUsage.Models[0].Label != "visible-model" || realtime.CurrentUsage.Models[0].Tokens != 60 {
		t.Fatalf("expected current usage to exclude warmup-only model, got %+v", realtime.CurrentUsage.Models)
	}
}

func TestBuildUsageOverviewRealtimeWithFilterUsesRecentCacheFallbackLabels(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-realtime-fallback.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{{
		APIGroupKey: "provider-a",
		Model:       "gpt-5",
		AuthType:    "oauth",
		Source:      "auth-source@example.com",
		AuthIndex:   "auth-index",
		Timestamp:   now.Add(-2 * time.Minute),
		InputTokens: 10,
		TotalTokens: 100,
	}, {
		APIGroupKey: "provider-a",
		Model:       "gpt-5",
		AuthType:    "apikey",
		Provider:    "Provider Display",
		AuthIndex:   "provider-index",
		Timestamp:   now.Add(-1 * time.Minute),
		InputTokens: 20,
		TotalTokens: 200,
	}})

	realtime, err := BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		APIGroupKey:     "provider-a",
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	}, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache returned error: %v", err)
	}

	if len(realtime.CurrentUsage.AuthFiles) != 1 ||
		realtime.CurrentUsage.AuthFiles[0].Key != "auth-index" ||
		realtime.CurrentUsage.AuthFiles[0].Label != "auth-source@example.com" ||
		realtime.CurrentUsage.AuthFiles[0].Tokens != 100 {
		t.Fatalf("expected auth file fallback to use source label, got %+v", realtime.CurrentUsage.AuthFiles)
	}
	if len(realtime.CurrentUsage.AIProviders) != 1 ||
		realtime.CurrentUsage.AIProviders[0].Key != "provider-index" ||
		realtime.CurrentUsage.AIProviders[0].Label != "Provider Display" ||
		realtime.CurrentUsage.AIProviders[0].Tokens != 200 {
		t.Fatalf("expected ai provider fallback to use provider label, got %+v", realtime.CurrentUsage.AIProviders)
	}
}

func TestBuildUsageOverviewRealtimeWithFilterFallsBackToDBWhenRecentCacheIsNil(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-realtime-db-fallback.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{{
		APIGroupKey:  "provider-a",
		Model:        "gpt-5",
		AuthType:     "oauth",
		Source:       "db-source@example.com",
		AuthIndex:    "auth-db",
		Timestamp:    now.Add(-10 * time.Minute),
		InputTokens:  10,
		OutputTokens: 20,
		TotalTokens:  30,
	}}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	realtime, err := BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		RealtimeWindow:  "15m",
		RealtimeEndTime: &now,
	}, nil)
	if err != nil {
		t.Fatalf("BuildUsageOverviewRealtimeWithFilterAndRecentCache returned error: %v", err)
	}

	if len(realtime.CurrentUsage.Models) != 1 ||
		realtime.CurrentUsage.Models[0].Key != "gpt-5" ||
		realtime.CurrentUsage.Models[0].Tokens != 30 {
		t.Fatalf("expected realtime db fallback to populate model usage, got %+v", realtime.CurrentUsage.Models)
	}
	if len(realtime.RequestLevel) != 30 || realtime.RequestLevel[10].Requests != 1 {
		t.Fatalf("expected realtime db fallback to populate request level, got %+v", realtime.RequestLevel[10])
	}
}

func TestBuildUsageOverviewWithFilterUsesRecentCacheForCoveredBoundaryEvents(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-cache-boundary.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 12, 20, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{{
		APIGroupKey:         "provider-a",
		Model:               "gpt-5",
		AuthType:            "oauth",
		Source:              "auth-user@example.com",
		AuthIndex:           "auth-1",
		Timestamp:           start.Add(10 * time.Minute),
		InputTokens:         100,
		OutputTokens:        50,
		ReasoningTokens:     7,
		CachedTokens:        25,
		CacheReadTokens:     3,
		CacheCreationTokens: 4,
		TotalTokens:         150,
	}, {
		APIGroupKey:  "provider-b",
		Model:        "gpt-5",
		AuthType:     "apikey",
		Provider:     "Provider B",
		AuthIndex:    "provider-b",
		Timestamp:    start.Add(11 * time.Minute),
		InputTokens:  900,
		OutputTokens: 100,
		TotalTokens:  1000,
	}})

	filter := dto.UsageQueryFilter{
		Range:       "custom",
		StartTime:   &start,
		EndTime:     &end,
		QueryNow:    &now,
		APIGroupKey: "provider-a",
	}
	overview, err := BuildUsageOverviewWithFilterAndRecentCache(db, filter, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilterAndRecentCache returned error: %v", err)
	}

	if overview.Usage.TotalRequests != 1 || overview.Usage.SuccessCount != 1 || overview.Usage.TotalTokens != 150 {
		t.Fatalf("expected cached boundary event in usage totals, got %+v", overview.Usage)
	}
	if overview.Summary.InputTokens != 100 || overview.Summary.CachedTokens != 25 || overview.Summary.ReasoningTokens != 7 {
		t.Fatalf("expected cached boundary event in summary, got %+v", overview.Summary)
	}
	if overview.Series.Requests["2026-06-10T12:00:00Z"] != 1 || overview.Series.Tokens["2026-06-10T12:00:00Z"] != 150 {
		t.Fatalf("expected cached boundary event in series, got %+v", overview.Series)
	}
}

func TestBuildUsageOverviewWithFilterUsesOpenEndedRecentCacheForCurrentRightBoundary(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-cache-open-right.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 0, 5, 0, time.UTC)
	start := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{{
		APIGroupKey: "provider-a",
		Model:       "gpt-5",
		AuthType:    "oauth",
		Source:      "auth-user@example.com",
		AuthIndex:   "auth-1",
		Timestamp:   now,
		InputTokens: 40,
		TotalTokens: 100,
	}})
	overview, err := BuildUsageOverviewWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		Range:       "24h",
		StartTime:   &start,
		EndTime:     &end,
		QueryNow:    &now,
		APIGroupKey: "provider-a",
	}, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilterAndRecentCache returned error: %v", err)
	}

	if overview.Usage.TotalRequests != 1 || overview.Usage.TotalTokens != 100 {
		t.Fatalf("expected open-ended right boundary cache event, got %+v", overview.Usage)
	}
}

func TestBuildUsageOverviewWithFilterUsesBoundedRecentCacheForHistoricalCustomRightBoundary(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-cache-bounded-custom.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 12, 20, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{{
		APIGroupKey: "provider-a",
		Model:       "gpt-5",
		AuthType:    "oauth",
		Source:      "inside@example.com",
		AuthIndex:   "auth-inside",
		Timestamp:   start.Add(10 * time.Minute),
		InputTokens: 20,
		TotalTokens: 50,
	}, {
		APIGroupKey: "provider-a",
		Model:       "gpt-5",
		AuthType:    "oauth",
		Source:      "after@example.com",
		AuthIndex:   "auth-after",
		Timestamp:   end.Add(5 * time.Minute),
		InputTokens: 80,
		TotalTokens: 200,
	}})
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events returned error: %v", err)
	}

	overview, err := BuildUsageOverviewWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		Range:       "custom",
		StartTime:   &start,
		EndTime:     &end,
		QueryNow:    &now,
		APIGroupKey: "provider-a",
	}, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilterAndRecentCache returned error: %v", err)
	}

	if overview.Usage.TotalRequests != 1 || overview.Usage.TotalTokens != 50 {
		t.Fatalf("expected historical custom right boundary to stop at end, got %+v", overview.Usage)
	}
}

func TestBuildUsageOverviewWithFilterClampsFutureCustomEndToQueryNow(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-cache-future-custom.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	queryNow := time.Date(2026, 6, 10, 12, 30, 0, 0, time.UTC)
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 23, 59, 59, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return queryNow }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{{
		APIGroupKey: "provider-a",
		Model:       "gpt-5",
		AuthType:    "oauth",
		Source:      "today@example.com",
		AuthIndex:   "auth-today",
		Timestamp:   start.Add(10 * time.Minute),
		InputTokens: 40,
		TotalTokens: 90,
	}})
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events returned error: %v", err)
	}

	overview, err := BuildUsageOverviewWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		Range:       "custom",
		StartTime:   &start,
		EndTime:     &end,
		QueryNow:    &queryNow,
		APIGroupKey: "provider-a",
	}, cache)
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilterAndRecentCache returned error: %v", err)
	}

	if overview.Usage.TotalRequests != 1 || overview.Usage.TotalTokens != 90 {
		t.Fatalf("expected future-ended custom range to read current boundary from cache, got %+v", overview.Usage)
	}
}

func TestBuildUsageOverviewWithFilterDoesNotFallbackToDBForEmptyCoveredRightBoundaryCache(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-overview-cache-empty-right.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Date(2026, 6, 10, 12, 20, 0, 0, time.UTC)
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 10, 12, 20, 0, 0, time.UTC)
	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events returned error: %v", err)
	}

	overview, err := BuildUsageOverviewWithFilterAndRecentCache(db, dto.UsageQueryFilter{
		Range:       "custom",
		StartTime:   &start,
		EndTime:     &end,
		QueryNow:    &now,
		APIGroupKey: "provider-a",
	}, cache)
	if err != nil {
		t.Fatalf("expected covered empty right boundary cache not to query DB, got %v", err)
	}
	if overview.Usage.TotalRequests != 0 || overview.Usage.TotalTokens != 0 {
		t.Fatalf("expected empty overview from covered empty cache, got %+v", overview.Usage)
	}
}

func TestUsageOverviewRealtimeWindowSupportsThirtyMinutes(t *testing.T) {
	window, span := usageOverviewRealtimeWindow("30m")
	if window != 30*time.Minute || span != time.Minute {
		t.Fatalf("expected 30m realtime window to use 60s buckets, got window=%s span=%s", window, span)
	}
	if label := usageOverviewRealtimeWindowLabel(window); label != "30m" {
		t.Fatalf("expected 30m realtime window label, got %q", label)
	}
	aggregationWindow := usageOverviewRealtimeAggregationWindow(window)
	if aggregationWindow != 5*time.Minute {
		t.Fatalf("expected 30m realtime window to aggregate over 5m, got %s", aggregationWindow)
	}
	if bucketCount := usageOverviewRealtimeAggregationBucketCount(span, aggregationWindow); bucketCount != 5 {
		t.Fatalf("expected 30m realtime aggregation to cover 5 buckets, got %d", bucketCount)
	}
}

func TestUsageOverviewRealtimeWindowDropsFiveMinutePreset(t *testing.T) {
	window, span := usageOverviewRealtimeWindow("5m")
	if window != 15*time.Minute || span != 30*time.Second {
		t.Fatalf("expected removed 5m realtime window to fall back to 15m, got window=%s span=%s", window, span)
	}
	if label := usageOverviewRealtimeWindowLabel(window); label != "15m" {
		t.Fatalf("expected removed 5m realtime window to label as 15m, got %q", label)
	}
}

func stringPtr(value string) *string {
	return &value
}
