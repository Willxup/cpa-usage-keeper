package test

import (
	"sync"
	"testing"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func TestCatalogResolverKeepsOneImmutableSnapshot(t *testing.T) {
	t.Parallel()

	oldSnapshot := compileSnapshot(t, pricing.ModelConfig{Pricing: testPricingWithPrompt("model-a", 1)})
	newSnapshot := compileSnapshot(t, pricing.ModelConfig{Pricing: testPricingWithPrompt("model-a", 2)})
	catalog := pricing.NewCatalog(oldSnapshot)
	oldResolver := catalog.NewResolver()
	catalog.Replace(newSnapshot)
	newResolver := catalog.NewResolver()
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})

	assertResultCost(t, oldResolver.Calculate(subject), 1)
	assertResultCost(t, newResolver.Calculate(subject), 2)
	if catalog.Snapshot() != newSnapshot {
		t.Fatal("expected catalog to publish the replacement snapshot")
	}
}

func TestCatalogConcurrentReadersObserveWholeSnapshots(t *testing.T) {
	first := compileSnapshot(t, pricing.ModelConfig{Pricing: testPricingWithPrompt("model-a", 1)})
	second := compileSnapshot(t, pricing.ModelConfig{Pricing: testPricingWithPrompt("model-a", 2)})
	catalog := pricing.NewCatalog(first)
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})

	var wg sync.WaitGroup
	for reader := 0; reader < 8; reader++ {
		wg.Go(func() {
			for index := 0; index < 1000; index++ {
				got := catalog.NewResolver().Calculate(subject).Cost.TotalCostUSD
				if got != 1 && got != 2 {
					t.Errorf("reader observed partial snapshot cost %v", got)
					return
				}
			}
		})
	}
	for index := 0; index < 1000; index++ {
		if index%2 == 0 {
			catalog.Replace(second)
		} else {
			catalog.Replace(first)
		}
	}
	wg.Wait()
}
