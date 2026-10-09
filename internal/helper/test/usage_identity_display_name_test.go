package test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
)

func TestUsageIdentityDisplayName(t *testing.T) {
	alias := "  Friendly Account  "
	for _, test := range []struct {
		name     string
		identity entities.UsageIdentity
		want     string
	}{
		{"provider prefix", entities.UsageIdentity{
			Name: "Provider Name", Prefix: "Team Prefix",
			AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-auth-index",
		}, "Team Prefix"},
		{"auth file alias", entities.UsageIdentity{
			Alias: &alias, Name: "Upstream Auth Name",
			AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "auth-1",
		}, "Friendly Account"},
		{"provider alias", entities.UsageIdentity{
			Alias: &alias, Name: "Provider Name", Prefix: "Team Prefix", BaseURL: "https://api.openai.com/v1",
			AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-auth-index",
		}, "Friendly Account"},
		{"provider prefix and URL", entities.UsageIdentity{
			Name: "Provider Name", Prefix: "Team Prefix", BaseURL: "https://api.openai.com/v1/",
			AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-auth-index",
		}, "Team Prefix @ api.openai.com"},
		{"provider URL", entities.UsageIdentity{
			Name: "codex", BaseURL: "https://chatgpt.com/backend-api/codex/",
			AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "codex-auth-index",
		}, "chatgpt.com/backend-api/codex"},
		{"compatibility name", entities.UsageIdentity{
			Name: "OpenRouter", Prefix: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
			AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "openai", Provider: "OpenRouter", Identity: "openrouter-auth-index",
		}, "OpenRouter"},
		{"compatibility masked key", entities.UsageIdentity{
			Name: "Fireworks", Prefix: "fireworks", BaseURL: "https://api.fireworks.ai/inference/v1",
			AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "openai", Provider: "Fireworks", Identity: "fireworks-auth-index",
			LookupKey: "fw-abcdefghijklmnopqrstuvwxyz123456",
		}, "Fireworks @ fw-*********123456"},
		{"compatibility unknown key", entities.UsageIdentity{
			Name: "Fireworks", AuthType: entities.UsageIdentityAuthTypeAIProvider,
			Type: "openai", Provider: "Fireworks", Identity: "fireworks-auth-index", LookupKey: " unknown ",
		}, "Fireworks"},
		{"compatibility unnamed provider", entities.UsageIdentity{
			Prefix: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
			AuthType: entities.UsageIdentityAuthTypeAIProvider, Type: "openai", Provider: "openai", Identity: "openrouter-auth-index",
		}, "openrouter @ openrouter.ai/api"},
		{"unnamed auth file", entities.UsageIdentity{
			AuthType: entities.UsageIdentityAuthTypeAuthFile, Provider: "Claude",
		}, "Claude"},
		{"prefix only", entities.UsageIdentity{
			Prefix: "Team Prefix", AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-auth-index",
		}, "Team Prefix"},
		{"name only", entities.UsageIdentity{
			Name: "Provider Name", AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-auth-index",
		}, "Provider Name"},
		{"provider only", entities.UsageIdentity{
			Provider: "OpenAI", AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-auth-index",
		}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := helper.UsageIdentityDisplayName(test.identity); got != test.want {
				t.Fatalf("display name = %q, want %q", got, test.want)
			}
		})
	}
}
