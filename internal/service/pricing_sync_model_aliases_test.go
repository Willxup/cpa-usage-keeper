package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPricingSyncModelAliasesReturnsDefaultsWithoutFile(t *testing.T) {
	aliases, err := LoadPricingSyncModelAliases("")
	if err != nil {
		t.Fatalf("LoadPricingSyncModelAliases returned error: %v", err)
	}

	if got := pricingSyncModelAliasesForModel("codex-auto-review", aliases); len(got) != 1 || got[0] != "gpt-5.3-codex" {
		t.Fatalf("expected default codex alias, got %#v", got)
	}
	if got := pricingSyncModelAliasesForModel("gpt-5.3-codex-spark", aliases); len(got) != 1 || got[0] != "gpt-5.4-mini" {
		t.Fatalf("expected default spark alias, got %#v", got)
	}
}

func TestLoadPricingSyncModelAliasesMergesOverridesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing-sync-model-aliases.json")
	content := `{
  "codex-auto-review": "gpt-5.4-mini",
  "custom-review": ["gpt-5.3-codex", "gpt-5.3-codex"],
  "gpt-5.3-codex-spark": null
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write alias file: %v", err)
	}

	aliases, err := LoadPricingSyncModelAliases(path)
	if err != nil {
		t.Fatalf("LoadPricingSyncModelAliases returned error: %v", err)
	}

	if got := pricingSyncModelAliasesForModel("codex-auto-review", aliases); len(got) != 1 || got[0] != "gpt-5.4-mini" {
		t.Fatalf("expected codex-auto-review override, got %#v", got)
	}
	if got := pricingSyncModelAliasesForModel("custom-review", aliases); len(got) != 1 || got[0] != "gpt-5.3-codex" {
		t.Fatalf("expected custom-review alias list, got %#v", got)
	}
	if got := pricingSyncModelAliasesForModel("gpt-5.3-codex-spark", aliases); len(got) != 0 {
		t.Fatalf("expected spark alias override to disable built-in mapping, got %#v", got)
	}
}
