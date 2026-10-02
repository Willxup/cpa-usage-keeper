package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

func completeServiceConfig(model string, price float64) pricing.ModelPricingConfig {
	return pricing.ModelPricingConfig{
		Model: model, PricingStyle: entities.ModelPricingStyleOpenAI,
		BasePrices: pricing.BasePrices{Input: price}, ModelMultiplier: 1,
		ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{},
	}
}

func completeServiceFee(catalog *pricing.Catalog, model string, tokens int64) pricing.FeeResult {
	subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: model}, helper.UsageTokenCostInput{InputTokens: tokens})
	subject.Timestamp = time.Date(2026, 9, 23, 21, 0, 0, 0, time.Local)
	return catalog.NewResolver().CalculateFee(subject)
}

func TestCompletePricingSavePersistsZeroAndRoundTripsBranches(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	initial, err := provider.ListPricingModels(context.Background())
	if err != nil || initial.ConfigRevision != 0 || len(initial.Models) != 0 {
		t.Fatalf("initial complete pricing list: %+v err=%v", initial, err)
	}
	var stateRows int64
	if err := db.Model(&entities.PricingState{}).Count(&stateRows).Error; err != nil || stateRows != 0 {
		t.Fatalf("read-only list created revision row: count=%d err=%v", stateRows, err)
	}
	config := completeServiceConfig("model-a", 2)
	config.ModelMultiplier = 0
	config.ConditionalMultipliers = []pricing.RuleConfig{
		{Key: " AUTH_INDEX ", Value: "auth-a", Multiplier: 2},
		{Key: "response_service_tier", Value: "priority", Multiplier: 3},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 4},
		{Key: "endpoint", Value: "/v1/responses", Multiplier: 5},
		{Key: "service_tier", Value: "free", Multiplier: 0},
	}
	threshold, start, end := int64(200_000), "20:00", "08:00"
	config.Branches = []pricing.PriceBranch{{
		ID: "night", Name: "Night", Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold},
		Period: pricing.PeriodCondition{Type: pricing.PeriodWindow, Start: &start, End: &end},
		Prices: pricing.BasePrices{Input: 4, Output: 6, CacheRead: 0.5, CacheWrite: 1},
	}}
	saved, err := provider.SavePricingModel(context.Background(), config)
	if err != nil || saved.ConfigRevision != 1 || saved.Model != "model-a" {
		t.Fatalf("save full config: response=%+v err=%v", saved, err)
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || listed.ConfigRevision != 1 || len(listed.Models) != 1 {
		t.Fatalf("list full config: response=%+v err=%v", listed, err)
	}
	if listed.Models[0].ModelMultiplier != 0 || len(listed.Models[0].ConditionalMultipliers) != 5 || listed.Models[0].ConditionalMultipliers[4].Multiplier != 0 || len(listed.Models[0].Branches) != 1 {
		t.Fatalf("saved zero/rules/branch changed: %+v", listed.Models[0])
	}
	var row entities.ModelPriceSetting
	if err := db.Where("model = ?", "model-a").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.PriceMultiplier == nil || *row.PriceMultiplier != 0 {
		t.Fatalf("DB model multiplier changed: %+v", row.PriceMultiplier)
	}
	var branches []pricing.PriceBranch
	if err := json.Unmarshal([]byte(row.BranchesJSON), &branches); err != nil || len(branches) != 1 || branches[0].ID != "night" {
		t.Fatalf("DB branch JSON changed: %q err=%v", row.BranchesJSON, err)
	}
	var zeroRule entities.ModelPriceRule
	if err := db.Where("model_price_setting_id = ? AND key = ?", row.ID, "service_tier").Take(&zeroRule).Error; err != nil || zeroRule.Multiplier != 0 || zeroRule.CreatedAt.IsZero() || zeroRule.UpdatedAt.IsZero() {
		t.Fatalf("DB zero rule changed: %+v err=%v", zeroRule, err)
	}
	if result := completeServiceFee(catalog, "model-a", 210_000); !result.Available || result.TotalCostUSD != 0 {
		t.Fatalf("zero model multiplier must remain free: %+v", result)
	}

	listed.Models[0].ModelMultiplier = 1
	saved, err = provider.SavePricingModel(context.Background(), listed.Models[0])
	if err != nil || saved.ConfigRevision != 2 {
		t.Fatalf("enable stored branch price: response=%+v err=%v", saved, err)
	}
	if result := completeServiceFee(catalog, "model-a", 210_000); !result.Available || math.Abs(result.TotalCostUSD-0.84) > 1e-9 {
		t.Fatalf("persisted branch price not applied: %+v", result)
	}
	listed.Models[0].ConditionalMultipliers = []pricing.RuleConfig{}
	listed.Models[0].Branches = []pricing.PriceBranch{}
	saved, err = provider.SavePricingModel(context.Background(), listed.Models[0])
	if err != nil || saved.ConfigRevision != 3 {
		t.Fatalf("clear rules/branches: response=%+v err=%v", saved, err)
	}
	if err := db.Where("model = ?", "model-a").Take(&row).Error; err != nil || row.BranchesJSON != "[]" {
		t.Fatalf("branches were not cleared: %+v err=%v", row, err)
	}
	var count int64
	if err := db.Model(&entities.ModelPriceRule{}).Where("model_price_setting_id = ?", row.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rules were not cleared: count=%d err=%v", count, err)
	}
}

