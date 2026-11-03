package test

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// TestUsageCostRecalculationPagesUseRealInstants 验证 ID 空洞、H 上限及 DST 回拨时的实际时刻筛选。
func TestUsageCostRecalculationPagesUseRealInstants(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	previousLocal := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	writer, reader := openRecalculationPools(t)
	firstHour, _ := time.Parse(time.RFC3339, "2026-11-01T01:30:00-04:00")
	secondHour, _ := time.Parse(time.RFC3339, "2026-11-01T01:30:00-05:00")
	start, _ := time.Parse(time.RFC3339, "2026-11-01T01:00:00-05:00")
	end, _ := time.Parse(time.RFC3339, "2026-11-01T02:00:00-05:00")
	events := []entities.UsageEvent{
		{ID: 1, EventKey: "first-hour", Model: "priced", APIGroupKey: "key", Timestamp: firstHour, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)},
		{ID: 1001, EventKey: "second-hour", Model: "priced", APIGroupKey: "key", Timestamp: secondHour, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)},
		{ID: 1003, EventKey: "after-H", Model: "priced", APIGroupKey: "key", Timestamp: secondHour, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)},
	}
	if err := writer.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		id     int64
		suffix string
	}{{1, "-04:00"}, {1001, "-05:00"}} {
		var stored string
		if err := writer.Raw("SELECT CAST(timestamp AS TEXT) FROM usage_events WHERE id = ?", sample.id).Scan(&stored).Error; err != nil || len(stored) < len(sample.suffix) || stored[len(stored)-len(sample.suffix):] != sample.suffix {
			t.Fatalf("DST offset for event %d: %q err=%v", sample.id, stored, err)
		}
	}
	scope := repository.UsageCostRecalculationScope{Start: start, End: end, MaxEventID: 1001}
	count, err := repository.CountUsageCostRecalculationEvents(context.Background(), reader, scope)
	if err != nil || count != 1 {
		t.Fatalf("count actual instants: count=%d err=%v", count, err)
	}
	page, cursor, done, err := repository.LoadUsageCostRecalculationPage(context.Background(), reader, scope, 0)
	if err != nil || len(page) != 1 || page[0].ID != 1001 || cursor != 1001 || !done {
		t.Fatalf("fixed H page: ids=%v cursor=%d done=%v err=%v", recalcEventIDs(page), cursor, done, err)
	}
	page, cursor, done, err = repository.LoadUsageCostRecalculationPage(context.Background(), reader, scope, cursor)
	if err != nil || len(page) != 0 || cursor != scope.MaxEventID || !done {
		t.Fatalf("completed hole page: ids=%v cursor=%d done=%v err=%v", recalcEventIDs(page), cursor, done, err)
	}
}

