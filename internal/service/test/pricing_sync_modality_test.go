package test

import (
	"math"
	"testing"
)

func TestPricingSyncSkipsExplicitNonTextOutputModels(t *testing.T) {
	const catalog = `{
		"gemini/gemini-2.5-pro-preview-tts":{"litellm_provider":"gemini","mode":"chat","supported_output_modalities":["audio"],"input_cost_per_token":0.000001,"output_cost_per_token":0.00002},
		"gemini/lyria-3-clip-preview":{"litellm_provider":"gemini","mode":"chat","supported_output_modalities":["audio"],"input_cost_per_token":0,"output_cost_per_token":0,"output_cost_per_image":0.04},
		"image-model":{"litellm_provider":"openai","mode":"responses","supported_output_modalities":["image"],"input_cost_per_token":0,"output_cost_per_token":0},
		"no-output":{"litellm_provider":"openai","mode":"completion","supported_output_modalities":[],"input_cost_per_token":0,"output_cost_per_token":0},
		"mixed-model":{"litellm_provider":"openai","mode":"chat","supported_output_modalities":["audio","text"],"input_cost_per_token":0.0000025,"output_cost_per_token":0.00001},
		"free-text":{"litellm_provider":"openai","mode":"chat","supported_output_modalities":["text"],"input_cost_per_token":0,"output_cost_per_token":0},
		"legacy-model":{"litellm_provider":"openai","mode":"completion","input_cost_per_token":0.0000025,"output_cost_per_token":0.00001},
		"null-modalities":{"litellm_provider":"openai","mode":"responses","supported_output_modalities":null,"input_cost_per_token":0.0000025,"output_cost_per_token":0.00001}
	}`
	want := map[string]float64{"mixed-model": 2.5, "free-text": 0, "legacy-model": 2.5, "null-modalities": 2.5}
	excluded := []string{"gemini-2.5-pro-preview-tts", "lyria-3-clip-preview", "image-model", "no-output"}
	names := append([]string{}, excluded...)
	for name := range want {
		names = append(names, name)
	}
	preview := previewReviewCatalog(t, "litellm", catalog, names...)
	if len(preview.Matches) != len(want) || len(preview.UnmatchedModels) != len(excluded) {
		t.Fatalf("expected only text-capable or unspecified models: %+v", preview)
	}
	for _, match := range preview.Matches {
		price, ok := want[match.Model]
		if !ok || math.Abs(match.PromptPricePer1M-price) > 1e-10 {
			t.Errorf("unexpected text price: %+v", match)
		}
		if match.CacheReadPricePer1M != 0 || match.CacheWritePricePer1M != 0 {
			t.Errorf("missing cache prices must remain zero: %+v", match)
		}
	}
	for _, model := range preview.UnmatchedModels {
		if _, ok := want[model]; ok {
			t.Errorf("text-capable model became unmatched: %s", model)
		}
	}
}
