package test

import (
	"context"
	"testing"

	"cpa-usage-keeper/internal/quota"
)

type fakeProviderHandler struct{}

func (fakeProviderHandler) Check(context.Context, quota.ProviderInput) (quota.ProviderOutput, error) {
	return quota.ProviderOutput{}, nil
}

func TestProviderRegistrySupportsQuotaIdentityTypes(t *testing.T) {
	registry := quota.NewProviderRegistry(map[string]quota.ProviderHandler{
		"antigravity": fakeProviderHandler{},
		"codex":       fakeProviderHandler{},
		"gemini-cli":  fakeProviderHandler{},
		"claude":      fakeProviderHandler{},
		"kimi":        fakeProviderHandler{},
		"xai":         fakeProviderHandler{},
	})

	for _, identityType := range []string{"antigravity", "codex", "gemini-cli", "claude", "kimi", "xai"} {
		if _, ok := registry.Provider(identityType); !ok {
			t.Fatalf("expected registry to support %q", identityType)
		}
	}
}

func TestDefaultProviderRegistrySupportedTypes(t *testing.T) {
	registry := quota.NewDefaultProviderRegistry(&recordingManagementCaller{}, quota.DefaultProviderConfigs())
	for identityType, want := range map[string]bool{
		"antigravity": true, "codex": true, "gemini-cli": true, "claude": true, "kimi": true, "xai": true,
		"kimi-ai": true, "kimi.ai": true, "kimi.com": true,
		"kimi-future": false, "kimi.ai-backup": false,
		"gemini-cli-code-assist": false, "vertex": false,
	} {
		if _, ok := registry.Provider(identityType); ok != want {
			t.Fatalf("Provider(%q) found=%t, want %t", identityType, ok, want)
		}
	}
}

func TestProviderRegistryNormalizesIdentityTypes(t *testing.T) {
	registry := quota.NewProviderRegistry(map[string]quota.ProviderHandler{
		"codex": fakeProviderHandler{},
	})

	if _, ok := registry.Provider("  Codex  "); !ok {
		t.Fatal("expected registry to normalize identity type")
	}
}
