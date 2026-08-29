package test

import (
	"sync"
	"testing"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func TestCatalogResolverKeepsOneImmutableSnapshot(t *testing.T) {
	t.Parallel()

	oldSnapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{Pricing: testPricingWithPrompt("model-a", 1)}})
	if err != nil {
		t.Fatalf("CompileSnapshot old: %v", err)
	}
	newSnapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{Pricing: testPricingWithPrompt("model-a", 2)}})
	if err != nil {
		t.Fatalf("CompileSnapshot new: %v", err)
	}
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
	first, err := pricing.CompileSnapshot([]pricing.ModelConfig{{Pricing: testPricingWithPrompt("model-a", 1)}})
	if err != nil {
		t.Fatalf("CompileSnapshot first: %v", err)
	}
	second, err := pricing.CompileSnapshot([]pricing.ModelConfig{{Pricing: testPricingWithPrompt("model-a", 2)}})
	if err != nil {
		t.Fatalf("CompileSnapshot second: %v", err)
	}
	catalog := pricing.NewCatalog(first)
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})

	var wg sync.WaitGroup
	for reader := 0; reader < 8; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := 0; index < 1000; index++ {
				got := catalog.NewResolver().Calculate(subject).Cost.TotalCostUSD
				if got != 1 && got != 2 {
					t.Errorf("reader observed partial snapshot cost %v", got)
					return
				}
			}
		}()
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
