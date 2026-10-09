package test

import (
	"math"
	"strings"
	"testing"
)

func TestPricingSyncPreservesFineTuningWithUnicodePrefixes(t *testing.T) {
	const base = "gpt-3.5-turbo"
	const tuned = "ft:" + base
	const full = tuned + ":org:suffix:id"
	catalogs := map[string]string{
		"models-dev": `{"openai":{"models":{
			"gpt-3.5-turbo":{"cost":{"input":0.5,"output":1.5}},
			"ft:gpt-3.5-turbo":{"cost":{"input":3,"output":6}},
			"ft:gpt-3.5-turbo:org:suffix:id":{"cost":{"input":3,"output":6}},
			"ft:x":{"cost":{"input":4,"output":8}}
		}}}`,
		"litellm": `{
			"gpt-3.5-turbo":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":5e-7,"output_cost_per_token":15e-7},
			"ft:gpt-3.5-turbo":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":3e-6,"output_cost_per_token":6e-6},
			"ft:gpt-3.5-turbo:org:suffix:id":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":3e-6,"output_cost_per_token":6e-6},
			"ft:x":{"litellm_provider":"openai","mode":"chat","input_cost_per_token":4e-6,"output_cost_per_token":8e-6}
		}`,
	}
	for source, catalog := range catalogs {
		for _, tc := range []struct {
			name, want string
			price      float64
		}{
			{"İstanbul/" + tuned, tuned, 3},
			{"İstanbul:" + tuned, tuned, 3},
			{"Ⱥ/custom/" + tuned, tuned, 3},
			{strings.Repeat("Ⱥ", 10) + "/ft:x", "ft:x", 4},
			{"渠道/FT:" + base, tuned, 3},
			{"İstanbul/fT:" + base, tuned, 3},
			{"İstanbul/" + full, full, 3},
			{"İstanbul/" + base, base, 0.5},
			{"soft:custom/" + base, base, 0.5},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				defer func() {
					if failure := recover(); failure != nil {
						t.Errorf("Unicode prefix must not panic: %v", failure)
					}
				}()
				preview := previewReviewCatalog(t, source, catalog, tc.name)
				if len(preview.Matches) != 1 || preview.Matches[0].MatchedModel != tc.want || math.Abs(preview.Matches[0].PromptPricePer1M-tc.price) > 1e-10 {
					t.Fatalf("expected %s pricing: %+v", tc.want, preview)
				}
			})
		}
	}
}
