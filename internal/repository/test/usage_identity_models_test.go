package test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLoadUsageIdentityModelsUsesOnlyIdentityDailyStats(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	identities := []entities.UsageIdentity{
		{ID: 1, AuthType: entities.UsageIdentityAuthTypeCredential, Identity: "auth-one", Name: "Same"},
		{ID: 2, AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "auth-two", Name: "Same"},
		{ID: 3, AuthType: entities.UsageIdentityAuthTypeCredential, Identity: "", Name: "Empty"},
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, 7)
	priced, zero := 2.5, 0.0
	available, missing := int64(0), int64(1)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: start, AuthIndex: "auth-one", APIGroupKey: "active-key", Model: "upstream", RequestCount: 2, InputTokens: 100, OutputTokens: 50, CacheReadTokens: 20, CacheCreationTokens: 10, ReasoningTokens: 5, TotalTokens: 150, CostUSD: &priced, UnavailableCostCount: &available},
		{BucketStart: end.AddDate(0, 0, -1), AuthIndex: "auth-one", APIGroupKey: "deleted-key", Model: "upstream", RequestCount: 1, InputTokens: 40, OutputTokens: 10, CacheReadTokens: 4, CacheCreationTokens: 3, ReasoningTokens: 2, TotalTokens: 50, CostUSD: &zero, UnavailableCostCount: &missing},
		{BucketStart: start, AuthIndex: "auth-one", APIGroupKey: "missing-key", Model: "upstream", RequestCount: 3, TotalTokens: 30, CostUSD: &zero, UnavailableCostCount: &missing},
		{BucketStart: start, AuthIndex: "auth-one", APIGroupKey: "active-key", Model: "different", RequestCount: 4, TotalTokens: 40, CostUSD: &priced, UnavailableCostCount: &available},
		{BucketStart: end, AuthIndex: "auth-one", APIGroupKey: "active-key", Model: "outside", RequestCount: 99, CostUSD: &priced, UnavailableCostCount: &available},
		{BucketStart: start, AuthIndex: "auth-two", APIGroupKey: "active-key", Model: "other-identity", RequestCount: 9, CostUSD: &priced, UnavailableCostCount: &available},
		{BucketStart: start, AuthIndex: "", APIGroupKey: "active-key", Model: "unidentified", RequestCount: 9, CostUSD: &priced, UnavailableCostCount: &available},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// 没有明细和 API key 表时仍应成功，证明不能补扫事件或按下游 key 过滤。
	if err := db.Migrator().DropTable(&entities.UsageEvent{}, &entities.CPAAPIKey{}); err != nil {
		t.Fatal(err)
	}
	got, err := repository.LoadUsageIdentityModels(ctx, db, 1, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("models = %+v", got)
	}
	other, combined := got[0], got[1]
	if other.Model != "different" || other.RequestCount != 4 {
		t.Fatalf("different model = %+v", other)
	}
	if combined.Model != "upstream" || combined.RequestCount != 6 || combined.InputTokens != 140 || combined.OutputTokens != 60 || combined.CacheReadTokens != 24 || combined.CacheCreationTokens != 13 || combined.ReasoningTokens != 7 || combined.TotalTokens != 230 || combined.UnavailableCostCount != 2 {
		t.Fatalf("cross-key model aggregation = %+v", combined)
	}
	assertFloatClose(t, combined.CostUSD, 2.5)
	second, err := repository.LoadUsageIdentityModels(ctx, db, 2, start, end)
	if err != nil || len(second) != 1 || second[0].Model != "other-identity" {
		t.Fatalf("same-name identity leaked: %+v, %v", second, err)
	}
	empty, err := repository.LoadUsageIdentityModels(ctx, db, 3, start, end)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty identity matched unidentified buckets: %+v, %v", empty, err)
	}
	if err := repository.ResetUsageIdentityStats(ctx, db, 1, start); err != nil {
		t.Fatal(err)
	}
	after, err := repository.LoadUsageIdentityModels(ctx, db, 1, start, end)
	if err != nil || fmt.Sprint(after) != fmt.Sprint(got) {
		t.Fatalf("cumulative reset changed model history: %+v, %v", after, err)
	}
}

func TestLoadUsageIdentityModelsRejectsUnbackfilledOrNonFiniteCost(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cost        *float64
		unavailable *int64
	}{
		{name: "missing cost", unavailable: new(int64)},
		{name: "missing availability", cost: new(float64)},
		{name: "infinite cost", cost: floatPtr(math.Inf(1)), unavailable: new(int64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			if err := db.Create(&entities.UsageIdentity{ID: 1, AuthType: 1, Identity: "identity"}).Error; err != nil {
				t.Fatal(err)
			}
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
			goodCost, goodAvailability := 1.0, int64(0)
			rows := []entities.UsageOverviewDailyStat{
				{BucketStart: start, AuthIndex: "identity", APIGroupKey: "a", Model: "model", CostUSD: &goodCost, UnavailableCostCount: &goodAvailability},
				{BucketStart: start, AuthIndex: "identity", APIGroupKey: "b", Model: "model", CostUSD: tc.cost, UnavailableCostCount: tc.unavailable},
			}
			if err := db.Create(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repository.LoadUsageIdentityModels(context.Background(), db, 1, start, start.AddDate(0, 0, 1)); err == nil {
				t.Fatal("expected incomplete cost to fail rather than SUM silently omitting it")
			}
		})
	}
}

func floatPtr(value float64) *float64 { return &value }

type identityModelsQueryLogger struct {
	logger.Interface
	queries *[]string
}

func (l identityModelsQueryLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	query, rows := fc()
	*l.queries = append(*l.queries, query)
	l.Interface.Trace(ctx, begin, func() (string, int64) { return query, rows }, err)
}

