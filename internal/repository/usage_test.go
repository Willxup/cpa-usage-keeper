package repository

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

func assertAnalysisCostClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000000001 {
		t.Fatalf("expected cost %.9f, got %.9f", want, got)
	}
}

func TestListUsageEventsWithFilterPreservesEventFields(t *testing.T) {
	db := openUsageTestDatabase(t)
	ttftMS := int64(45)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "provider-a", Model: "claude-sonnet", ExecutorType: "responses", Endpoint: "POST /v1/messages", Timestamp: time.Date(2026, 4, 16, 9, 0, 0, 0, time.UTC), Source: "codex-a", AuthIndex: "1", Failed: false, LatencyMS: 100, TTFTMS: &ttftMS, InputTokens: 10, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 0, CacheReadTokens: 7, CacheCreationTokens: 8, TotalTokens: 35},
		{EventKey: "event-2", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), Source: "codex-b", AuthIndex: "2", Failed: true, LatencyMS: 200, InputTokens: 2, OutputTokens: 3, ReasoningTokens: 0, CachedTokens: 0, TotalTokens: 5},
		{EventKey: "event-3", APIGroupKey: "provider-b", Model: "claude-opus", Timestamp: time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC), Source: "codex-c", AuthIndex: "3", Failed: false, LatencyMS: 300, InputTokens: 100, OutputTokens: 50, ReasoningTokens: 25, CachedTokens: 10, TotalTokens: 185},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	page, err := ListUsageEventsWithFilter(db, repodto.UsageQueryFilter{Page: 1, PageSize: 10, Limit: 10})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.Events[2].CacheReadTokens != 7 || page.Events[2].CacheCreationTokens != 8 {
		t.Fatalf("expected cache token event list fields to be preserved, got %+v", page.Events[2])
	}
	if page.Events[2].TTFTMS == nil || *page.Events[2].TTFTMS != 45 {
		t.Fatalf("expected ttft_ms event list field to be preserved, got %+v", page.Events[2].TTFTMS)
	}
	if page.Events[2].Endpoint != "POST /v1/messages" {
		t.Fatalf("expected endpoint event list field to be preserved, got %q", page.Events[2].Endpoint)
	}
	if page.Events[2].ExecutorType != "responses" {
		t.Fatalf("expected executor_type event list field to be preserved, got %q", page.Events[2].ExecutorType)
	}
}

func TestUsageOverviewDailyBucketUsesLocalTime(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	bucketKey, bucketMinutes := usageOverviewBucket(time.Date(2026, 4, 16, 23, 30, 0, 0, time.UTC), true)

	if bucketKey != "2026-04-17" || bucketMinutes != 24*60 {
		t.Fatalf("expected local day bucket 2026-04-17/1440, got %s/%d", bucketKey, bucketMinutes)
	}
}

func TestBuildUsageOverviewWithFilterFiltersByAPIGroupKey(t *testing.T) {
	db := openUsageTestDatabase(t)
	insertAPIKeyFilterEvents(t, db)

	if err := AggregateUsageOverviewStats(context.Background(), db, time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats returned error: %v", err)
	}
	start := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 20, 11, 0, 0, 0, time.UTC)
	overview, err := BuildUsageOverviewWithFilter(db, repodto.UsageQueryFilter{APIGroupKey: "sk-target-key", Range: "custom", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}
	if overview.Summary.RequestCount != 2 || overview.Summary.TokenCount != 70 {
		t.Fatalf("expected only target key events in overview summary, got %+v", overview.Summary)
	}
	if overview.Usage.TotalRequests != 2 || overview.Usage.TotalTokens != 70 {
		t.Fatalf("expected target key aggregate only in usage totals, got %+v", overview.Usage)
	}
}

