package test

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

var analysisProjectionFixedColumns = []string{
	"bucket_start",
	"api_group_key",
	"model",
	"auth_index",
	"model_alias",
	"request_count",
	"input_tokens",
	"output_tokens",
	"reasoning_tokens",
	"cache_read_tokens",
	"cache_creation_tokens",
	"total_tokens",
}

func TestAnalysisProjectionSelectsOnlyFixedColumnsWithoutPricingRules(t *testing.T) {
	tests := []struct {
		name      string
		start     time.Time
		end       time.Time
		wantTable string
	}{
		{
			name:      "hourly",
			start:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local),
			end:       time.Date(2026, 1, 1, 1, 0, 0, 0, time.Local),
			wantTable: "usage_overview_hourly_stats",
		},
		{
			name:      "daily",
			start:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local),
			end:       time.Date(2026, 1, 4, 0, 0, 0, 0, time.Local),
			wantTable: "usage_overview_daily_stats",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDatabase(t)
			queries := captureAnalysisRollupQueries(t, db)
			if _, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
				Range: "custom", StartTime: &tt.start, EndTime: &tt.end, EndExclusive: true,
			}, emptyPricingResolverForTest()); err != nil {
				t.Fatalf("BuildAnalysisWithFilter: %v", err)
			}

			query := requireSingleAnalysisRollupQuery(t, *queries, tt.wantTable)
			if got := analysisProjectionSelectColumns(t, query); !reflect.DeepEqual(got, analysisProjectionFixedColumns) {
				t.Fatalf("selected columns = %#v, want %#v\nSQL: %s", got, analysisProjectionFixedColumns, query)
			}
			if strings.Contains(query, " group by ") {
				t.Fatalf("Analysis projection must not use GROUP BY: %s", query)
			}
		})
	}
}

func TestAnalysisProjectionAddsOnlyActivePricingDimensions(t *testing.T) {
	tests := []struct {
		name         string
		rules        []pricing.RuleConfig
		wantOptional []string
	}{
		{
			name: "service tier and endpoint",
			rules: []pricing.RuleConfig{
				{Key: "service_tier", Value: "priority", Multiplier: 2},
				{Key: "endpoint", Value: "/responses", Multiplier: 3},
			},
			wantOptional: []string{"service_tier", "endpoint"},
		},
		{
			name:         "response service tier",
			rules:        []pricing.RuleConfig{{Key: "response_service_tier", Value: "batch", Multiplier: 2}},
			wantOptional: []string{"response_service_tier"},
		},
		{
			name:         "reasoning effort",
			rules:        []pricing.RuleConfig{{Key: "reasoning_effort", Value: "xhigh", Multiplier: 2}},
			wantOptional: []string{"reasoning_effort"},
		},
		{
			name:         "executor type",
			rules:        []pricing.RuleConfig{{Key: "executor_type", Value: "cli", Multiplier: 2}},
			wantOptional: []string{"executor_type"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDatabase(t)
			queries := captureAnalysisRollupQueries(t, db)
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
			end := start.Add(time.Hour)
			if _, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
				Range: "custom", StartTime: &start, EndTime: &end, EndExclusive: true,
			}, repositoryPricingResolver(t, tt.rules)); err != nil {
				t.Fatalf("BuildAnalysisWithFilter: %v", err)
			}

			query := requireSingleAnalysisRollupQuery(t, *queries, "usage_overview_hourly_stats")
			want := append(append([]string{}, analysisProjectionFixedColumns...), tt.wantOptional...)
			if got := analysisProjectionSelectColumns(t, query); !reflect.DeepEqual(got, want) {
				t.Fatalf("selected columns = %#v, want %#v\nSQL: %s", got, want, query)
			}
		})
	}
}

