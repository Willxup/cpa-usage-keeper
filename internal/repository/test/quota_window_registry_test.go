package test

import (
	"slices"
	"testing"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

func TestQuotaHistoryProviderRegistryLookupAndDeterministicOrder(t *testing.T) {
	providers := repositorydto.SupportedQuotaHistoryProviders()
	if !slices.Equal(providers, []string{"codex", "claude"}) {
		t.Fatalf("unexpected supported quota history providers: %v", providers)
	}
	// 修改返回切片不能污染注册表内部顺序。
	providers[0] = "mutated"
	if again := repositorydto.SupportedQuotaHistoryProviders(); !slices.Equal(again, []string{"codex", "claude"}) {
		t.Fatalf("registry slice was mutated by caller: %v", again)
	}

	codexSpec, ok := repositorydto.LookupQuotaHistoryProvider(" Codex ")
	if !ok || codexSpec.Provider != "codex" || codexSpec.ModelScoped || len(codexSpec.Windows) != 2 {
		t.Fatalf("unexpected codex provider spec: ok=%v spec=%+v", ok, codexSpec)
	}
	claudeSpec, ok := repositorydto.LookupQuotaHistoryProvider("CLAUDE")
	if !ok || claudeSpec.Provider != "claude" || claudeSpec.ModelScoped || len(claudeSpec.Windows) != 2 {
		t.Fatalf("unexpected claude provider spec: ok=%v spec=%+v", ok, claudeSpec)
	}
	if _, ok := repositorydto.LookupQuotaHistoryProvider("unknown"); ok {
		t.Fatal("expected unknown provider lookup to fail")
	}

	if keys := repositorydto.AllowedQuotaKeys("codex"); !slices.Equal(keys, []string{"rate_limit.primary_window", "rate_limit.secondary_window"}) {
		t.Fatalf("unexpected codex allowed keys: %v", keys)
	}
	if keys := repositorydto.AllowedQuotaKeys("claude"); !slices.Equal(keys, []string{"five_hour", "seven_day"}) {
		t.Fatalf("unexpected claude allowed keys: %v", keys)
	}
	if keys := repositorydto.AllowedQuotaKeys("unknown"); len(keys) != 0 {
		t.Fatalf("expected empty allowed keys for unknown provider, got %v", keys)
	}

	primary, ok := repositorydto.QuotaWindowSpecByRole("claude", "PRIMARY")
	if !ok || primary.QuotaKey != "five_hour" || primary.Order != 0 {
		t.Fatalf("unexpected claude primary window spec: ok=%v spec=%+v", ok, primary)
	}
	byKey, ok := repositorydto.QuotaWindowSpecByKey("codex", "rate_limit.secondary_window")
	if !ok || byKey.WindowRole != "secondary" || byKey.Order != 1 {
		t.Fatalf("unexpected codex secondary window spec: ok=%v spec=%+v", ok, byKey)
	}
}
