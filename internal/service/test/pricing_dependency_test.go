package test

import (
	"testing"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/service"
)

func TestUsageServiceRequiresExplicitPricingCatalog(t *testing.T) {
	t.Parallel()

	catalog := pricing.NewCatalog(pricing.EmptySnapshot())
	if provider := service.NewUsageService(nil, catalog); provider == nil {
		t.Fatal("expected usage provider")
	}
	assertPanics(t, func() { service.NewUsageService(nil, nil) })
}

func emptyPricingCatalogForTest() *pricing.Catalog {
	return pricing.NewCatalog(pricing.EmptySnapshot())
}

func TestPricingServiceRequiresExplicitPricingCatalog(t *testing.T) {
	t.Parallel()

	db := openUsageServiceTestDatabase(t)
	catalog := pricing.NewCatalog(pricing.EmptySnapshot())
	deps, stop := newPricingTestDependencies(t, db, catalog)
	provider := service.NewPricingServiceWithRecalculation(db, catalog, deps)
	t.Cleanup(func() { stop(provider) })
	if provider == nil {
		t.Fatal("expected pricing provider")
	}
	assertPanics(t, func() { service.NewPricingServiceWithRecalculation(db, nil, deps) })
}

func assertPanics(t *testing.T, callback func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected constructor to reject a missing pricing catalog")
		}
	}()
	callback()
}
