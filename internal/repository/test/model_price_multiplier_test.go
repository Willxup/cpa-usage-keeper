package test

import (
	"math"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUpsertModelPriceSettingPersistsDefaultAndZeroMultiplier(t *testing.T) {
	zero := 0.0
	for _, tc := range []struct {
		name       string
		multiplier *float64
		want       float64
	}{
		{name: "omitted", want: 1},
		{name: "zero", multiplier: &zero, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			created, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
				Model: "model-a", PromptPricePer1M: 3, CompletionPricePer1M: 15, CacheReadPricePer1M: 0.3,
				PriceMultiplier: tc.multiplier,
			})
			if err != nil {
				t.Fatalf("UpsertModelPriceSetting: %v", err)
			}
			if created.PriceMultiplier == nil || *created.PriceMultiplier != tc.want {
				t.Fatalf("created multiplier = %v, want %v", created.PriceMultiplier, tc.want)
			}
			settings, err := repository.ListModelPriceSettings(db)
			if err != nil {
				t.Fatalf("ListModelPriceSettings: %v", err)
			}
			if len(settings) != 1 || settings[0].PriceMultiplier == nil || *settings[0].PriceMultiplier != tc.want {
				t.Fatalf("listed multiplier = %+v, want %v", settings, tc.want)
			}
		})
	}
}

func TestUpsertModelPriceSettingRejectsInvalidMultiplier(t *testing.T) {
	for name, multiplier := range map[string]float64{
		"negative": -1,
		"nan":      math.NaN(),
		"infinite": math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			db := openTestDatabase(t)

			_, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{
				Model:                "invalid-" + name,
				PromptPricePer1M:     3,
				CompletionPricePer1M: 15,
				CacheReadPricePer1M:  0.3,
				PriceMultiplier:      &multiplier,
			})
			if err == nil || !strings.Contains(err.Error(), "price_multiplier") {
				t.Fatalf("expected price_multiplier validation error, got %v", err)
			}
		})
	}
}
