package test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestUsageServiceComparisonsKeepStoredFeesAcrossPricingMutations(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	key := entities.CPAAPIKey{APIKey: "sk-comparison-current", KeyAlias: "Current Key"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	sharedLabel := "Shared Display"
	identities := []entities.UsageIdentity{
		{Name: "Auth Source", Alias: &sharedLabel, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "auth-one", Type: "codex"},
		{Name: "Provider Source", Alias: &sharedLabel, AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-one", Type: "codex"},
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	feeAuth, feeProvider, feeUnknown := 2.5, 3.75, 1.25
	available, unavailable := int64(0), int64(1)
	// 两个同名来源按稳定身份分别计入；未知身份只进入模型和 Key，不伪造凭据归属。
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: day, APIGroupKey: key.APIKey, Model: "model-a", ModelAlias: "alias-a", AuthIndex: "auth-one", RequestCount: 2, SuccessCount: 2, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &feeAuth, UnavailableCostCount: &available},
		{BucketStart: day, APIGroupKey: key.APIKey, Model: "model-a", AuthIndex: "provider-one", RequestCount: 1, SuccessCount: 1, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: &feeProvider, UnavailableCostCount: &available},
		{BucketStart: day, APIGroupKey: "sk-comparison-deleted", Model: "model-unknown", AuthIndex: "missing-identity", RequestCount: 1, SuccessCount: 1, InputTokens: 100, TotalTokens: 100, CostUSD: &feeUnknown, UnavailableCostCount: &unavailable},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog := pricing.NewCatalog(snapshot)
	provider, ok := service.NewUsageService(db, catalog).(service.UsageComparisonProvider)
	if !ok {
		t.Fatal("usage service does not expose comparisons")
	}
	end := day.AddDate(0, 0, 1)
	filter := servicedto.UsageFilter{Range: "custom", CustomUnit: "day", StartTime: &day, EndTime: &end, EndExclusive: true}

	assertComparison := func() {
		t.Helper()
		result, err := provider.GetUsageOverviewComparisons(context.Background(), filter)
		if err != nil {
			t.Fatal(err)
		}
		if result.Comparisons == nil {
			t.Fatal("comparison result missing")
		}
		models, keys := result.Comparisons.Models, result.Comparisons.APIKeys
		if len(models) != 2 || models["alias-a"] != nil || models["model-a"] == nil || models["model-unknown"] == nil ||
			models["model-a"].Requests != 3 || models["model-a"].TotalTokens != 2_000_000 ||
			!models["model-a"].CostAvailable || !usageFilterCostClose(models["model-a"].CostUSD, 6.25) ||
			models["model-unknown"].CostAvailable || !usageFilterCostClose(models["model-unknown"].CostUSD, 1.25) {
			t.Fatalf("model comparison lost stored fee/availability: %+v", models)
		}
		if len(keys) != 2 || keys[key.APIKey] == nil || keys["sk-comparison-deleted"] == nil ||
			!keys[key.APIKey].CostAvailable || !usageFilterCostClose(keys[key.APIKey].CostUSD, 6.25) ||
			keys["sk-comparison-deleted"].CostAvailable {
			t.Fatalf("Key comparison lost stored fee/deleted history: %+v", keys)
		}
		authFiles, aiProviders := result.Comparisons.AuthFiles, result.Comparisons.AIProviders
		if len(authFiles) != 1 || len(aiProviders) != 1 || authFiles["auth-one"] == nil || aiProviders["provider-one"] == nil ||
			authFiles["auth-one"].Label != sharedLabel || aiProviders["provider-one"].Label != sharedLabel ||
			!authFiles["auth-one"].CostAvailable || !aiProviders["provider-one"].CostAvailable ||
			!usageFilterCostClose(authFiles["auth-one"].CostUSD, feeAuth) || !usageFilterCostClose(aiProviders["provider-one"].CostUSD, feeProvider) {
			t.Fatalf("typed identity comparison lost stored fee/alias: auth=%+v provider=%+v", authFiles, aiProviders)
		}
		filtered := filter
		filtered.APIKeyID = strconv.FormatInt(key.ID, 10)
		result, err = provider.GetUsageOverviewComparisons(context.Background(), filtered)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Comparisons.APIKeys) != 1 || result.Comparisons.APIKeys[key.APIKey] == nil || len(result.Comparisons.Models) != 1 ||
			result.Comparisons.Models["model-a"] == nil || !usageFilterCostClose(result.Comparisons.Models["model-a"].CostUSD, 6.25) {
			t.Fatalf("filtered comparison escaped API Key scope: %+v", result.Comparisons)
		}
	}
	assertComparison()
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 90}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Replace(snapshot)
	assertComparison()
	if err := repository.DeleteModelPriceSettingRequired(db, "model-a"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Replace(snapshot)
	assertComparison()
}

func TestUsageServiceComparisonsUseStoredFeesAtPartialHourBoundaries(t *testing.T) {
	withUsageServiceLocation(t, "UTC")
	db := openUsageServiceTestDatabase(t)
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 9}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 2, 12, 5, 0, 0, time.UTC)
	end := start.Add(50 * time.Minute)
	now := end.Add(time.Hour)
	events := []entities.UsageEvent{
		storedUsageEventFee(entities.UsageEvent{EventKey: "comparison-boundary-a", APIGroupKey: "sk-boundary", Model: "model-a", Timestamp: start.Add(5 * time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000}, 0.4, true),
		storedUsageEventFee(entities.UsageEvent{EventKey: "comparison-boundary-b", APIGroupKey: "sk-boundary", Model: "model-b", Timestamp: end.Add(-5 * time.Minute), InputTokens: 50, TotalTokens: 50}, 0.7, false),
		storedUsageEventFee(entities.UsageEvent{EventKey: "comparison-outside", APIGroupKey: "sk-boundary", Model: "model-a", Timestamp: end.Add(time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000}, 99, true),
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := service.NewUsageService(db, pricing.NewCatalog(snapshot)).(service.UsageComparisonProvider)
	if !ok {
		t.Fatal("usage service does not expose comparisons")
	}
	result, err := provider.GetUsageOverviewComparisons(context.Background(), servicedto.UsageFilter{
		Range: "custom", StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Comparisons == nil || len(result.Comparisons.Models) != 2 || len(result.Comparisons.APIKeys) != 1 {
		t.Fatalf("partial-hour comparison dimensions: %+v", result.Comparisons)
	}
	modelA, modelB := result.Comparisons.Models["model-a"], result.Comparisons.Models["model-b"]
	key := result.Comparisons.APIKeys["sk-boundary"]
	if modelA == nil || modelA.Requests != 1 || !modelA.CostAvailable || !usageFilterCostClose(modelA.CostUSD, 0.4) ||
		modelB == nil || modelB.Requests != 1 || modelB.CostAvailable || !usageFilterCostClose(modelB.CostUSD, 0.7) ||
		key == nil || key.Requests != 2 || key.CostAvailable || !usageFilterCostClose(key.CostUSD, 1.1) {
		t.Fatalf("partial-hour comparison lost persisted fee/availability: models=%+v keys=%+v", result.Comparisons.Models, result.Comparisons.APIKeys)
	}
}
