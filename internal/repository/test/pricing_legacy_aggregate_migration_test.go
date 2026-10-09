package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
)

// 旧物理表夹具保留原始事件字段；重建使用归一化分组。
func openPublishedPricingOverviewFixture(t *testing.T) publishedPricingEventFixture {
	t.Helper()
	fixture := openPublishedPricingEventFixture(t)
	// 原 C=1 时归档不可能包含 id=2；仅由专门的冷热已覆盖用例再插入它。
	if err := fixture.writer.Exec("DELETE FROM usage_events_archive WHERE id = 2").Error; err != nil {
		t.Fatal(err)
	}
	alignPublishedPricingOverviewBuckets(t, fixture)
	return fixture
}

func alignPublishedPricingOverviewBuckets(t *testing.T, fixture publishedPricingEventFixture) {
	t.Helper()
	instant := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC).In(time.Local)
	hour := timeutil.FormatStorageTime(instant.Truncate(time.Hour))
	day := timeutil.FormatStorageTime(time.Date(instant.Year(), instant.Month(), instant.Day(), 0, 0, 0, 0, instant.Location()))
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		bucket := hour
		if table == "usage_overview_daily_stats" {
			bucket = day
		}
		if err := fixture.writer.Table(table).Where("id IN ?", []int64{1, 3}).Updates(map[string]any{"api_group_key": "unknown", "bucket_start": bucket}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyPricingOverviewRebuildLeavesCBehindHForNormalAggregation(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	ctx := context.Background()
	baseline, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Fixed.OverviewCursor != 1 || baseline.Fixed.HotMaxID != 3 || baseline.Fixed.ArchiveMaxID != 0 {
		t.Fatalf("固定 C/H 不符: %+v", baseline.Fixed)
	}
	if err := repository.CompleteLegacyPricingData(ctx, fixture.writer, fixture.reader, baseline); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertLegacyOverviewFee(t, fixture, table, "model-a", 0.00078, 0, 1, 120)
		var count int64
		if err := fixture.writer.Table(table).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s 把 C 后事件提前计入: count=%d err=%v", table, count, err)
		}
	}
	var state entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&state).Error; err != nil || !state.DataComplete || !state.SchemaComplete {
		t.Fatalf("M6 完成标志错误: %+v %v", state, err)
	}
	var checkpoint int64
	if err := fixture.writer.Table("usage_aggregation_checkpoints").Where("name = ?", "overview").Select("last_aggregated_usage_event_id").Scan(&checkpoint).Error; err != nil || checkpoint != 1 {
		t.Fatalf("Overview 水位被 M5 改写: %d %v", checkpoint, err)
	}
	// 启动追赶沿原水位处理剩余已计价事件；重复追赶不能双计。
	for range 2 {
		if err := repository.AggregateUsageOverviewStats(ctx, fixture.writer, time.Now()); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
			assertLegacyOverviewFee(t, fixture, table, "model-a", 0.00078, 0, 1, 120)
			assertLegacyOverviewFee(t, fixture, table, "missing-model", 0, 1, 1, 100)
		}
	}
}

func TestLegacyPricingOverviewCompletesEmptyPublishedDatabase(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	for _, table := range []string{"usage_events", "usage_events_archive", "usage_overview_hourly_stats", "usage_overview_daily_stats", "usage_aggregation_checkpoints"} {
		if err := fixture.writer.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil || baseline.Fixed == nil || baseline.Fixed.OverviewCursor != 0 || baseline.Fixed.HotMaxID != 0 || baseline.Fixed.ArchiveMaxID != 0 {
		t.Fatalf("空旧库固定边界失败: %+v %v", baseline.Fixed, err)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), fixture.writer, fixture.reader, baseline); err != nil {
		t.Fatalf("空旧库不得要求虚构 Overview 行: %v", err)
	}
	var state entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&state).Error; err != nil || !state.SchemaComplete || !state.DataComplete {
		t.Fatalf("空旧库未完成费用阶段: %+v %v", state, err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var count int64
		if err := fixture.writer.Table(table).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("空旧库被插入虚假 %s 桶: %d %v", table, count, err)
		}
	}
}

func TestLegacyPricingOverviewReplacesOldBucketsAtRebuildStart(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.writer.Exec("UPDATE usage_overview_hourly_stats SET cost_usd = 7, unavailable_cost_count = 0 WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), fixture.writer, fixture.reader, baseline); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertLegacyOverviewFee(t, fixture, table, "model-a", 0.00078, 0, 1, 120)
	}
}

