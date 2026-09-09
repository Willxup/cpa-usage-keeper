package test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestPricingSyncKeepsFullModelIDBeforeRegionalAliases(t *testing.T) {
	for _, source := range []string{"models-dev", "litellm"} {
		for _, model := range []string{"meta.llama3-70b-instruct-v1:0", "anthropic.claude-sonnet-4-5-20250929-v1:0"} {
			for _, namespace := range []string{"", "bedrock/"} {
				t.Run(source+"/"+namespace+model, func(t *testing.T) {
					canonical := namespace + model
					regional := "bedrock/ap-south-1/" + model
					catalog := regionalReviewCatalog(t, source, canonical, regional, 2.65)
					want := map[string]string{
						model:                     canonical,
						"custom/" + model:         canonical,
						"custom:" + model:         canonical,
						regional:                  regional,
						"custom/" + regional:      regional,
						"custom:" + regional:      regional,
						strings.ToUpper(regional): regional,
					}
					names := make([]string, 0, len(want))
					for name := range want {
						names = append(names, name)
					}
					preview := previewReviewCatalog(t, source, catalog, names...)
					if len(preview.Matches) != len(want) {
						t.Fatalf("unexpected preview: %+v", preview)
					}
					for _, match := range preview.Matches {
						price := 2.65
						if want[match.Model] == regional {
							price = 3.18
						}
						if match.MatchedModel != want[match.Model] || math.Abs(match.PromptPricePer1M-price) > 1e-10 {
							t.Errorf("expected %s pricing: %+v", want[match.Model], match)
						}
					}
				})
			}
		}
	}
}

func TestPricingSyncFallsBackFromUnavailableFullModelIDPrice(t *testing.T) {
	for _, source := range []string{"models-dev", "litellm"} {
		t.Run(source, func(t *testing.T) {
			const model = "meta.llama3-70b-instruct-v1:0"
			const regional = "bedrock/ap-south-1/" + model
			preview := previewReviewCatalog(t, source, regionalReviewCatalog(t, source, model, regional, -1), model)
			if len(preview.Matches) != 1 || preview.Matches[0].MatchedModel != regional {
				t.Fatalf("expected usable regional fallback: %+v", preview)
			}
		})
	}
}

func regionalReviewCatalog(t *testing.T, source, canonical, regional string, canonicalInput float64) string {
	t.Helper()
	entries := map[string]any{}
	for id, price := range map[string]float64{canonical: canonicalInput, regional: 3.18} {
		if source == "models-dev" {
			entries[id] = map[string]any{"cost": map[string]float64{"input": price, "output": 3.5}}
		} else {
			provider := "bedrock"
			if id == canonical && strings.Contains(id, "claude") {
				provider = "bedrock_converse"
			}
			entries[id] = map[string]any{"litellm_provider": provider, "mode": "chat", "input_cost_per_token": price / 1e6, "output_cost_per_token": 3.5e-6}
		}
	}
	if source == "models-dev" {
		entries = map[string]any{"amazon-bedrock": map[string]any{"models": entries}}
	}
	body, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
