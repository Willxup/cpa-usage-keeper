package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type stubModelsFetcher struct {
	result *response.ModelsResult
	err    error
}

func (s stubModelsFetcher) FetchModels(context.Context) (*response.ModelsResult, error) {
	return s.result, s.err
}

type pricingCatalogTransport struct{ body string }

func (transport pricingCatalogTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(transport.body))}, nil
}

func newPricingTestDependencies(t *testing.T, db *gorm.DB, catalog *pricing.Catalog) (service.PricingRecalculationDependencies, func(service.PricingProvider)) {
	t.Helper()
	recent, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	quotas := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(nil))
	lifecycle, cancel := context.WithCancel(context.Background())
	deps := service.PricingRecalculationDependencies{
		LifecycleCtx: lifecycle,
		Sync:         service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, RecentUsageEvents: recent}),
		Aggregation:  poller.NewUsageAggregationRunner(db),
		CostReadGate: service.NewCostReadGate(), Recent: recent, Quota: quotas, Now: time.Now,
	}
	return deps, func(provider service.PricingProvider) {
		cancel()
		if provider != nil {
			provider.WaitPricingRecalculation()
		}
		quotas.StopRefreshTasks()
		quotas.WaitRefreshTasks()
		recent.Close()
	}
}

func newPricingTestProvider(t *testing.T, db *gorm.DB, catalog *pricing.Catalog, fetchers ...service.ModelsFetcher) service.PricingProvider {
	t.Helper()
	deps, stop := newPricingTestDependencies(t, db, catalog)
	provider := service.NewPricingServiceWithRecalculation(db, catalog, deps, fetchers...)
	t.Cleanup(func() { stop(provider) })
	return provider
}

func newCatalogPricingService(t *testing.T, db *gorm.DB) (service.PricingProvider, *pricing.Catalog) {
	t.Helper()
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("load pricing snapshot: %v", err)
	}
	catalog := pricing.NewCatalog(snapshot)
	return newPricingTestProvider(t, db, catalog), catalog
}

func assertPricingDatabasePrompt(t *testing.T, db *gorm.DB, model string, want float64) {
	t.Helper()
	var row struct{ PromptPricePer1M float64 }
	if err := db.Table("model_price_settings").Select("prompt_price_per1_m").Where("model = ?", model).Take(&row).Error; err != nil {
		t.Fatalf("load stored price %q: %v", model, err)
	}
	if row.PromptPricePer1M != want {
		t.Fatalf("stored price for %q = %v, want %v", model, row.PromptPricePer1M, want)
	}
}

func TestPricingModelOptionsMergeCPAAndLocalModelsOrFallBack(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		fetcher stubModelsFetcher
		want    []string
	}{
		{"merge", stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: []models.ModelInfo{{ID: "zeta-model"}, {ID: " local-model "}, {ID: "zeta-model"}}}}}, []string{"local-model", "zeta-model"}},
		{"CPA failure", stubModelsFetcher{err: errors.New("CPA unavailable")}, []string{"local-model"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := openUsageServiceTestDatabase(t)
			if err := db.Create(&entities.UsageEvent{EventKey: "local-model-event", Model: "local-model"}).Error; err != nil {
				t.Fatal(err)
			}
			provider := newPricingTestProvider(t, db, emptyPricingCatalogForTest(), testCase.fetcher)
			got, err := provider.ListUsedModels(context.Background())
			if err != nil || !slices.Equal(got, testCase.want) {
				t.Fatalf("model options = %v, err = %v; want %v", got, err, testCase.want)
			}
		})
	}
}

func TestCompletePricingSaveDoesNotRequireModelInCPAList(t *testing.T) {
	provider := newPricingTestProvider(t, openUsageServiceTestDatabase(t), pricing.NewCatalog(pricing.EmptySnapshot()),
		stubModelsFetcher{err: errors.New("CPA unavailable")})
	input := pricing.ModelPricingConfig{Model: "custom/model", PricingStyle: "claude", BasePrices: pricing.BasePrices{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
		ModelMultiplier: 1, ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{}}
	if _, err := provider.SavePricingModel(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || len(listed.Models) != 1 || listed.Models[0].Model != input.Model || listed.Models[0].BasePrices != input.BasePrices {
		t.Fatalf("saved configuration = %+v, err = %v", listed, err)
	}
}
