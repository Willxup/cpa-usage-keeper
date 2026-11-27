package test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

//go:linkname repoLookupQuotaHistoryProvider cpa-usage-keeper/internal/repository.lookupQuotaHistoryProvider
var repoLookupQuotaHistoryProvider func(string) (repositorydto.QuotaHistoryProviderSpec, bool)

//go:linkname repoSupportedQuotaHistoryProviders cpa-usage-keeper/internal/repository.supportedQuotaHistoryProviders
var repoSupportedQuotaHistoryProviders func() []string

func installSyntheticModelScopedProvider(t *testing.T) {
	t.Helper()
	prevLookup := repoLookupQuotaHistoryProvider
	prevSupported := repoSupportedQuotaHistoryProviders
	spec := repositorydto.QuotaHistoryProviderSpec{
		Provider:    "pooltest",
		ModelScoped: true,
		Windows: []repositorydto.QuotaWindowSpec{
			{WindowRole: "alpha_5h", QuotaKey: "pool.alpha.5h", GroupKey: "alpha", GroupLabel: "Alpha", Order: 0},
			{WindowRole: "beta_5h", QuotaKey: "pool.beta.5h", GroupKey: "beta", GroupLabel: "Beta", Order: 1},
		},
		MatchModel: func(quotaKey string, model string) bool {
			normalized := strings.ToLower(strings.TrimSpace(model))
			switch quotaKey {
			case "pool.alpha.5h":
				return strings.HasPrefix(normalized, "alpha-")
			case "pool.beta.5h":
				return strings.HasPrefix(normalized, "beta-")
			default:
				return false
			}
		},
	}
	repoLookupQuotaHistoryProvider = func(provider string) (repositorydto.QuotaHistoryProviderSpec, bool) {
		if strings.EqualFold(strings.TrimSpace(provider), "pooltest") {
			return spec, true
		}
		return prevLookup(provider)
	}
	repoSupportedQuotaHistoryProviders = func() []string {
		return append(prevSupported(), "pooltest")
	}
	t.Cleanup(func() {
		repoLookupQuotaHistoryProvider = prevLookup
		repoSupportedQuotaHistoryProviders = prevSupported
	})
}

func seedModelScopedCycle(t *testing.T, db *gorm.DB, authIndex, quotaKey string, start, reset time.Time, percents []int, obsTimes []time.Time) entities.QuotaCycle {
	t.Helper()
	cycle := entities.QuotaCycle{
		Provider:        "pooltest",
		AuthIndex:       authIndex,
		QuotaKey:        quotaKey,
		WindowSeconds:   int64(reset.Sub(start) / time.Second),
		ResetAtSource:   entities.QuotaResetAtSourceAbsolute,
		WindowStartedAt: start,
		ResetAt:         reset,
		FirstObservedAt: obsTimes[0],
		LastObservedAt:  obsTimes[len(obsTimes)-1],
		BoundaryVersion: 1,
		DirtyVersion:    1,
		Dirty:           false,
		CreatedAt:       obsTimes[0],
		UpdatedAt:       obsTimes[len(obsTimes)-1],
	}
	if err := db.Create(&cycle).Error; err != nil {
		t.Fatalf("seed model-scoped cycle %s: %v", quotaKey, err)
	}
	for i, percent := range percents {
		seg := entities.QuotaPercentSegment{
			CycleID:          cycle.ID,
			RemainingPercent: percent,
			FirstObservedAt:  obsTimes[i],
			LastObservedAt:   obsTimes[i],
			ObservationCount: 1,
			CreatedAt:        obsTimes[i],
			UpdatedAt:        obsTimes[i],
		}
		if err := db.Create(&seg).Error; err != nil {
			t.Fatalf("seed model-scoped segment %s/%d: %v", quotaKey, percent, err)
		}
	}
	return cycle
}