func TestLoadUsageIdentityModelsUsesAuthDateIndexAndBoundedDailyRows(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.Local)
	if err := db.Create(&entities.UsageIdentity{ID: 1, AuthType: 1, Identity: "target"}).Error; err != nil {
		t.Fatal(err)
	}
	cost, available := 1.0, int64(0)
	var rows []entities.UsageOverviewDailyStat
	for identity := 0; identity < 50; identity++ {
		key := fmt.Sprintf("identity-%d", identity)
		if identity == 0 {
			key = "target"
		}
		for day := 0; day < 35; day++ {
			for _, tier := range []string{"default", "priority"} {
				rows = append(rows, entities.UsageOverviewDailyStat{BucketStart: start.AddDate(0, 0, day), AuthIndex: key, APIGroupKey: "key", Model: "model", ServiceTier: tier, RequestCount: 1, CostUSD: &cost, UnavailableCostCount: &available})
			}
		}
	}
	if err := db.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	var queries []string
	capturedDB := db.Session(&gorm.Session{Logger: identityModelsQueryLogger{Interface: db.Logger, queries: &queries}})
	models, err := repository.LoadUsageIdentityModels(context.Background(), capturedDB, 1, start.AddDate(0, 0, 3), start.AddDate(0, 0, 10))
	if err != nil || len(models) != 1 || models[0].RequestCount != 14 {
		t.Fatalf("bounded models = %+v, error %v", models, err)
	}
	var query string
	for _, statement := range queries {
		lower := strings.ToLower(statement)
		if strings.Contains(lower, "usage_events") || strings.Contains(lower, "cpa_api_keys") || strings.Contains(lower, "usage_overview_hourly_stats") {
			t.Fatalf("unexpected query: %s", statement)
		}
		if strings.Contains(lower, "usage_overview_daily_stats") {
			query = statement
		}
	}
	groupedSQL := strings.ToLower(query)
	if !strings.Contains(groupedSQL, "group by `model` order by model asc") || strings.Contains(groupedSQL, "model_alias") {
		t.Fatalf("expected daily SQL grouped only by model: %v", queries)
	}
	var plan []struct{ Detail string }
	if err := db.Raw("EXPLAIN QUERY PLAN "+query, "target", start.AddDate(0, 0, 3).Format(time.RFC3339Nano), start.AddDate(0, 0, 10).Format(time.RFC3339Nano)).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	details := fmt.Sprint(plan)
	if !strings.Contains(details, "idx_usage_overview_daily_stats_auth_bucket") || !strings.Contains(details, "bucket_start>?") || !strings.Contains(details, "bucket_start<?") {
		t.Fatalf("identity/date index not used: %s", details)
	}
	var selected int64
	if err := db.Model(&entities.UsageOverviewDailyStat{}).Where("auth_index = ? AND bucket_start >= ? AND bucket_start < ?", "target", start.AddDate(0, 0, 3).Format(time.RFC3339Nano), start.AddDate(0, 0, 10).Format(time.RFC3339Nano)).Count(&selected).Error; err != nil {
		t.Fatal(err)
	}
	if selected != 14 || len(rows) != 3500 {
		t.Fatalf("controlled filtered rows = %d of %d", selected, len(rows))
	}
	t.Logf("query plan: %s; qualifying daily rows: %d/%d", details, selected, len(rows))
}

func TestLoadUsageIdentityModelsUsesAuthDateIndexAcrossDST(t *testing.T) {
	location := setAnalysisLatencyTestTimezone(t, "America/Los_Angeles")
	db := openTestDatabase(t)
	start := time.Date(2026, 10, 27, 0, 0, 0, 0, location)
	end := time.Date(2026, 11, 3, 0, 0, 0, 0, location)
	if err := db.Create(&entities.UsageIdentity{ID: 1, AuthType: 1, Identity: "target"}).Error; err != nil {
		t.Fatal(err)
	}
	cost, available := 1.0, int64(0)
	args := []any{"target"}
	for day := 0; day < 7; day++ {
		at := time.Date(2026, 10, 27+day, 12, 0, 0, 0, location)
		_, bucket := overview.BucketKeysForEvent(entities.UsageEvent{Timestamp: at})
		args = append(args, timeutil.FormatStorageTime(bucket.BucketStart))
		if err := db.Create(&entities.UsageOverviewDailyStat{BucketStart: bucket.BucketStart, AuthIndex: "target", APIGroupKey: "key", Model: "model", RequestCount: 1, CostUSD: &cost, UnavailableCostCount: &available}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var queries []string
	capturedDB := db.Session(&gorm.Session{Logger: identityModelsQueryLogger{Interface: db.Logger, queries: &queries}})
	models, err := repository.LoadUsageIdentityModels(context.Background(), capturedDB, 1, start, end)
	if err != nil || len(models) != 1 || models[0].RequestCount != 7 {
		t.Fatalf("DST models = %+v, error %v", models, err)
	}
	var query string
	for _, statement := range queries {
		if strings.Contains(strings.ToLower(statement), "usage_overview_daily_stats") {
			query = statement
		}
	}
	if query == "" || !strings.Contains(strings.ToLower(query), "bucket_start in") {
		t.Fatalf("expected DST bucket key query: %v", queries)
	}
	var plan []struct{ Detail string }
	if err := db.Raw("EXPLAIN QUERY PLAN "+query, args...).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	details := fmt.Sprint(plan)
	if !strings.Contains(details, "idx_usage_overview_daily_stats_auth_bucket") || !strings.Contains(details, "bucket_start=?") {
		t.Fatalf("DST identity/date index not used: %s", details)
	}
	t.Logf("DST query plan: %s; selected 7 daily rows", details)
}