func TestCompletePricingSaveFailureKeepsDatabaseRevisionAndCatalog(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	_, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 1))
	if err != nil {
		t.Fatal(err)
	}
	before := catalog.Snapshot()
	bad := completeServiceConfig("model-a", 7)
	threshold := int64(0)
	bad.Branches = []pricing.PriceBranch{
		{ID: "a", Name: "A", Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold}, Period: pricing.PeriodCondition{Type: pricing.PeriodAll}, Prices: pricing.BasePrices{Input: 2}},
		{ID: "b", Name: "B", Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold}, Period: pricing.PeriodCondition{Type: pricing.PeriodAll}, Prices: pricing.BasePrices{Input: 3}},
	}
	_, err = provider.SavePricingModel(context.Background(), bad)
	var conflict *pricing.BranchConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, service.ErrInvalidPricingInput) {
		t.Fatalf("expected structured conflict through error chain, got %v", err)
	}
	if catalog.Snapshot() != before {
		t.Fatal("failed candidate published")
	}
	assertPricingDatabasePrompt(t, db, "model-a", 1)
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || listed.ConfigRevision != 1 {
		t.Fatalf("failed save changed revision: %+v err=%v", listed, err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_full_pricing_rule BEFORE INSERT ON model_price_rules BEGIN SELECT RAISE(ABORT, 'rule insert rejected'); END`).Error; err != nil {
		t.Fatal(err)
	}
	withRule := completeServiceConfig("model-a", 9)
	withRule.ConditionalMultipliers = []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}}
	if _, err := provider.SavePricingModel(context.Background(), withRule); err == nil {
		t.Fatal("expected rule insert failure")
	}
	assertPricingDatabasePrompt(t, db, "model-a", 1)
	if catalog.Snapshot() != before {
		t.Fatal("rule insert failure published candidate")
	}
	listed, err = provider.ListPricingModels(context.Background())
	if err != nil || listed.ConfigRevision != 1 {
		t.Fatalf("rule failure changed revision: %+v err=%v", listed, err)
	}
}

func TestCompletePricingCandidateCompileFailureRollsBackWrittenPrice(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("other-model", 1)); err != nil {
		t.Fatal(err)
	}
	before := catalog.Snapshot()
	var other entities.ModelPriceSetting
	if err := db.Where("model = ?", "other-model").Take(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entities.ModelPriceRule{ModelPriceSettingID: other.ID, Key: "unsupported_field", Value: "value", Multiplier: 2}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 9))
	if !errors.Is(err, service.ErrInvalidPricingInput) || !errors.Is(err, repository.ErrInvalidPricingSnapshot) {
		t.Fatalf("candidate compile failure lost error chain: %v", err)
	}
	assertPricingDatabasePrompt(t, db, "model-a", 1)
	if catalog.Snapshot() != before {
		t.Fatal("failed candidate published")
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || listed.ConfigRevision != 2 {
		t.Fatalf("failed candidate changed revision: %+v err=%v", listed, err)
	}
}

func TestCompletePricingCommitFailureRollsBackRevisionAndPublication(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 1)); err != nil {
		t.Fatal(err)
	}
	before := catalog.Snapshot()
	if err := db.Exec(`CREATE TABLE pricing_commit_failure_probe (
		model_price_setting_id INTEGER,
		FOREIGN KEY(model_price_setting_id) REFERENCES model_price_settings(id) DEFERRABLE INITIALLY DEFERRED
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_complete_pricing_commit
		AFTER UPDATE ON model_price_settings
		BEGIN
			INSERT INTO pricing_commit_failure_probe(model_price_setting_id) VALUES (9223372036854775807);
		END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 7)); err == nil {
		t.Fatal("expected deferred commit failure")
	}
	assertPricingDatabasePrompt(t, db, "model-a", 1)
	if catalog.Snapshot() != before {
		t.Fatal("failed commit published candidate")
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || listed.ConfigRevision != 1 {
		t.Fatalf("failed commit changed revision: %+v err=%v", listed, err)
	}
}

func TestCompletePricingDeletePreservesStoredEventCost(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	config := completeServiceConfig("model-a", 2)
	config.ConditionalMultipliers = []pricing.RuleConfig{{Key: "service_tier", Value: "priority", Multiplier: 2}}
	if _, err := provider.SavePricingModel(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	var setting entities.ModelPriceSetting
	if err := db.Where("model = ?", "model-a").Take(&setting).Error; err != nil {
		t.Fatal(err)
	}
	cost, available := 3.25, true
	event := entities.UsageEvent{EventKey: "priced-event", Model: "model-a", Timestamp: time.Now(), CostUSD: &cost, CostAvailable: &available}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	deleted, err := provider.DeletePricingModel(context.Background(), "model-a")
	if err != nil || deleted.ConfigRevision != 2 {
		t.Fatalf("delete config: response=%+v err=%v", deleted, err)
	}
	var persisted entities.UsageEvent
	if err := db.First(&persisted, event.ID).Error; err != nil || persisted.CostUSD == nil || *persisted.CostUSD != cost {
		t.Fatalf("stored event cost changed: %+v err=%v", persisted, err)
	}
	if result := completeServiceFee(catalog, "model-a", 1); result.Available {
		t.Fatalf("deleted config still in catalog: %+v", result)
	}
	var ruleCount int64
	if err := db.Model(&entities.ModelPriceRule{}).Where("model_price_setting_id = ?", setting.ID).Count(&ruleCount).Error; err != nil || ruleCount != 0 {
		t.Fatalf("deleted config retained rules: count=%d err=%v", ruleCount, err)
	}
	if _, err := provider.DeletePricingModel(context.Background(), "model-a"); !errors.Is(err, service.ErrPricingModelNotFound) {
		t.Fatalf("missing model delete should fail: %v", err)
	}
}

func TestCompletePricingConcurrentSavesPublishLastWholeConfigWithoutBlockingOldResolver(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, catalog := newCatalogPricingService(t, db)
	if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("model-a", 1)); err != nil {
		t.Fatal(err)
	}
	oldResolver := catalog.NewResolver()
	var wg sync.WaitGroup
	results := make(chan struct {
		response servicedto.SavePricingModelResponse
		price    float64
		err      error
	}, 2)
	for _, price := range []float64{2, 3} {
		wg.Go(func() {
			config := completeServiceConfig("model-a", price)
			threshold := int64(2_000_000)
			config.ConditionalMultipliers = []pricing.RuleConfig{{Key: "service_tier", Value: fmt.Sprintf("tier-%.0f", price), Multiplier: price}}
			config.Branches = []pricing.PriceBranch{{
				ID: fmt.Sprintf("branch-%.0f", price), Name: "Large",
				Context: pricing.ContextCondition{Type: pricing.ContextGT, Threshold: &threshold},
				Period:  pricing.PeriodCondition{Type: pricing.PeriodAll}, Prices: pricing.BasePrices{Input: price * 10},
			}}
			response, err := provider.SavePricingModel(context.Background(), config)
			results <- struct {
				response servicedto.SavePricingModelResponse
				price    float64
				err      error
			}{response, price, err}
		})
	}
	wg.Wait()
	close(results)
	lastPrice := 0.0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.response.ConfigRevision == 3 {
			lastPrice = result.price
		}
	}
	if lastPrice == 0 {
		t.Fatal("no final revision found")
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || listed.ConfigRevision != 3 || len(listed.Models) != 1 || listed.Models[0].BasePrices.Input != lastPrice ||
		len(listed.Models[0].ConditionalMultipliers) != 1 || listed.Models[0].ConditionalMultipliers[0].Value != fmt.Sprintf("tier-%.0f", lastPrice) ||
		len(listed.Models[0].Branches) != 1 || listed.Models[0].Branches[0].ID != fmt.Sprintf("branch-%.0f", lastPrice) || listed.Models[0].Branches[0].Prices.Input != lastPrice*10 {
		t.Fatalf("last whole config not published: %+v err=%v", listed, err)
	}
	oldSubject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})
	if oldCost := oldResolver.CalculateFee(oldSubject); math.Abs(oldCost.TotalCostUSD-1) > 1e-9 {
		t.Fatalf("old resolver price changed: %+v", oldCost)
	}
	if newCost := catalog.NewResolver().CalculateFee(oldSubject); math.Abs(newCost.TotalCostUSD-lastPrice) > 1e-9 {
		t.Fatalf("new resolver price mismatch: %+v", newCost)
	}
	assertPricingDatabasePrompt(t, db, "model-a", lastPrice)
}

