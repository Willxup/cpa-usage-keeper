package test

import "cpa-usage-keeper/internal/pricing"

func emptyPricingCatalogForTest() *pricing.Catalog {
	return pricing.NewCatalog(pricing.EmptySnapshot())
}
