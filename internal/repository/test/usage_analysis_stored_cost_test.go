package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func analysisCostPtr(value float64) *float64 { return &value }

func analysisCountPtr(value int64) *int64 { return &value }

// Analysis 的趋势、构成及效率共用已存汇总费用，当前模型价不能改写历史金额。
func TestAnalysisReadsStoredHourlyCostInsteadOfCurrentPrice(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	upsertUsageCostResolverPrice(t, db, "model-a", 9)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "key-a", DisplayKey: "Key A"}).Error; err != nil {
		t.Fatalf("seed active Key: %v", err)
	}
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cost, unavailable := 2.5, int64(0)
	if err := db.Create(&entities.UsageOverviewHourlyStat{
		BucketStart: start, APIGroupKey: "key-a", Model: "model-a", RequestCount: 1, SuccessCount: 1,
		InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &cost, UnavailableCostCount: &unavailable,
	}).Error; err != nil {
		t.Fatalf("seed stored hourly fee: %v", err)
	}
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("build Analysis: %v", err)
	}
	if !finiteStoredOverviewTestCost(analysis.CostSummary.TotalCostUSD, 2.5) {
		t.Fatalf("Analysis total re-priced stored fee: %+v", analysis.CostSummary)
	}
	if len(analysis.TokenUsage) != 1 || !finiteStoredOverviewTestCost(analysis.TokenUsage[0].CostUSD, 2.5) || analysis.TokenUsage[0].TotalTokens != 1_000_000 || !analysis.TokenUsage[0].CostAvailable {
		t.Fatalf("hourly trend lost stored fee or original tokens: %+v", analysis.TokenUsage)
	}
	if len(analysis.ModelEfficiency) != 1 || !finiteStoredOverviewTestCost(analysis.ModelEfficiency[0].CostPerRequestUSD, 2.5) {
		t.Fatalf("model efficiency changed its request denominator: %+v", analysis.ModelEfficiency)
	}
}

// 长范围 Analysis 只用日汇总的已存金额，当前报价与原始事件均不参与。
func TestAnalysisReadsStoredDailyCostWithoutRawEvents(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	upsertUsageCostResolverPrice(t, db, "model-a", 9)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "key-a", DisplayKey: "Key A"}).Error; err != nil {
		t.Fatalf("seed active Key: %v", err)
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 4)
	bucket := start.AddDate(0, 0, 1)
	cost, unavailable := 4.75, int64(0)
	if err := db.Create(&entities.UsageOverviewDailyStat{
		BucketStart: bucket, APIGroupKey: "key-a", Model: "model-a", RequestCount: 2,
		InputTokens: 2_000_000, TotalTokens: 2_000_000, CostUSD: &cost, UnavailableCostCount: &unavailable,
	}).Error; err != nil {
		t.Fatalf("seed daily fee: %v", err)
	}
	if err := db.Migrator().DropTable(&entities.UsageEvent{}); err != nil {
		t.Fatalf("drop raw events: %v", err)
	}
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "day", StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("build daily Analysis: %v", err)
	}
	if !finiteStoredOverviewTestCost(analysis.CostSummary.TotalCostUSD, 4.75) || len(analysis.TokenUsage) != 1 || !finiteStoredOverviewTestCost(analysis.TokenUsage[0].CostUSD, 4.75) || analysis.TokenUsage[0].TotalTokens != 2_000_000 {
		t.Fatalf("daily Analysis did not preserve stored fee and tokens: %+v", analysis)
	}
}

// 免费请求与缺价请求都存零金额，但只有缺价使聚合费用不可用。
func TestAnalysisSeparatesStoredFreeAndUnavailableCosts(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	if err := db.Create(&entities.CPAAPIKey{APIKey: "key-a", DisplayKey: "Key A"}).Error; err != nil {
		t.Fatalf("seed active Key: %v", err)
	}
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	rows := []entities.UsageOverviewHourlyStat{
		{BucketStart: start, APIGroupKey: "key-a", Model: "priced", RequestCount: 1, TotalTokens: 10, CostUSD: analysisCostPtr(2), UnavailableCostCount: analysisCountPtr(0)},
		{BucketStart: start, APIGroupKey: "key-a", Model: "free", RequestCount: 1, TotalTokens: 20, CostUSD: analysisCostPtr(0), UnavailableCostCount: analysisCountPtr(0)},
		{BucketStart: start, APIGroupKey: "key-a", Model: "missing", RequestCount: 1, TotalTokens: 30, CostUSD: analysisCostPtr(0), UnavailableCostCount: analysisCountPtr(1)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed mixed availability: %v", err)
	}
	analysis, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true})
	if err != nil {
		t.Fatalf("build mixed Analysis: %v", err)
	}
	if !finiteStoredOverviewTestCost(analysis.CostSummary.TotalCostUSD, 2) || analysis.CostSummary.CostAvailable || len(analysis.TokenUsage) != 1 || analysis.TokenUsage[0].TotalTokens != 60 || analysis.TokenUsage[0].CostAvailable {
		t.Fatalf("mixed availability misreported: %+v", analysis)
	}
	models := make(map[string]bool, len(analysis.ModelComposition))
	for _, item := range analysis.ModelComposition {
		models[item.Key] = item.CostAvailable
	}
	if !models["free"] || models["missing"] || !models["priced"] {
		t.Fatalf("model availability lost free/missing distinction: %+v", analysis.ModelComposition)
	}
}

// 未回填的小时或日费用不能被 Analysis 当成合法零金额展示。
func TestAnalysisRejectsUnbackfilledStoredCosts(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	for _, grain := range []string{"hour", "day"} {
		for _, missing := range []string{"cost", "availability"} {
			t.Run(grain+"/"+missing, func(t *testing.T) {
				db := openTestDatabase(t)
				if err := db.Create(&entities.CPAAPIKey{APIKey: "key-a", DisplayKey: "Key A"}).Error; err != nil {
					t.Fatalf("seed active Key: %v", err)
				}
				start := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
				end := start.Add(time.Hour)
				cost, unavailable := analysisCostPtr(1), analysisCountPtr(0)
				if missing == "cost" {
					cost = nil
				} else {
					unavailable = nil
				}
				if grain == "day" {
					end = start.AddDate(0, 0, 2)
					if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: start, APIGroupKey: "key-a", Model: "model-a", RequestCount: 1, CostUSD: cost, UnavailableCostCount: unavailable}).Error; err != nil {
						t.Fatalf("seed NULL daily fee: %v", err)
					}
				} else if err := db.Create(&entities.UsageOverviewHourlyStat{BucketStart: start, APIGroupKey: "key-a", Model: "model-a", RequestCount: 1, CostUSD: cost, UnavailableCostCount: unavailable}).Error; err != nil {
					t.Fatalf("seed NULL hourly fee: %v", err)
				}
				if _, err := repository.BuildAnalysisWithFilter(db, repodto.UsageQueryFilter{Range: "custom", CustomUnit: grain, StartTime: &start, EndTime: &end, EndExclusive: true}); err == nil {
					t.Fatalf("Analysis accepted unbackfilled %s %s", grain, missing)
				}
			})
		}
	}
}
