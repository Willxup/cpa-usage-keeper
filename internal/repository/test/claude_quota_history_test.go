package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

func TestQuotaHistoryProviderKeyIsolationAcrossWriteReadAndDelete(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	reset := now.Add(5 * time.Hour)
	codex := codexQuotaHistoryObservation("shared-auth", "primary", 18_000, reset, 91, now)
	claude := codexQuotaHistoryObservation("shared-auth", "primary", 18_000, reset, 75, now)
	claude.Provider, claude.QuotaKey = "claude", "five_hour"
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{codex, claude}); err != nil {
		t.Fatal(err)
	}
	var cycles []entities.QuotaCycle
	if err := db.Order("provider ASC").Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 2 || cycles[0].Provider != "claude" || cycles[0].QuotaKey != "five_hour" || cycles[1].Provider != "codex" || cycles[1].QuotaKey != "rate_limit.primary_window" {
		t.Fatalf("provider/key were not isolated: %+v", cycles)
	}
	state, err := repository.LoadLatestQuotaHistoryState(context.Background(), db, "claude", "shared-auth", "primary")
	if err != nil || !state.Found || state.TailRemainingPercent != 75 {
		t.Fatalf("Claude recovery: %+v err=%v", state, err)
	}
	read, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{
		Provider: "claude", AuthIndex: "shared-auth", Now: now.Add(time.Minute), RangeStart: now.Add(-30 * 24 * time.Hour),
	}, codexQuotaEfficiencyPricingResolver(t))
	if err != nil || len(read.Cycles) != 1 || read.Cycles[0].LastRemainingPercent == nil || *read.Cycles[0].LastRemainingPercent != 75 {
		t.Fatalf("Claude history query crossed provider boundary: %+v err=%v", read, err)
	}
	if _, err := repository.DeleteQuotaCycle(context.Background(), db, "codex", "shared-auth", cycles[0].ID); err == nil {
		t.Fatal("Codex deletion accepted Claude cycle")
	}
	if _, err := repository.DeleteQuotaCycle(context.Background(), db, "claude", "shared-auth", cycles[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 1 || cycles[0].Provider != "codex" {
		t.Fatalf("Claude deletion touched Codex cycle: %+v", cycles)
	}
}

func TestQuotaHistoryUsageIsolatedByProvider(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	for _, provider := range []string{"codex", "claude"} {
		observation := codexQuotaHistoryObservation("shared-auth", "primary", 18000, now.Add(time.Hour), 75, now.Add(-time.Hour))
		observation.Provider = provider
		observation.QuotaKey, _ = repositorydto.QuotaWindowKey(provider, "primary")
		if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{observation}); err != nil {
			t.Fatal(err)
		}
	}
	for i, provider := range []string{"codex", "claude"} {
		event := usageEventForQuotaEfficiency(provider, "oauth", "shared-auth", now.Add(-30*time.Minute), int64(i+1)*1000000)
		event.Provider = provider
		seedCodexQuotaEfficiencyUsage(t, db, event)
	}
	for i, provider := range []string{"codex", "claude"} {
		result, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, repositorydto.CodexQuotaEfficiencyQuery{Provider: provider, AuthIndex: "shared-auth", Now: now, RangeStart: now.Add(-24 * time.Hour)}, codexQuotaEfficiencyPricingResolver(t))
		if err != nil || len(result.Cycles) != 1 {
			t.Fatalf("%s: %+v %v", provider, result, err)
		}
		assertCodexQuotaEfficiencyUsage(t, result.Cycles[0].Usage, int64(i+1)*1000000, float64(i+1), true)
	}
}
