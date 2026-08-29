package test

import (
	"fmt"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

func TestReplaceModelPriceRulesReplacesCompleteOrderedCollection(t *testing.T) {
	db := openTestDatabase(t)
	seedModelPriceRulePrice(t, db, "model-a")

	first, err := replaceModelPriceRulesInTransaction(db, "model-a", []repodto.ModelPriceRuleInput{
		{Key: "service_tier", Value: "priority", Multiplier: 2},
		{Key: "reasoning_effort", Value: "xhigh", Multiplier: 1.5},
	})
	if err != nil {
		t.Fatalf("ReplaceModelPriceRules first: %v", err)
	}
	if len(first) != 2 || first[0].Key != "service_tier" || first[1].Key != "reasoning_effort" {
		t.Fatalf("expected returned rules to preserve input order, got %+v", first)
	}

	second, err := replaceModelPriceRulesInTransaction(db, "model-a", []repodto.ModelPriceRuleInput{{Key: "endpoint", Value: "/v1/responses", Multiplier: 3}})
	if err != nil {
		t.Fatalf("ReplaceModelPriceRules second: %v", err)
	}
	if len(second) != 1 || second[0].Key != "endpoint" {
		t.Fatalf("expected one replacement rule, got %+v", second)
	}
	listed, err := repository.ListModelPriceRules(db)
	if err != nil {
		t.Fatalf("ListModelPriceRules: %v", err)
	}
	if len(listed) != 1 || listed[0].Key != "endpoint" {
		t.Fatalf("expected old rules to be deleted, got %+v", listed)
	}

	cleared, err := replaceModelPriceRulesInTransaction(db, "model-a", []repodto.ModelPriceRuleInput{})
	if err != nil {
		t.Fatalf("ReplaceModelPriceRules clear: %v", err)
	}
	if cleared == nil || len(cleared) != 0 {
		t.Fatalf("expected non-nil empty rules, got %#v", cleared)
	}
	listed, err = repository.ListModelPriceRules(db)
	if err != nil || len(listed) != 0 {
		t.Fatalf("expected cleared rules, got %+v, err=%v", listed, err)
	}
}

func TestReplaceModelPriceRulesRejectsMissingModel(t *testing.T) {
	db := openTestDatabase(t)
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := repository.ReplaceModelPriceRules(tx, "missing", []repodto.ModelPriceRuleInput{{Key: "service_tier", Value: "priority", Multiplier: 2}})
		return err
	})
	if err == nil {
		t.Fatal("expected missing model to fail")
	}
}

func TestModelPriceRulesCascadeWhenPriceIsDeleted(t *testing.T) {
	db := openTestDatabase(t)
	seedModelPriceRulePrice(t, db, "model-a")
	if _, err := replaceModelPriceRulesInTransaction(db, "model-a", []repodto.ModelPriceRuleInput{{Key: "service_tier", Value: "priority", Multiplier: 2}}); err != nil {
		t.Fatalf("seed model price rule: %v", err)
	}
	if err := repository.DeleteModelPriceSetting(db, "model-a"); err != nil {
		t.Fatalf("DeleteModelPriceSetting: %v", err)
	}
	rules, err := repository.ListModelPriceRules(db)
	if err != nil {
		t.Fatalf("ListModelPriceRules: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected cascade delete, got %+v", rules)
	}
}

func TestReplaceModelPriceRulesRollsBackAllBatches(t *testing.T) {
	db := openTestDatabase(t)
	seedModelPriceRulePrice(t, db, "model-a")
	if _, err := replaceModelPriceRulesInTransaction(db, "model-a", []repodto.ModelPriceRuleInput{{Key: "service_tier", Value: "existing", Multiplier: 2}}); err != nil {
		t.Fatalf("seed existing model price rule: %v", err)
	}

	rules := make([]repodto.ModelPriceRuleInput, 0, 401)
	for index := 0; index < 400; index++ {
		rules = append(rules, repodto.ModelPriceRuleInput{Key: "service_tier", Value: fmt.Sprintf("tier-%03d", index), Multiplier: 2})
	}
	rules = append(rules, rules[0])
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := repository.ReplaceModelPriceRules(tx, "model-a", rules)
		return err
	})
	if err == nil {
		t.Fatal("expected duplicate in a later insert batch to fail")
	}

	listed, listErr := repository.ListModelPriceRules(db)
	if listErr != nil {
		t.Fatalf("ListModelPriceRules: %v", listErr)
	}
	if len(listed) != 1 || listed[0].Value != "existing" {
		t.Fatalf("expected complete transaction rollback, got %+v", listed)
	}
}

func replaceModelPriceRulesInTransaction(db *gorm.DB, model string, rules []repodto.ModelPriceRuleInput) ([]entities.ModelPriceRule, error) {
	var result []entities.ModelPriceRule
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = repository.ReplaceModelPriceRules(tx, model, rules)
		return err
	})
	return result, err
}

func seedModelPriceRulePrice(t *testing.T, db *gorm.DB, model string) {
	t.Helper()
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:            model,
		PromptPricePer1M: 1,
	}); err != nil {
		t.Fatalf("seed model price %q: %v", model, err)
	}
}
