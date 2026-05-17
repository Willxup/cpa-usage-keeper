package providerconfig

import (
	"encoding/json"
	"testing"
)

func TestProviderKeyConfigUnmarshalsBaseURLVariants(t *testing.T) {
	for _, field := range []string{"base-url", "base_url", "baseURL"} {
		t.Run(field, func(t *testing.T) {
			data := []byte(`{"apiKey":"provider-key","auth-index":"provider-auth","` + field + `":"https://api.openai.com/v1"}`)

			var cfg ProviderKeyConfig
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("unmarshal provider key config: %v", err)
			}
			if cfg.BaseURL != "https://api.openai.com/v1" {
				t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://api.openai.com/v1")
			}
		})
	}
}

func TestProviderKeyConfigUnmarshalsSyncMetadataFields(t *testing.T) {
	data := []byte(`{"apiKey":"provider-key","auth-index":"provider-auth","prefix":"team","priority":8,"disabled":false,"note":"primary provider"}`)

	var cfg ProviderKeyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal provider key config: %v", err)
	}
	if cfg.Prefix != "team" || cfg.Priority == nil || *cfg.Priority != 8 || cfg.Disabled == nil || *cfg.Disabled || cfg.Note == nil || *cfg.Note != "primary provider" {
		t.Fatalf("expected sync metadata fields to decode, got %+v", cfg)
	}
}

func TestOpenAICompatibilityConfigUnmarshalsProviderLevelSyncMetadataFields(t *testing.T) {
	data := []byte(`{"name":"OpenRouter","prefix":"openrouter","base-url":"https://openrouter.ai/api/v1","priority":4,"disabled":true,"note":"shared provider","api-key-entries":[{"apiKey":"provider-key","auth-index":"openrouter-auth"}]}`)

	var cfg OpenAICompatibilityConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal openai compatibility config: %v", err)
	}
	if cfg.Priority == nil || *cfg.Priority != 4 || cfg.Disabled == nil || !*cfg.Disabled || cfg.Note == nil || *cfg.Note != "shared provider" {
		t.Fatalf("expected provider-level sync metadata fields to decode, got %+v", cfg)
	}
}

func TestOpenAICompatibilityConfigUnmarshalsBaseURLVariants(t *testing.T) {
	for _, field := range []string{"base-url", "base_url", "baseURL"} {
		t.Run(field, func(t *testing.T) {
			data := []byte(`{"name":"OpenRouter","prefix":"openrouter","` + field + `":"https://openrouter.ai/api/v1","api-key-entries":[{"apiKey":"provider-key","auth-index":"openrouter-auth"}]}`)

			var cfg OpenAICompatibilityConfig
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("unmarshal openai compatibility config: %v", err)
			}
			if cfg.BaseURL != "https://openrouter.ai/api/v1" {
				t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://openrouter.ai/api/v1")
			}
		})
	}
}
