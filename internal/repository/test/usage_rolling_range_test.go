package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestBuildAnalysisKeepsTwentyFourHourAndDirectOneDayRangesOnHourlyStats(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "analysis-rolling-hour.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	end := time.Date(2026, 5, 21, 9, 14, 21, 0, location)
	start := end.Add(-24 * time.Hour)
	currentHour := time.Date(2026, 5, 21, 9, 0, 0, 0, location)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: currentHour, APIGroupKey: "sk-target-key", Model: "claude-sonnet",
		RequestCount: 6, InputTokens: 90, OutputTokens: 10, TotalTokens: 100,
	}).Error; err != nil {
		t.Fatalf("insert current hour stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	for _, rangeName := range []string{"24h", "1d"} {
		t.Run(rangeName, func(t *testing.T) {
			analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: rangeName, StartTime: &start, EndTime: &end}, emptyPricingResolverForTest())
			if err != nil {
				t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
			}
			if analysis.Granularity != repodto.AnalysisGranularityHourly {
				t.Fatalf("expected hourly granularity, got %q", analysis.Granularity)
			}
			if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(currentHour) || analysis.TokenUsage[0].TotalTokens != 100 {
				t.Fatalf("expected %s range to include current hourly stats, got %+v", rangeName, analysis.TokenUsage)
			}
		})
	}
}

func TestBuildAnalysisMatchesOverviewCustomRollupRouting(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	dayStart := time.Date(2026, 5, 20, 0, 0, 0, 0, location)
	hourStart := time.Date(2026, 5, 20, 8, 0, 0, 0, location)
	for _, testCase := range []struct {
		name            string
		unit            string
		start           time.Time
		end             time.Time
		wantGranularity repodto.AnalysisGranularity
		wantBucket      time.Time
		wantTokens      int64
		wantTable       string
	}{
		{
			name: "custom day uses daily including a single day", unit: "day",
			start: dayStart, end: dayStart.AddDate(0, 0, 1),
			wantGranularity: repodto.AnalysisGranularityDaily, wantBucket: dayStart, wantTokens: 100,
			wantTable: "usage_overview_daily_stats",
		},
		{
			name: "custom hour uses hourly", unit: "hour",
			start: hourStart, end: hourStart.Add(5 * time.Hour),
			wantGranularity: repodto.AnalysisGranularityHourly, wantBucket: hourStart, wantTokens: 900,
			wantTable: "usage_overview_hourly_stats",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "analysis-custom-rollup.db")})
			if err != nil {
				t.Fatalf("OpenDatabase returned error: %v", err)
			}
			closeTestDatabase(t, db)
			if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
				t.Fatalf("insert CPA API key: %v", err)
			}
			if err := db.Create(&entities.UsageOverviewDailyStat{
				BucketStart: dayStart, APIGroupKey: "sk-target-key", Model: "claude-sonnet",
				RequestCount: 1, InputTokens: 90, OutputTokens: 10, TotalTokens: 100,
			}).Error; err != nil {
				t.Fatalf("insert daily stat: %v", err)
			}
			if err := db.Create(&entities.UsageOverviewHourlyStat{
				BucketStart: hourStart, APIGroupKey: "sk-target-key", Model: "claude-sonnet",
				RequestCount: 9, InputTokens: 800, OutputTokens: 100, TotalTokens: 900,
			}).Error; err != nil {
				t.Fatalf("insert hourly stat: %v", err)
			}
			if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
				t.Fatalf("drop usage_events: %v", err)
			}

			queries := captureAnalysisRollupQueries(t, db)
			analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
				Range: "custom", CustomUnit: testCase.unit, StartTime: &testCase.start, EndTime: &testCase.end, EndExclusive: true,
			}, emptyPricingResolverForTest())
			if err != nil {
				t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
			}
			if analysis.Granularity != testCase.wantGranularity {
				t.Fatalf("granularity = %q, want %q", analysis.Granularity, testCase.wantGranularity)
			}
			if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(testCase.wantBucket) || analysis.TokenUsage[0].TotalTokens != testCase.wantTokens {
				t.Fatalf("token usage = %+v, want bucket=%s total_tokens=%d", analysis.TokenUsage, testCase.wantBucket, testCase.wantTokens)
			}
			if len(*queries) != 1 {
				t.Fatalf("Analysis rollup queries = %#v, want one query", *queries)
			}
			requireSingleAnalysisRollupQuery(t, *queries, testCase.wantTable)
		})
	}
}