func TestLegacyPricingOverviewRebuildHotColdSameBucketAndIDGap(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	for _, statement := range []string{
		"UPDATE usage_events SET id = 5 WHERE id = 3",
		"INSERT INTO usage_events_archive (id,event_key,model,auth_index,timestamp,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,failed) VALUES (2,'cold-priced','model-a','hot-a','2026-09-01T10:02:00Z',50,10,0,0,60,0)",
		"UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id = 5 WHERE name = 'overview'",
		"UPDATE usage_overview_hourly_stats SET request_count = 2, success_count = 2, input_tokens = 150, output_tokens = 30, total_tokens = 180 WHERE id = 1",
		"UPDATE usage_overview_daily_stats SET request_count = 2, success_count = 2, input_tokens = 150, output_tokens = 30, total_tokens = 180 WHERE id = 1",
		`INSERT INTO usage_overview_hourly_stats (id,bucket_start,api_group_key,model,auth_index,model_alias,service_tier,response_service_tier,reasoning_effort,endpoint,executor_type,request_count,success_count,failure_count,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens) VALUES (3,'2026-09-01T10:00:00Z','unknown','missing-model','','','','','','','',1,1,0,90,10,0,0,0,0,100)`,
		`INSERT INTO usage_overview_daily_stats (id,bucket_start,api_group_key,model,auth_index,model_alias,service_tier,response_service_tier,reasoning_effort,endpoint,executor_type,request_count,success_count,failure_count,input_tokens,output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens) VALUES (3,'2026-09-01T00:00:00Z','unknown','missing-model','','','','','','','',1,1,0,90,10,0,0,0,0,100)`,
	} {
		if err := fixture.writer.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	alignPublishedPricingOverviewBuckets(t, fixture)
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), fixture.writer, fixture.reader, baseline); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertLegacyOverviewFee(t, fixture, table, "model-a", 0.0012, 0, 2, 180)
		assertLegacyOverviewFee(t, fixture, table, "missing-model", 0, 1, 1, 100)
	}
}

func TestLegacyPricingOverviewAdvancesPastEmptyHotIDRange(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	for _, statement := range []string{
		"INSERT INTO usage_events_archive (id,event_key,model,auth_index,timestamp,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,failed) VALUES (1,'hot-priced','model-a','hot-a','2026-09-01T10:00:00Z',100,20,10,5,120,0)",
		"DELETE FROM usage_events WHERE id = 1",
	} {
		if err := fixture.writer.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Fixed.OverviewCursor != 1 || baseline.Fixed.HotMaxID != 3 || baseline.Fixed.ArchiveMaxID != 1 {
		t.Fatalf("空热前缀的 C/H 不符: %+v", baseline.Fixed)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), fixture.writer, fixture.reader, baseline); err != nil {
		t.Fatalf("空热前缀不应被误认缺明细: %v", err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertLegacyOverviewFee(t, fixture, table, "model-a", 0.00078, 0, 1, 120)
	}
	var state entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&state).Error; err != nil || state.CursorsJSON == nil {
		t.Fatalf("读取空热前缀游标: %+v %v", state, err)
	}
	var progress struct {
		Overview *struct {
			HotAfterID     int64 `json:"hot_after_id"`
			ArchiveAfterID int64 `json:"archive_after_id"`
		} `json:"overview"`
	}
	if err := json.Unmarshal([]byte(*state.CursorsJSON), &progress); err != nil || progress.Overview == nil || progress.Overview.HotAfterID != 1 || progress.Overview.ArchiveAfterID != 1 {
		t.Fatalf("空热前缀未到达固定 C: %+v %v", progress, err)
	}
}

