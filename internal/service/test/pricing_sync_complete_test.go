package test

import (
	"context"
	"sync"
	"testing"

	"cpa-usage-keeper/internal/pricing"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestPricingSyncApplySerializesWithCompleteModelSave(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, _ := newCatalogPricingService(t, db)
	initial := completeServiceConfig("model-a", 1)
	if _, err := provider.SavePricingModel(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	completeSave := completeServiceConfig("model-a", 5)
	completeSave.PricingStyle = "claude"
	completeSave.ModelMultiplier = 2
	completeSave.ConditionalMultipliers = []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}}
	threshold := int64(100)
	completeSave.Branches = []pricing.PriceBranch{{
		ID: "large", Name: "Large", Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold},
		Period: pricing.PeriodCondition{Type: pricing.PeriodAll}, Prices: pricing.BasePrices{Input: 7},
	}}
	apply := servicedto.PricingSyncApplyRequest{Source: "models-dev", Items: []servicedto.PricingSyncApplyItem{{
		Model: "model-a", BasePrices: pricing.BasePrices{Input: 7}, PricingStyle: "openai",
	}}}
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	errors := make(chan error, 2)
	go func() {
		defer workers.Done()
		<-start
		_, err := provider.SavePricingModel(context.Background(), completeSave)
		errors <- err
	}()
	go func() {
		defer workers.Done()
		<-start
		_, err := provider.ApplyPricingSync(context.Background(), apply)
		errors <- err
	}()
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent complete save/apply: %v", err)
		}
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || listed.ConfigRevision != 3 || len(listed.Models) != 1 {
		t.Fatalf("concurrent mutation lost revision: %+v err=%v", listed, err)
	}
	model := listed.Models[0]
	if model.PricingStyle != "claude" || model.ModelMultiplier != 2 || len(model.ConditionalMultipliers) != 1 || len(model.Branches) != 1 || (model.BasePrices.Input != 5 && model.BasePrices.Input != 7) {
		t.Fatalf("sync overwrote complete model fields: %+v", model)
	}
}
