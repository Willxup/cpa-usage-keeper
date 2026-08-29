package test

import (
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestAnalysisModelUsageAggregatesHourlyBucketsByNormalizedModel(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 8, 1, 10, 0, 0, 0, time.Local)
	end := start.Add(2 * time.Hour)
	if err := db.Create(&[]entities.CPAAPIKey{
		{APIKey: "group-a", DisplayKey: "sk-***a"},
		{APIKey: "group-b", DisplayKey: "sk-***b"},
	}).Error; err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	if err := db.Create(&[]entities.UsageOverviewHourlyStat{
		{BucketStart: start, APIGroupKey: "group-a", Model: "model-alpha", RequestCount: 1, TotalTokens: 100},
		{BucketStart: start, APIGroupKey: "group-b", Model: " model-alpha ", RequestCount: 2, TotalTokens: 50},
		{BucketStart: start, APIGroupKey: "group-a", Model: "model-beta", RequestCount: 4, TotalTokens: 200},
		{BucketStart: start, APIGroupKey: "group-a", Model: " ", RequestCount: 2, TotalTokens: 10},
		{BucketStart: start, APIGroupKey: "group-b", Model: "", RequestCount: 3, TotalTokens: 15},
		{BucketStart: start.Add(time.Hour), APIGroupKey: "group-a", Model: "model-alpha", RequestCount: 1, TotalTokens: 25},
		{BucketStart: start.Add(time.Hour), APIGroupKey: "group-a", Model: "model-gamma", RequestCount: 5, TotalTokens: 300},
	}).Error; err != nil {
		t.Fatalf("seed hourly stats: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
	}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter: %v", err)
	}

	want := []repodto.AnalysisModelUsageRecord{
		{Bucket: start, Model: "model-alpha", TotalTokens: 150, Requests: 3},
		{Bucket: start, Model: "model-beta", TotalTokens: 200, Requests: 4},
		{Bucket: start, Model: "unknown", TotalTokens: 25, Requests: 5},
		{Bucket: start.Add(time.Hour), Model: "model-alpha", TotalTokens: 25, Requests: 1},
		{Bucket: start.Add(time.Hour), Model: "model-gamma", TotalTokens: 300, Requests: 5},
	}
	if !reflect.DeepEqual(analysis.ModelUsage, want) {
		t.Fatalf("model usage = %+v, want %+v", analysis.ModelUsage, want)
	}
}

func TestAnalysisModelUsageUsesDailyBuckets(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 2)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "group-a", DisplayKey: "sk-***a"}).Error; err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	if err := db.Create(&[]entities.UsageOverviewDailyStat{
		{BucketStart: start, APIGroupKey: "group-a", Model: "model-alpha", RequestCount: 2, TotalTokens: 120},
		{BucketStart: start, APIGroupKey: "group-a", Model: "model-beta", RequestCount: 3, TotalTokens: 240},
		{BucketStart: start.AddDate(0, 0, 1), APIGroupKey: "group-a", Model: "model-alpha", RequestCount: 4, TotalTokens: 360},
	}).Error; err != nil {
		t.Fatalf("seed daily stats: %v", err)
	}

	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true,
	}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter: %v", err)
	}

	want := []repodto.AnalysisModelUsageRecord{
		{Bucket: start, Model: "model-alpha", TotalTokens: 120, Requests: 2},
		{Bucket: start, Model: "model-beta", TotalTokens: 240, Requests: 3},
		{Bucket: start.AddDate(0, 0, 1), Model: "model-alpha", TotalTokens: 360, Requests: 4},
	}
	if analysis.Granularity != repodto.AnalysisGranularityDaily || !reflect.DeepEqual(analysis.ModelUsage, want) {
		t.Fatalf("daily model usage = %+v, want %+v", analysis.ModelUsage, want)
	}
}
