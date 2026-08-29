package test

import (
	"fmt"
	"testing"

	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

func BenchmarkResolver(b *testing.B) {
	for _, ruleCount := range []int{0, 2, 10, 100} {
		b.Run(fmt.Sprintf("rules_%d", ruleCount), func(b *testing.B) {
			rules := make([]pricing.RuleConfig, 0, ruleCount)
			for index := 0; index < ruleCount; index++ {
				rules = append(rules, pricing.RuleConfig{
					Key:        "service_tier",
					Value:      fmt.Sprintf("tier-%d", index),
					Multiplier: 1.01,
				})
			}
			snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{
				Pricing: testPricingWithPrompt("model-a", 1),
				Rules:   rules,
			}})
			if err != nil {
				b.Fatalf("CompileSnapshot returned error: %v", err)
			}
			resolver := pricing.NewCatalog(snapshot).NewResolver()
			subject := pricing.NewCostSubject(pricing.UsageDimensions{Model: "model-a", ServiceTier: "tier-1"}, helper.UsageTokenCostInput{InputTokens: 1_000_000})

			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				_ = resolver.Calculate(subject)
			}
		})
	}
}
