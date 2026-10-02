package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/gorm"
)

type publishedPricingEventFixture struct {
	writer    *gorm.DB
	reader    *gorm.DB
	path      string
	backupDir string
}

// 新费用列由旧物理表上的结构迁移添加；fixture 直接建费用改版前的业务列，不依赖新实体 AutoMigrate。
func openPublishedPricingEventFixture(t *testing.T) publishedPricingEventFixture {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "published.db")
	writer, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(reader, writer) })
	for _, statement := range []string{
		`CREATE TABLE usage_events (
			id INTEGER PRIMARY KEY, event_key TEXT, api_group_key TEXT, provider TEXT, endpoint TEXT, auth_type TEXT,
			request_id TEXT, session_id TEXT, parent_session_id TEXT, client_ip TEXT, x_forwarded_for TEXT,
			user_agent TEXT, model TEXT, model_alias TEXT, response_model TEXT, reasoning_effort TEXT,
			service_tier TEXT, response_service_tier TEXT, executor_type TEXT, timestamp TEXT, source TEXT,
			auth_index TEXT, failed BOOLEAN, status_code INTEGER, generate BOOLEAN, stream BOOLEAN,
			latency_ms INTEGER, ttft_ms INTEGER, input_tokens INTEGER, output_tokens INTEGER,
			reasoning_tokens INTEGER, cached_tokens INTEGER, cache_read_tokens INTEGER,
			cache_creation_tokens INTEGER, total_tokens INTEGER, created_at TEXT
		)`,
		`CREATE TABLE usage_events_archive (
			id INTEGER PRIMARY KEY, event_key TEXT, api_group_key TEXT, provider TEXT, endpoint TEXT, auth_type TEXT,
			request_id TEXT, session_id TEXT, parent_session_id TEXT, client_ip TEXT, x_forwarded_for TEXT,
			user_agent TEXT, model TEXT, model_alias TEXT, response_model TEXT, reasoning_effort TEXT,
			service_tier TEXT, response_service_tier TEXT, executor_type TEXT, timestamp TEXT, source TEXT,
			auth_index TEXT, failed BOOLEAN, status_code INTEGER, generate BOOLEAN, stream BOOLEAN,
			latency_ms INTEGER, ttft_ms INTEGER, input_tokens INTEGER, output_tokens INTEGER,
			reasoning_tokens INTEGER, cached_tokens INTEGER, cache_read_tokens INTEGER,
			cache_creation_tokens INTEGER, total_tokens INTEGER, created_at TEXT, archived_at TEXT
		)`,
		`CREATE TABLE usage_overview_hourly_stats (
			id INTEGER PRIMARY KEY, bucket_start TEXT, api_group_key TEXT, model TEXT, auth_index TEXT,
			model_alias TEXT, service_tier TEXT, response_service_tier TEXT, reasoning_effort TEXT,
			endpoint TEXT, executor_type TEXT, request_count INTEGER, success_count INTEGER,
			failure_count INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER,
			cached_tokens INTEGER, cache_read_tokens INTEGER, cache_creation_tokens INTEGER,
			total_tokens INTEGER, created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE usage_overview_daily_stats (
			id INTEGER PRIMARY KEY, bucket_start TEXT, api_group_key TEXT, model TEXT, auth_index TEXT,
			model_alias TEXT, service_tier TEXT, response_service_tier TEXT, reasoning_effort TEXT,
			endpoint TEXT, executor_type TEXT, request_count INTEGER, success_count INTEGER,
			failure_count INTEGER, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER,
			cached_tokens INTEGER, cache_read_tokens INTEGER, cache_creation_tokens INTEGER,
			total_tokens INTEGER, created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE usage_aggregation_checkpoints (name TEXT PRIMARY KEY, last_aggregated_usage_event_id INTEGER, created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE model_price_settings (
			id INTEGER PRIMARY KEY, model TEXT, pricing_style TEXT, prompt_price_per1_m REAL,
			completion_price_per1_m REAL, cache_read_price_per1_m REAL,
			cache_creation_price_per1_m REAL, price_multiplier REAL, created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE model_price_rules (
			id INTEGER PRIMARY KEY, model_price_setting_id INTEGER, key TEXT, value TEXT,
			multiplier REAL, created_at TEXT, updated_at TEXT
		)`,
		`INSERT INTO usage_events (id,event_key,model,auth_index,timestamp,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,failed)
			VALUES (1,'hot-priced','model-a','hot-a','2026-09-01T10:00:00Z',100,20,10,5,120,0),
			       (3,'hot-missing','missing-model','','2026-09-01T10:01:00Z',90,10,0,0,100,0)`,
		`INSERT INTO usage_events_archive (id,event_key,model,auth_index,timestamp,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,failed)
			VALUES (2,'cold-priced','model-a','','2026-08-01T10:00:00Z',50,10,0,0,60,0)`,
		`INSERT INTO model_price_settings (id,model,pricing_style,prompt_price_per1_m,completion_price_per1_m,cache_read_price_per1_m,cache_creation_price_per1_m,price_multiplier)
			VALUES (1,'model-a','openai',2,4,0.5,1,1.5)`,
		`INSERT INTO model_price_rules (id,model_price_setting_id,key,value,multiplier) VALUES (1,1,'auth_index','hot-a',2)`,
		`INSERT INTO usage_aggregation_checkpoints (name,last_aggregated_usage_event_id) VALUES ('overview',1)`,
		`INSERT INTO usage_overview_hourly_stats
			(id,bucket_start,api_group_key,model,auth_index,model_alias,service_tier,response_service_tier,
			reasoning_effort,endpoint,executor_type,request_count,success_count,failure_count,input_tokens,
			output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens)
			VALUES (1,'2026-09-01T10:00:00Z','','model-a','hot-a','','','','','','',1,1,0,100,20,0,0,10,5,120)`,
		`INSERT INTO usage_overview_daily_stats
			(id,bucket_start,api_group_key,model,auth_index,model_alias,service_tier,response_service_tier,
			reasoning_effort,endpoint,executor_type,request_count,success_count,failure_count,input_tokens,
			output_tokens,reasoning_tokens,cached_tokens,cache_read_tokens,cache_creation_tokens,total_tokens)
			VALUES (1,'2026-09-01T00:00:00Z','','model-a','hot-a','','','','','','',1,1,0,100,20,0,0,10,5,120)`,
	} {
		if err := writer.Exec(statement).Error; err != nil {
			t.Fatalf("建立已发布旧物理表：%v", err)
		}
	}
	if err := migration.MarkAllAsApplied(writer); err != nil {
		t.Fatalf("记录已发布版本：%v", err)
	}
	if err := writer.Exec("DELETE FROM schema_migrations WHERE version = ?", "20261002_pricing_storage_structure").Error; err != nil {
		t.Fatal(err)
	}
	state, err := repository.BootstrapPricingInitialization(context.Background(), writer)
	if err != nil || state.InitKind != repository.PricingInitKindLegacy {
		t.Fatalf("固定旧库身份：state=%+v err=%v", state, err)
	}
	if err := repository.EnsurePricingBootstrapInbox(context.Background(), writer); err != nil {
		t.Fatalf("建立持久接收表：%v", err)
	}
	return publishedPricingEventFixture{writer: writer, reader: reader, path: path, backupDir: filepath.Join(root, "backups")}
}

