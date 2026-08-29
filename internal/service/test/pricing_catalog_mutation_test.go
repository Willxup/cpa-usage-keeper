package test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"

	"gorm.io/gorm"
)

func TestPricingCatalogListReadsImmutableSnapshotInsteadOfDatabase(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	seedPricingCatalogPrice(t, db, "model-a", 1)
	pricingProvider, catalog := newCatalogPricingService(t, db)

	seedPricingCatalogPrice(t, db, "model-a", 9)
	settings, err := pricingProvider.ListPricing(context.Background())
	if err != nil {
		t.Fatalf("ListPricing: %v", err)
	}
	if len(settings) != 1 || settings[0].PromptPricePer1M != 1 {
		t.Fatalf("expected cached price 1 after direct DB edit, got %+v", settings)
	}
	assertPricingCatalogCost(t, catalog, "model-a", 1)
}

func TestPricingMutationPublishesUpdateAndDeleteOnlyAfterCommit(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	pricingProvider, catalog := newCatalogPricingService(t, db)

	setting, err := pricingProvider.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:            "model-a",
		PromptPricePer1M: 3,
	})
	if err != nil {
		t.Fatalf("UpdatePricing: %v", err)
	}
	if setting.Model != "model-a" || setting.PromptPricePer1M != 3 {
		t.Fatalf("unexpected updated price: %+v", setting)
	}
	assertPricingCatalogCost(t, catalog, "model-a", 3)

	if err := pricingProvider.DeletePricing(context.Background(), "model-a"); err != nil {
		t.Fatalf("DeletePricing: %v", err)
	}
	result := catalog.NewResolver().Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a"}, helper.UsageTokenCostInput{InputTokens: 1}))
	if result.Available {
		t.Fatalf("expected deleted price to disappear from catalog, got %+v", result)
	}
	var count int64
	if err := db.Model(&struct{ Model string }{}).Table("model_price_settings").Where("model = ?", "model-a").Count(&count).Error; err != nil {
		t.Fatalf("count deleted price: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleted DB price, got %d", count)
	}
}

func TestPricingBatchMutationPublishesAllModelsTogether(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	pricingProvider, catalog := newCatalogPricingService(t, db)
	ruleSnapshotQueries := 0
	const callbackName = "test:count-batch-pricing-rule-snapshot-loads"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "model_price_rules" {
			ruleSnapshotQueries++
		}
	}); err != nil {
		t.Fatalf("register pricing snapshot query counter: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
	})

	settings, err := pricingProvider.UpdatePricingBatch(context.Background(), []servicedto.UpdatePricingInput{
		{Model: "model-a", PromptPricePer1M: 2},
		{Model: "model-b", PromptPricePer1M: 3},
	})
	if err != nil {
		t.Fatalf("UpdatePricingBatch: %v", err)
	}
	if len(settings) != 2 || settings[0].Model != "model-a" || settings[1].Model != "model-b" {
		t.Fatalf("unexpected batch result: %+v", settings)
	}
	assertPricingCatalogCost(t, catalog, "model-a", 2)
	assertPricingCatalogCost(t, catalog, "model-b", 3)
	if ruleSnapshotQueries != 1 {
		t.Fatalf("expected one complete snapshot load for the batch, got %d rule-table queries", ruleSnapshotQueries)
	}
}

func TestPricingBatchMutationRollsBackEveryModelWhenCandidateIsInvalid(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	seedPricingCatalogPrice(t, db, "model-a", 1)
	seedPricingCatalogPrice(t, db, "model-b", 1)
	pricingProvider, catalog := newCatalogPricingService(t, db)

	_, err := pricingProvider.UpdatePricingBatch(context.Background(), []servicedto.UpdatePricingInput{
		{Model: "model-a", PromptPricePer1M: 5},
		{Model: "model-b", PromptPricePer1M: math.MaxFloat64},
	})
	if !errors.Is(err, service.ErrInvalidPricingInput) {
		t.Fatalf("expected invalid pricing input, got %v", err)
	}
	assertPricingDatabasePrompt(t, db, "model-a", 1)
	assertPricingDatabasePrompt(t, db, "model-b", 1)
	assertPricingCatalogCost(t, catalog, "model-a", 1)
	assertPricingCatalogCost(t, catalog, "model-b", 1)
}