func TestBuildAnalysisWithFilterUsesOverviewStatsWithoutUsageEvents(t *testing.T) {
	db := openUsageTestDatabase(t)
	bucket := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart:  bucket,
		APIGroupKey:  "sk-target-key",
		Model:        "claude-sonnet",
		RequestCount: 2,
		InputTokens:  10,
		OutputTokens: 20,
		TotalTokens:  30,
	}).Error; err != nil {
		t.Fatalf("insert hourly stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}
	start := bucket
	end := bucket.Add(time.Hour)

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error after dropping usage_events: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || analysis.TokenUsage[0].TotalTokens != 30 || analysis.TokenUsage[0].Requests != 2 {
		t.Fatalf("expected analysis to come from overview hourly stats, got %+v", analysis.TokenUsage)
	}
	if len(analysis.APIKeyComposition) != 1 || analysis.APIKeyComposition[0].Key != "sk-target-key" {
		t.Fatalf("expected API composition from overview stats, got %+v", analysis.APIKeyComposition)
	}
}

func TestBuildAnalysisWithFilterCalculatesCostInsightsFromOverviewStats(t *testing.T) {
	db := openUsageTestDatabase(t)
	bucket := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if _, err := UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "gpt-4o",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	}); err != nil {
		t.Fatalf("upsert gpt price: %v", err)
	}
	if _, err := UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                   "claude-sonnet",
		PricingStyle:            entities.ModelPricingStyleClaude,
		PromptPricePer1M:        10,
		CompletionPricePer1M:    20,
		CachePricePer1M:         1,
		CacheCreationPricePer1M: 12.5,
	}); err != nil {
		t.Fatalf("upsert claude price: %v", err)
	}
	if err := db.Create([]entities.UsageOverviewHourlyStat{
		{
			BucketStart:     bucket,
			APIGroupKey:     "sk-target-key",
			Model:           "gpt-4o",
			RequestCount:    2,
			InputTokens:     1_000_000,
			OutputTokens:    500_000,
			ReasoningTokens: 50_000,
			CachedTokens:    200_000,
			TotalTokens:     1_750_000,
		},
		{
			BucketStart:         bucket.Add(time.Hour),
			APIGroupKey:         "sk-target-key",
			Model:               "claude-sonnet",
			RequestCount:        1,
			InputTokens:         1_300_000,
			OutputTokens:        500_000,
			CachedTokens:        200_000,
			CacheReadTokens:     200_000,
			CacheCreationTokens: 100_000,
			TotalTokens:         2_000_000,
		},
	}).Error; err != nil {
		t.Fatalf("insert hourly stats: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}
	start := bucket
	end := bucket.Add(2 * time.Hour)

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}

	if len(analysis.TokenUsage) != 2 {
		t.Fatalf("expected two costed token buckets, got %+v", analysis.TokenUsage)
	}
	assertAnalysisCostClose(t, analysis.TokenUsage[0].CostUSD, 9.96)
	assertAnalysisCostClose(t, analysis.TokenUsage[1].CostUSD, 21.45)
	if !analysis.TokenUsage[0].CostAvailable || !analysis.TokenUsage[1].CostAvailable {
		t.Fatalf("expected bucket cost to be available, got %+v", analysis.TokenUsage)
	}
	assertAnalysisCostClose(t, analysis.CostBreakdown.InputCostUSD, 12.4)
	assertAnalysisCostClose(t, analysis.CostBreakdown.OutputCostUSD, 17.5)
	assertAnalysisCostClose(t, analysis.CostBreakdown.CachedCostUSD, 1.51)
	assertAnalysisCostClose(t, analysis.CostBreakdown.TotalCostUSD, 31.41)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected aggregate cost to be available, got %+v", analysis.CostBreakdown)
	}
	if len(analysis.APIKeyComposition) != 1 {
		t.Fatalf("expected one api composition row, got %+v", analysis.APIKeyComposition)
	}
	assertAnalysisCostClose(t, analysis.APIKeyComposition[0].CostUSD, 31.41)
	if !analysis.APIKeyComposition[0].CostAvailable {
		t.Fatalf("expected api composition cost to be available, got %+v", analysis.APIKeyComposition[0])
	}
	if len(analysis.Heatmap) != 2 {
		t.Fatalf("expected two heatmap cells, got %+v", analysis.Heatmap)
	}
	if analysis.Heatmap[0].Model != "claude-sonnet" || analysis.Heatmap[0].InputTokens != 1_300_000 || analysis.Heatmap[0].OutputTokens != 500_000 || analysis.Heatmap[0].CachedTokens != 200_000 {
		t.Fatalf("expected heatmap token detail for claude, got %+v", analysis.Heatmap[0])
	}
	assertAnalysisCostClose(t, analysis.Heatmap[0].CostUSD, 21.45)
	if len(analysis.ModelEfficiency) != 2 {
		t.Fatalf("expected two model efficiency rows, got %+v", analysis.ModelEfficiency)
	}
	if analysis.ModelEfficiency[0].Model != "claude-sonnet" || analysis.ModelEfficiency[0].Requests != 1 {
		t.Fatalf("expected model efficiency sorted by cost desc, got %+v", analysis.ModelEfficiency)
	}
	assertAnalysisCostClose(t, analysis.ModelEfficiency[0].CostPerRequestUSD, 21.45)
	assertAnalysisCostClose(t, analysis.ModelEfficiency[0].OutputTokensPerRequest, 500_000)
	assertAnalysisCostClose(t, analysis.ModelEfficiency[0].CacheRate, 200_000.0/1_300_000.0)
	if analysis.ModelEfficiency[1].Model != "gpt-4o" {
		t.Fatalf("expected second model efficiency row for gpt-4o, got %+v", analysis.ModelEfficiency)
	}
	assertAnalysisCostClose(t, analysis.ModelEfficiency[1].OutputTokensPerRequest, 250_000)
	if analysis.ModelEfficiency[0].OutputTokensPerRequest == 0 || analysis.ModelEfficiency[0].CacheRate == 0 {
		t.Fatalf("unexpected model efficiency metrics: %+v", analysis.ModelEfficiency[0])
	}
}

