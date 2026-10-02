package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

type nonCostUsageSnapshot struct {
	identity    entities.UsageIdentity
	activity    []entities.UsageActivityStat
	latency     []entities.UsageLatencyStat
	checkpoints repository.UsageAggregationCheckpointSnapshot
	ranking     ranking.Metrics
}

// 只改价格和事件费用后，身份、活动、延迟及排行仍保持原请求/Token与已提交水位。
func TestUsageNonCostAggregatesIgnorePriceAndStoredFeeChanges(t *testing.T) {
	withRepositoryTestLocation(t, "UTC")
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "authfile", Identity: "auth-a", Name: "Auth A"}).Error; err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 1}); err != nil {
		t.Fatalf("seed initial model price: %v", err)
	}
	firstFee, available, ttft := 0.00002, true, int64(100)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey: "non-cost-event", APIGroupKey: "key-a", Model: "model-a", AuthType: "oauth", AuthIndex: "auth-a",
		Timestamp: now.Add(-5 * time.Minute), InputTokens: 20, OutputTokens: 10, TotalTokens: 30,
		TTFTMS: &ttft, LatencyMS: 500, CostUSD: &firstFee, CostAvailable: &available,
	}}); err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	for _, aggregate := range []struct {
		name string
		run  func(context.Context, *gorm.DB, time.Time) error
	}{
		{"identity", repository.AggregateUsageIdentityStats},
		{"activity", repository.AggregateUsageActivityStats},
		{"latency", repository.AggregateUsageLatencyStats},
	} {
		if err := aggregate.run(ctx, db, now); err != nil {
			t.Fatalf("aggregate %s: %v", aggregate.name, err)
		}
	}
	before := loadNonCostUsageSnapshot(t, db, now)
	if before.identity.TotalRequests != 1 || before.identity.TotalTokens != 30 || before.identity.LastAggregatedUsageEventID != 1 ||
		len(before.activity) == 0 || len(before.latency) != 2 || before.checkpoints.ActivityCursor != 1 || before.checkpoints.LatencyCursor != 1 ||
		before.ranking.RequestCount != 1 || before.ranking.TotalTokens != 30 {
		t.Fatalf("baseline did not exercise every non-fee consumer: %+v", before)
	}
	if _, err := repository.UpsertModelPriceSetting(db, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 9}); err != nil {
		t.Fatalf("change current model price: %v", err)
	}
	if err := db.Model(&entities.UsageEvent{}).Where("event_key = ?", "non-cost-event").Updates(map[string]any{"cost_usd": 0.00018, "cost_available": true}).Error; err != nil {
		t.Fatalf("rewrite only stored fee: %v", err)
	}
	for _, aggregate := range []struct {
		name string
		run  func(context.Context, *gorm.DB, time.Time) error
	}{
		{"identity", repository.AggregateUsageIdentityStats},
		{"activity", repository.AggregateUsageActivityStats},
		{"latency", repository.AggregateUsageLatencyStats},
	} {
		if err := aggregate.run(ctx, db, now); err != nil {
			t.Fatalf("repeat %s after fee rewrite: %v", aggregate.name, err)
		}
	}
	after := loadNonCostUsageSnapshot(t, db, now)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("price/fee rewrite changed non-cost consumers:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func loadNonCostUsageSnapshot(t *testing.T, db *gorm.DB, now time.Time) nonCostUsageSnapshot {
	t.Helper()
	var result nonCostUsageSnapshot
	if err := db.Where("identity = ?", "auth-a").First(&result.identity).Error; err != nil {
		t.Fatalf("load identity totals: %v", err)
	}
	if err := db.Order("id asc").Find(&result.activity).Error; err != nil {
		t.Fatalf("load Activity stats: %v", err)
	}
	if err := db.Order("id asc").Find(&result.latency).Error; err != nil {
		t.Fatalf("load Latency stats: %v", err)
	}
	var err error
	result.checkpoints, err = repository.LoadUsageAggregationCheckpointSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("load rollup checkpoints: %v", err)
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	result.ranking, err = ranking.NewAggregator(db).AggregateDay(context.Background(), start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("aggregate Ranking day: %v", err)
	}
	return result
}
