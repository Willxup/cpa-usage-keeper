package test

import (
	"context"
	"errors"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

func TestLoadPricingSnapshotBuildsResolverFromPricesAndRules(t *testing.T) {
	db := openTestDatabase(t)
	seedModelPriceRulePrice(t, db, "model-a")
	if _, err := replaceModelPriceRulesInTransaction(db, "model-a", []repodto.ModelPriceRuleInput{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 3},
	}); err != nil {
		t.Fatalf("seed pricing rules: %v", err)
	}

	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("LoadPricingSnapshot: %v", err)
	}
	resolver := pricing.NewCatalog(snapshot).NewResolver()
	result := resolver.Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority", ReasoningEffort: "xhigh"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
	if !result.Available || result.Cost.TotalCostUSD != 6 || result.RuleMultiplier != 6 {
		t.Fatalf("unexpected snapshot calculation: %+v", result)
	}
}

func TestLoadPricingSnapshotInsideTransactionSeesUncommittedRules(t *testing.T) {
	db := openTestDatabase(t)
	seedModelPriceRulePrice(t, db, "model-a")
	rollback := errors.New("rollback candidate")
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := repository.ReplaceModelPriceRules(tx, "model-a", []repodto.ModelPriceRuleInput{{Key: "service_tier", Value: "priority", Multiplier: 4}}); err != nil {
			return err
		}
		snapshot, err := repository.LoadPricingSnapshot(context.Background(), tx)
		if err != nil {
			return err
		}
		result := pricing.NewCatalog(snapshot).NewResolver().Calculate(pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "priority"}, helper.UsageTokenCostInput{InputTokens: 1_000_000}))
		if result.Cost.TotalCostUSD != 4 {
			t.Fatalf("expected transaction-local candidate cost 4, got %+v", result)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("expected rollback sentinel, got %v", err)
	}

	snapshot, err := repository.LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("load snapshot after rollback: %v", err)
	}
	configs := snapshot.ModelConfigs()
	if len(configs) != 1 || len(configs[0].Rules) != 0 {
		t.Fatalf("expected rolled-back rules to stay absent, got %+v", configs)
	}
}

func TestLoadPricingSnapshotRejectsInvalidPersistedRule(t *testing.T) {
	db := openTestDatabase(t)
	seedModelPriceRulePrice(t, db, "model-a")
	var setting entities.ModelPriceSetting
	if err := db.Where("model = ?", "model-a").Take(&setting).Error; err != nil {
		t.Fatalf("load seeded model price: %v", err)
	}
	if err := db.Create(&entities.ModelPriceRule{
		ModelPriceSettingID: setting.ID,
		Key:                 "provider",
		Value:               "openai",
		Multiplier:          2,
	}).Error; err != nil {
		t.Fatalf("seed invalid persisted rule: %v", err)
	}
	if _, err := repository.LoadPricingSnapshot(context.Background(), db); err == nil {
		t.Fatal("expected invalid persisted rule to fail snapshot compilation")
	}
}
