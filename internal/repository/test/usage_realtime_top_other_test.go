package test

import (
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestUsageRealtimeTopItemsIncludeExactOtherTotals(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	end := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	models := []string{"Other", "model-b", "model-c", "model-d", "model-e", "model-f", "model-g"}
	tokens := []int64{700, 600, 500, 400, 300, 200, 100}
	for index, model := range models {
		event := entities.UsageEvent{
			EventKey: model, Timestamp: end.Add(-time.Minute), Model: model,
			APIGroupKey: model, AuthType: "oauth", AuthIndex: model,
			InputTokens: tokens[index], TotalTokens: tokens[index],
		}
		if err := db.Create(&event).Error; err != nil {
			t.Fatalf("seed event %s: %v", model, err)
		}
		identity := entities.UsageIdentity{
			Name: model, AuthType: entities.UsageIdentityAuthTypeAuthFile,
			AuthTypeName: "oauth", Identity: model, Type: "codex", CreatedAt: end, UpdatedAt: end,
		}
		if err := db.Create(&identity).Error; err != nil {
			t.Fatalf("seed identity %s: %v", model, err)
		}
	}
	realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{
		RealtimeWindow: "15m", RealtimeEndTime: &end,
	}, emptyPricingResolverForTest())
	if err != nil {
		t.Fatalf("build realtime: %v", err)
	}
	for dimension, items := range map[string][]repodto.RealtimeUsageTopItemRecord{
		"models":     realtime.CurrentUsage.Models,
		"api_keys":   realtime.CurrentUsage.APIKeys,
		"auth_files": realtime.CurrentUsage.AuthFiles,
	} {
		if len(items) != 6 {
			t.Fatalf("%s has %d rows, want Top 5 plus Other: %+v", dimension, len(items), items)
		}
		if items[0].Key != "Other" || items[0].Tokens != 700 {
			t.Fatalf("%s real Other row lost: %+v", dimension, items)
		}
		other := items[5]
		if other.Key != "__realtime_others__" || other.Tokens != 300 || other.Requests != 2 || other.CostUSD != nil || math.Abs(other.Share-300.0/2800.0*100) > 1e-9 {
			t.Fatalf("%s Other totals are wrong: %+v", dimension, other)
		}
		var summedTokens int64
		var summedRequests int64
		var summedShare float64
		for _, item := range items {
			summedTokens += item.Tokens
			summedRequests += item.Requests
			summedShare += item.Share
		}
		if summedTokens != 2800 || summedRequests != 7 || math.Abs(summedShare-100) > 1e-9 {
			t.Fatalf("%s does not cover all usage: tokens=%d requests=%d share=%f", dimension, summedTokens, summedRequests, summedShare)
		}
	}
}

func TestUsageRealtimeOtherCostPreservesKnownAndUnknownSemantics(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	for _, test := range []struct {
		name      string
		priceLast bool
		wantKnown bool
	}{
		{name: "all remainder priced", priceLast: true, wantKnown: true},
		{name: "one remainder unpriced", priceLast: false, wantKnown: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openTestDatabase(t)
			end := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
			configs := make([]pricing.ModelConfig, 0, 7)
			for index, tokens := range []int64{700, 600, 500, 400, 300, 200, 100} {
				model := string(rune('a' + index))
				if err := db.Create(&entities.UsageEvent{
					EventKey: model, Timestamp: end.Add(-time.Minute), Model: model,
					APIGroupKey: model, InputTokens: tokens, TotalTokens: tokens,
				}).Error; err != nil {
					t.Fatalf("seed event %s: %v", model, err)
				}
				if index < 6 || test.priceLast {
					configs = append(configs, pricing.ModelConfig{Pricing: entities.ModelPriceSetting{
						Model: model, PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 1,
					}})
				}
			}
			snapshot, err := pricing.CompileSnapshot(configs)
			if err != nil {
				t.Fatalf("compile prices: %v", err)
			}
			realtime, err := repository.BuildUsageOverviewRealtimeWithFilter(db, repodto.UsageQueryFilter{
				RealtimeWindow: "15m", RealtimeEndTime: &end,
			}, pricing.NewCatalog(snapshot).NewResolver())
			if err != nil {
				t.Fatalf("build realtime: %v", err)
			}
			items := realtime.CurrentUsage.Models
			if len(items) != 6 {
				t.Fatalf("expected six rows, got %+v", items)
			}
			other := items[5]
			if test.wantKnown {
				if other.CostUSD == nil || math.Abs(*other.CostUSD-0.0003) > 1e-12 {
					t.Fatalf("expected sum of known costs, got %+v", other)
				}
			} else if other.CostUSD != nil {
				t.Fatalf("expected unknown cost, got %+v", other)
			}
		})
	}
}
