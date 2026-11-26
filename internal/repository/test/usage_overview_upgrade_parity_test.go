package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
)

func TestUsageOverviewReadConsumersKeepResultsAfterSixDimensionsUpgrade(t *testing.T) {
	ctx := context.Background()
	db := openTestDatabase(t)
	// 从当前测试库退回两张真正的 20261009 十维统计表，其余业务 schema 不变。
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		if err := db.Exec("DROP TABLE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`DELETE FROM schema_migrations WHERE version IN
		('20261009_pricing_storage_structure', '20261123_usage_overview_six_dimensions')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.RunPricingStorageStructure(db); err != nil {
		t.Fatal(err)
	}

	day := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	hour := day.Add(10 * time.Hour)
	hourEnd, dayEnd := hour.Add(time.Hour), day.AddDate(0, 0, 2)
	identity := entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeCredential, Identity: "auth-a", Name: "Credential A"}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]entities.CPAAPIKey{
		{APIKey: "key-a", DisplayKey: "key-a-display"}, {APIKey: "key-b", DisplayKey: "key-b-display"},
		{APIKey: "unused", DisplayKey: "unused-display"}, {APIKey: "deleted", DisplayKey: "deleted-display", IsDeleted: true},
	}).Error; err != nil {
		t.Fatal(err)
	}
	aliasA, aliasB := "alias-a", "alias-b"
	ttft := int64(500)
	base := entities.UsageEvent{
		EventKey: "before-a", Timestamp: hour.Add(time.Minute), APIGroupKey: "key-a", Model: "model-a", AuthIndex: identity.Identity,
		ServiceTier: "fast", ReasoningEffort: "high", ModelAlias: &aliasA, ResponseServiceTier: "default", Endpoint: "/one", ExecutorType: "http",
		InputTokens: 100, OutputTokens: 40, ReasoningTokens: 10, CachedTokens: 30, CacheReadTokens: 25, CacheCreationTokens: 5, TotalTokens: 150,
		LatencyMS: 1000, TTFTMS: &ttft, CostUSD: recalcCost(0.13), CostAvailable: recalcAvailable(true),
	}
	variant := base
	variant.EventKey, variant.Timestamp = "before-b", hour.Add(2*time.Minute)
	variant.ModelAlias, variant.ResponseServiceTier, variant.Endpoint, variant.ExecutorType = &aliasB, "priority", "/two", "ws"
	variant.Failed, variant.OutputTokens, variant.TotalTokens, variant.LatencyMS = true, 20, 130, 2000
	variant.CostUSD, variant.CostAvailable = recalcCost(0), recalcAvailable(false)
	otherDay := base
	otherDay.EventKey, otherDay.Timestamp, otherDay.Model, otherDay.APIGroupKey = "before-c", base.Timestamp.AddDate(0, 0, 1), "model-b", "key-b"
	deleted := base
	deleted.EventKey, deleted.APIGroupKey, deleted.AuthIndex = "before-deleted-key", "deleted", "different-auth"
	covered := []entities.UsageEvent{base, variant, otherDay, deleted}
	if _, _, err := repository.InsertUsageEvents(db, covered); err != nil {
		t.Fatal(err)
	}
	page, err := migration.BuildLegacyPricingOverviewPage(covered)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return page.Apply(tx, dayEnd) }); err != nil {
		t.Fatal(err)
	}
	watermark := entities.UsageAggregationCheckpoint{
		Name: entities.UsageAggregationCheckpointOverview, LastAggregatedUsageEventID: int64(len(covered)), StatsUpdatedAt: &dayEnd,
	}
	if err := db.Create(&watermark).Error; err != nil {
		t.Fatal(err)
	}

	filters := []dto.UsageQueryFilter{
		{Range: "custom", CustomUnit: "hour", StartTime: &hour, EndTime: &hourEnd, EndExclusive: true, QueryNow: &dayEnd},
		{Range: "custom", CustomUnit: "day", StartTime: &day, EndTime: &dayEnd, EndExclusive: true, QueryNow: &dayEnd},
	}
	before := readOverviewUpgradeConsumers(t, db, filters, identity)
	if before.Overview[0].Summary.RequestCount != 3 || before.Overview[1].Summary.RequestCount != 4 ||
		len(before.Comparisons[1].Comparisons.Models["model-a"].TokenBuckets) == 0 ||
		len(before.Analysis[0].ModelSpeed.Series) != 1 || len(before.Analysis[1].ModelSpeed.Series) != 2 ||
		len(before.OverviewKeys[1]) != 2 || len(before.AnalysisKeys[0]) != 1 ||
		len(before.IdentityModels) != 2 || before.Quota.Tokens != 430 || before.Quota.CostAvailable {
		t.Fatalf("fixture did not hit all rollup consumers: %+v", before)
	}
	// 两个被合并的请求等权平均，端到端 TPS 分别为 40 和 10；缺价和失败不排除速度。
	firstSpeed := before.Analysis[0].ModelSpeed.Series[0].Samples[0]
	if firstSpeed.SpeedSampleCount != 2 || firstSpeed.MeanTPS == nil {
		t.Fatalf("missing TPS sample: %+v", firstSpeed)
	}
	assertFloatClose(t, *firstSpeed.MeanTPS, 25)

	if err := repository.RunDatabaseMigrationsWithBackup(ctx, db, db, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	after := readOverviewUpgradeConsumers(t, db, filters, identity)
	compareOverviewUpgradePayloads(t, before, after)
	assertUsageOverviewCheckpoint(t, db, int64(len(covered)))

	// 旧记录仍在热表；新请求精确追赶一次，并累加到已合并的行而不是重计既有请求。
	newEvent := variant
	newEvent.EventKey, newEvent.Timestamp = "after-upgrade", hour.Add(3*time.Minute)
	newEvent.CostUSD, newEvent.CostAvailable = recalcCost(0.27), recalcAvailable(true)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{newEvent}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repository.AggregateUsageOverviewStats(ctx, db, dayEnd); err != nil {
			t.Fatal(err)
		}
	}
	assertUsageOverviewCheckpoint(t, db, int64(len(covered)+1))
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var requests, missing int64
		var fee float64
		var rows int64
		if err := db.Table(table).Where("api_group_key = ? AND model = ? AND auth_index = ?", "key-a", "model-a", identity.Identity).
			Select("COUNT(*), SUM(request_count), SUM(unavailable_cost_count), SUM(cost_usd)").Row().Scan(&rows, &requests, &missing, &fee); err != nil ||
			rows != 1 || requests != 3 || missing != 1 {
			t.Fatalf("%s did not catch up once: rows=%d requests=%d missing=%d err=%v", table, rows, requests, missing, err)
		}
		assertFloatClose(t, fee, 0.4)
	}
}