// TestUsageCostRecalculationBatchRollsBackWithDailyFailure 验证事件、小时、日费用同事务提交，重试不双加。
func TestUsageCostRecalculationBatchRollsBackWithDailyFailure(t *testing.T) {
	writer, reader := openRecalculationPools(t)
	when := time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local)
	pricedAlias := "priced"
	events := []entities.UsageEvent{
		{EventKey: "priced", APIGroupKey: "key", Model: "priced", Timestamp: when, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)},
		{EventKey: "missing", APIGroupKey: "key", Model: "missing", ModelAlias: &pricedAlias, Timestamp: when, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(3), CostAvailable: recalcAvailable(true)},
	}
	if err := writer.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), writer, when.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	scope := repository.UsageCostRecalculationScope{Start: when.Truncate(time.Hour), End: when.Add(time.Hour), MaxEventID: events[1].ID}
	page, _, _, err := repository.LoadUsageCostRecalculationPage(context.Background(), reader, scope, 0)
	if err != nil || len(page) != 2 {
		t.Fatalf("load batch: len=%d err=%v", len(page), err)
	}
	if err := writer.Exec(`CREATE TRIGGER fail_recalc_daily BEFORE UPDATE OF cost_usd ON usage_overview_daily_stats BEGIN SELECT RAISE(ABORT, 'daily failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	resolver := recalcResolver(t)
	if err := repository.ApplyUsageCostRecalculationBatch(context.Background(), writer, scope, page, resolver); err == nil {
		t.Fatal("daily failure accepted")
	}
	assertRecalculationFeeState(t, writer, events[0].ID, 1, true, "priced", 1, 0)
	assertRecalculationFeeState(t, writer, events[1].ID, 3, true, "missing", 3, 0)
	if err := writer.Exec(`DROP TRIGGER fail_recalc_daily`).Error; err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.ApplyUsageCostRecalculationBatch(context.Background(), writer, scope, page, resolver); err != nil {
			t.Fatalf("apply attempt %d: %v", attempt, err)
		}
	}
	assertRecalculationFeeState(t, writer, events[0].ID, 2, true, "priced", 2, 0)
	assertRecalculationFeeState(t, writer, events[1].ID, 2, true, "missing", 2, 0)
	assertUsageOverviewCheckpoint(t, writer, events[1].ID)
}

// TestUsageCostRecalculationKeepsOutsideRange 验证首末残桶及规范化维度的全桶已存费用覆盖。
func TestUsageCostRecalculationKeepsOutsideRange(t *testing.T) {
	writer, reader := openRecalculationPools(t)
	day := time.Date(2026, 9, 23, 0, 0, 0, 0, time.Local)
	alias := "priced"
	events := []entities.UsageEvent{
		{EventKey: "before-start", APIGroupKey: "\tkey\u00a0", Model: "\u3000reported ", ModelAlias: &alias, Timestamp: day.Add(22*time.Hour + 30*time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(4), CostAvailable: recalcAvailable(true)},
		{EventKey: "last-nanosecond", APIGroupKey: "\tkey\u00a0", Model: "\u3000reported ", ModelAlias: &alias, Timestamp: day.Add(24*time.Hour - time.Nanosecond), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)},
		{EventKey: "after-midnight", APIGroupKey: "\tkey\u00a0", Model: "\u3000reported ", ModelAlias: &alias, Timestamp: day.Add(24*time.Hour + 10*time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)},
		{EventKey: "after-end", APIGroupKey: "\tkey\u00a0", Model: "\u3000reported ", ModelAlias: &alias, Timestamp: day.Add(24*time.Hour + 40*time.Minute), InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(7), CostAvailable: recalcAvailable(true)},
	}
	if err := writer.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), writer, day.Add(26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	scope := repository.UsageCostRecalculationScope{Start: events[1].Timestamp.Truncate(time.Hour), End: day.Add(24*time.Hour + 30*time.Minute), MaxEventID: events[len(events)-1].ID}
	page, _, _, err := repository.LoadUsageCostRecalculationPage(context.Background(), reader, scope, 0)
	if err != nil || len(page) != 2 {
		t.Fatalf("selected page: ids=%v err=%v", recalcEventIDs(page), err)
	}
	if err := repository.ApplyUsageCostRecalculationBatch(context.Background(), writer, scope, page, recalcResolver(t)); err != nil {
		t.Fatal(err)
	}
	var hourly []entities.UsageOverviewHourlyStat
	if err := writer.Where("model = ?", "reported").Order("bucket_start").Find(&hourly).Error; err != nil {
		t.Fatal(err)
	}
	expectedHours := make(map[int64]float64)
	for index, event := range events {
		bucket := event.Timestamp.Truncate(time.Hour).Unix()
		amount := []float64{4, 2, 2, 7}[index]
		expectedHours[bucket] += amount
	}
	if len(hourly) != len(expectedHours) {
		t.Fatalf("hourly buckets=%d, want %d", len(hourly), len(expectedHours))
	}
	for _, row := range hourly {
		bucket := row.BucketStart.Unix()
		want := expectedHours[bucket]
		if row.CostUSD == nil || !recalcClose(*row.CostUSD, want) {
			t.Fatalf("hour %s fee=%v, want %g", row.BucketStart, row.CostUSD, want)
		}
	}
	var daily []entities.UsageOverviewDailyStat
	if err := writer.Where("model = ?", "reported").Order("bucket_start").Find(&daily).Error; err != nil {
		t.Fatal(err)
	}
	if len(daily) != 2 || !recalcClose(*daily[0].CostUSD, 6) || !recalcClose(*daily[1].CostUSD, 9) {
		t.Fatalf("full-day fees=%+v", daily)
	}
	for _, event := range []struct {
		id   int64
		want float64
	}{{events[0].ID, 4}, {events[3].ID, 7}} {
		var stored entities.UsageEvent
		if err := writer.First(&stored, event.id).Error; err != nil || stored.CostUSD == nil || !recalcClose(*stored.CostUSD, event.want) {
			t.Fatalf("outside event %d changed: %+v err=%v", event.id, stored, err)
		}
	}
}

// TestUsageCostRecalculationStreamsMoreThanOneGroupBatch 验证业务句柄经独立 Reader 路由持续读 Rows 时，Writer 可提交满页。
func TestUsageCostRecalculationStreamsMoreThanOneGroupBatch(t *testing.T) {
	writer, _ := openRecalculationPools(t)
	when := time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local)
	hour := when.Truncate(time.Hour)
	day := time.Date(when.Year(), when.Month(), when.Day(), 0, 0, 0, 0, time.Local)
	events := make([]entities.UsageEvent, 1001)
	hourly := make([]entities.UsageOverviewHourlyStat, len(events))
	daily := make([]entities.UsageOverviewDailyStat, len(events))
	for index := range events {
		model := fmt.Sprintf("model-%04d", index)
		events[index] = entities.UsageEvent{EventKey: model, APIGroupKey: "key", Model: model, Timestamp: when, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)}
		hourly[index] = entities.UsageOverviewHourlyStat{BucketStart: hour, APIGroupKey: "key", Model: model, RequestCount: 1, CostUSD: recalcCost(2), UnavailableCostCount: recalcCount(0), CreatedAt: when, UpdatedAt: when}
		daily[index] = entities.UsageOverviewDailyStat{BucketStart: day, APIGroupKey: "key", Model: model, RequestCount: 1, CostUSD: recalcCost(2), UnavailableCostCount: recalcCount(0), CreatedAt: when, UpdatedAt: when}
	}
	for _, records := range []any{&events, &hourly, &daily} {
		if err := writer.CreateInBatches(records, 100).Error; err != nil {
			t.Fatal(err)
		}
	}
	scope := repository.UsageCostRecalculationScope{Start: hour, End: hour.Add(time.Hour), MaxEventID: events[len(events)-1].ID}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// 同一业务 GORM 句柄的 Read/Write clause 必须实际路由两个物理池，不能仅因句柄相同拒绝。
	count, err := repository.CountUsageCostRecalculationEvents(ctx, writer, scope)
	if err != nil || count != int64(len(events)) {
		t.Fatalf("count grouped events: count=%d err=%v", count, err)
	}
	page, cursor, done, err := repository.LoadUsageCostRecalculationPage(ctx, writer, scope, 0)
	if err != nil || len(page) != 1000 || done || cursor != events[999].ID {
		t.Fatalf("first event page: len=%d cursor=%d done=%v err=%v", len(page), cursor, done, err)
	}

}

// TestUsageCostRecalculationRejectsMissingRowsAndInvalidOldFees 验证旧值或必要桶不可用时不提交事件改价。
func TestUsageCostRecalculationRejectsMissingRowsAndInvalidOldFees(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate string
	}{
		{"missing hourly bucket", "DELETE FROM usage_overview_hourly_stats"},
		{"old amount is NULL", "UPDATE usage_events SET cost_usd = NULL"},
		{"old availability is NULL", "UPDATE usage_events SET cost_available = NULL"},
		{"old amount is infinite", "UPDATE usage_events SET cost_usd = 1e999"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			writer, reader := openRecalculationPools(t)
			when := time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local)
			event := entities.UsageEvent{EventKey: "target", APIGroupKey: "key", Model: "priced", Timestamp: when, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)}
			if err := writer.Create(&event).Error; err != nil {
				t.Fatal(err)
			}
			if err := repository.AggregateUsageOverviewStats(context.Background(), writer, when.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			scope := repository.UsageCostRecalculationScope{Start: when.Truncate(time.Hour), End: when.Add(time.Hour), MaxEventID: event.ID}
			page, _, _, err := repository.LoadUsageCostRecalculationPage(context.Background(), reader, scope, 0)
			if err != nil || len(page) != 1 {
				t.Fatalf("load target: len=%d err=%v", len(page), err)
			}
			if err := writer.Exec(testCase.mutate).Error; err != nil {
				t.Fatal(err)
			}
			if err := repository.ApplyUsageCostRecalculationBatch(context.Background(), writer, scope, page, recalcResolver(t)); err == nil {
				t.Fatal("corrupt old fee or missing bucket was accepted")
			}
			var daily entities.UsageOverviewDailyStat
			if err := writer.Take(&daily).Error; err != nil || daily.CostUSD == nil || !recalcClose(*daily.CostUSD, 1) || daily.RequestCount != 1 || daily.TotalTokens != 1_000_000 {
				t.Fatalf("failed batch changed daily bucket: %+v err=%v", daily, err)
			}
			var stored entities.UsageEvent
			if err := writer.First(&stored, event.ID).Error; err != nil {
				t.Fatal(err)
			}
			if testCase.name == "missing hourly bucket" && (stored.CostUSD == nil || !recalcClose(*stored.CostUSD, 1)) {
				t.Fatalf("missing bucket left event updated: %+v", stored)
			}
			assertUsageOverviewCheckpoint(t, writer, event.ID)
		})
	}
}

// TestUsageCostRecalculationRejectsFiniteDeltaOverflow 验证有限新旧金额造成桶溢出时事件及另一种桶一起回滚。
func TestUsageCostRecalculationRejectsFiniteDeltaOverflow(t *testing.T) {
	for _, grain := range []string{"hourly", "daily"} {
		t.Run(grain, func(t *testing.T) {
			writer, reader := openRecalculationPools(t)
			when := time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local)
			event := entities.UsageEvent{EventKey: "finite-large", APIGroupKey: "key", Model: "priced", Timestamp: when, InputTokens: int64(9_223_372_036_854_775_807), CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)}
			if err := writer.Create(&event).Error; err != nil {
				t.Fatal(err)
			}
			if err := repository.AggregateUsageOverviewStats(context.Background(), writer, when.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			table := "usage_overview_" + grain + "_stats"
			large := math.MaxFloat64 * 0.75
			if err := writer.Table(table).Where("id > ?", 0).Update("cost_usd", large).Error; err != nil {
				t.Fatal(err)
			}
			snapshot, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{{
				Model: "priced", PricingStyle: "openai", BasePrices: pricing.BasePrices{Input: 1e295}, ModelMultiplier: 1,
				ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{},
			}}, time.Local)
			if err != nil {
				t.Fatal(err)
			}
			scope := repository.UsageCostRecalculationScope{Start: when.Truncate(time.Hour), End: when.Add(time.Hour), MaxEventID: event.ID}
			page, _, _, err := repository.LoadUsageCostRecalculationPage(context.Background(), reader, scope, 0)
			if err != nil || len(page) != 1 {
				t.Fatalf("load large fee event: len=%d err=%v", len(page), err)
			}
			if err := repository.ApplyUsageCostRecalculationBatch(context.Background(), writer, scope, page, pricing.NewCatalog(snapshot).NewResolver()); err == nil {
				t.Fatal("finite delta overflow accepted")
			}
			var stored entities.UsageEvent
			if err := writer.First(&stored, event.ID).Error; err != nil || stored.CostUSD == nil || !recalcClose(*stored.CostUSD, 1) {
				t.Fatalf("overflow left event updated: %+v err=%v", stored, err)
			}
			for _, statTable := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
				var cost float64
				if err := writer.Table(statTable).Select("cost_usd").Where("id > ?", 0).Scan(&cost).Error; err != nil {
					t.Fatal(err)
				}
				want := 1.0
				if statTable == table {
					want = large
				}
				if !recalcClose(cost/want, 1) {
					t.Fatalf("%s fee=%g, want %g", statTable, cost, want)
				}
			}
		})
	}
}

func openRecalculationPools(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	writer, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "recalculation.db")})
	if err != nil {
		t.Fatal(err)
	}
	writerSQL, _ := writer.DB()
	readerSQL, _ := reader.DB()
	closeResolverTestPools(t, writerSQL, readerSQL)
	return writer, reader
}

func recalcResolver(t *testing.T) pricing.Resolver {
	t.Helper()
	snapshot, err := pricing.CompilePricingSnapshot([]pricing.ModelPricingConfig{{
		Model: "priced", PricingStyle: "openai", BasePrices: pricing.BasePrices{Input: 2}, ModelMultiplier: 1,
		ConditionalMultipliers: []pricing.RuleConfig{}, Branches: []pricing.PriceBranch{},
	}}, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return pricing.NewCatalog(snapshot).NewResolver()
}

func recalcCost(value float64) *float64 { return &value }
func recalcAvailable(value bool) *bool  { return &value }
func recalcCount(value int64) *int64    { return &value }

func recalcEventIDs(events []entities.UsageEvent) []int64 {
	ids := make([]int64, len(events))
	for index := range events {
		ids[index] = events[index].ID
	}
	return ids
}

func recalcClose(actual, want float64) bool {
	return !math.IsNaN(actual) && !math.IsInf(actual, 0) && math.Abs(actual-want) < 1e-10
}

func assertRecalculationFeeState(t *testing.T, db *gorm.DB, eventID int64, wantCost float64, wantAvailable bool, model string, wantBucketCost float64, wantUnavailable int64) {
	t.Helper()
	var event entities.UsageEvent
	if err := db.First(&event, eventID).Error; err != nil || event.CostUSD == nil || event.CostAvailable == nil || !recalcClose(*event.CostUSD, wantCost) || *event.CostAvailable != wantAvailable {
		t.Fatalf("event %d fee=%+v err=%v", eventID, event, err)
	}
	var hourly entities.UsageOverviewHourlyStat
	var daily entities.UsageOverviewDailyStat
	if err := db.Where("model = ?", model).Take(&hourly).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("model = ?", model).Take(&daily).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		cost             *float64
		unavailable      *int64
		requests, tokens int64
	}{
		{hourly.CostUSD, hourly.UnavailableCostCount, hourly.RequestCount, hourly.TotalTokens},
		{daily.CostUSD, daily.UnavailableCostCount, daily.RequestCount, daily.TotalTokens},
	} {
		if row.cost == nil || row.unavailable == nil || !recalcClose(*row.cost, wantBucketCost) || *row.unavailable != wantUnavailable || row.requests != 1 || row.tokens != 1_000_000 {
			t.Fatalf("model %s bucket changed: %+v", model, row)
		}
	}
}

func TestUsageCostRecalculationMissingPricePreservesFeeUntilRetry(t *testing.T) {
	db, _ := openRecalculationPools(t)
	when := time.Date(2026, 9, 23, 10, 15, 0, 0, time.Local)
	event := entities.UsageEvent{EventKey: "price-missing", Model: "priced", Timestamp: when, InputTokens: 1_000_000, TotalTokens: 1_000_000, CostUSD: recalcCost(1), CostAvailable: recalcAvailable(true)}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, when.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	scope := repository.UsageCostRecalculationScope{Start: when.Truncate(time.Hour), End: when.Add(time.Hour), MaxEventID: event.ID}
	page, _, _, err := repository.LoadUsageCostRecalculationPage(context.Background(), db, scope, 0)
	if err != nil {
		t.Fatal(err)
	}
	empty := pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver()
	if err := repository.ApplyUsageCostRecalculationBatch(context.Background(), db, scope, page, empty); err == nil {
		t.Fatal("missing price accepted")
	}
	assertRecalculationFeeState(t, db, event.ID, 1, true, "priced", 1, 0)
	if err := repository.ApplyUsageCostRecalculationBatch(context.Background(), db, scope, page, recalcResolver(t)); err != nil {
		t.Fatal(err)
	}
	assertRecalculationFeeState(t, db, event.ID, 2, true, "priced", 2, 0)
}