func TestAnalysisProjectionPreservesHourlyResultsAndAPIKeyFiltering(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Date(2026, 2, 1, 6, 0, 0, 0, time.Local)
	end := bucket.Add(time.Hour)
	if err := db.Create(&[]entities.CPAAPIKey{
		{APIKey: "group-a", DisplayKey: "sk-***a"},
		{APIKey: "group-deleted", DisplayKey: "sk-***deleted", IsDeleted: true},
	}).Error; err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	seedAnalysisProjectionIdentities(t, db, "identity-a")
	if err := db.Create(&[]entities.UsageOverviewHourlyStat{
		{
			BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", AuthIndex: "identity-a", ModelAlias: "alias-a",
			ServiceTier: "priority", Endpoint: "/responses", RequestCount: 2, InputTokens: 1_000_000, OutputTokens: 300_000,
			ReasoningTokens: 100_000, CacheReadTokens: 200_000, CacheCreationTokens: 100_000, TotalTokens: 1_300_000,
		},
		{
			BucketStart: bucket, APIGroupKey: "group-deleted", Model: "model-a", RequestCount: 99,
			InputTokens: 99_000_000, TotalTokens: 99_000_000,
		},
	}).Error; err != nil {
		t.Fatalf("seed hourly stats: %v", err)
	}
	resolver := analysisProjectionPricingResolver(t, []pricing.RuleConfig{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "endpoint", Value: "/responses", Multiplier: 3},
	})
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", StartTime: &bucket, EndTime: &end, EndExclusive: true,
	}, resolver)
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter: %v", err)
	}

	if analysis.Granularity != repodto.AnalysisGranularityHourly || len(analysis.TokenUsage) != 1 {
		t.Fatalf("unexpected hourly result: %+v", analysis)
	}
	usage := analysis.TokenUsage[0]
	if !usage.Bucket.Equal(bucket) || usage.Requests != 2 || usage.InputTokens != 1_000_000 || usage.OutputTokens != 300_000 ||
		usage.ReasoningTokens != 100_000 || usage.CacheReadTokens != 200_000 || usage.CacheCreationTokens != 100_000 || usage.TotalTokens != 1_300_000 || !usage.CostAvailable {
		t.Fatalf("unexpected hourly token usage: %+v", usage)
	}
	assertFloatClose(t, usage.CostUSD, 9.3)
	assertAnalysisProjectionComposition(t, analysis.APIKeyComposition, "group-a", "", 2, 1_300_000, 9.3)
	assertAnalysisProjectionComposition(t, analysis.ModelComposition, "alias-a", "alias-a", 2, 1_300_000, 9.3)
	assertAnalysisProjectionComposition(t, analysis.AuthFilesComposition, "identity-a", "Auth Account", 2, 1_300_000, 9.3)
	assertAnalysisProjectionComposition(t, analysis.AIProviderComposition, "identity-a", "Provider Account", 2, 1_300_000, 9.3)
	if len(analysis.Heatmap) != 1 || analysis.Heatmap[0].APIKey != "group-a" || analysis.Heatmap[0].Model != "alias-a" ||
		analysis.Heatmap[0].Requests != 2 || analysis.Heatmap[0].TotalTokens != 1_300_000 || !analysis.Heatmap[0].CostAvailable {
		t.Fatalf("unexpected heatmap: %+v", analysis.Heatmap)
	}
	assertFloatClose(t, analysis.Heatmap[0].CostUSD, 9.3)
	if len(analysis.ModelEfficiency) != 1 {
		t.Fatalf("unexpected model efficiency: %+v", analysis.ModelEfficiency)
	}
	efficiency := analysis.ModelEfficiency[0]
	if efficiency.Model != "alias-a" || efficiency.Requests != 2 || efficiency.TotalTokens != 1_300_000 || !efficiency.CostAvailable {
		t.Fatalf("unexpected model efficiency: %+v", efficiency)
	}
	assertFloatClose(t, efficiency.CostUSD, 9.3)
	assertFloatClose(t, efficiency.CostPerRequestUSD, 4.65)
	assertFloatClose(t, efficiency.OutputTokensPerRequest, 150_000)
	assertFloatClose(t, efficiency.CacheReadRate, 0.2)
	assertFloatClose(t, analysis.CostBreakdown.UncachedInputCostUSD, 4.2)
	assertFloatClose(t, analysis.CostBreakdown.CacheReadCostUSD, 0.6)
	assertFloatClose(t, analysis.CostBreakdown.CacheWriteCostUSD, 0.9)
	assertFloatClose(t, analysis.CostBreakdown.OutputCostUSD, 3.6)
	assertFloatClose(t, analysis.CostBreakdown.TotalCostUSD, 9.3)
	if !analysis.CostBreakdown.CostAvailable {
		t.Fatalf("expected available cost breakdown: %+v", analysis.CostBreakdown)
	}

	if err := db.Create(&entities.CPAAPIKey{APIKey: "group-b", DisplayKey: "sk-***b"}).Error; err != nil {
		t.Fatalf("seed second active API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: bucket, APIGroupKey: "group-b", Model: "model-a", RequestCount: 7, InputTokens: 7_000, TotalTokens: 7_000,
	}).Error; err != nil {
		t.Fatalf("seed filtered hourly stat: %v", err)
	}
	filtered, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", APIGroupKey: "group-a", StartTime: &bucket, EndTime: &end, EndExclusive: true,
	}, resolver)
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter with API key: %v", err)
	}
	assertAnalysisProjectionComposition(t, filtered.APIKeyComposition, "group-a", "", 2, 1_300_000, 9.3)
}

