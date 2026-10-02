package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// TestUsageCostRecalculationNanosecondBoundary 验证非整秒截止以及乱序 ID 不改变 [S,T) 集合。
func TestUsageCostRecalculationNanosecondBoundary(t *testing.T) {
	db, _ := openRecalculationPools(t)
	start := time.Date(2026, 9, 23, 10, 0, 0, 0, time.Local).Truncate(time.Hour)
	end := start.Add(30*time.Minute + 123456789*time.Nanosecond)
	instants := []time.Time{end, start.Add(-time.Nanosecond), start, end.Add(-time.Nanosecond), start.Add(time.Nanosecond), end.Add(time.Nanosecond)}
	ids := []int64{1, 2, 5, 8, 9, 11}
	for index, instant := range instants {
		event := entities.UsageEvent{
			ID: ids[index], EventKey: instant.Format(time.RFC3339Nano), Timestamp: instant,
			Model: "priced", APIGroupKey: "boundary-key", CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true),
		}
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}
	scope := repository.UsageCostRecalculationScope{Start: start, End: end, MaxEventID: 11}
	count, err := repository.CountUsageCostRecalculationEvents(context.Background(), db, scope)
	if err != nil || count != 3 {
		t.Fatalf("nanosecond count=%d err=%v", count, err)
	}
	page, cursor, done, err := repository.LoadUsageCostRecalculationPage(context.Background(), db, scope, 0)
	if err != nil || !done || cursor != 11 || !reflect.DeepEqual(recalcEventIDs(page), []int64{5, 8, 9}) {
		t.Fatalf("nanosecond page=%v cursor=%d done=%v err=%v", recalcEventIDs(page), cursor, done, err)
	}
}

// TestUsageCostRecalculationRetainsCommittedBatchAndOriginalFacts 验证部分提交后的真实数据库状态。
// 后一批失败不撤销前一批；从旧读取页重新执行必须读取当前旧费用，且不改任何非费用事实。
func TestUsageCostRecalculationRetainsCommittedBatchAndOriginalFacts(t *testing.T) {
	db, _ := openRecalculationPools(t)
	ctx := context.Background()
	when := time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local)
	events := make([]entities.UsageEvent, 4)
	for index := range events {
		events[index] = entities.UsageEvent{
			EventKey:    []string{"committed-a", "committed-b", "rolled-back-a", "rolled-back-b"}[index],
			APIGroupKey: "acceptance-key", Model: "priced", AuthIndex: "acceptance-credential",
			Timestamp: when.Add(time.Duration(index) * time.Second), Endpoint: "/v1/responses",
			InputTokens: 1_000_000, TotalTokens: 1_000_000, Failed: index == 1,
			CostUSD: recalcCost(3), CostAvailable: recalcAvailable(index != 1),
		}
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(ctx, db, when.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before := captureRecalculationFacts(t, db)
	scope := repository.UsageCostRecalculationScope{
		Start: when.Truncate(time.Hour), End: when.Add(time.Hour), MaxEventID: events[len(events)-1].ID,
	}
	page, _, done, err := repository.LoadUsageCostRecalculationPage(ctx, db, scope, 0)
	if err != nil || !done || len(page) != 4 {
		t.Fatalf("load complete target: count=%d done=%v err=%v", len(page), done, err)
	}
	resolver := recalcResolver(t)
	if err := repository.ApplyUsageCostRecalculationBatch(ctx, db, scope, page[:2], resolver); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_acceptance_daily BEFORE UPDATE OF cost_usd ON usage_overview_daily_stats BEGIN SELECT RAISE(ABORT, 'acceptance daily write failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyUsageCostRecalculationBatch(ctx, db, scope, page[2:], resolver); err == nil {
		t.Fatal("second batch unexpectedly succeeded")
	}
	assertAcceptanceStoredFees(t, db, []float64{2, 2, 3, 3}, 10)
	if after := captureRecalculationFacts(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("partial recalculation changed original facts: before=%+v after=%+v", before, after)
	}
	if err := db.Exec(`DROP TRIGGER fail_acceptance_daily`).Error; err != nil {
		t.Fatal(err)
	}
	// 故意复用旧 page：前两条数据库费用已是 2，不得再次按旧页的 3 计算差额。
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.ApplyUsageCostRecalculationBatch(ctx, db, scope, page, resolver); err != nil {
			t.Fatalf("repeat batch %d: %v", attempt, err)
		}
		// 同一个业务句柄应通过 dbresolver 使用独立读池，不应误判为单物理连接。
		if err := repository.FinalizeUsageCostRecalculationStats(ctx, db, db, scope); err != nil {
			t.Fatalf("finalize %d: %v", attempt, err)
		}
		assertAcceptanceStoredFees(t, db, []float64{2, 2, 2, 2}, 8)
	}
	if after := captureRecalculationFacts(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("completed recalculation changed original facts: before=%+v after=%+v", before, after)
	}
	assertUsageOverviewCheckpoint(t, db, events[len(events)-1].ID)
}

type recalculationOriginalFacts struct {
	Events []entities.UsageEvent
	Hourly []entities.UsageOverviewHourlyStat
	Daily  []entities.UsageOverviewDailyStat
}

// captureRecalculationFacts 仅去掉允许变动的费用字段，保留原时间、身份、计数、Token 和内部更新时间供对照。
func captureRecalculationFacts(t *testing.T, db *gorm.DB) recalculationOriginalFacts {
	t.Helper()
	var facts recalculationOriginalFacts
	if err := db.Order("id").Find(&facts.Events).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&facts.Hourly).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&facts.Daily).Error; err != nil {
		t.Fatal(err)
	}
	for index := range facts.Events {
		facts.Events[index].CostUSD, facts.Events[index].CostAvailable = nil, nil
	}
	for index := range facts.Hourly {
		facts.Hourly[index].CostUSD, facts.Hourly[index].UnavailableCostCount = nil, nil
	}
	for index := range facts.Daily {
		facts.Daily[index].CostUSD, facts.Daily[index].UnavailableCostCount = nil, nil
	}
	return facts
}

// assertAcceptanceStoredFees 分别核对事件和两类统计，失败请求仍按有效 Token 计费。
func assertAcceptanceStoredFees(t *testing.T, db *gorm.DB, want []float64, total float64) {
	t.Helper()
	var events []entities.UsageEvent
	if err := db.Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != len(want) {
		t.Fatalf("event count=%d want=%d", len(events), len(want))
	}
	for index, event := range events {
		if event.CostUSD == nil || !recalcClose(*event.CostUSD, want[index]) || event.CostAvailable == nil || !*event.CostAvailable {
			t.Fatalf("event %d fee/state mismatch: %+v", index, event)
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var row struct {
			CostUSD              *float64
			UnavailableCostCount *int64
		}
		if err := db.Table(table).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.CostUSD == nil || !recalcClose(*row.CostUSD, total) || row.UnavailableCostCount == nil || *row.UnavailableCostCount != 0 {
			t.Fatalf("%s fee/state mismatch: %+v want=%v", table, row, total)
		}
	}
}
