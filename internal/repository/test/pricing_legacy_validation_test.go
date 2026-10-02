package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestVerifyLegacyPricingDataAcceptsCoveredHotColdAndExplicitZero(t *testing.T) {
	fixture, baseline := pricingLegacyValidationFixture(t, pricingLegacyFixtureOptions{})
	if err := repository.VerifyLegacyPricingData(context.Background(), fixture.reader, baseline); err != nil {
		t.Fatalf("valid fixed hot/cold costs rejected: %v", err)
	}
}

func TestVerifyLegacyPricingDataAcceptsEmptyHotWithCoveredArchive(t *testing.T) {
	fixture, baseline := pricingLegacyValidationFixture(t, pricingLegacyFixtureOptions{coldOnly: true})
	if baseline.Fixed.HotMaxID != 0 || baseline.Fixed.ArchiveMaxID != 2 || baseline.Fixed.OverviewCursor != 2 {
		t.Fatalf("wrong archive-only bounds: %+v", baseline.Fixed)
	}
	if err := repository.VerifyLegacyPricingData(context.Background(), fixture.reader, baseline); err != nil {
		t.Fatalf("valid archive-only data rejected: %v", err)
	}
}

func TestVerifyLegacyPricingDataAcceptsEmptyPublishedDatabaseWithoutCheckpointRow(t *testing.T) {
	fixture, baseline := pricingLegacyValidationFixture(t, pricingLegacyFixtureOptions{empty: true})
	if baseline.Fixed.HotMaxID != 0 || baseline.Fixed.ArchiveMaxID != 0 || baseline.Fixed.OverviewCursor != 0 {
		t.Fatalf("wrong empty bounds: %+v", baseline.Fixed)
	}
	if err := repository.VerifyLegacyPricingData(context.Background(), fixture.reader, baseline); err != nil {
		t.Fatalf("empty published database rejected: %v", err)
	}
}

func TestVerifyLegacyPricingDataAllowsLargeGroupRoundingButRejectsMaterialDifference(t *testing.T) {
	fixture, baseline := pricingLegacyValidationFixture(t, pricingLegacyFixtureOptions{largeGroup: true})
	// 同桶两条事件分别持久化费用，汇总金额允许普通 float64 累加尾差。
	for _, statement := range []string{
		"UPDATE usage_events SET cost_usd = 1000000.1 WHERE id = 1",
		"UPDATE usage_events SET cost_usd = 2000000.2 WHERE id = 3",
		"UPDATE usage_overview_hourly_stats SET cost_usd = 3000000.300001 WHERE id = 1",
		"UPDATE usage_overview_daily_stats SET cost_usd = 3000000.300001 WHERE id = 1",
	} {
		if err := fixture.writer.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.VerifyLegacyPricingData(context.Background(), fixture.reader, baseline); err != nil {
		t.Fatalf("large group rounding rejected: %v", err)
	}
	if err := fixture.writer.Exec("UPDATE usage_overview_daily_stats SET cost_usd = cost_usd + 0.01 WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyLegacyPricingData(context.Background(), fixture.reader, baseline); err == nil || !strings.Contains(err.Error(), "cost differs") {
		t.Fatalf("material cost difference passed or wrong error: %v", err)
	}
}

func TestVerifyLegacyPricingDataRejectsChangedFactsAndCosts(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		statement string
		want      string
	}{
		{"hot NULL fee", "UPDATE usage_events SET cost_usd = NULL WHERE id = 1", "cost_usd"},
		{"cold NULL fee", "UPDATE usage_events_archive SET cost_usd = NULL WHERE id = 2", "cost_usd"},
		{"NULL availability", "UPDATE usage_events SET cost_available = NULL WHERE id = 3", "cost_available"},
		{"nonfinite fee", "UPDATE usage_events SET cost_usd = 1e999 WHERE id = 1", "cost_usd"},
		{"wrong hourly fee", "UPDATE usage_overview_hourly_stats SET cost_usd = cost_usd + 0.01 WHERE id = 1", "cost"},
		{"wrong daily unavailable", "UPDATE usage_overview_daily_stats SET unavailable_cost_count = 1 WHERE id = 1", "unavailable"},
		{"changed event fact", "UPDATE usage_events SET total_tokens = total_tokens + 1 WHERE id = 1", "total_tokens"},
		{"changed C", "UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id = 2 WHERE name = 'overview'", "checkpoint"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture, baseline := pricingLegacyValidationFixture(t, pricingLegacyFixtureOptions{})
			if err := fixture.writer.Exec(scenario.statement).Error; err != nil {
				t.Fatal(err)
			}
			err := repository.VerifyLegacyPricingData(context.Background(), fixture.reader, baseline)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), scenario.want) {
				t.Fatalf("corruption passed or wrong error: %v", err)
			}
		})
	}
}