func TestAnalysisProjectionPreservesDailyResultsWithoutBoundaryHourlyRows(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 2, 1, 5, 0, 0, 0, time.Local)
	end := time.Date(2026, 2, 4, 8, 0, 0, 0, time.Local)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "group-a", DisplayKey: "sk-***a"}).Error; err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	seedAnalysisProjectionIdentities(t, db, "identity-a")
	sharedHourly := func(bucket time.Time, requests, input, output, reasoning, cacheRead, cacheCreation, total int64) entities.UsageOverviewHourlyStat {
		return entities.UsageOverviewHourlyStat{
			BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", AuthIndex: "identity-a", ModelAlias: "alias-a",
			ResponseServiceTier: "batch", ReasoningEffort: "xhigh", ExecutorType: "cli", RequestCount: requests,
			InputTokens: input, OutputTokens: output, ReasoningTokens: reasoning, CacheReadTokens: cacheRead,
			CacheCreationTokens: cacheCreation, TotalTokens: total,
		}
	}
	if err := db.Create(&[]entities.UsageOverviewHourlyStat{
		sharedHourly(time.Date(2026, 2, 1, 6, 0, 0, 0, time.Local), 1, 100, 10, 2, 20, 10, 110),
		sharedHourly(time.Date(2026, 2, 4, 2, 0, 0, 0, time.Local), 3, 300, 30, 6, 60, 30, 330),
	}).Error; err != nil {
		t.Fatalf("seed boundary hourly stats: %v", err)
	}
	if err := db.Create(&[]entities.UsageOverviewDailyStat{
		{
			BucketStart: time.Date(2026, 2, 2, 0, 0, 0, 0, time.Local), APIGroupKey: "group-a", Model: "model-a",
			AuthIndex: "identity-a", ModelAlias: "alias-a", ResponseServiceTier: "batch", ReasoningEffort: "xhigh", ExecutorType: "cli",
			RequestCount: 2, InputTokens: 200, OutputTokens: 20, ReasoningTokens: 4, CacheReadTokens: 40,
			CacheCreationTokens: 20, TotalTokens: 220,
		},
		{
			BucketStart: time.Date(2026, 2, 4, 0, 0, 0, 0, time.Local), APIGroupKey: "group-a", Model: "model-a",
			AuthIndex: "identity-a", ModelAlias: "alias-a", ResponseServiceTier: "batch", ReasoningEffort: "xhigh", ExecutorType: "cli",
			RequestCount: 3, InputTokens: 300, OutputTokens: 30, ReasoningTokens: 6, CacheReadTokens: 60,
			CacheCreationTokens: 30, TotalTokens: 330,
		},
	}).Error; err != nil {
		t.Fatalf("seed daily stat: %v", err)
	}
	resolver := analysisProjectionPricingResolver(t, []pricing.RuleConfig{
		{Key: "response_service_tier", Value: "batch", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
		{Key: "executor_type", Value: "cli", Multiplier: 4},
	})
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", StartTime: &start, EndTime: &end, EndExclusive: true,
	}, resolver)
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter: %v", err)
	}

	if analysis.Granularity != repodto.AnalysisGranularityDaily || len(analysis.TokenUsage) != 2 {
		t.Fatalf("unexpected daily result: %+v", analysis)
	}
	wantBuckets := []struct {
		bucket   time.Time
		requests int64
		tokens   int64
	}{
		{time.Date(2026, 2, 2, 0, 0, 0, 0, time.Local), 2, 220},
		{time.Date(2026, 2, 4, 0, 0, 0, 0, time.Local), 3, 330},
	}
	for i, want := range wantBuckets {
		got := analysis.TokenUsage[i]
		if !got.Bucket.Equal(want.bucket) || got.Requests != want.requests || got.TotalTokens != want.tokens || !got.CostAvailable {
			t.Fatalf("token usage[%d] = %+v, want bucket=%v requests=%d total=%d", i, got, want.bucket, want.requests, want.tokens)
		}
	}
	assertAnalysisProjectionComposition(t, analysis.APIKeyComposition, "group-a", "", 5, 550, 0.0138)
	assertAnalysisProjectionComposition(t, analysis.ModelComposition, "alias-a", "alias-a", 5, 550, 0.0138)
	assertAnalysisProjectionComposition(t, analysis.AuthFilesComposition, "identity-a", "Auth Account", 5, 550, 0.0138)
	assertAnalysisProjectionComposition(t, analysis.AIProviderComposition, "identity-a", "Provider Account", 5, 550, 0.0138)
	if len(analysis.Heatmap) != 1 || analysis.Heatmap[0].Requests != 5 || analysis.Heatmap[0].TotalTokens != 550 {
		t.Fatalf("unexpected daily heatmap: %+v", analysis.Heatmap)
	}
	assertFloatClose(t, analysis.Heatmap[0].CostUSD, 0.0138)
	if len(analysis.ModelEfficiency) != 1 || analysis.ModelEfficiency[0].Requests != 5 || analysis.ModelEfficiency[0].TotalTokens != 550 {
		t.Fatalf("unexpected daily model efficiency: %+v", analysis.ModelEfficiency)
	}
	assertFloatClose(t, analysis.ModelEfficiency[0].CostUSD, 0.0138)
	assertFloatClose(t, analysis.CostBreakdown.UncachedInputCostUSD, 0.0084)
	assertFloatClose(t, analysis.CostBreakdown.CacheReadCostUSD, 0.0012)
	assertFloatClose(t, analysis.CostBreakdown.CacheWriteCostUSD, 0.0018)
	assertFloatClose(t, analysis.CostBreakdown.OutputCostUSD, 0.0024)
	assertFloatClose(t, analysis.CostBreakdown.TotalCostUSD, 0.0138)
}