func TestBuildAnalysisUsesOnlyDailyStatsForRollingDayRanges(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	for _, testCase := range []struct {
		name        string
		rangeName   string
		days        int
		wantBuckets []struct {
			bucket time.Time
			tokens int64
		}
	}{
		{
			name: "twelve days includes the start day", rangeName: "12d", days: 12,
			wantBuckets: []struct {
				bucket time.Time
				tokens int64
			}{
				{bucket: time.Date(2026, 7, 15, 0, 0, 0, 0, location), tokens: 671_404_633},
				{bucket: time.Date(2026, 7, 27, 0, 0, 0, 0, location), tokens: 222},
			},
		},
		{
			name: "thirteen days keeps the full left day", rangeName: "13d", days: 13,
			wantBuckets: []struct {
				bucket time.Time
				tokens int64
			}{
				{bucket: time.Date(2026, 7, 15, 0, 0, 0, 0, location), tokens: 671_404_633},
				{bucket: time.Date(2026, 7, 27, 0, 0, 0, 0, location), tokens: 222},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "analysis-daily-range.db")})
			if err != nil {
				t.Fatalf("OpenDatabase returned error: %v", err)
			}
			closeTestDatabase(t, db)

			end := time.Date(2026, 7, 27, 17, 14, 21, 0, location)
			start := end.Add(-time.Duration(testCase.days) * 24 * time.Hour)
			if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
				t.Fatalf("insert CPA API key: %v", err)
			}
			if err := db.Create(&[]entities.UsageOverviewDailyStat{
				{
					BucketStart: time.Date(2026, 7, 15, 0, 0, 0, 0, location), APIGroupKey: "sk-target-key", Model: "claude-sonnet",
					RequestCount: 5_177, InputTokens: 668_642_798, OutputTokens: 2_761_835, TotalTokens: 671_404_633,
				},
				{
					BucketStart: time.Date(2026, 7, 27, 0, 0, 0, 0, location), APIGroupKey: "sk-target-key", Model: "claude-sonnet",
					RequestCount: 2, InputTokens: 200, OutputTokens: 22, TotalTokens: 222,
				},
			}).Error; err != nil {
				t.Fatalf("insert daily stats: %v", err)
			}
			if err := db.Create(&[]entities.UsageOverviewHourlyStat{
				{
					BucketStart: time.Date(2026, 7, 15, 18, 0, 0, 0, location), APIGroupKey: "sk-target-key", Model: "claude-sonnet",
					RequestCount: 2_724, InputTokens: 338_914_706, OutputTokens: 1_251_623, TotalTokens: 340_166_329,
				},
				{
					BucketStart: time.Date(2026, 7, 27, 17, 0, 0, 0, location), APIGroupKey: "sk-target-key", Model: "claude-sonnet",
					RequestCount: 1, InputTokens: 100, OutputTokens: 11, TotalTokens: 111,
				},
			}).Error; err != nil {
				t.Fatalf("insert hourly decoys: %v", err)
			}
			if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
				t.Fatalf("drop usage_events: %v", err)
			}

			queries := captureAnalysisRollupQueries(t, db)
			analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
				Range: testCase.rangeName, StartTime: &start, EndTime: &end,
			}, emptyPricingResolverForTest())
			if err != nil {
				t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
			}
			if analysis.Granularity != repodto.AnalysisGranularityDaily {
				t.Fatalf("expected daily granularity, got %q", analysis.Granularity)
			}
			wantRangeStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
			wantRangeEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
			if analysis.RangeStart == nil || !analysis.RangeStart.Equal(wantRangeStart) || analysis.RangeEnd == nil || !analysis.RangeEnd.Equal(wantRangeEnd) {
				t.Fatalf("range = [%v, %v), want [%v, %v)", analysis.RangeStart, analysis.RangeEnd, wantRangeStart, wantRangeEnd)
			}
			if len(analysis.TokenUsage) != len(testCase.wantBuckets) {
				t.Fatalf("token usage = %+v, want %d daily buckets", analysis.TokenUsage, len(testCase.wantBuckets))
			}
			for index, want := range testCase.wantBuckets {
				got := analysis.TokenUsage[index]
				if !got.Bucket.Equal(want.bucket) || got.TotalTokens != want.tokens {
					t.Fatalf("token usage[%d] = %+v, want bucket=%s total_tokens=%d", index, got, want.bucket, want.tokens)
				}
			}
			if len(*queries) != 1 {
				t.Fatalf("Analysis rollup queries = %#v, want one daily query", *queries)
			}
			requireSingleAnalysisRollupQuery(t, *queries, "usage_overview_daily_stats")
		})
	}
}

func TestBuildAnalysisIncludesCurrentDailyStatsForRollingDayRange(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "analysis-rolling-day.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	end := time.Date(2026, 5, 21, 9, 14, 21, 0, location)
	start := end.Add(-13 * 24 * time.Hour)
	currentDay := time.Date(2026, 5, 21, 0, 0, 0, 0, location)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: currentDay, APIGroupKey: "sk-target-key", Model: "claude-sonnet",
		RequestCount: 6, InputTokens: 90, OutputTokens: 10, TotalTokens: 100,
	}).Error; err != nil {
		t.Fatalf("insert current day stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "13d", StartTime: &start, EndTime: &end}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || !analysis.TokenUsage[0].Bucket.Equal(currentDay) || analysis.TokenUsage[0].TotalTokens != 100 {
		t.Fatalf("expected rolling day range to include current daily stats, got %+v", analysis.TokenUsage)
	}
}
