package test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBuildCodexQuotaEfficiencyHistoryReadsOnlyPersistedTablesAndExposesFlags(t *testing.T) {
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	// 写入一个带 83% -> 79% 跨档下降的进行中周期。
	resetAt := now.Add(2 * time.Hour)
	for _, obs := range []repositorydto.CodexMainQuotaObservation{
		{
			Provider:         "codex",
			AuthIndex:        "auth-parity",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 83,
			FirstObservedAt:  now.Add(-2 * time.Hour),
			LastObservedAt:   now.Add(-2 * time.Hour),
			ObservationCount: 1,
		},
		{
			Provider:         "codex",
			AuthIndex:        "auth-parity",
			WindowRole:       "primary",
			WindowSeconds:    5 * 3600,
			ResetAtSource:    string(entities.QuotaResetAtSourceAbsolute),
			ResetAt:          resetAt,
			RemainingPercent: 79,
			FirstObservedAt:  now.Add(-1 * time.Hour),
			LastObservedAt:   now.Add(-1 * time.Hour),
			ObservationCount: 1,
		},
	} {
		if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{obs}); err != nil {
			t.Fatalf("write observation: %v", err)
		}
	}

	// 插入首观测前、下降段内、尾观测后三条事件。
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-parity", now.Add(-2*time.Hour-30*time.Minute), false, 1600, 0.10, true)
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-parity", now.Add(-90*time.Minute), false, 3200, 0.40, true)
	insertQuotaTestEvent(t, db, "codex", "oauth", "auth-parity", now.Add(-30*time.Minute), true, 1600, 0.20, true)

	// 1. 首次未结算前：返回 usage_ready=false，且对 usage_events / usage_events_archive 查询次数为 0。
	var uncomputedStatements []quotaEfficiencyProjectionStatement
	tracedDB := db.Session(&gorm.Session{Logger: quotaEfficiencyProjectionLogger{
		Interface:  logger.Default.LogMode(logger.Silent),
		statements: &uncomputedStatements,
	}})
	uncomputed, err := repository.BuildCodexQuotaEfficiencyHistory(ctx, tracedDB, repositorydto.CodexQuotaEfficiencyQuery{
		Provider:   "codex",
		AuthIndex:  "auth-parity",
		Now:        now,
		RangeStart: now.Add(-30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("query uncomputed history: %v", err)
	}
	assertZeroUsageEventQueries(t, uncomputedStatements)
	if len(uncomputed.Cycles) != 1 || uncomputed.Cycles[0].UsageReady || len(uncomputed.Cycles[0].Transitions) != 1 || uncomputed.Cycles[0].Transitions[0].UsageReady {
		t.Fatalf("expected uncomputed cycle and transition usage_ready=false: %+v", uncomputed.Cycles)
	}

	// 2. 持久化重算后：所有 11 项指标、跨档每 1% 效率均准确对齐，且查询仍对明细表 0 次 SQL。
	syncAllQuotaCyclesForTest(t, db, now)

	var readyStatements []quotaEfficiencyProjectionStatement
	tracedReadyDB := db.Session(&gorm.Session{Logger: quotaEfficiencyProjectionLogger{
		Interface:  logger.Default.LogMode(logger.Silent),
		statements: &readyStatements,
	}})
	ready, err := repository.BuildCodexQuotaEfficiencyHistory(ctx, tracedReadyDB, repositorydto.CodexQuotaEfficiencyQuery{
		Provider:   "codex",
		AuthIndex:  "auth-parity",
		Now:        now,
		RangeStart: now.Add(-30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("query persisted history: %v", err)
	}
	assertZeroUsageEventQueries(t, readyStatements)

	if len(ready.Cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(ready.Cycles))
	}
	cycle := ready.Cycles[0]
	if cycle.Status != "current" || !cycle.UsageReady || !cycle.Usage.CostAvailable ||
		cycle.Usage.Requests != 3 || cycle.Usage.SuccessfulRequests != 2 || cycle.Usage.FailedRequests != 1 ||
		cycle.Usage.TotalTokens != 6400 || math.Abs(cycle.Usage.TotalCostUSD-0.70) > 1e-9 {
		t.Fatalf("unexpected persisted cycle usage: %+v", cycle)
	}
	if len(cycle.Transitions) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(cycle.Transitions))
	}
	tr := cycle.Transitions[0]
	if !tr.UsageReady || tr.PercentagePoints != 4 || tr.IsDirect || !tr.CostPerPointAvailable ||
		tr.Usage.Requests != 1 || tr.Usage.TotalTokens != 3200 ||
		math.Abs(tr.TokensPerPoint-800) > 1e-9 || math.Abs(tr.CostPerPoint-0.10) > 1e-9 {
		t.Fatalf("unexpected persisted transition: %+v", tr)
	}

	// 3. 当 cost_stale=true 时，周期与下降段的 cost_available 必须置为 false，同时保留请求与 Token 数据。
	if err := db.Model(&entities.QuotaCycle{}).Where("id = ?", cycle.ID).Update("cost_stale", true).Error; err != nil {
		t.Fatalf("mark cost_stale: %v", err)
	}
	stale, err := repository.BuildCodexQuotaEfficiencyHistory(ctx, db, repositorydto.CodexQuotaEfficiencyQuery{
		Provider:   "codex",
		AuthIndex:  "auth-parity",
		Now:        now,
		RangeStart: now.Add(-30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("query cost_stale history: %v", err)
	}
	if !stale.Cycles[0].UsageReady || !stale.Cycles[0].CostStale || stale.Cycles[0].Usage.CostAvailable || stale.Cycles[0].Usage.TotalTokens != 6400 {
		t.Fatalf("expected cost_stale cycle to keep tokens and set cost_stale=true/cost_available=false: %+v", stale.Cycles[0])
	}
	if !stale.Cycles[0].Transitions[0].UsageReady || stale.Cycles[0].Transitions[0].Usage.CostAvailable || stale.Cycles[0].Transitions[0].CostPerPointAvailable {
		t.Fatalf("expected cost_stale transition to set cost_available=false: %+v", stale.Cycles[0].Transitions[0])
	}
}

func assertZeroUsageEventQueries(t *testing.T, statements []quotaEfficiencyProjectionStatement) {
	t.Helper()
	for _, stmt := range statements {
		lower := strings.ToLower(stmt.sql)
		if strings.Contains(lower, "usage_events") {
			t.Fatalf("expected 0 SQL queries to usage_events/usage_events_archive, got: %s", stmt.sql)
		}
	}
}
