package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLoadPublishedPricingConfigsBeforeFeeSchema(t *testing.T) {
	db := openPublishedPricingFixture(t)
	if err := db.Exec(`INSERT INTO model_price_settings VALUES
		(1, 'vendor/model', 'claude', 2, 3, 0.5, 1, 2),
		(2, 'free', 'openai', 10, 20, 1, 2, 0)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO model_price_rules VALUES
		(1, 1, 'response_service_tier', 'Priority', 0.5),
		(2, 1, 'reasoning_effort', 'xhigh', 3)`).Error; err != nil {
		t.Fatal(err)
	}
	configs, err := repository.LoadPublishedPricingConfigs(context.Background(), db)
	if err != nil || len(configs) != 2 {
		t.Fatalf("read price baseline without future columns: configs=%+v err=%v", configs, err)
	}
	snapshot, err := pricing.CompilePricingSnapshot(configs, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := snapshot.PricingModelConfig("vendor/model")
	if !ok || model.Branches == nil || len(model.Branches) != 0 || len(model.ConditionalMultipliers) != 2 || model.PricingStyle != "claude" {
		t.Fatalf("old configuration was not preserved: %+v", model)
	}
	fee := pricing.NewCatalog(snapshot).NewResolver().CalculateFee(repository.UsageEventCostSubject(entities.UsageEvent{
		Model: "vendor/model", InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30, CacheCreationTokens: 10,
		ResponseServiceTier: "Priority", ReasoningEffort: "xhigh",
	}))
	// 普通输入60、输出20、缓存读30／写10；模型倍率2与条件0.5、3连乘。
	want := (60*2 + 20*3 + 30*0.5 + 10*1) * 3 / 1e6
	if !fee.Available || math.IsNaN(fee.TotalCostUSD) || math.Abs(fee.TotalCostUSD-want) > 1e-12 {
		t.Fatalf("legacy fee=%+v, want %g", fee, want)
	}
	free := pricing.NewCatalog(snapshot).NewResolver().CalculateFee(repository.UsageEventCostSubject(entities.UsageEvent{Model: "free", InputTokens: 100}))
	if free.TotalCostUSD != 0 || !free.Available {
		t.Fatalf("explicit zero multiplier was lost: %+v", free)
	}
	if db.Migrator().HasColumn("model_price_settings", "branches_json") {
		t.Fatal("baseline reader changed the old schema")
	}
}

func TestLoadPublishedPricingConfigsRejectsOrphanRules(t *testing.T) {
	db := openPublishedPricingFixture(t)
	if err := db.Exec(`INSERT INTO model_price_rules VALUES (1, 9, 'reasoning_effort', 'xhigh', 2)`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.LoadPublishedPricingConfigs(context.Background(), db); err == nil {
		t.Fatal("orphaned old rule was silently dropped")
	}
}

func TestLoadPublishedPricingConfigsEmptyAndNullableMultiplier(t *testing.T) {
	db := openPublishedPricingFixture(t)
	configs, err := repository.LoadPublishedPricingConfigs(context.Background(), db)
	if err != nil || configs == nil || len(configs) != 0 {
		t.Fatalf("empty price baseline: configs=%+v err=%v", configs, err)
	}
	if err := db.Exec(`INSERT INTO model_price_settings VALUES (1, 'legacy-null', 'openai', 1, 2, 0, 0, NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	configs, err = repository.LoadPublishedPricingConfigs(context.Background(), db)
	if err != nil || len(configs) != 1 || configs[0].ModelMultiplier != 1 || configs[0].ConditionalMultipliers == nil {
		t.Fatalf("legacy NULL multiplier should retain previous default 1: configs=%+v err=%v", configs, err)
	}
}

// openPublishedPricingFixture 使用M2已有的物理列，故意不建费用分支或当前实体的其他列。
func openPublishedPricingFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	for _, statement := range []string{
		`CREATE TABLE model_price_settings (id INTEGER PRIMARY KEY, model TEXT, pricing_style TEXT, prompt_price_per1_m REAL, completion_price_per1_m REAL, cache_read_price_per1_m REAL, cache_creation_price_per1_m REAL, price_multiplier REAL)`,
		`CREATE TABLE model_price_rules (id INTEGER PRIMARY KEY, model_price_setting_id INTEGER, key TEXT, value TEXT, multiplier REAL)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