type overviewUpgradeConsumers struct {
	Overview, Comparisons [2]*dto.UsageOverviewRecord
	Analysis              [2]*dto.AnalysisRecord
	OverviewKeys          [2][]entities.CPAAPIKey
	AnalysisKeys          [2][]entities.CPAAPIKey
	Quota                 repository.UsageWindowStats
	GroupedQuota          repository.UsageWindowGroupedStats
	IdentityModels        []repository.UsageIdentityModelUsage
}

func readOverviewUpgradeConsumers(t *testing.T, db *gorm.DB, filters []dto.UsageQueryFilter, identity entities.UsageIdentity) overviewUpgradeConsumers {
	t.Helper()
	ctx := context.Background()
	var result overviewUpgradeConsumers
	var err error
	for i, filter := range filters {
		if result.Overview[i], err = repository.BuildUsageOverviewWithFilterAndRecentCache(db, filter, nil); err != nil {
			t.Fatal(err)
		}
		if result.Comparisons[i], err = repository.BuildUsageOverviewComparisonsWithFilterAndRecentCache(db, filter, nil); err != nil {
			t.Fatal(err)
		}
		if result.Analysis[i], err = repository.BuildAnalysisWithFilter(db, filter); err != nil {
			t.Fatal(err)
		}
		if result.OverviewKeys[i], err = repository.ListUsageAPIKeyOptions(db, "overview", filter, nil); err != nil {
			t.Fatal(err)
		}
		if result.AnalysisKeys[i], err = repository.ListUsageAPIKeyOptions(db, "analysis", filter, nil); err != nil {
			t.Fatal(err)
		}
	}
	days := filters[1]
	calculator := newUsageWindowCalculatorForTest(t, db)
	if result.Quota, err = calculator.SumByAuthIndex(ctx, identity.Identity, *days.StartTime, days.EndTime); err != nil {
		t.Fatal(err)
	}
	if result.GroupedQuota, err = calculator.SumGroupsByAuthIndex(ctx, identity.Identity, *days.StartTime, days.EndTime, func(model string) (string, bool) {
		return model, true
	}); err != nil {
		t.Fatal(err)
	}
	if result.IdentityModels, err = repository.LoadUsageIdentityModels(ctx, db, identity.ID, *days.StartTime, *days.EndTime); err != nil {
		t.Fatal(err)
	}
	return result
}

// 对比完整序列化结果；计数、桶和标签精确匹配，费用及速度允许浮点累加顺序差异。
func compareOverviewUpgradePayloads(t *testing.T, before, after overviewUpgradeConsumers) {
	t.Helper()
	decode := func(value any) any {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	var compare func(string, any, any)
	compare = func(path string, a, b any) {
		t.Helper()
		switch x := a.(type) {
		case map[string]any:
			y, ok := b.(map[string]any)
			if !ok || len(x) != len(y) {
				t.Fatalf("%s object changed: %v -> %v", path, a, b)
			}
			for key, value := range x {
				next, found := y[key]
				if !found {
					t.Fatalf("%s lost field %q", path, key)
				}
				compare(path+"."+key, value, next)
			}
		case []any:
			y, ok := b.([]any)
			if !ok || len(x) != len(y) {
				t.Fatalf("%s array changed: %v -> %v", path, a, b)
			}
			for i := range x {
				compare(fmt.Sprintf("%s[%d]", path, i), x[i], y[i])
			}
		case json.Number:
			y, ok := b.(json.Number)
			if ok {
				ai, aerr := x.Int64()
				bi, berr := y.Int64()
				if aerr == nil && berr == nil && ai == bi {
					return
				}
				if aerr != nil || berr != nil {
					af, aerr := x.Float64()
					bf, berr := y.Float64()
					if aerr == nil && berr == nil && math.Abs(af-bf) <= 1e-9 {
						return
					}
				}
			}
			t.Fatalf("%s numeric value changed: %v -> %v", path, a, b)
		default:
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("%s changed: %v -> %v", path, a, b)
			}
		}
	}
	compare("rollups", decode(before), decode(after))
}
