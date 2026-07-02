package test

import (
	"math"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUpsertModelPriceSettingDefaultsMultiplierToOne(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "model-price-multiplier-default.db")

	created, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "default-model",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
	})
	if err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	if created.PriceMultiplier == nil || *created.PriceMultiplier != 1 {
		t.Fatalf("expected omitted price multiplier to default to 1, got %+v", created.PriceMultiplier)
	}

	settings, err := repository.ListModelPriceSettings(db)
	if err != nil {
		t.Fatalf("ListModelPriceSettings returned error: %v", err)
	}
	if len(settings) != 1 || settings[0].PriceMultiplier == nil || *settings[0].PriceMultiplier != 1 {
		t.Fatalf("expected listed price multiplier to be 1, got %+v", settings)
	}
}

func TestUpsertModelPriceSettingPreservesExplicitZeroMultiplier(t *testing.T) {
	db := openUsageCostResolverDatabase(t, "model-price-multiplier-zero.db")
	zero := 0.0

	created, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
		Model:                "free-model",
		PromptPricePer1M:     3,
		CompletionPricePer1M: 15,
		CachePricePer1M:      0.3,
		PriceMultiplier:      &zero,
	})
	if err != nil {
		t.Fatalf("UpsertModelPriceSetting returned error: %v", err)
	}
	if created.PriceMultiplier == nil || *created.PriceMultiplier != 0 {
		t.Fatalf("expected explicit zero price multiplier, got %+v", created.PriceMultiplier)
	}

	settings, err := repository.ListModelPriceSettings(db)
	if err != nil {
		t.Fatalf("ListModelPriceSettings returned error: %v", err)
	}
	if len(settings) != 1 || settings[0].PriceMultiplier == nil || *settings[0].PriceMultiplier != 0 {
		t.Fatalf("expected listed price multiplier to preserve zero, got %+v", settings)
	}
}

func TestUpsertModelPriceSettingRejectsInvalidMultiplier(t *testing.T) {
	for name, multiplier := range map[string]float64{
		"negative": -1,
		"nan":      math.NaN(),
		"infinite": math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			db := openUsageCostResolverDatabase(t, "model-price-multiplier-invalid-"+name+".db")

			_, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
				Model:                "invalid-" + name,
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CachePricePer1M:      0.3,
				PriceMultiplier:      &multiplier,
			})
			if err == nil || !strings.Contains(err.Error(), "price_multiplier") {
				t.Fatalf("expected price_multiplier validation error, got %v", err)
			}
		})
	}
}
