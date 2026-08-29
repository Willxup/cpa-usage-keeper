package test

import (
	"testing"

	"cpa-usage-keeper/internal/pricing"
	. "cpa-usage-keeper/internal/quota"
)

func TestQuotaServiceRequiresExplicitPricingCatalog(t *testing.T) {
	t.Parallel()

	catalog := pricing.NewCatalog(pricing.EmptySnapshot())
	service := NewServiceWithRegistry(nil, NewProviderRegistry(nil), catalog)
	service.StopRefreshTasks()

	defer func() {
		if recover() == nil {
			t.Fatal("expected constructor to reject a missing pricing catalog")
		}
	}()
	NewServiceWithRegistry(nil, NewProviderRegistry(nil), nil)
}

func emptyPricingCatalogForTest() *pricing.Catalog {
	return pricing.NewCatalog(pricing.EmptySnapshot())
}