func TestPricingRulesMutationPublishesNormalizedCompleteCollection(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	seedPricingCatalogPrice(t, db, "model-a", 2)
	pricingProvider, catalog := newCatalogPricingService(t, db)
	two := 2.0
	three := 3.0

	rules, err := pricingProvider.ReplacePricingRules(context.Background(), servicedto.ReplacePricingRulesInput{
		Model: " model-a ",
		Rules: []servicedto.PricingRuleInput{
			{Key: " SERVICE_TIER ", Value: " priority ", Multiplier: &two},
			{Key: "reasoning_effort", Value: " xhigh ", Multiplier: &three},
		},
	})
	if err != nil {
		t.Fatalf("ReplacePricingRules: %v", err)
	}
	if len(rules) != 2 || rules[0].Key != "service_tier" || rules[0].Value != "priority" || rules[0].Multiplier != 2 {
		t.Fatalf("unexpected normalized rules: %+v", rules)
	}
	listed, err := pricingProvider.ListPricingRules(context.Background(), "model-a")
	if err != nil {
		t.Fatalf("ListPricingRules: %v", err)
	}
	if len(listed) != 2 || listed[1].Multiplier != 3 {
		t.Fatalf("unexpected cached rules: %+v", listed)
	}
	result := catalog.NewResolver().Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	if !result.Available || result.Cost.TotalCostUSD != 12 {
		t.Fatalf("expected price 2 * rule 2 * rule 3, got %+v", result)
	}

	cleared, err := pricingProvider.ReplacePricingRules(context.Background(), servicedto.ReplacePricingRulesInput{Model: "model-a", Rules: []servicedto.PricingRuleInput{}})
	if err != nil {
		t.Fatalf("clear pricing rules: %v", err)
	}
	if cleared == nil || len(cleared) != 0 {
		t.Fatalf("expected non-nil empty rule response, got %#v", cleared)
	}
	assertPricingCatalogCost(t, catalog, "model-a", 2)
}

func TestPricingMutationCandidateCompileFailureRollsBackDatabaseAndCatalog(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	seedPricingCatalogPrice(t, db, "model-a", 1)
	pricingProvider, catalog := newCatalogPricingService(t, db)

	_, err := pricingProvider.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{
		Model:            "model-a",
		PromptPricePer1M: math.MaxFloat64,
	})
	if err == nil {
		t.Fatal("expected unsafe candidate snapshot to fail")
	}
	assertPricingDatabasePrompt(t, db, "model-a", 1)
	assertPricingCatalogCost(t, catalog, "model-a", 1)
}

func TestPricingMutationCommitFailureKeepsPreviousCatalog(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	seedPricingCatalogPrice(t, db, "model-a", 1)
	pricingProvider, catalog := newCatalogPricingService(t, db)
	if err := db.Exec(`CREATE TABLE pricing_commit_failure_probe (
		model_price_setting_id INTEGER,
		FOREIGN KEY(model_price_setting_id) REFERENCES model_price_settings(id) DEFERRABLE INITIALLY DEFERRED
	)`).Error; err != nil {
		t.Fatalf("create deferred failure table: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_pricing_commit
		AFTER UPDATE ON model_price_settings
		BEGIN
			INSERT INTO pricing_commit_failure_probe(model_price_setting_id) VALUES (9223372036854775807);
		END`).Error; err != nil {
		t.Fatalf("create deferred failure trigger: %v", err)
	}

	_, err := pricingProvider.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{Model: "model-a", PromptPricePer1M: 7})
	if err == nil {
		t.Fatal("expected deferred foreign key failure at commit")
	}
	assertPricingDatabasePrompt(t, db, "model-a", 1)
	assertPricingCatalogCost(t, catalog, "model-a", 1)
}

func TestPricingMutationConcurrentWritesLeaveDatabaseAndCatalogConsistent(t *testing.T) {
	db := openPricingServiceTestDatabase(t)
	seedPricingCatalogPrice(t, db, "model-a", 1)
	pricingProvider, catalog := newCatalogPricingService(t, db)

	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, prompt := range []float64{2, 3} {
		prompt := prompt
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := pricingProvider.UpdatePricing(context.Background(), servicedto.UpdatePricingInput{Model: "model-a", PromptPricePer1M: prompt})
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent UpdatePricing: %v", err)
		}
	}

	var row struct{ PromptPricePer1M float64 }
	if err := db.Table("model_price_settings").Select("prompt_price_per1_m").Where("model = ?", "model-a").Take(&row).Error; err != nil {
		t.Fatalf("load final DB price: %v", err)
	}
	assertPricingCatalogCost(t, catalog, "model-a", row.PromptPricePer1M)
}

func newCatalogPricingService(t *testing.T, db *gorm.DB) (service.PricingProvider, *pricing.Catalog) {
	t.Helper()
	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadPricingSnapshot: %v", err)
	}
	catalog := pricing.NewCatalog(snapshot)
	return service.NewPricingService(db, catalog), catalog
}

func seedPricingCatalogPrice(t *testing.T, db *gorm.DB, model string, prompt float64) {
	t.Helper()
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: model, PromptPricePer1M: prompt}); err != nil {
		t.Fatalf("seed price %q: %v", model, err)
	}
}

func assertPricingDatabasePrompt(t *testing.T, db *gorm.DB, model string, want float64) {
	t.Helper()
	var row struct{ PromptPricePer1M float64 }
	if err := db.Table("model_price_settings").Select("prompt_price_per1_m").Where("model = ?", model).Take(&row).Error; err != nil {
		t.Fatalf("load DB price %q: %v", model, err)
	}
	if row.PromptPricePer1M != want {
		t.Fatalf("DB prompt price = %v, want %v", row.PromptPricePer1M, want)
	}
}

func assertPricingCatalogCost(t *testing.T, catalog *pricing.Catalog, model string, want float64) {
	t.Helper()
	result := catalog.NewResolver().Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: model}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	if !result.Available || result.Cost.TotalCostUSD != want {
		t.Fatalf("catalog cost for %q = %+v, want %v", model, result, want)
	}
}