type pricingLegacyFixtureOptions struct {
	coldOnly   bool
	empty      bool
	largeGroup bool
}

func pricingLegacyValidationFixture(t *testing.T, options pricingLegacyFixtureOptions) (publishedPricingEventFixture, repository.PricingLegacyBaseline) {
	t.Helper()
	fixture := openPublishedPricingEventFixture(t)
	if options.empty {
		for _, statement := range []string{
			"DELETE FROM usage_events", "DELETE FROM usage_events_archive",
			"DELETE FROM usage_overview_hourly_stats", "DELETE FROM usage_overview_daily_stats",
			"DELETE FROM usage_aggregation_checkpoints",
		} {
			if err := fixture.writer.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	} else if options.coldOnly {
		for _, statement := range []string{
			"DELETE FROM usage_events",
			"DELETE FROM usage_overview_hourly_stats",
			"DELETE FROM usage_overview_daily_stats",
			"UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id = 2 WHERE name = 'overview'",
		} {
			if err := fixture.writer.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	} else if err := fixture.writer.Exec("UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id = 3 WHERE name = 'overview'").Error; err != nil {
		t.Fatal(err)
	}
	if options.largeGroup {
		for _, statement := range []string{
			"UPDATE usage_events SET model = 'model-a', auth_index = 'hot-a' WHERE id = 3",
			"UPDATE usage_overview_hourly_stats SET request_count = 2, success_count = 2, input_tokens = 190, output_tokens = 30, total_tokens = 220 WHERE id = 1",
			"UPDATE usage_overview_daily_stats SET request_count = 2, success_count = 2, input_tokens = 190, output_tokens = 30, total_tokens = 220 WHERE id = 1",
		} {
			if err := fixture.writer.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		if options.empty {
			break
		}
		archiveBucket, missingBucket := "2026-08-01T10:00:00Z", "2026-09-01T10:00:00Z"
		if table == "usage_overview_daily_stats" {
			archiveBucket, missingBucket = "2026-08-01T00:00:00Z", "2026-09-01T00:00:00Z"
		}
		insert := `INSERT INTO ` + table + ` (id,bucket_start,api_group_key,model,auth_index,model_alias,service_tier,response_service_tier,reasoning_effort,endpoint,executor_type,request_count,success_count,failure_count,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens)
		VALUES (2,?,'','model-a','','','','','','','',1,1,0,50,10,0,0,0,0,60)`
		if err := fixture.writer.Exec(insert, archiveBucket).Error; err != nil {
			t.Fatal(err)
		}
		if !options.coldOnly && !options.largeGroup {
			insert = `INSERT INTO ` + table + ` (id,bucket_start,api_group_key,model,auth_index,model_alias,service_tier,response_service_tier,reasoning_effort,endpoint,executor_type,request_count,success_count,failure_count,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens)
			VALUES (3,?,'','missing-model','','','','','','','',1,1,0,90,10,0,0,0,0,100)`
			if err := fixture.writer.Exec(insert, missingBucket).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var costs []struct {
		ID            int64
		CostUSD       float64
		CostAvailable bool
	}
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		var rows []struct {
			ID            int64
			CostUSD       float64
			CostAvailable bool
		}
		if err := fixture.reader.Table(table).Select("id, cost_usd, cost_available").Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		costs = append(costs, rows...)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		for _, cost := range costs {
			if options.largeGroup && cost.ID == 3 {
				continue
			}
			unavailable := int64(0)
			if !cost.CostAvailable {
				unavailable = 1
			}
			if err := fixture.writer.Table(table).Where("id = ?", cost.ID).Updates(map[string]any{"cost_usd": cost.CostUSD, "unavailable_cost_count": unavailable}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if options.largeGroup {
			var amount float64
			for _, cost := range costs {
				if cost.ID == 1 || cost.ID == 3 {
					amount += cost.CostUSD
				}
			}
			if err := fixture.writer.Table(table).Where("id = 1").Update("cost_usd", amount).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var state entities.PricingMigrationState
	if err := fixture.reader.Where("id = 1").Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	var cursors map[string]any
	if state.CursorsJSON == nil || json.Unmarshal([]byte(*state.CursorsJSON), &cursors) != nil {
		t.Fatal("missing event cursors")
	}
	min := func(a, b int64) int64 {
		if a < b {
			return a
		}
		return b
	}
	cursors["overview"] = map[string]int64{"hot_after_id": min(baseline.Fixed.OverviewCursor, baseline.Fixed.HotMaxID), "archive_after_id": min(baseline.Fixed.OverviewCursor, baseline.Fixed.ArchiveMaxID)}
	encoded, err := json.Marshal(cursors)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.writer.Model(&entities.PricingMigrationState{}).Where("id = 1").Updates(map[string]any{"cursors_json": string(encoded), "phase": "overview_backfilling"}).Error; err != nil {
		t.Fatal(err)
	}
	return fixture, baseline
}