func TestBuildAnalysisWithFilterMarksCostUnavailableForUnpricedStats(t *testing.T) {
	db := openUsageTestDatabase(t)
	bucket := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart:  bucket,
		APIGroupKey:  "sk-target-key",
		Model:        "unpriced-model",
		RequestCount: 2,
		InputTokens:  1_000,
		OutputTokens: 500,
		TotalTokens:  1_500,
	}).Error; err != nil {
		t.Fatalf("insert hourly stat: %v", err)
	}
	start := bucket
	end := bucket.Add(time.Hour)

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}

	if analysis.CostBreakdown.CostAvailable || analysis.TokenUsage[0].CostAvailable || analysis.APIKeyComposition[0].CostAvailable || analysis.Heatmap[0].CostAvailable || analysis.ModelEfficiency[0].CostAvailable {
		t.Fatalf("expected all analysis cost surfaces to be unavailable, got cost=%+v buckets=%+v api=%+v heatmap=%+v efficiency=%+v", analysis.CostBreakdown, analysis.TokenUsage, analysis.APIKeyComposition, analysis.Heatmap, analysis.ModelEfficiency)
	}
	if analysis.CostBreakdown.TotalCostUSD != 0 || analysis.TokenUsage[0].CostUSD != 0 || analysis.ModelEfficiency[0].CostUSD != 0 {
		t.Fatalf("expected unpriced stats to contribute zero computed cost, got cost=%+v buckets=%+v efficiency=%+v", analysis.CostBreakdown, analysis.TokenUsage, analysis.ModelEfficiency)
	}
}

