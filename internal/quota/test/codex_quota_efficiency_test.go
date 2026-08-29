package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"

	"gorm.io/gorm"
)

func TestGetCodexQuotaHistorySelectsCurrentRealWindow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		AuthType: entities.UsageIdentityAuthTypeAuthFile,
		Identity: "codex-auth",
		Provider: "codex",
		Type:     "codex",
	})
	seedQuotaEfficiencyServiceCycle(t, db, "codex-auth", entities.CodexQuotaWindowRolePrimary, 7*24*time.Hour, now.Add(-4*24*time.Hour), now.Add(3*24*time.Hour))
	seedQuotaEfficiencyServiceCycle(t, db, "codex-auth", entities.CodexQuotaWindowRoleSecondary, 5*time.Hour, now.Add(-10*time.Hour), now.Add(-5*time.Hour))
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(nil))

	response, err := service.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{AuthIndex: "codex-auth", Now: now})
	if err != nil {
		t.Fatalf("GetCodexQuotaHistory returned error: %v", err)
	}
	if len(response.Windows) != 1 || response.SelectedWindow == nil {
		t.Fatalf("expected only the role present in the latest response, got %+v", response)
	}
	if response.SelectedWindow.WindowRole != "primary" || response.SelectedWindow.WindowSeconds != int64((7*24*time.Hour)/time.Second) {
		t.Fatalf("expected current Primary Weekly selection, got %+v", response.SelectedWindow)
	}
	if len(response.Cycles) != 1 || response.Cycles[0].Status != "current" || response.Cycles[0].LastRemainingPercent == nil || *response.Cycles[0].LastRemainingPercent != 77 || response.Cycles[0].ObservationCount != 1 {
		t.Fatalf("unexpected selected series cycles: %+v", response)
	}
	if response.Cycles[0].WindowSeconds != int64((7*24*time.Hour)/time.Second) || !response.Cycles[0].EffectiveStartedAt.Equal(response.Cycles[0].WindowStartedAt) || !response.Cycles[0].EffectiveEndedAt.Equal(response.Cycles[0].ResetAt) {
		t.Fatalf("unexpected current cycle effective boundaries: %+v", response.Cycles[0])
	}
}

func TestGetCodexQuotaHistoryRejectsUnsupportedIdentityAndInvalidRole(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	db := openQuotaTestDB(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		AuthType: entities.UsageIdentityAuthTypeAuthFile,
		Identity: "claude-auth",
		Provider: "claude",
		Type:     "claude",
	})
	service := newQuotaServiceWithRegistry(t, db, quota.NewProviderRegistry(nil))

	if _, err := service.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{AuthIndex: "claude-auth", Now: now}); !errors.Is(err, quota.ErrUnsupportedType) {
		t.Fatalf("expected unsupported type, got %v", err)
	}
	invalidRole := "additional"
	if _, err := service.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{AuthIndex: "claude-auth", WindowRole: &invalidRole, Now: now}); !errors.Is(err, quota.ErrValidation) {
		t.Fatalf("expected role validation error before identity lookup, got %v", err)
	}
	if _, err := service.GetCodexQuotaHistory(context.Background(), quota.CodexQuotaHistoryRequest{AuthIndex: "missing-auth", Now: now}); !errors.Is(err, quota.ErrNotFound) {
		t.Fatalf("expected missing identity error, got %v", err)
	}
}

func seedQuotaEfficiencyServiceCycle(t *testing.T, db *gorm.DB, authIndex string, role entities.CodexQuotaWindowRole, duration time.Duration, start, reset time.Time) {
	t.Helper()
	quotaKey := "rate_limit.primary_window"
	if role == entities.CodexQuotaWindowRoleSecondary {
		quotaKey = "rate_limit.secondary_window"
	}
	cycle := entities.QuotaCycle{
		Provider:        "codex",
		AuthIndex:       authIndex,
		QuotaKey:        quotaKey,
		WindowSeconds:   int64(duration / time.Second),
		ResetAtSource:   entities.QuotaResetAtSourceAbsolute,
		WindowStartedAt: start,
		ResetAt:         reset,
		FirstObservedAt: start.Add(time.Minute),
		LastObservedAt:  reset.Add(-time.Minute),
		CreatedAt:       start.Add(time.Minute),
		UpdatedAt:       reset.Add(-time.Minute),
	}
	if err := db.Create(&cycle).Error; err != nil {
		t.Fatalf("seed quota efficiency service cycle: %v", err)
	}
	segment := entities.QuotaPercentSegment{
		CycleID:          cycle.ID,
		RemainingPercent: 77,
		FirstObservedAt:  cycle.FirstObservedAt,
		LastObservedAt:   cycle.LastObservedAt,
		ObservationCount: 1,
		CreatedAt:        cycle.CreatedAt,
		UpdatedAt:        cycle.UpdatedAt,
	}
	if err := db.Create(&segment).Error; err != nil {
		t.Fatalf("seed quota efficiency service segment: %v", err)
	}
}
