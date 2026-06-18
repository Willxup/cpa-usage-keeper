package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPricingSyncRuntimeConfigUsesDefaultsWithoutEnvFile(t *testing.T) {
	config, err := loadPricingSyncRuntimeConfigFromSource(PricingSyncRuntimeConfigSource{})
	if err != nil {
		t.Fatalf("loadPricingSyncRuntimeConfigFromSource returned error: %v", err)
	}

	if len(config.ModelAliases) != 0 {
		t.Fatalf("expected no default aliases, got %#v", config.ModelAliases)
	}
	if config.OpenAIOfficial.UserAgent != defaultPricingSyncOpenAIOfficialUserAgent {
		t.Fatalf("expected default user agent %q, got %#v", defaultPricingSyncOpenAIOfficialUserAgent, config.OpenAIOfficial.UserAgent)
	}
}

func TestLoadPricingSyncRuntimeConfigFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "PRICING_SYNC_MODEL_ALIASES_JSON={\"custom-review\":[\"gpt-5.3-codex\",\"gpt-5.3-codex\"],\"codex-auto-review\":\"gpt-5.4-mini\"}\nPRICING_SYNC_OPENAI_OFFICIAL_USER_AGENT=custom-agent/1.0\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	config, err := loadPricingSyncRuntimeConfigFromSource(PricingSyncRuntimeConfigSource{EnvFilePath: path})
	if err != nil {
		t.Fatalf("loadPricingSyncRuntimeConfigFromSource returned error: %v", err)
	}

	if got := pricingSyncModelAliasesForModel("codex-auto-review", config.ModelAliases); len(got) != 1 || got[0] != "gpt-5.4-mini" {
		t.Fatalf("expected codex-auto-review override, got %#v", got)
	}
	if got := pricingSyncModelAliasesForModel("custom-review", config.ModelAliases); len(got) != 1 || got[0] != "gpt-5.3-codex" {
		t.Fatalf("expected custom-review alias list, got %#v", got)
	}
	if config.OpenAIOfficial.UserAgent != "custom-agent/1.0" {
		t.Fatalf("expected custom user agent, got %#v", config.OpenAIOfficial.UserAgent)
	}
}

func TestPricingSyncRuntimeConfigProviderHotReloadsEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("PRICING_SYNC_MODEL_ALIASES_JSON={\"custom-review\":\"gpt-5.3-codex\"}\n"), 0o600); err != nil {
		t.Fatalf("write initial env file: %v", err)
	}

	provider, err := NewPricingSyncRuntimeConfigProvider(PricingSyncRuntimeConfigSource{EnvFilePath: path})
	if err != nil {
		t.Fatalf("NewPricingSyncRuntimeConfigProvider returned error: %v", err)
	}

	initial := provider.Current()
	if got := pricingSyncModelAliasesForModel("custom-review", initial.ModelAliases); len(got) != 1 || got[0] != "gpt-5.3-codex" {
		t.Fatalf("expected initial alias, got %#v", got)
	}

	if err := os.WriteFile(path, []byte("PRICING_SYNC_MODEL_ALIASES_JSON={\"custom-review\":\"gpt-5.4-mini\"}\nPRICING_SYNC_OPENAI_OFFICIAL_USER_AGENT=reloaded-agent/2.0\n"), 0o600); err != nil {
		t.Fatalf("write reloaded env file: %v", err)
	}
	reloadedAt := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, reloadedAt, reloadedAt); err != nil {
		t.Fatalf("update env mtime: %v", err)
	}

	reloaded := provider.Current()
	if got := pricingSyncModelAliasesForModel("custom-review", reloaded.ModelAliases); len(got) != 1 || got[0] != "gpt-5.4-mini" {
		t.Fatalf("expected reloaded alias, got %#v", got)
	}
	if reloaded.OpenAIOfficial.UserAgent != "reloaded-agent/2.0" {
		t.Fatalf("expected reloaded user agent, got %#v", reloaded.OpenAIOfficial.UserAgent)
	}
}

func TestPricingSyncRuntimeConfigProviderKeepsLastGoodConfigOnReloadFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("PRICING_SYNC_MODEL_ALIASES_JSON={\"custom-review\":\"gpt-5.3-codex\"}\n"), 0o600); err != nil {
		t.Fatalf("write initial env file: %v", err)
	}

	provider, err := NewPricingSyncRuntimeConfigProvider(PricingSyncRuntimeConfigSource{EnvFilePath: path})
	if err != nil {
		t.Fatalf("NewPricingSyncRuntimeConfigProvider returned error: %v", err)
	}

	if err := os.WriteFile(path, []byte("PRICING_SYNC_MODEL_ALIASES_JSON={bad json}\n"), 0o600); err != nil {
		t.Fatalf("write invalid env file: %v", err)
	}
	reloadedAt := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, reloadedAt, reloadedAt); err != nil {
		t.Fatalf("update env mtime: %v", err)
	}

	config := provider.Current()
	if got := pricingSyncModelAliasesForModel("custom-review", config.ModelAliases); len(got) != 1 || got[0] != "gpt-5.3-codex" {
		t.Fatalf("expected last good alias after invalid reload, got %#v", got)
	}
}