func TestBuildAnalysisWithFilterExcludesMissingAndDeletedCPAAPIKeys(t *testing.T) {
	db := openUsageTestDatabase(t)
	bucket := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	deletedAt := bucket.Add(time.Hour)
	if err := db.Create([]entities.CPAAPIKey{
		{APIKey: "sk-active-key", DisplayKey: "sk-*********active"},
		{APIKey: "sk-deleted-key", DisplayKey: "sk-*********deleted", IsDeleted: true, LastSyncedAt: &deletedAt},
	}).Error; err != nil {
		t.Fatalf("insert CPA API keys: %v", err)
	}
	if err := db.Create([]entities.UsageOverviewHourlyStat{
		{BucketStart: bucket, APIGroupKey: "sk-active-key", Model: "claude-sonnet", RequestCount: 2, InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
		{BucketStart: bucket, APIGroupKey: "sk-deleted-key", Model: "claude-opus", RequestCount: 3, InputTokens: 30, OutputTokens: 40, TotalTokens: 70},
		{BucketStart: bucket, APIGroupKey: "sk-missing-key", Model: "gpt-4", RequestCount: 4, InputTokens: 50, OutputTokens: 60, TotalTokens: 110},
	}).Error; err != nil {
		t.Fatalf("insert hourly stats: %v", err)
	}
	start := bucket
	end := bucket.Add(time.Hour)

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.APIKeyComposition) != 1 || analysis.APIKeyComposition[0].Key != "sk-active-key" || analysis.APIKeyComposition[0].TotalTokens != 30 {
		t.Fatalf("expected only active CPA API key stats, got %+v", analysis.APIKeyComposition)
	}
	if len(analysis.ModelComposition) != 1 || analysis.ModelComposition[0].Key != "claude-sonnet" {
		t.Fatalf("expected models from active CPA API key only, got %+v", analysis.ModelComposition)
	}
	if len(analysis.Heatmap) != 1 || analysis.Heatmap[0].APIKey != "sk-active-key" {
		t.Fatalf("expected heatmap from active CPA API key only, got %+v", analysis.Heatmap)
	}
	if len(analysis.TokenUsage) != 1 || analysis.TokenUsage[0].TotalTokens != 30 || analysis.TokenUsage[0].Requests != 2 {
		t.Fatalf("expected token usage from active CPA API key only, got %+v", analysis.TokenUsage)
	}
}

