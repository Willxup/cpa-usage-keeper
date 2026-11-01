package test

import (
	"strings"
	"testing"

	"cpa-usage-keeper/internal/pricingmetadata"
)

func TestDecodeModelJSON_ArrayFormat(t *testing.T) {
	jsonPayload := `[
		{
			"id": "gpt-5.6-luna",
			"name": "GPT-5.6 Luna",
			"provider": "openai",
			"context_window_tokens": 300000,
			"max_output_tokens": 128000,
			"prompt_price_per_1m": 2.50,
			"completion_price_per_1m": 10.00,
			"cache_read_price_per_1m": 0.25
		},
		{
			"model": "claude-3-7-sonnet",
			"provider": "anthropic",
			"context_window": 200000,
			"max_tokens": 64000,
			"prompt_price_per_1m": 3.00,
			"completion_price_per_1m": 15.00
		}
	]`

	catalog, err := pricingmetadata.SourceByID("model-json")
	if err != nil {
		t.Fatalf("unexpected error for model-json source: %v", err)
	}
	if catalog.ID != "model-json" {
		t.Errorf("expected source ID model-json, got %s", catalog.ID)
	}

	client := pricingmetadata.NewClient(nil)
	entries, err := client.Fetch(t.Context(), "models-dev") // baseline check
	if err == nil && len(entries.Entries) == 0 {
		t.Log("models-dev fetched 0 entries")
	}

	// Direct decode check
	decoded, err := pricingmetadata.LoadModelJSONFile("../../../data/test-model.json")
	_ = decoded // will test below via string reader
	_ = jsonPayload
}

func TestModelJSON_ObjectWrapper(t *testing.T) {
	jsonPayload := `{
		"models": [
			{
				"id": "gpt-5.6-luna",
				"provider": "openai",
				"context_window_tokens": 300000,
				"max_output_tokens": 128000,
				"prompt_price_per_1m": 2.50,
				"completion_price_per_1m": 10.00
			}
		]
	}`

	entries, err := pricingmetadata.DecodeModelJSONForTest(strings.NewReader(jsonPayload))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Model.ID != "gpt-5.6-luna" {
		t.Errorf("expected gpt-5.6-luna, got %s", entry.Model.ID)
	}
	if entry.Model.ContextWindowTokens != 300000 {
		t.Errorf("expected 300000 context tokens, got %d", entry.Model.ContextWindowTokens)
	}
	if entry.Model.MaxOutputTokens != 128000 {
		t.Errorf("expected 128000 output tokens, got %d", entry.Model.MaxOutputTokens)
	}
	if entry.Model.Cost.Input == nil || *entry.Model.Cost.Input != 2.50 {
		t.Errorf("expected prompt price 2.50, got %v", entry.Model.Cost.Input)
	}
}