func TestCompletePricingReadsLegacyLegalRulesWithoutLoss(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	zero := 0.0
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PricingStyle: "claude", PromptPricePer1M: 2, PriceMultiplier: &zero}); err != nil {
		t.Fatal(err)
	}
	legacyRules := []repodto.ModelPriceRuleInput{
		{Key: "api_group_key", Value: "group-a", Multiplier: 2},
		{Key: "model", Value: "model-a", Multiplier: 2},
		{Key: "auth_index", Value: "auth-a", Multiplier: 2},
		{Key: "model_alias", Value: "alias-a", Multiplier: 2},
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "response_service_tier", Value: "priority", Multiplier: 3},
		{Key: "endpoint", Value: "/v1/responses", Multiplier: 4},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 5},
		{Key: "executor_type", Value: "claude", Multiplier: 2},
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := repository.ReplaceModelPriceRules(tx, "model-a", legacyRules)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	provider, _ := newCatalogPricingService(t, db)
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || len(listed.Models) != 1 || len(listed.Models[0].ConditionalMultipliers) != len(legacyRules) || listed.Models[0].ModelMultiplier != 0 {
		t.Fatalf("legacy rules not loaded: %+v err=%v", listed, err)
	}
	for index, rule := range legacyRules {
		got := listed.Models[0].ConditionalMultipliers[index]
		if got.Key != rule.Key || got.Value != rule.Value || got.Multiplier != rule.Multiplier {
			t.Fatalf("legacy rule %d changed on read: got %+v want %+v", index, got, rule)
		}
	}
	if _, err := provider.SavePricingModel(context.Background(), listed.Models[0]); err != nil {
		t.Fatalf("resave legacy config: %v", err)
	}
	again, err := provider.ListPricingModels(context.Background())
	if err != nil || len(again.Models[0].ConditionalMultipliers) != len(legacyRules) || again.Models[0].ModelMultiplier != 0 {
		t.Fatalf("legacy rules lost after save: %+v err=%v", again, err)
	}
	for index, rule := range legacyRules {
		got := again.Models[0].ConditionalMultipliers[index]
		if got.Key != rule.Key || got.Value != rule.Value || got.Multiplier != rule.Multiplier {
			t.Fatalf("legacy rule %d changed on save: got %+v want %+v", index, got, rule)
		}
	}
}