func TestBuildAnalysisWithFilterBuildsIdentityCompositionsFromActiveUsageIdentities(t *testing.T) {
	db := openUsageTestDatabase(t)
	bucket := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	deletedAt := bucket.Add(time.Hour)
	if err := db.Create([]entities.CPAAPIKey{
		{APIKey: "sk-active-key", DisplayKey: "sk-*********active"},
		{APIKey: "sk-deleted-key", DisplayKey: "sk-*********deleted", IsDeleted: true, LastSyncedAt: &deletedAt},
	}).Error; err != nil {
		t.Fatalf("insert CPA API keys: %v", err)
	}
	if err := db.Create([]entities.UsageIdentity{
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "auth_file", Identity: "auth-file-1", Name: "Auth File One"},
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "auth_file", Identity: "auth-file-deleted", Name: "Deleted Auth File", IsDeleted: true},
		{AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "ai_provider", Identity: "provider-1", Name: "Provider One", Prefix: "Team Prefix", BaseURL: "https://api.openai.com/v1/"},
		{AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "ai_provider", Identity: "shared-index", Name: "Provider Shared", Prefix: "Shared Prefix"},
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "auth_file", Identity: "shared-index", Name: "Auth Shared"},
	}).Error; err != nil {
		t.Fatalf("insert usage identities: %v", err)
	}
	if err := db.Create([]entities.UsageOverviewHourlyStat{
		{BucketStart: bucket, APIGroupKey: "sk-active-key", Model: "claude-sonnet", AuthIndex: "auth-file-1", RequestCount: 2, InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
		{BucketStart: bucket, APIGroupKey: "sk-active-key", Model: "claude-3-sonnet", AuthIndex: "auth-file-1", RequestCount: 1, InputTokens: 5, OutputTokens: 5, TotalTokens: 10},
		{BucketStart: bucket, APIGroupKey: "sk-active-key", Model: "claude-opus", AuthIndex: "provider-1", RequestCount: 3, InputTokens: 40, OutputTokens: 20, TotalTokens: 60},
		{BucketStart: bucket, APIGroupKey: "sk-active-key", Model: "claude-haiku", AuthIndex: "auth-file-deleted", RequestCount: 4, InputTokens: 50, OutputTokens: 10, TotalTokens: 60},
		{BucketStart: bucket, APIGroupKey: "sk-active-key", Model: "gpt-4", AuthIndex: "missing-index", RequestCount: 5, InputTokens: 60, OutputTokens: 20, TotalTokens: 80},
		{BucketStart: bucket, APIGroupKey: "sk-active-key", Model: "claude-sonnet", AuthIndex: "shared-index", ModelAlias: "alias-a", RequestCount: 6, InputTokens: 70, OutputTokens: 20, TotalTokens: 90},
		{BucketStart: bucket, APIGroupKey: "sk-deleted-key", Model: "claude-sonnet", AuthIndex: "provider-1", RequestCount: 7, InputTokens: 80, OutputTokens: 20, TotalTokens: 100},
	}).Error; err != nil {
		t.Fatalf("insert hourly stats: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}
	start := bucket
	end := bucket.Add(time.Hour)

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}

	if len(analysis.AuthFilesComposition) != 2 {
		t.Fatalf("expected two auth file composition rows, got %+v", analysis.AuthFilesComposition)
	}
	if analysis.AuthFilesComposition[0].Key != "shared-index" || analysis.AuthFilesComposition[0].Label != "Auth Shared" || analysis.AuthFilesComposition[0].TotalTokens != 90 {
		t.Fatalf("expected shared auth file row first, got %+v", analysis.AuthFilesComposition)
	}
	if analysis.AuthFilesComposition[1].Key != "auth-file-1" || analysis.AuthFilesComposition[1].Label != "Auth File One" || analysis.AuthFilesComposition[1].TotalTokens != 40 || analysis.AuthFilesComposition[1].Requests != 3 {
		t.Fatalf("expected merged auth file row second, got %+v", analysis.AuthFilesComposition)
	}
	if len(analysis.AIProviderComposition) != 2 {
		t.Fatalf("expected two ai provider composition rows, got %+v", analysis.AIProviderComposition)
	}
	if analysis.AIProviderComposition[0].Key != "shared-index" || analysis.AIProviderComposition[0].Label != "Provider Shared(Shared Prefix)" || analysis.AIProviderComposition[0].TotalTokens != 90 {
		t.Fatalf("expected shared provider row first, got %+v", analysis.AIProviderComposition)
	}
	if analysis.AIProviderComposition[1].Key != "provider-1" || analysis.AIProviderComposition[1].Label != "Provider One(Team Prefix @ api.openai.com)" || analysis.AIProviderComposition[1].TotalTokens != 60 {
		t.Fatalf("expected active provider row second, got %+v", analysis.AIProviderComposition)
	}
}

func TestBuildAnalysisWithFilterKeepsHeatmapPairsSeparateWhenValuesContainDelimiter(t *testing.T) {
	db := openUsageTestDatabase(t)
	bucket := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	if err := db.Create([]entities.CPAAPIKey{
		{APIKey: "sk-a\x00claude", DisplayKey: "sk-*********claude"},
		{APIKey: "sk-a", DisplayKey: "sk-*********a"},
	}).Error; err != nil {
		t.Fatalf("insert CPA API keys: %v", err)
	}
	if err := db.Create([]entities.UsageOverviewHourlyStat{
		{BucketStart: bucket, APIGroupKey: "sk-a\x00claude", Model: "sonnet", RequestCount: 1, TotalTokens: 10},
		{BucketStart: bucket, APIGroupKey: "sk-a", Model: "claude\x00sonnet", RequestCount: 2, TotalTokens: 20},
	}).Error; err != nil {
		t.Fatalf("insert hourly stats: %v", err)
	}
	start := bucket
	end := bucket.Add(time.Hour)

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.Heatmap) != 2 {
		t.Fatalf("expected two distinct heatmap cells, got %+v", analysis.Heatmap)
	}
}

