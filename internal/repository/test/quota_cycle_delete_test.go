package test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

func TestDeleteCodexQuotaCycleKeepsRemainingRoleHistoryVisible(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Now().Truncate(time.Second)
	old := seedCodexQuotaEfficiencyRoleCycle(t, db, "delete-auth", entities.CodexQuotaWindowRolePrimary,
		now.Add(-4*24*time.Hour), now.Add(3*24*time.Hour), []codexQuotaEfficiencySegmentSeed{{remaining: 33, first: now.Add(-2 * time.Hour), last: now.Add(-2 * time.Hour)}})
	latest := seedCodexQuotaEfficiencyRoleCycle(t, db, "delete-auth", entities.CodexQuotaWindowRolePrimary,
		now.Add(-2*24*time.Hour), now.Add(5*24*time.Hour), []codexQuotaEfficiencySegmentSeed{{remaining: 100, first: now.Add(-time.Hour), last: now.Add(-time.Hour)}})
	seedCodexQuotaEfficiencyRoleCycle(t, db, "delete-auth", entities.CodexQuotaWindowRoleSecondary,
		now.Add(-time.Hour), now.Add(4*time.Hour), []codexQuotaEfficiencySegmentSeed{{remaining: 80, first: now.Add(-time.Hour), last: now.Add(-time.Hour)}})
	if _, err := repository.DeleteCodexQuotaCycle(context.Background(), db, "delete-auth", latest.ID); err != nil {
		t.Fatal(err)
	}
	role := "primary"
	query := repositorydto.CodexQuotaEfficiencyQuery{AuthIndex: "delete-auth", WindowRole: &role, Now: now, RangeStart: now.Add(-30 * 24 * time.Hour)}
	history, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, query, codexQuotaEfficiencyPricingResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Windows) != 2 || history.SelectedWindow == nil || len(history.Cycles) != 1 || history.Cycles[0].ID != old.ID || history.Cycles[0].Status != "current" {
		t.Fatalf("remaining primary history is inaccessible: %+v", history)
	}
	defaultQuery := query
	defaultQuery.WindowRole = nil
	defaultHistory, err := repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, defaultQuery, codexQuotaEfficiencyPricingResolver(t))
	if err != nil || defaultHistory.SelectedWindow == nil || defaultHistory.SelectedWindow.WindowRole != "secondary" {
		t.Fatalf("older history replaced the latest default role: %+v, %v", defaultHistory, err)
	}
	// 最新角色自然到期后，仍保留它作为默认历史；旧角色较远的 reset 不能抢回默认选择。
	defaultQuery.Now = now.Add(5 * time.Hour)
	defaultHistory, err = repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, defaultQuery, codexQuotaEfficiencyPricingResolver(t))
	if err != nil || defaultHistory.SelectedWindow == nil || defaultHistory.SelectedWindow.WindowRole != "secondary" || len(defaultHistory.Cycles) != 1 || defaultHistory.Cycles[0].Status != "completed" {
		t.Fatalf("expired latest role lost its default selection: %+v, %v", defaultHistory, err)
	}
	if _, err := repository.DeleteCodexQuotaCycle(context.Background(), db, "delete-auth", old.ID); err != nil {
		t.Fatal(err)
	}
	history, err = repository.BuildCodexQuotaEfficiencyHistory(context.Background(), db, query, codexQuotaEfficiencyPricingResolver(t))
	if err != nil || len(history.Windows) != 1 || history.SelectedWindow != nil {
		t.Fatalf("empty primary role still selectable: %+v, %v", history, err)
	}
}