func insertModelScopedEvent(t *testing.T, db *gorm.DB, provider, authIndex, model string, ts time.Time, tokens int64, cost float64, costAvailable bool) int64 {
	t.Helper()
	event := entities.UsageEvent{
		EventKey:            ts.Format(time.RFC3339Nano) + "-" + authIndex + "-" + model,
		APIGroupKey:         "key-1",
		Provider:            provider,
		AuthType:            "oauth",
		AuthIndex:           authIndex,
		Model:               model,
		Timestamp:           ts,
		InputTokens:         tokens / 2,
		OutputTokens:        tokens / 2,
		TotalTokens:         tokens,
		CostUSD:             &cost,
		CostAvailable:       &costAvailable,
		CreatedAt:           ts,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("insert model-scoped event %s: %v", model, err)
	}
	return event.ID
}

func TestAdvanceQuotaHistoryDirtyCursorModelScopedIsolatesPoolsAndHandlesNullColumns(t *testing.T) {
	installSyntheticModelScopedProvider(t)
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	alphaCycle := seedModelScopedCycle(t, db, "ag-auth", "pool.alpha.5h", now.Add(-3*time.Hour), now.Add(2*time.Hour), []int{95}, []time.Time{now.Add(-2 * time.Hour)})
	betaCycle := seedModelScopedCycle(t, db, "ag-auth", "pool.beta.5h", now.Add(-3*time.Hour), now.Add(2*time.Hour), []int{90}, []time.Time{now.Add(-2 * time.Hour)})
	secondAccountBetaCycle := seedModelScopedCycle(t, db, "ag-auth-2", "pool.beta.5h", now.Add(-3*time.Hour), now.Add(2*time.Hour), []int{88}, []time.Time{now.Add(-2 * time.Hour)})

	// 写入多账号 beta 池事件、未知模型事件，以及含 NULL 列的旧迁移兼容行；不得标脏空闲的 alpha 池，且多账号查询不能互相污染 WHERE 条件。
	insertModelScopedEvent(t, db, "pooltest", "ag-auth", "beta-sonnet", now.Add(-time.Hour), 1000, 0.2, true)
	insertModelScopedEvent(t, db, "pooltest", "ag-auth-2", "beta-sonnet", now.Add(-55*time.Minute), 1000, 0.2, true)
	insertModelScopedEvent(t, db, "pooltest", "ag-auth", "unknown-model", now.Add(-50*time.Minute), 500, 0.1, true)
	if err := db.Exec(
		`INSERT INTO usage_events (event_key, api_group_key, provider, auth_type, auth_index, model, timestamp, created_at) VALUES (?, ?, NULL, NULL, NULL, NULL, ?, ?)`,
		"null-cols-event", "key-1", timeutil.FormatStorageTime(now.Add(-40*time.Minute)), timeutil.FormatStorageTime(now),
	).Error; err != nil {
		t.Fatalf("insert nullable row: %v", err)
	}

	res, err := repository.AdvanceQuotaHistoryDirtyCursor(ctx, db, now, 100)
	if err != nil {
		t.Fatalf("advance model-scoped dirty cursor: %v", err)
	}
	if res.ScannedEvents != 4 || res.DirtyCyclesMarked != 2 || len(res.DirtyCycleIDs) != 2 || res.DirtyCycleIDs[0] != betaCycle.ID || res.DirtyCycleIDs[1] != secondAccountBetaCycle.ID {
		t.Fatalf("expected only beta cycles [%d %d] marked dirty, got %+v", betaCycle.ID, secondAccountBetaCycle.ID, res)
	}
	if loadQuotaCycleByID(t, db, alphaCycle.ID).Dirty {
		t.Fatalf("expected idle alpha cycle to remain clean")
	}
	if !loadQuotaCycleByID(t, db, betaCycle.ID).Dirty || !loadQuotaCycleByID(t, db, secondAccountBetaCycle.ID).Dirty {
		t.Fatalf("expected both accounts' beta cycles to be marked dirty")
	}
}