// 保存入口拒绝完全覆盖默认单价，失败不得写配置或推进版本。
func TestCompletePricingRejectsUnconditionalBranch(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, _ := newCatalogPricingService(t, db)
	config := completeServiceConfig("all-branch", 1)
	config.Branches = []pricing.PriceBranch{{ID: "all", Name: "All", Context: pricing.ContextCondition{Type: pricing.ContextAll}, Period: pricing.PeriodCondition{Type: pricing.PeriodAll}, Prices: pricing.BasePrices{Input: 2}}}
	_, err := provider.SavePricingModel(context.Background(), config)
	var validation *pricing.ValidationError
	if !errors.Is(err, service.ErrInvalidPricingInput) || !errors.As(err, &validation) || validation.Code != "default_conflict" {
		t.Fatalf("expected default-price conflict, got %v", err)
	}
	result, err := provider.ListPricingModels(context.Background())
	if err != nil || len(result.Models) != 0 || result.ConfigRevision != 0 {
		t.Fatalf("invalid save changed config: %+v %v", result, err)
	}
}

func TestCompletePricingSavesDateOnlyBranch(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	provider, _ := newCatalogPricingService(t, db)
	config := completeServiceConfig("date-only", 1)
	config.Branches = []pricing.PriceBranch{{
		ID: "weekday", Name: "Weekday", Days: pricing.DaysWeekday,
		Context: pricing.ContextCondition{Type: pricing.ContextAll}, Period: pricing.PeriodCondition{Type: pricing.PeriodAll},
		Prices: pricing.BasePrices{Input: 2},
	}}
	if _, err := provider.SavePricingModel(context.Background(), config); err != nil {
		t.Fatalf("date-only branch should keep default fallback: %v", err)
	}
	listed, err := provider.ListPricingModels(context.Background())
	if err != nil || len(listed.Models) != 1 || len(listed.Models[0].Branches) != 1 || listed.Models[0].Branches[0].Days != pricing.DaysWeekday {
		t.Fatalf("date-only branch round trip: %+v, %v", listed, err)
	}
}
