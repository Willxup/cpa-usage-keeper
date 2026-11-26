package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

func openQuotaDirtyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "quota-dirty.db")})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func clearQuotaCyclesDirty(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Model(&entities.QuotaCycle{}).Where("1 = 1").Update("dirty", false).Error; err != nil {
		t.Fatalf("clear dirty flags: %v", err)
	}
}

func loadQuotaCycleByID(t *testing.T, db *gorm.DB, id int64) entities.QuotaCycle {
	t.Helper()
	var cycle entities.QuotaCycle
	if err := db.Where("id = ?", id).Take(&cycle).Error; err != nil {
		t.Fatalf("load quota cycle %d: %v", id, err)
	}
	return cycle
}

func TestQuotaObservationChangesMarkAffectedCyclesDirty(t *testing.T) {
	db := openQuotaDirtyTestDB(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

	// 1. 新建周期默认标脏且 boundary_version=1。
	weeklyReset := base.Add(7 * 24 * time.Hour)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-1",
		WindowRole:       "primary",
		WindowSeconds:    7 * 24 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceRelative),
		ResetAt:          weeklyReset,
		RemainingPercent: 95,
		FirstObservedAt:  base,
		LastObservedAt:   base,
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write initial weekly observation: %v", err)
	}

	var weekly entities.QuotaCycle
	if err := db.Where("auth_index = ?", "auth-1").Take(&weekly).Error; err != nil {
		t.Fatalf("load weekly cycle: %v", err)
	}
	if !weekly.Dirty || weekly.UsageReady || weekly.BoundaryVersion != 1 || weekly.DirtyVersion != 1 {
		t.Fatalf("unexpected new cycle state: %+v", weekly)
	}

	// 2. 同百分比心跳不应触发标脏或递增 boundary_version。
	clearQuotaCyclesDirty(t, db)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-1",
		WindowRole:       "primary",
		WindowSeconds:    7 * 24 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceRelative),
		ResetAt:          weeklyReset,
		RemainingPercent: 95,
		FirstObservedAt:  base.Add(5 * time.Minute),
		LastObservedAt:   base.Add(5 * time.Minute),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write heartbeat observation: %v", err)
	}
	weekly = loadQuotaCycleByID(t, db, weekly.ID)
	if weekly.Dirty || weekly.BoundaryVersion != 1 {
		t.Fatalf("same-percent heartbeat should not mark dirty: %+v", weekly)
	}

	// 3. 新增下降段必须标脏并递增 boundary_version。
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-1",
		WindowRole:       "primary",
		WindowSeconds:    7 * 24 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceRelative),
		ResetAt:          weeklyReset,
		RemainingPercent: 90,
		FirstObservedAt:  base.Add(10 * time.Minute),
		LastObservedAt:   base.Add(10 * time.Minute),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write drop observation: %v", err)
	}
	weekly = loadQuotaCycleByID(t, db, weekly.ID)
	if !weekly.Dirty || weekly.BoundaryVersion <= 1 {
		t.Fatalf("expected drop observation to mark dirty and bump boundary_version: %+v", weekly)
	}
	prevVer := weekly.BoundaryVersion

	// 4. 边界升级（relative -> absolute）必须标脏并递增 boundary_version。
	clearQuotaCyclesDirty(t, db)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-1",
		WindowRole:       "primary",
		WindowSeconds:    7 * 24 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          weeklyReset.Add(30 * time.Second),
		RemainingPercent: 90,
		FirstObservedAt:  base.Add(15 * time.Minute),
		LastObservedAt:   base.Add(15 * time.Minute),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write boundary upgrade observation: %v", err)
	}
	weekly = loadQuotaCycleByID(t, db, weekly.ID)
	if !weekly.Dirty || weekly.BoundaryVersion <= prevVer {
		t.Fatalf("expected boundary upgrade to mark dirty and bump boundary_version: %+v", weekly)
	}
	prevVer = weekly.BoundaryVersion

	// 5. 可信来源尾段回退（90% -> 92%）必须标脏并递增 boundary_version。
	clearQuotaCyclesDirty(t, db)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-1",
		WindowRole:       "primary",
		WindowSeconds:    7 * 24 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          weeklyReset.Add(30 * time.Second),
		RemainingPercent: 92,
		FirstObservedAt:  base.Add(20 * time.Minute),
		LastObservedAt:   base.Add(20 * time.Minute),
		ObservationCount: 1,
		Authoritative:    true,
	}}); err != nil {
		t.Fatalf("write authoritative tail correction: %v", err)
	}
	weekly = loadQuotaCycleByID(t, db, weekly.ID)
	if !weekly.Dirty || weekly.BoundaryVersion <= prevVer {
		t.Fatalf("expected tail correction to mark dirty and bump boundary_version: %+v", weekly)
	}
	prevVer = weekly.BoundaryVersion

	// 6. 同角色新建 5h 绕行周期会裁剪原 Weekly 的有效右边界，两者均需标脏。
	clearQuotaCyclesDirty(t, db)
	fiveHourReset := base.Add(4 * time.Hour)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-1",
		WindowRole:       "primary",
		WindowSeconds:    5 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          fiveHourReset,
		RemainingPercent: 80,
		FirstObservedAt:  base.Add(25 * time.Minute),
		LastObservedAt:   base.Add(25 * time.Minute),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write 5h detour observation: %v", err)
	}
	weekly = loadQuotaCycleByID(t, db, weekly.ID)
	if !weekly.Dirty || weekly.BoundaryVersion <= prevVer {
		t.Fatalf("expected overlapping sibling weekly cycle to be marked dirty: %+v", weekly)
	}
	var detour entities.QuotaCycle
	if err := db.Where("auth_index = ? AND id <> ?", "auth-1", weekly.ID).Take(&detour).Error; err != nil {
		t.Fatalf("load 5h detour cycle: %v", err)
	}
	if !detour.Dirty || detour.BoundaryVersion != 1 {
		t.Fatalf("expected new 5h detour cycle dirty: %+v", detour)
	}

	// 7. 跨窗口复用原 Weekly 周期，必须将复用的 Weekly 与绕行 5h 周期一并标脏。
	clearQuotaCyclesDirty(t, db)
	weeklyPrevVer := weekly.BoundaryVersion
	detourPrevVer := detour.BoundaryVersion
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{{
		Provider:         "codex",
		AuthIndex:        "auth-1",
		WindowRole:       "primary",
		WindowSeconds:    7 * 24 * 3600,
		ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
		ResetAt:          weeklyReset.Add(30 * time.Second),
		RemainingPercent: 92,
		FirstObservedAt:  base.Add(30 * time.Minute),
		LastObservedAt:   base.Add(30 * time.Minute),
		ObservationCount: 1,
	}}); err != nil {
		t.Fatalf("write resumed weekly observation: %v", err)
	}
	weekly = loadQuotaCycleByID(t, db, weekly.ID)
	detour = loadQuotaCycleByID(t, db, detour.ID)
	if !weekly.Dirty || weekly.BoundaryVersion <= weeklyPrevVer {
		t.Fatalf("expected reused weekly cycle dirty: %+v", weekly)
	}
	if !detour.Dirty || detour.BoundaryVersion <= detourPrevVer {
		t.Fatalf("expected detour sibling cycle dirty on reuse: %+v", detour)
	}

	// 8. 删除 5h 绕行周期会恢复同角色周期的有效右边界，必须将相邻周期标脏。
	clearQuotaCyclesDirty(t, db)
	weeklyPrevVer = weekly.BoundaryVersion
	if _, err := repository.DeleteQuotaCycle(ctx, db, "codex", "auth-1", detour.ID); err != nil {
		t.Fatalf("delete detour cycle: %v", err)
	}
	weekly = loadQuotaCycleByID(t, db, weekly.ID)
	if !weekly.Dirty || weekly.BoundaryVersion <= weeklyPrevVer {
		t.Fatalf("expected remaining weekly cycle dirty after sibling deletion: %+v", weekly)
	}
}
