package service

import (
	"testing"

	"cpa-usage-keeper/internal/pricingmetadata"
)

func TestPricingSyncRequiresExplicitCachePrices(t *testing.T) {
	zero, input, output := 0.0, 1.0, 2.0
	for _, tc := range []struct {
		name        string
		read, write *float64
		want        bool
	}{
		{"both missing", nil, nil, false},
		{"read missing", nil, &zero, false},
		{"write missing", &zero, nil, false},
		{"explicit zeros", &zero, &zero, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := pricingmetadata.Model{ID: "model", Cost: pricingmetadata.Cost{Input: &input, Output: &output, CacheRead: tc.read, CacheWrite: tc.write}}
			match, ok := buildPricingSyncMatch("model", model, "provider", "provider")
			if ok != tc.want {
				t.Fatalf("match=%+v ok=%v", match, ok)
			}
		})
	}
}