func closePublishedPricingPools(reader, writer *gorm.DB) {
	if reader != nil && reader != writer {
		if sqlDB, err := reader.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	if writer != nil {
		if sqlDB, err := writer.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}

func TestPublishedPricingEventFixtureHasOldPhysicalColumns(t *testing.T) {
	fixture := openPublishedPricingEventFixture(t)
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		for _, column := range []string{"cost_usd", "cost_available"} {
			if fixture.writer.Migrator().HasColumn(table, column) {
				t.Fatalf("旧物理表提前有 %s.%s", table, column)
			}
		}
	}
	if fixture.writer.Migrator().HasColumn("model_price_settings", "branches_json") {
		t.Fatal("旧价格表提前有 branches_json")
	}
}

func TestLegacyPricingEventsBackfillHotColdWithFixedPublishedPrice(t *testing.T) {
	fixture := openPublishedPricingEventFixture(t)
	ctx := context.Background()
	baseline, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, fixture.backupDir, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("首次冷热明细回填：%v", err)
	}
	if baseline.Fixed == nil || baseline.Fixed.OverviewCursor != 1 || baseline.Fixed.HotMaxID != 3 || baseline.Fixed.ArchiveMaxID != 2 || len(baseline.Fixed.Configs) != 1 {
		t.Fatalf("M3 未固定旧迁移后的 C/H/等价价：%+v", baseline.Fixed)
	}
	assertStoredPricingEvent(t, fixture.writer, "usage_events", 1, 0.00078, true)
	assertStoredPricingEvent(t, fixture.writer, "usage_events", 3, 0, false)
	assertStoredPricingEvent(t, fixture.writer, "usage_events_archive", 2, 0.00021, true)
	var raw struct {
		InputTokens int64
		AuthIndex   string
		Timestamp   string
	}
	if err := fixture.writer.Table("usage_events").Select("input_tokens, auth_index, timestamp").Where("id = 1").Scan(&raw).Error; err != nil || raw.InputTokens != 100 || raw.AuthIndex != "hot-a" || raw.Timestamp != "2026-09-01T10:00:00Z" {
		t.Fatalf("M4 改动了旧原始事实：row=%+v err=%v", raw, err)
	}
	var state entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	if !state.SchemaComplete || state.DataComplete || state.BackupPath == nil || state.CursorsJSON == nil || state.Phase != "events_backfilled" {
		t.Fatalf("M4 阶段标记不实：%+v", state)
	}
	var cursors repository.PricingMigrationEventCursors
	if err := json.Unmarshal([]byte(*state.CursorsJSON), &cursors); err != nil || cursors.SchemaVersion != 1 || cursors.HotAfterID != 3 || cursors.ArchiveAfterID != 2 {
		t.Fatalf("冷热提交水位错误：%+v err=%v", cursors, err)
	}
	if _, err := repository.ProtectPricingLegacyMigration(ctx, fixture.writer, fixture.reader, filepath.Join(t.TempDir(), "different-backups"), time.Now()); err != nil {
		t.Fatalf("M3 扩展后原 M1 备份仍应可复验：%v", err)
	}
}