func TestDeleteCodexQuotaCycleResolvesObservationOwnershipBeforeDeletion(t *testing.T) {
	for _, detour := range []bool{false, true} {
		for _, target := range []int{0, 1} {
			t.Run(fmt.Sprintf("detour=%t/target=%d", detour, target), func(t *testing.T) {
				db := openTestDatabase(t)
				base := time.Now().Add(-time.Hour)
				reset := base.Add(5 * time.Hour)
				old := codexQuotaHistoryObservation("delete-auth", "primary", 18000, reset, 60, base)
				neighbor := codexQuotaHistoryObservation("delete-auth", "primary", 18000, reset.Add(3*time.Minute), 90, base.Add(time.Minute))
				calibrated := codexQuotaHistoryObservation("delete-auth", "primary", 18000, reset.Add(90*time.Second), 90, base.Add(2*time.Minute))
				calibrated.Authoritative = true
				if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{old, neighbor, calibrated}); err != nil {
					t.Fatal(err)
				}
				var cycles []entities.QuotaCycle
				if err := db.Order("id").Find(&cycles).Error; err != nil || len(cycles) != 2 {
					t.Fatalf("adjacent cycles: %+v, %v", cycles, err)
				}
				if detour {
					weekly := codexQuotaHistoryObservation("delete-auth", "primary", 604800, base.Add(7*24*time.Hour), 80, base.Add(3*time.Minute))
					if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{weekly}); err != nil {
						t.Fatal(err)
					}
				}
				deletion, err := repository.DeleteCodexQuotaCycle(context.Background(), db, "delete-auth", cycles[target].ID)
				if err != nil {
					t.Fatal(err)
				}
				if deletion.MatchesObservation(calibrated) != (target == 1) {
					t.Fatalf("calibrated observation assigned to wrong parent: target=%d", target)
				}
			})
		}
	}
}

func TestDeleteCodexQuotaCyclePreservesOtherCyclesAndUsage(t *testing.T) {
	db := openTestDatabase(t)
	base := time.Now().Add(-time.Hour)
	reset := base.Add(7 * 24 * time.Hour)
	observations := []repositorydto.CodexMainQuotaObservation{
		codexQuotaHistoryObservation("delete-auth", "primary", 604800, reset, 33, base),
		codexQuotaHistoryObservation("delete-auth", "primary", 604800, reset.Add(time.Hour), 100, base.Add(time.Minute)),
		codexQuotaHistoryObservation("other-auth", "primary", 604800, reset, 77, base),
		codexQuotaHistoryObservation("delete-auth", "secondary", 18000, reset, 88, base),
	}
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, observations); err != nil {
		t.Fatal(err)
	}
	bad, _ := loadCodexQuotaHistoryRows(t, db, "delete-auth", "primary")
	usage := entities.UsageEvent{EventKey: "delete-preserves-usage", AuthIndex: "delete-auth", Timestamp: base, TotalTokens: 42}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	var before []entities.QuotaCycle
	db.Where("id <> ?", bad.ID).Order("id").Find(&before)
	if _, err := repository.DeleteCodexQuotaCycle(context.Background(), db, "other-auth", bad.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("wrong-account delete: %v", err)
	}
	deleted, err := repository.DeleteCodexQuotaCycle(context.Background(), db, "delete-auth", bad.ID)
	if err != nil || deleted.ID != bad.ID {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	var after []entities.QuotaCycle
	db.Order("id").Find(&after)
	if len(after) != len(before) {
		t.Fatalf("remaining cycles: %+v", after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("unrelated cycle changed: %+v", after[i])
		}
	}
	var count int64
	db.Model(&entities.QuotaPercentSegment{}).Where("cycle_id = ?", bad.ID).Count(&count)
	if count != 0 {
		t.Fatalf("remaining children: %d", count)
	}
	var persisted entities.UsageEvent
	if err := db.First(&persisted, usage.ID).Error; err != nil || persisted.TotalTokens != 42 {
		t.Fatalf("usage changed: %+v %v", persisted, err)
	}
	if _, err := repository.DeleteCodexQuotaCycle(context.Background(), db, "delete-auth", bad.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("repeat delete: %v", err)
	}
}

func TestDeleteCodexQuotaCycleRollsBackWhenParentDeleteFails(t *testing.T) {
	db := openTestDatabase(t)
	base := time.Now()
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{
		codexQuotaHistoryObservation("delete-auth", "primary", 18000, base.Add(time.Hour), 80, base),
	}); err != nil {
		t.Fatal(err)
	}
	cycle, segments := loadCodexQuotaHistoryRows(t, db, "delete-auth", "primary")
	if err := db.Exec("CREATE TRIGGER refuse_cycle_delete BEFORE DELETE ON quota_cycles BEGIN SELECT RAISE(ABORT, 'blocked'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.DeleteCodexQuotaCycle(context.Background(), db, "delete-auth", cycle.ID); err == nil {
		t.Fatal("expected delete failure")
	}
	after, remaining := loadCodexQuotaHistoryRows(t, db, "delete-auth", "primary")
	if after != cycle || len(remaining) != len(segments) {
		t.Fatalf("partial delete: %+v %+v", after, remaining)
	}
}
