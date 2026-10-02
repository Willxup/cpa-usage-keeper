package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func emptyPricingCatalogForTest() *pricing.Catalog {
	return pricing.NewCatalog(pricing.EmptySnapshot())
}

func newAPIPricingProvider(t *testing.T, db *gorm.DB, catalog *pricing.Catalog, fetchers ...service.ModelsFetcher) service.PricingProvider {
	t.Helper()
	recent, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	quotas := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(nil))
	lifecycle, cancel := context.WithCancel(context.Background())
	provider := service.NewPricingServiceWithRecalculation(db, catalog, service.PricingRecalculationDependencies{
		LifecycleCtx: lifecycle,
		Sync:         service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: catalog, RecentUsageEvents: recent}),
		Aggregation:  poller.NewUsageAggregationRunner(db),
		CostReadGate: service.NewCostReadGate(), Recent: recent, Quota: quotas, Now: time.Now,
	}, fetchers...)
	t.Cleanup(func() {
		cancel()
		provider.WaitPricingRecalculation()
		quotas.StopRefreshTasks()
		quotas.WaitRefreshTasks()
		recent.Close()
	})
	return provider
}

func newPricingRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if method != http.MethodGet {
		request.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