func TestLegacyPricingOverviewPageFailureResumesWithoutDoubleAdd(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	for _, statement := range []string{
		"UPDATE usage_events SET model = 'model-a', auth_index = 'hot-a', input_tokens = 90, output_tokens = 10, total_tokens = 100 WHERE id = 3",
		"INSERT INTO usage_events_archive (id,event_key,model,auth_index,timestamp,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,failed) VALUES (2,'cold-priced','model-a','hot-a','2026-09-01T10:02:00Z',50,10,0,0,60,0)",
		"UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id = 1003 WHERE name = 'overview'",
		"UPDATE usage_overview_hourly_stats SET request_count = 1003, success_count = 1003, input_tokens = 1240, output_tokens = 40, total_tokens = 1280 WHERE id = 1",
		"UPDATE usage_overview_daily_stats SET request_count = 1003, success_count = 1003, input_tokens = 1240, output_tokens = 40, total_tokens = 1280 WHERE id = 1",
		"UPDATE usage_events SET latency_ms = 2000, ttft_ms = 1000",
		"UPDATE usage_events_archive SET latency_ms = 4000, ttft_ms = 1000",
	} {
		if err := fixture.writer.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows := make([]map[string]any, 0, 1000)
	for id := int64(4); id <= 1003; id++ {
		rows = append(rows, map[string]any{
			"id": id, "event_key": "extra-priced", "api_group_key": "", "model": "model-a", "auth_index": "hot-a",
			"timestamp": "2026-09-01T10:02:00Z", "input_tokens": 1, "output_tokens": 1,
			"latency_ms": 2000, "ttft_ms": 1000,
			"cache_read_tokens": 0, "cache_creation_tokens": 0, "total_tokens": 2, "failed": false,
		})
	}
	if err := fixture.writer.Table("usage_events").CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	baseline, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.writer.Exec(`CREATE TRIGGER fail_second_overview_rebuild_page BEFORE UPDATE OF cost_usd ON usage_overview_hourly_stats WHEN OLD.cost_usd IS NOT NULL BEGIN SELECT RAISE(ABORT, 'forced second rebuild page failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteLegacyPricingData(ctx, fixture.writer, fixture.reader, baseline); err == nil {
		t.Fatal("第二页故障被误报成完成")
	}
	var failed entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&failed).Error; err != nil || failed.DataComplete || failed.CursorsJSON == nil {
		t.Fatalf("故障后的状态不符: %+v %v", failed, err)
	}
	var progress struct {
		Overview *struct {
			HotAfterID int64 `json:"hot_after_id"`
		} `json:"overview"`
	}
	if err := json.Unmarshal([]byte(*failed.CursorsJSON), &progress); err != nil || progress.Overview == nil || progress.Overview.HotAfterID != 1001 {
		t.Fatalf("失败页游标必须与金额一起回滚: %+v %v", progress, err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertOverviewSpeedTotals(t, fixture.reader, table, "model-a", 514, 1000, 1028, 1000)
	}
	if _, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, filepath.Join(t.TempDir(), "unused"), time.Now()); err != nil {
		t.Fatalf("M4 重入不得破坏 M5 游标: %v", err)
	}
	var afterM4 entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&afterM4).Error; err != nil || afterM4.CursorsJSON == nil || *afterM4.CursorsJSON != *failed.CursorsJSON {
		t.Fatalf("M4 重入覆写汇总游标: %+v %v", afterM4, err)
	}
	if err := fixture.writer.Exec("DROP TRIGGER fail_second_overview_rebuild_page").Error; err != nil {
		t.Fatal(err)
	}
	closePublishedPricingPools(fixture.reader, fixture.writer)
	reopened, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: fixture.path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(reader, reopened) })
	if err := repository.CompleteLegacyPricingData(ctx, reopened, reader, baseline); err != nil {
		t.Fatalf("重启续页: %v", err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		assertOverviewSpeedTotals(t, reader, table, "model-a", 517.5, 1003, 1030+10.0/3, 1003)
	}
	var cost sql.NullFloat64
	var unavailable sql.NullInt64
	if err := reopened.Table("usage_overview_hourly_stats").Select("cost_usd, unavailable_cost_count").Where("model = ?", "model-a").Row().Scan(&cost, &unavailable); err != nil || !cost.Valid || !unavailable.Valid {
		t.Fatalf("续跑费用未提交: %+v %+v %v", cost, unavailable, err)
	}
	var expected float64
	if err := reopened.Table("usage_events").Select("COALESCE(SUM(cost_usd), 0)").Scan(&expected).Error; err != nil {
		t.Fatal(err)
	}
	var cold float64
	if err := reopened.Table("usage_events_archive").Select("COALESCE(SUM(cost_usd), 0)").Scan(&cold).Error; err != nil {
		t.Fatal(err)
	}
	if math.Abs(cost.Float64-(expected+cold)) > 1e-9 || unavailable.Int64 != 0 {
		t.Fatalf("重启双加或漏加: cost=%g expected=%g cold=%g unavailable=%d", cost.Float64, expected, cold, unavailable.Int64)
	}
}

func TestLegacyPricingOverviewCorruptCostBlocksCompletion(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.writer.Exec("UPDATE usage_events SET cost_usd = NULL WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), fixture.writer, fixture.reader, baseline); err == nil {
		t.Fatal("invalid event cost was accepted")
	}
	var state entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&state).Error; err != nil || state.DataComplete {
		t.Fatalf("invalid completion: %+v %v", state, err)
	}
}

func TestLegacyPricingOverviewRejectsMissingRebuildCursor(t *testing.T) {
	fixture := openPublishedPricingOverviewFixture(t)
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var state entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	// 阶段被错误推进而未同时留下重建游标时，不得重清或猜测恢复位置。
	if err := fixture.writer.Model(&entities.PricingMigrationState{}).Where("id = ?", 1).Update("phase", "overview_rebuilding").Error; err != nil {
		t.Fatal(err)
	}
	err = repository.CompleteLegacyPricingData(context.Background(), fixture.writer, fixture.reader, baseline)
	if err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("缺少汇总游标却继续回填: %v", err)
	}
	if err := fixture.writer.Where("id = ?", 1).Take(&state).Error; err != nil || state.DataComplete {
		t.Fatalf("游标损坏后误标完成: %+v %v", state, err)
	}
}

func assertLegacyOverviewFee(t *testing.T, fixture publishedPricingEventFixture, table string, model string, wantCost float64, wantUnavailable, wantCount, wantTokens int64) {
	t.Helper()
	var cost sql.NullFloat64
	var unavailable sql.NullInt64
	var count, tokens int64
	if err := fixture.writer.Table(table).Select("cost_usd, unavailable_cost_count, request_count, total_tokens").Where("model = ?", model).Row().Scan(&cost, &unavailable, &count, &tokens); err != nil {
		t.Fatal(err)
	}
	if !cost.Valid || !unavailable.Valid || math.IsNaN(cost.Float64) || math.IsInf(cost.Float64, 0) || math.Abs(cost.Float64-wantCost) > 1e-12 || unavailable.Int64 != wantUnavailable || count != wantCount || tokens != wantTokens {
		t.Fatalf("%s/%s 重建结果不符: cost=%+v unavailable=%+v count=%d tokens=%d", table, model, cost, unavailable, count, tokens)
	}
}

// 原始时间可携带不同 offset；重建及 M6 必须使用同一项目时区，且保留原始事实。
func TestLegacyPricingOverviewRebuildTimezoneBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, zone, timestamp, day string }{
		{"utc_to_shanghai_next_day", "Asia/Shanghai", "2026-09-01T23:30:00Z", "2026-09-02"},
		{"offset_to_utc_previous_day", "UTC", "2026-09-02T00:30:00+08:00", "2026-09-01"},
		{"new_york_summer", "America/New_York", "2026-07-02T03:30:00Z", "2026-07-01"},
		{"new_york_winter", "America/New_York", "2026-01-02T04:30:00Z", "2026-01-01"},
		{"dst_fall_back", "America/New_York", "2026-11-01T06:30:00Z", "2026-11-01"},
		{"fractional_last_second", "Asia/Shanghai", "2026-09-01T15:59:59.999999999Z", "2026-09-01"},
		{"legacy_local", "Asia/Kathmandu", "2026-09-02 00:30:00", "2026-09-02"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			location, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			previous := time.Local
			time.Local = location
			t.Cleanup(func() { time.Local = previous })
			fixture := openPublishedPricingOverviewFixture(t)
			if err := fixture.writer.Exec("UPDATE usage_events SET timestamp = ? WHERE id = 1", tc.timestamp).Error; err != nil {
				t.Fatal(err)
			}
			if err := fixture.writer.Exec("INSERT INTO usage_events_archive (id,event_key,model,auth_index,timestamp,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,failed) VALUES (2,'cold-zone','model-a','hot-a',?,50,10,0,0,60,0)", tc.timestamp).Error; err != nil {
				t.Fatal(err)
			}
			if err := fixture.writer.Exec("UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id = 2 WHERE name = 'overview'").Error; err != nil {
				t.Fatal(err)
			}
			baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.CompleteLegacyPricingData(context.Background(), fixture.writer, fixture.reader, baseline); err != nil {
				t.Fatalf("timezone-consistent rebuild should complete: %v", err)
			}
			var daily entities.UsageOverviewDailyStat
			if err := fixture.reader.Take(&daily).Error; err != nil {
				t.Fatal(err)
			}
			if daily.BucketStart.In(location).Format("2006-01-02") != tc.day || daily.RequestCount != 2 {
				t.Fatalf("unexpected daily bucket: %+v", daily)
			}
			for _, table := range []string{"usage_events", "usage_events_archive"} {
				var timestamp string
				if err := fixture.reader.Table(table).Select("timestamp").Where("id <= 2").Scan(&timestamp).Error; err != nil || timestamp != tc.timestamp {
					t.Fatalf("%s original time changed: %q %v", table, timestamp, err)
				}
			}
		})
	}
}
