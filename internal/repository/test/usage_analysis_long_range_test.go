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

func TestBuildAnalysisUsesLongCustomDayRollupsWithoutUsageEvents(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "long-custom-analysis.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	bucket := start.AddDate(0, 0, 90)
	end := start.AddDate(0, 0, 121)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-long-custom", DisplayKey: "sk-*********custom"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: bucket, APIGroupKey: "sk-long-custom", Model: "gpt-5",
		RequestCount: 3, InputTokens: 120, OutputTokens: 30, TotalTokens: 150,
	}).Error; err != nil {
		t.Fatalf("insert daily stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true,
	}, emptyPricingResolverForTest())

	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if analysis.Granularity != repodto.AnalysisGranularityDaily || len(analysis.TokenUsage) != 1 {
		t.Fatalf("expected one daily Analysis bucket, got %+v", analysis)
	}
	if !analysis.TokenUsage[0].Bucket.Equal(bucket) || analysis.TokenUsage[0].Requests != 3 || analysis.TokenUsage[0].TotalTokens != 150 {
		t.Fatalf("expected long custom Analysis data from daily rollup, got %+v", analysis.TokenUsage[0])
	}
}