func TestLegacyPricingEventsBackfillWithoutPriceKeepsExplicitUnavailableZero(t *testing.T) {
	fixture := openPublishedPricingEventFixture(t)
	for _, table := range []string{"model_price_rules", "model_price_settings"} {
		if err := fixture.writer.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now())
	if err != nil || baseline.Fixed == nil || len(baseline.Fixed.Configs) != 0 {
		t.Fatalf("空旧价表仍应完成明细回填：fixed=%+v err=%v", baseline.Fixed, err)
	}
	for _, event := range []struct {
		table string
		id    int64
	}{{"usage_events", 1}, {"usage_events", 3}, {"usage_events_archive", 2}} {
		assertStoredPricingEvent(t, fixture.writer, event.table, event.id, 0, false)
	}
}

func TestLegacyPricingEventsBackfillResumesAfterSecondPageFailure(t *testing.T) {
	fixture := openPublishedPricingEventFixture(t)
	// 初始热表已有 ID 1、3；再加 1000 条，保证第二页先更新一行再故障回滚。
	rows := make([]map[string]any, 0, 1000)
	for id := int64(4); id <= 1003; id++ {
		rows = append(rows, map[string]any{
			"id": id, "event_key": "extra-priced", "model": "model-a", "timestamp": "2026-09-01T10:02:00Z",
			"input_tokens": 1, "output_tokens": 0, "cache_read_tokens": 0, "cache_creation_tokens": 0,
			"total_tokens": 1, "failed": false,
		})
	}
	if err := fixture.writer.Table("usage_events").CreateInBatches(rows, 50).Error; err != nil {
		t.Fatalf("准备跨页热表：%v", err)
	}
	if err := fixture.writer.Exec(`CREATE TRIGGER fail_second_pricing_page BEFORE UPDATE ON usage_events
		WHEN OLD.id = 1003 BEGIN SELECT RAISE(ABORT, 'forced second page failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, fixture.backupDir, time.Now()); err == nil {
		t.Fatal("第二页故障被误报成回填成功")
	}
	var failedState entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&failedState).Error; err != nil {
		t.Fatal(err)
	}
	if !failedState.SchemaComplete || failedState.DataComplete || failedState.BaselineJSON == nil || failedState.CursorsJSON == nil {
		t.Fatalf("故障后丢失已提交阶段或误标完成：%+v", failedState)
	}
	var failedCursors repository.PricingMigrationEventCursors
	if err := json.Unmarshal([]byte(*failedState.CursorsJSON), &failedCursors); err != nil || failedCursors.HotAfterID != 1001 || failedCursors.ArchiveAfterID != 0 {
		t.Fatalf("故障页金额与水位没有共同回滚：%+v err=%v", failedCursors, err)
	}
	assertStoredPricingEvent(t, fixture.writer, "usage_events", 1, 0.00078, true)
	assertNullPricingEvent(t, fixture.writer, "usage_events", 1002)
	assertNullPricingEvent(t, fixture.writer, "usage_events", 1003)
	assertNullPricingEvent(t, fixture.writer, "usage_events_archive", 2)
	if err := fixture.writer.Exec("DROP TRIGGER fail_second_pricing_page").Error; err != nil {
		t.Fatal(err)
	}
	// 固定基线后即使现库配置改变，重启也不能让剩余页和归档使用另一套价格。
	if err := fixture.writer.Exec("UPDATE model_price_settings SET prompt_price_per1_m = 200, price_multiplier = 10").Error; err != nil {
		t.Fatal(err)
	}
	// 关闭全部旧连接再打开同一文件，证明不靠内存价格/游标继续；备份路径保持 M1 唯一值。
	oldBackupPath := *failedState.BackupPath
	closePublishedPricingPools(fixture.reader, fixture.writer)
	reopened, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: fixture.path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(reader, reopened) })
	resumed, err := repository.MigrateLegacyPricingEvents(ctx, reopened, reader, filepath.Join(t.TempDir(), "not-used"), time.Now())
	if err != nil || resumed.Fixed == nil || resumed.Fixed.HotMaxID != 1003 {
		t.Fatalf("重启未沿固定基线继续：baseline=%+v err=%v", resumed.Fixed, err)
	}
	var completedState entities.PricingMigrationState
	if err := reopened.Where("id = ?", 1).Take(&completedState).Error; err != nil || completedState.BackupPath == nil || *completedState.BackupPath != oldBackupPath || !completedState.SchemaComplete || completedState.DataComplete {
		t.Fatalf("重启覆盖旧备份或误标数据完成：state=%+v err=%v", completedState, err)
	}
	assertStoredPricingEvent(t, reopened, "usage_events", 1002, 0.000003, true)
	assertStoredPricingEvent(t, reopened, "usage_events", 1, 0.00078, true)
	assertStoredPricingEvent(t, reopened, "usage_events_archive", 2, 0.00021, true)
}

func TestLegacyPricingMigrationRepairsSchemaFlagAfterVersionCommit(t *testing.T) {
	fixture := openPublishedPricingEventFixture(t)
	if err := fixture.writer.Exec(`CREATE TRIGGER fail_schema_completion
		BEFORE UPDATE OF schema_complete ON pricing_migration_state
		WHEN NEW.schema_complete = 1 BEGIN SELECT RAISE(ABORT, 'forced schema flag failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, fixture.backupDir, time.Now()); err == nil {
		t.Fatal("结构标志写入失败被误报成完成")
	}
	var interrupted entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&interrupted).Error; err != nil {
		t.Fatal(err)
	}
	if interrupted.SchemaComplete || interrupted.DataComplete || interrupted.BaselineJSON == nil || interrupted.CursorsJSON == nil {
		t.Fatalf("M3/结构版本与标志故障边界错误：%+v", interrupted)
	}
	var versionCount int64
	if err := fixture.writer.Table("schema_migrations").Where("version = ?", "20261002_pricing_storage_structure").Count(&versionCount).Error; err != nil || versionCount != 1 {
		t.Fatalf("结构版本未先独立提交：count=%d err=%v", versionCount, err)
	}
	assertNullPricingEvent(t, fixture.writer, "usage_events", 1)
	if err := fixture.writer.Exec("DROP TRIGGER fail_schema_completion").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, filepath.Join(t.TempDir(), "not-used"), time.Now()); err != nil {
		t.Fatalf("重启应从结构版本修复 schema_complete 并继续 M4：%v", err)
	}
	var resumed entities.PricingMigrationState
	if err := fixture.writer.Where("id = ?", 1).Take(&resumed).Error; err != nil || !resumed.SchemaComplete || resumed.DataComplete || resumed.Phase != "events_backfilled" {
		t.Fatalf("补标或阶段恢复错误：state=%+v err=%v", resumed, err)
	}
	assertStoredPricingEvent(t, fixture.writer, "usage_events", 1, 0.00078, true)
	// 后续 CMT14 已开始的阶段不能被重复 M4 降级成 events_backfilled。
	if err := fixture.writer.Model(&entities.PricingMigrationState{}).Where("id = ?", 1).Update("phase", "overview_backfilling").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, filepath.Join(t.TempDir(), "not-used-again"), time.Now()); err != nil {
		t.Fatalf("后续阶段重复入口：%v", err)
	}
	if err := fixture.writer.Where("id = ?", 1).Take(&resumed).Error; err != nil || resumed.Phase != "overview_backfilling" {
		t.Fatalf("重复 M4 倒退后续阶段：state=%+v err=%v", resumed, err)
	}
}

func assertStoredPricingEvent(t *testing.T, db *gorm.DB, table string, id int64, want float64, available bool) {
	t.Helper()
	var cost sql.NullFloat64
	var state sql.NullBool
	if err := db.Table(table).Select("cost_usd, cost_available").Where("id = ?", id).Row().Scan(&cost, &state); err != nil {
		t.Fatalf("读取 %s/%d 费用：%v", table, id, err)
	}
	if !cost.Valid || !state.Valid || math.IsNaN(cost.Float64) || math.IsInf(cost.Float64, 0) || math.Abs(cost.Float64-want) > 1e-12 || state.Bool != available {
		t.Fatalf("%s/%d 费用=%+v 可用=%+v，期望 %g/%t", table, id, cost, state, want, available)
	}
}

func assertNullPricingEvent(t *testing.T, db *gorm.DB, table string, id int64) {
	t.Helper()
	var cost sql.NullFloat64
	var available sql.NullBool
	if err := db.Table(table).Select("cost_usd, cost_available").Where("id = ?", id).Row().Scan(&cost, &available); err != nil || cost.Valid || available.Valid {
		t.Fatalf("%s/%d 未提交页必须仍为 NULL：cost=%+v available=%+v err=%v", table, id, cost, available, err)
	}
}