func TestApplyUsageCostRecalculationBatchModelScopedMarksOnlyMatchingPoolCostStale(t *testing.T) {
	installSyntheticModelScopedProvider(t)
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	alphaCycle := seedModelScopedCycle(t, db, "pool-price", "pool.alpha.5h", now.Add(-5*time.Hour), now.Add(-time.Hour), []int{95, 90}, []time.Time{now.Add(-4 * time.Hour), now.Add(-2 * time.Hour)})
	betaCycle := seedModelScopedCycle(t, db, "pool-price", "pool.beta.5h", now.Add(-5*time.Hour), now.Add(-time.Hour), []int{85, 80}, []time.Time{now.Add(-4 * time.Hour), now.Add(-2 * time.Hour)})

	alphaEvID := insertModelScopedEvent(t, db, "pooltest", "pool-price", "alpha-pro", now.Add(-3*time.Hour), 1_000_000, 0.50, true)
	betaEvID := insertModelScopedEvent(t, db, "pooltest", "pool-price", "beta-sonnet", now.Add(-3*time.Hour), 1_000_000, 0.40, true)

	if _, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, []entities.QuotaCycle{alphaCycle, betaCycle}, now, 0, false); err != nil {
		t.Fatalf("initial model-scoped recompute: %v", err)
	}
	var events []entities.UsageEvent
	if err := db.Where("id IN ?", []int64{alphaEvID, betaEvID}).Order("id ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	hourlyRows, dailyRows, _, err := overview.BuildRows(events)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyUsageOverviewAggregationPage(ctx, db, 0, betaEvID, hourlyRows, dailyRows, now); err != nil {
		t.Fatal(err)
	}

	// 仅调整 beta-sonnet 的定价；alpha-pro 保持原价，alpha 池周期不得被标为 cost_stale。
	mult := 1.0
	snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{
		{Pricing: entities.ModelPriceSetting{Model: "alpha-pro", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 0.5, CompletionPricePer1M: 0.5, PriceMultiplier: &mult}},
		{Pricing: entities.ModelPriceSetting{Model: "beta-sonnet", PricingStyle: entities.ModelPricingStyleOpenAI, PromptPricePer1M: 3.0, CompletionPricePer1M: 5.0, PriceMultiplier: &mult}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := pricing.NewCatalog(snapshot).NewResolver()
	scope := repository.UsageCostRecalculationScope{Start: now.Add(-6 * time.Hour), End: now, MaxEventID: betaEvID}
	pageEvents, _, _, err := repository.LoadUsageCostRecalculationPage(ctx, db, scope, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyUsageCostRecalculationBatch(ctx, db, scope, pageEvents, resolver); err != nil {
		t.Fatal(err)
	}

	if loadQuotaCycleByID(t, db, alphaCycle.ID).CostStale {
		t.Fatalf("alpha cycle must not be marked cost_stale when only beta model price changed")
	}
	if !loadQuotaCycleByID(t, db, betaCycle.ID).CostStale {
		t.Fatalf("expected beta cycle to be marked cost_stale")
	}

	recomputed, err := repository.RecomputeCostStaleQuotaCycles(ctx, db, now, scope.MaxEventID)
	if err != nil || recomputed != 1 {
		t.Fatalf("expected 1 recomputed cost_stale beta cycle, got count=%d err=%v", recomputed, err)
	}
	updatedBeta := loadQuotaCycleByID(t, db, betaCycle.ID)
	if updatedBeta.CostStale || updatedBeta.Requests != 1 || math.Abs(updatedBeta.CostUSD-4.0) > 1e-9 {
		t.Fatalf("unexpected recomputed beta cycle: %+v", updatedBeta)
	}
	updatedAlpha := loadQuotaCycleByID(t, db, alphaCycle.ID)
	if updatedAlpha.CostStale || updatedAlpha.Requests != 1 || math.Abs(updatedAlpha.CostUSD-0.50) > 1e-9 {
		t.Fatalf("alpha cycle should stay untouched with original cost: %+v", updatedAlpha)
	}
}

func TestComputeQuotaCyclePersistedUsageModelScopedIsolatesFastClockAndUnbackfilledEvents(t *testing.T) {
	installSyntheticModelScopedProvider(t)
	db := openQuotaAggregationTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

	alphaCycle := seedModelScopedCycle(t, db, "pool-stream", "pool.alpha.5h", now.Add(-3*time.Hour), now.Add(2*time.Hour), []int{100, 95}, []time.Time{now.Add(-2*time.Hour - 30*time.Minute), now.Add(-time.Hour)})
	betaCycle := seedModelScopedCycle(t, db, "pool-stream", "pool.beta.5h", now.Add(-3*time.Hour), now.Add(2*time.Hour), []int{100, 90}, []time.Time{now.Add(-2*time.Hour - 30*time.Minute), now.Add(-time.Hour)})

	// 正常池内事件
	insertModelScopedEvent(t, db, "pooltest", "pool-stream", "alpha-flash", now.Add(-2*time.Hour), 2000, 0.30, true)
	insertModelScopedEvent(t, db, "pooltest", "pool-stream", "beta-opus", now.Add(-2*time.Hour), 4000, 0.90, true)
	insertModelScopedEvent(t, db, "pooltest", "pool-stream", "other-model", now.Add(-2*time.Hour), 8000, 1.50, true)
	// beta 池插入一条时间偏快（> now）且未回填费用（NULL）的事件，验证完全不会影响 alpha 池的 NextFutureEventAt 或费用校验。
	futureTime := now.Add(30 * time.Second)
	if err := db.Exec(
		`INSERT INTO usage_events (event_key, api_group_key, provider, auth_type, auth_index, model, timestamp, total_tokens, cost_usd, cost_available, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?)`,
		"beta-future-unbackfilled", "key-1", "pooltest", "oauth", "pool-stream", "beta-opus",
		timeutil.FormatStorageTime(futureTime), 500, timeutil.FormatStorageTime(now),
	).Error; err != nil {
		t.Fatalf("insert beta future unbackfilled event: %v", err)
	}

	alphaComputed, err := repository.ComputeQuotaCyclePersistedUsage(ctx, db, alphaCycle, now, 0)
	if err != nil {
		t.Fatalf("alpha cycle computation must ignore beta future/unbackfilled events, got err: %v", err)
	}
	if !alphaComputed.NextFutureEventAt.IsZero() {
		t.Fatalf("alpha cycle must not record NextFutureEventAt from beta pool, got %s", alphaComputed.NextFutureEventAt)
	}
	if alphaComputed.Usage.Requests != 1 || alphaComputed.Usage.TotalTokens != 2000 || math.Abs(alphaComputed.Usage.CostUSD-0.30) > 1e-9 {
		t.Fatalf("unexpected alpha cycle usage: %+v", alphaComputed.Usage)
	}
	if len(alphaComputed.SegmentUsages) != 2 || alphaComputed.SegmentUsages[1].Usage.Requests != 1 || alphaComputed.SegmentUsages[1].Usage.TotalTokens != 2000 {
		t.Fatalf("unexpected alpha transition usage: %+v", alphaComputed.SegmentUsages)
	}
	applied, err := repository.ApplyComputedQuotaCycleUsage(ctx, db, alphaComputed, now, false)
	if err != nil || !applied {
		t.Fatalf("apply alpha computed usage: applied=%v err=%v", applied, err)
	}
	persistedAlpha := loadQuotaCycleByID(t, db, alphaCycle.ID)
	if persistedAlpha.Dirty || !persistedAlpha.UsageReady {
		t.Fatalf("expected alpha cycle clean and usage_ready=true, got %+v", persistedAlpha)
	}

	// beta 池自身遇到 beta-future 事件（哪怕费用为 NULL，未来事件跳过费用校验前记录 NextFutureEventAt）应保留 dirty。
	betaComputed, err := repository.ComputeQuotaCyclePersistedUsage(ctx, db, betaCycle, now, 0)
	if err != nil {
		t.Fatalf("compute beta cycle: %v", err)
	}
	if !betaComputed.NextFutureEventAt.Equal(futureTime) {
		t.Fatalf("expected beta cycle NextFutureEventAt=%s, got %s", futureTime, betaComputed.NextFutureEventAt)
	}
	if betaComputed.Usage.Requests != 1 || betaComputed.Usage.TotalTokens != 4000 || math.Abs(betaComputed.Usage.CostUSD-0.90) > 1e-9 {
		t.Fatalf("unexpected beta cycle usage: %+v", betaComputed.Usage)
	}
	applied, err = repository.ApplyComputedQuotaCycleUsage(ctx, db, betaComputed, now, false)
	if err != nil || !applied {
		t.Fatalf("apply beta computed usage: applied=%v err=%v", applied, err)
	}
	if !loadQuotaCycleByID(t, db, betaCycle.ID).Dirty {
		t.Fatalf("expected beta cycle with future event to stay dirty")
	}
}