func TestBuildAnalysisWithoutUsageEventsUsesOnlyRollupTables(t *testing.T) {
	db := openTestDatabase(t)
	bucket := time.Date(2026, 3, 1, 8, 0, 0, 0, time.Local)
	end := bucket.Add(time.Hour)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "group-a", DisplayKey: "sk-***a"}).Error; err != nil {
		t.Fatalf("seed API key: %v", err)
	}
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: bucket, APIGroupKey: "group-a", Model: "model-a", RequestCount: 1, InputTokens: 10, TotalTokens: 10,
	}).Error; err != nil {
		t.Fatalf("seed hourly stat: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop usage_events: %v", err)
	}
	queries := captureAllAnalysisQueries(t, db)
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{
		Range: "custom", StartTime: &bucket, EndTime: &end, EndExclusive: true,
	}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("BuildAnalysisWithFilter without usage_events: %v", err)
	}
	if len(analysis.TokenUsage) != 1 || analysis.TokenUsage[0].TotalTokens != 10 {
		t.Fatalf("unexpected Analysis result without usage_events: %+v", analysis)
	}
	foundRollup := false
	for _, query := range *queries {
		if strings.Contains(query, "usage_events") {
			t.Fatalf("Analysis queried usage_events: %s", query)
		}
		if strings.Contains(query, "usage_overview_hourly_stats") {
			foundRollup = true
		}
	}
	if !foundRollup {
		t.Fatalf("expected Analysis to query hourly rollup, got %#v", *queries)
	}
}

