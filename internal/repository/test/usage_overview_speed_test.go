package test

import (
	"context"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func assertOverviewSpeedTotals(t *testing.T, db *gorm.DB, table, model string, speed float64, count int64, decode float64, decodeCount int64) {
	t.Helper()
	var got struct {
		Speed, Decode      float64
		Count, DecodeCount int64
	}
	err := db.Table(table).Where("model = ?", model).Select("COALESCE(SUM(speed_tps_sum),0) AS speed, COALESCE(SUM(speed_sample_count),0) AS count, COALESCE(SUM(decode_speed_tps_sum),0) AS decode, COALESCE(SUM(decode_speed_sample_count),0) AS decode_count").Scan(&got).Error
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.Speed-speed) > 1e-9+1e-12*math.Abs(speed) || got.Count != count || math.Abs(got.Decode-decode) > 1e-9+1e-12*math.Abs(decode) || got.DecodeCount != decodeCount {
		t.Fatalf("%s/%s got %+v want speed=%g/%d decode=%g/%d", table, model, got, speed, count, decode, decodeCount)
	}
}

func TestUsageOverviewSpeedIncrementRollbackAndRetry(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 8, 10, 0, 0, 0, time.Local)
	first := entities.UsageEvent{EventKey: "speed-first", Model: "a", Timestamp: at, OutputTokens: 100, LatencyMS: 1000, TTFTMS: new(int64(500)), CostUSD: new(float64(0)), CostAvailable: new(true)}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{first}); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(ctx, db, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	later := []entities.UsageEvent{
		{EventKey: "speed-slow", Model: "a", Timestamp: at.Add(time.Minute), OutputTokens: 100, LatencyMS: 9000, TTFTMS: new(int64(1000)), Failed: true, CostUSD: new(float64(0)), CostAvailable: new(false)},
		{EventKey: "speed-missing-ttft", Model: "a", Timestamp: at.Add(24 * time.Hour), OutputTokens: 1, LatencyMS: 3000, CostUSD: new(float64(0)), CostAvailable: new(true)},
		{EventKey: "speed-other-model", Model: "b", Timestamp: at, OutputTokens: 50, LatencyMS: 1000, TTFTMS: new(int64(1000)), CostUSD: new(float64(0)), CostAvailable: new(true)},
	}
	if _, _, err := repository.InsertUsageEvents(db, later); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_speed_daily BEFORE UPDATE OF speed_tps_sum ON usage_overview_daily_stats BEGIN SELECT RAISE(ABORT,'speed daily failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(ctx, db, at.Add(25*time.Hour)); err == nil {
		t.Fatal("expected failed daily update")
	}
	assertUsageOverviewCheckpoint(t, db, 1)
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertOverviewSpeedTotals(t, db, table, "a", 100, 1, 200, 1)
		assertOverviewSpeedTotals(t, db, table, "b", 0, 0, 0, 0)
	}
	if err := db.Exec("DROP TRIGGER fail_speed_daily").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repository.AggregateUsageOverviewStats(ctx, db, at.Add(25*time.Hour)); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			assertOverviewSpeedTotals(t, db, table, "a", 100+100.0/9+1.0/3, 3, 212.5, 2)
			assertOverviewSpeedTotals(t, db, table, "b", 50, 1, 0, 0)
		}
		assertUsageOverviewCheckpoint(t, db, 4)
	}
}

func TestLegacyPricingRebuildSpeedAndCatchUp(t *testing.T) {
	f := openPublishedPricingEventFixture(t)
	for _, sql := range []string{
		"UPDATE usage_events SET latency_ms=2000,ttft_ms=1000 WHERE id=1",
		"UPDATE usage_events SET latency_ms=3000,ttft_ms=1000 WHERE id=3",
		"UPDATE usage_events_archive SET latency_ms=4000,ttft_ms=NULL WHERE id=2",
		"UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id=2 WHERE name='overview'",
	} {
		if err := f.writer.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	b, err := repository.MigrateLegacyPricingEvents(ctx, f.writer, f.reader, f.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repository.CompleteLegacyPricingData(ctx, f.writer, f.reader, b); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			assertOverviewSpeedTotals(t, f.reader, table, "model-a", 12.5, 2, 20, 1)
			assertOverviewSpeedTotals(t, f.reader, table, "missing-model", 0, 0, 0, 0)
		}
	}
	for range 2 {
		if err := repository.AggregateUsageOverviewStats(ctx, f.writer, time.Now()); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			assertOverviewSpeedTotals(t, f.reader, table, "model-a", 12.5, 2, 20, 1)
			assertOverviewSpeedTotals(t, f.reader, table, "missing-model", 10.0/3, 1, 5, 1)
		}
	}
}

func TestLegacyPricingCorruptSpeedBlocksCompletion(t *testing.T) {
	for _, column := range []string{"speed_tps_sum", "speed_sample_count", "decode_speed_tps_sum", "decode_speed_sample_count"} {
		t.Run(column, func(t *testing.T) {
			f := openPublishedPricingOverviewFixture(t)
			if err := f.writer.Exec("UPDATE usage_events SET latency_ms=2000,ttft_ms=1000 WHERE id=1").Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			b, err := repository.MigrateLegacyPricingEvents(ctx, f.writer, f.reader, f.backupDir, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := f.writer.Exec("CREATE TRIGGER corrupt_speed AFTER INSERT ON usage_overview_daily_stats BEGIN UPDATE usage_overview_daily_stats SET " + column + "=" + column + "+1 WHERE id=NEW.id; END").Error; err != nil {
				t.Fatal(err)
			}
			if err := repository.CompleteLegacyPricingData(ctx, f.writer, f.reader, b); err == nil {
				t.Fatal("corrupt speed must block completion")
			}
			var state entities.PricingMigrationState
			if err := f.reader.First(&state).Error; err != nil {
				t.Fatal(err)
			}
			if state.DataComplete {
				t.Fatal("corrupt speed marked complete")
			}
		})
	}
}