func TestBuildAnalysisWithFilterIncludesCurrentHourStatsInRollingHourlyRanges(t *testing.T) {
	withRepositoryTestLocation(t, "Asia/Shanghai")
	db := openUsageTestDatabase(t)
	start := time.Date(2026, 5, 21, 5, 14, 21, 0, time.Local)
	end := time.Date(2026, 5, 21, 9, 14, 21, 0, time.Local)
	currentHour := time.Date(2026, 5, 21, 9, 0, 0, 0, time.Local)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart:  currentHour,
		APIGroupKey:  "sk-target-key",
		Model:        "claude-sonnet",
		RequestCount: 6,
		InputTokens:  90,
		OutputTokens: 10,
		TotalTokens:  100,
	}).Error; err != nil {
		t.Fatalf("insert current hour stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "4h", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.TokenUsage) != 1 {
		t.Fatalf("expected current hour bucket only, got %+v", analysis.TokenUsage)
	}
	if !analysis.TokenUsage[0].Bucket.Equal(currentHour) || analysis.TokenUsage[0].TotalTokens != 100 || analysis.TokenUsage[0].Requests != 6 {
		t.Fatalf("expected current hour stat to be included, got %+v", analysis.TokenUsage[0])
	}
}

func TestBuildAnalysisWithFilterFillsTodayAndYesterdayHourlyBucketsFromStats(t *testing.T) {
	db := openUsageTestDatabase(t)
	start := time.Date(2026, 5, 14, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 5, 14, 23, 59, 59, 0, time.Local)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create([]entities.UsageOverviewHourlyStat{
		{
			BucketStart:  start.Add(22 * time.Hour),
			APIGroupKey:  "sk-target-key",
			Model:        "claude-sonnet",
			RequestCount: 3,
			InputTokens:  12,
			OutputTokens: 18,
			TotalTokens:  30,
		},
		{
			BucketStart:  start.Add(23 * time.Hour),
			APIGroupKey:  "sk-target-key",
			Model:        "claude-sonnet",
			RequestCount: 4,
			InputTokens:  20,
			OutputTokens: 30,
			TotalTokens:  50,
		},
	}).Error; err != nil {
		t.Fatalf("insert hourly stats: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "yesterday", StartTime: &start, EndTime: &end})
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
	}
	if len(analysis.TokenUsage) != 25 {
		t.Fatalf("expected 25 hourly boundary buckets, got %d: %+v", len(analysis.TokenUsage), analysis.TokenUsage)
	}
	for index, bucket := range analysis.TokenUsage {
		expectedBucket := start.Add(time.Duration(index) * time.Hour)
		if !bucket.Bucket.Equal(expectedBucket) {
			t.Fatalf("expected bucket %d to be %s, got %s", index, expectedBucket, bucket.Bucket)
		}
	}
	if analysis.TokenUsage[22].TotalTokens != 30 || analysis.TokenUsage[22].Requests != 3 {
		t.Fatalf("expected existing stat row to remain in 22:00 bucket, got %+v", analysis.TokenUsage[22])
	}
	if analysis.TokenUsage[23].TotalTokens != 50 || analysis.TokenUsage[23].Requests != 4 {
		t.Fatalf("expected 23:00 stat row to be included, got %+v", analysis.TokenUsage[23])
	}
	if analysis.TokenUsage[24].TotalTokens != 0 || analysis.TokenUsage[24].Requests != 0 {
		t.Fatalf("expected empty 24:00 boundary bucket, got %+v", analysis.TokenUsage[24])
	}
}

func TestBuildAnalysisWithFilterIncludesPartialCurrentDayInDailyRanges(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openUsageTestDatabase(t)
	start := time.Date(2026, 5, 11, 10, 15, 0, 0, time.UTC)
	end := time.Date(2026, 5, 18, 18, 30, 0, 0, time.UTC)
	yesterday := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	currentDayHour := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "sk-target-key", DisplayKey: "sk-*********target"}).Error; err != nil {
		t.Fatalf("insert CPA API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart:  yesterday,
		APIGroupKey:  "sk-target-key",
		Model:        "claude-sonnet",
		RequestCount: 2,
		InputTokens:  10,
		OutputTokens: 20,
		TotalTokens:  30,
	}).Error; err != nil {
		t.Fatalf("insert daily stat: %v", err)
	}
	if err := db.Create([]entities.UsageOverviewHourlyStat{
		{
			BucketStart:  yesterday.Add(9 * time.Hour),
			APIGroupKey:  "sk-target-key",
			Model:        "claude-sonnet",
			RequestCount: 8,
			InputTokens:  80,
			OutputTokens: 90,
			TotalTokens:  170,
		},
		{
			BucketStart:  currentDayHour,
			APIGroupKey:  "sk-target-key",
			Model:        "claude-sonnet",
			RequestCount: 4,
			InputTokens:  40,
			OutputTokens: 50,
			TotalTokens:  90,
		},
	}).Error; err != nil {
		t.Fatalf("insert hourly stats: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}

	for _, rangeValue := range []string{"7d", "30d"} {
		t.Run(rangeValue, func(t *testing.T) {
			analysis, err := BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: rangeValue, StartTime: &start, EndTime: &end})
			if err != nil {
				t.Fatalf("BuildAnalysisWithFilter returned error: %v", err)
			}

			if analysis.Granularity != "daily" {
				t.Fatalf("expected daily granularity, got %q", analysis.Granularity)
			}
			if len(analysis.TokenUsage) != 2 {
				t.Fatalf("expected yesterday and current-day buckets, got %+v", analysis.TokenUsage)
			}
			if !analysis.TokenUsage[0].Bucket.Equal(yesterday) || analysis.TokenUsage[0].TotalTokens != 30 || analysis.TokenUsage[0].Requests != 2 {
				t.Fatalf("expected yesterday daily stats first, got %+v", analysis.TokenUsage[0])
			}
			if !analysis.TokenUsage[1].Bucket.Equal(time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)) || analysis.TokenUsage[1].TotalTokens != 90 || analysis.TokenUsage[1].Requests != 4 {
				t.Fatalf("expected current-day hourly stats to be folded into daily bucket, got %+v", analysis.TokenUsage[1])
			}
			if len(analysis.APIKeyComposition) != 1 || analysis.APIKeyComposition[0].TotalTokens != 120 || analysis.APIKeyComposition[0].Requests != 6 {
				t.Fatalf("expected compositions to include daily and current-day hourly stats, got %+v", analysis.APIKeyComposition)
			}
		})
	}
}