func captureAnalysisRollupQueries(t *testing.T, db *gorm.DB) *[]string {
	t.Helper()
	rollupQueries := make([]string, 0, 3)
	callbackName := "test:capture-analysis-rollup-only:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		query := strings.ToLower(strings.Join(strings.Fields(tx.Statement.SQL.String()), " "))
		if strings.Contains(query, "usage_overview_hourly_stats") || strings.Contains(query, "usage_overview_daily_stats") {
			rollupQueries = append(rollupQueries, query)
		}
	}); err != nil {
		t.Fatalf("register rollup query callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	return &rollupQueries
}

func captureAllAnalysisQueries(t *testing.T, db *gorm.DB) *[]string {
	t.Helper()
	queries := make([]string, 0, 4)
	callbackName := "test:capture-analysis-all:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		queries = append(queries, strings.ToLower(strings.Join(strings.Fields(tx.Statement.SQL.String()), " ")))
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	return &queries
}

func requireSingleAnalysisRollupQuery(t *testing.T, queries []string, table string) string {
	t.Helper()
	matching := make([]string, 0, 1)
	for _, query := range queries {
		if strings.Contains(query, table) {
			matching = append(matching, query)
		}
	}
	if len(matching) != 1 {
		t.Fatalf("queries for %s = %#v, want exactly one", table, matching)
	}
	return matching[0]
}

func analysisProjectionSelectColumns(t *testing.T, query string) []string {
	t.Helper()
	selectStart := strings.Index(query, "select ")
	fromStart := strings.Index(query, " from ")
	if selectStart < 0 || fromStart < 0 || fromStart <= selectStart {
		t.Fatalf("cannot parse SELECT columns from %q", query)
	}
	parts := strings.Split(query[selectStart+len("select "):fromStart], ",")
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		column := strings.TrimSpace(strings.ReplaceAll(part, "`", ""))
		if dot := strings.LastIndex(column, "."); dot >= 0 {
			column = column[dot+1:]
		}
		columns = append(columns, column)
	}
	return columns
}

func seedAnalysisProjectionIdentities(t *testing.T, db *gorm.DB, identity string) {
	t.Helper()
	if err := db.Create(&[]entities.UsageIdentity{
		{AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "authfile", Identity: identity, Name: "Auth Account"},
		{AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: identity, Name: "Provider Account"},
	}).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}
}

func analysisProjectionPricingResolver(t *testing.T, rules []pricing.RuleConfig) pricing.Resolver {
	t.Helper()
	multiplier := 1.0
	snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{
		Pricing: entities.ModelPriceSetting{
			Model: "model-a", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 1,
			CompletionPricePer1M: 2, CacheReadPricePer1M: 0.5, CacheWritePricePer1M: 1.5, PriceMultiplier: &multiplier,
		},
		Rules: rules,
	}})
	if err != nil {
		t.Fatalf("CompileSnapshot: %v", err)
	}
	return pricing.NewCatalog(snapshot).NewResolver()
}

func assertAnalysisProjectionComposition(t *testing.T, got []repodto.AnalysisCompositionRecord, key, label string, requests, totalTokens int64, cost float64) {
	t.Helper()
	if len(got) != 1 || got[0].Key != key || got[0].Label != label || got[0].Requests != requests || got[0].TotalTokens != totalTokens || !got[0].CostAvailable {
		t.Fatalf("composition = %+v, want key=%q label=%q requests=%d total=%d", got, key, label, requests, totalTokens)
	}
	assertFloatClose(t, got[0].CostUSD, cost)
}

func assertFloatClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("got %.15f, want %.15f", got, want)
	}
}
