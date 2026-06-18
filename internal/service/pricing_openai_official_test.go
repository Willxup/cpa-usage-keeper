package service

import "testing"

func TestNormalizePricingSyncSourceDefaultsToOpenAIOfficial(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty defaults to official", input: "", want: pricingSyncSourceOpenAIOfficialID},
		{name: "official alias", input: "openai_official", want: pricingSyncSourceOpenAIOfficialID},
		{name: "official spaced alias", input: "OpenAI Official", want: pricingSyncSourceOpenAIOfficialID},
		{name: "models dev alias", input: "models_dev", want: pricingSyncSourceModelsDevID},
		{name: "models dev dotted alias", input: "models.dev", want: pricingSyncSourceModelsDevID},
		{name: "unknown falls back to official", input: "unknown", want: pricingSyncSourceOpenAIOfficialID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePricingSyncSource(tt.input); got != tt.want {
				t.Fatalf("normalizePricingSyncSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractOpenAIOfficialImageGenerationEntriesUsesTextInputAndImageOutput(t *testing.T) {
	entries := extractOpenAIOfficialImageGenerationEntries(map[string]any{
		"headings": []any{
			"Model",
			map[string]any{
				"__pricingTooltipHeading": map[string]any{
					"label": "Modality",
				},
			},
			"Input",
			"Cached input",
			"Output",
		},
		"groups": []any{
			map[string]any{
				"model": "gpt-image-2",
				"rows": []any{
					[]any{"Image", 8.0, 2.0, 30.0},
					[]any{"Text", 5.0, 1.25, "-"},
				},
			},
		},
	}, "standard")

	if len(entries) != 1 {
		t.Fatalf("expected one image generation entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.serviceTier != "" {
		t.Fatalf("expected image generation entry to use fallback tier, got %#v", entry)
	}
	if entry.model.ID != "gpt-image-2" {
		t.Fatalf("expected gpt-image-2 model id, got %#v", entry.model)
	}
	if entry.model.Cost.Input == nil || *entry.model.Cost.Input != 5 {
		t.Fatalf("expected text input price 5, got %#v", entry.model.Cost.Input)
	}
	if entry.model.Cost.CacheRead == nil || *entry.model.Cost.CacheRead != 1.25 {
		t.Fatalf("expected text cache price 1.25, got %#v", entry.model.Cost.CacheRead)
	}
	if entry.model.Cost.Output == nil || *entry.model.Cost.Output != 30 {
		t.Fatalf("expected image output price 30, got %#v", entry.model.Cost.Output)
	}
}
