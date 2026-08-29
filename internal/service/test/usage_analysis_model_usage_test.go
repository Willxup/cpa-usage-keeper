package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageServiceMapsAnalysisModelUsage(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	start := time.Date(2026, 8, 2, 9, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "group-a", DisplayKey: "sk-***a"}).Error; err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart:  start,
		APIGroupKey:  "group-a",
		Model:        "model-alpha",
		RequestCount: 3,
		TotalTokens:  420,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}

	analysis, err := service.NewUsageService(db, emptyPricingCatalogForTest()).GetAnalysis(context.Background(), servicedto.UsageFilter{
		Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true,
	})
	if err != nil {
		t.Fatalf("GetAnalysis: %v", err)
	}
	want := []servicedto.AnalysisModelUsage{{Bucket: start, Model: "model-alpha", TotalTokens: 420, Requests: 3}}
	if !reflect.DeepEqual(analysis.ModelUsage, want) {
		t.Fatalf("model usage = %+v, want %+v", analysis.ModelUsage, want)
	}
}