func TestListUsageEventsWithFilterFiltersByAPIGroupKey(t *testing.T) {
	db := openUsageTestDatabase(t)
	insertAPIKeyFilterEvents(t, db)

	page, err := ListUsageEventsWithFilter(db, repodto.UsageQueryFilter{APIGroupKey: "sk-target-key", Page: 1, PageSize: 100, Limit: 100})
	if err != nil {
		t.Fatalf("ListUsageEventsWithFilter returned error: %v", err)
	}
	if page.TotalCount != 2 || len(page.Events) != 2 {
		t.Fatalf("expected only target key events, got %+v", page)
	}
	for _, event := range page.Events {
		if event.APIGroupKey != "sk-target-key" {
			t.Fatalf("expected target key only, got %+v", page.Events)
		}
	}
}

func insertAPIKeyFilterEvents(t *testing.T, db *gorm.DB) {
	t.Helper()
	events := []entities.UsageEvent{
		{EventKey: "target-1", APIGroupKey: "sk-target-key", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC), Source: "source-a", AuthIndex: "1", Failed: false, LatencyMS: 100, InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
		{EventKey: "target-2", APIGroupKey: "sk-target-key", Model: "claude-opus", Timestamp: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC), Source: "source-b", AuthIndex: "2", Failed: true, LatencyMS: 200, InputTokens: 15, OutputTokens: 25, TotalTokens: 40},
		{EventKey: "other-1", APIGroupKey: "sk-other-key", Model: "claude-other", Timestamp: time.Date(2026, 4, 20, 11, 0, 0, 0, time.UTC), Source: "source-c", AuthIndex: "3", Failed: false, LatencyMS: 300, InputTokens: 100, OutputTokens: 200, TotalTokens: 300},
	}
	if _, _, err := InsertUsageEvents(db, events); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
}

func openUsageTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "dto.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	return db
}
