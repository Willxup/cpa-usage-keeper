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
	})

	for _, identityType := range []string{"antigravity", "codex", "gemini-cli", "claude", "kimi"} {
		if _, ok := registry.Provider(identityType); !ok {
			t.Fatalf("expected registry to support %q", identityType)
		}
	}
}

func TestDefaultProviderRegistrySupportsReferenceQuotaIdentityTypes(t *testing.T) {
	registry := quota.NewDefaultProviderRegistry(&recordingManagementCaller{}, quota.DefaultProviderConfigs())

	for _, identityType := range []string{"antigravity", "codex", "gemini-cli", "claude", "kimi"} {
		if _, ok := registry.Provider(identityType); !ok {
			t.Fatalf("expected default registry to support %q", identityType)
		}
	}
}

func TestDefaultProviderRegistryDoesNotExposeSupplementOrVertexTypes(t *testing.T) {
	registry := quota.NewDefaultProviderRegistry(&recordingManagementCaller{}, quota.DefaultProviderConfigs())

	for _, identityType := range []string{"gemini-cli-code-assist", "vertex"} {
		if _, ok := registry.Provider(identityType); ok {
			t.Fatalf("expected default registry not to expose %q", identityType)
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
