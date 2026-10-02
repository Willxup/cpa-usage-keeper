package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// 分析各费用视图共用已存金额，修改或删除价目不改变总额及原有 Token／请求分母。
func TestUsageServiceAnalysisKeepsStoredCostsAcrossViewsAndPricingChanges(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	ctx := context.Background()
	key := entities.CPAAPIKey{APIKey: "sk-analysis-stored-test", KeyAlias: "Analysis"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	identities := []entities.UsageIdentity{
		{Name: "Same name", AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "analysis-auth"},
		{Name: "Same name", AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "analysis-provider"},
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	feeAuth, feeProvider, excludedFee, available := 2.5, 3.75, 999.0, int64(0)
	rows := []entities.UsageOverviewHourlyStat{
		{BucketStart: start, APIGroupKey: key.APIKey, Model: "analysis-model", AuthIndex: "analysis-auth", RequestCount: 2, InputTokens: 200, OutputTokens: 100, TotalTokens: 300, CostUSD: &feeAuth, UnavailableCostCount: &available},
		{BucketStart: start, APIGroupKey: key.APIKey, Model: "analysis-model", AuthIndex: "analysis-provider", RequestCount: 2, InputTokens: 200, OutputTokens: 100, TotalTokens: 300, CostUSD: &feeProvider, UnavailableCostCount: &available},
		{BucketStart: start, APIGroupKey: "deleted-key", Model: "excluded-model", RequestCount: 1, TotalTokens: 9999, CostUSD: &excludedFee, UnavailableCostCount: &available},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "analysis-model", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadPricingSnapshot(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := pricing.NewCatalog(snapshot)
	provider := service.NewUsageService(db, catalog)
	filter := servicedto.UsageFilter{Range: "custom", CustomUnit: "hour", StartTime: &start, EndTime: &end, EndExclusive: true}
	assertViews := func() {
		t.Helper()
		result, err := provider.GetAnalysis(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if !result.CostSummary.CostAvailable || !usageFilterCostClose(result.CostSummary.TotalCostUSD, 6.25) {
			t.Fatalf("stored summary changed: %+v", result.CostSummary)
		}
		if len(result.TokenUsage) != 1 || !result.TokenUsage[0].CostAvailable || !usageFilterCostClose(result.TokenUsage[0].CostUSD, 6.25) || result.TokenUsage[0].TotalTokens != 600 || result.TokenUsage[0].Requests != 4 {
			t.Fatalf("trend fee or original counts changed: %+v", result.TokenUsage)
		}
		for _, view := range []struct {
			name  string
			items []servicedto.AnalysisCompositionItem
			key   string
			cost  float64
		}{
			{"model", result.ModelComposition, "analysis-model", 6.25},
			{"key", result.APIKeyComposition, key.APIKey, 6.25},
			{"auth", result.AuthFilesComposition, "analysis-auth", feeAuth},
			{"provider", result.AIProviderComposition, "analysis-provider", feeProvider},
		} {
			if len(view.items) != 1 || view.items[0].Key != view.key || !view.items[0].CostAvailable || !usageFilterCostClose(view.items[0].CostUSD, view.cost) {
				t.Fatalf("%s composition changed: %+v", view.name, view.items)
			}
		}
		if len(result.Heatmap) != 1 || !result.Heatmap[0].CostAvailable || !usageFilterCostClose(result.Heatmap[0].CostUSD, 6.25) || result.Heatmap[0].TotalTokens != 600 {
			t.Fatalf("heatmap changed: %+v", result.Heatmap)
		}
		if len(result.ModelEfficiency) != 1 || !result.ModelEfficiency[0].CostAvailable || !usageFilterCostClose(result.ModelEfficiency[0].CostUSD, 6.25) || !usageFilterCostClose(result.ModelEfficiency[0].CostPerRequestUSD, 1.5625) || result.ModelEfficiency[0].Requests != 4 || result.ModelEfficiency[0].OutputTokensPerRequest != 50 {
			t.Fatalf("cost efficiency changed its fee or denominator: %+v", result.ModelEfficiency)
		}
	}
	assertViews()
	for _, remove := range []bool{false, true} {
		if remove {
			err = repository.DeleteModelPriceSettingRequired(db, "analysis-model")
		} else {
			_, err = repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "analysis-model", PromptPricePer1M: 90})
		}
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err = repository.LoadPricingSnapshot(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		catalog.Replace(snapshot)
		assertViews()
	}
}
