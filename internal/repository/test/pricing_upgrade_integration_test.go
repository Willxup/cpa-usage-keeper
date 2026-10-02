package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const (
	pricingUpgradeChildPathEnv    = "KEEPER_TEST_PRICING_UPGRADE_DB"
	pricingUpgradeChildBackupEnv  = "KEEPER_TEST_PRICING_UPGRADE_BACKUP_DIR"
	pricingUpgradeInterruptedCode = 73
)

// TestPricingUpgradeSubprocess 只供本文件父测试启动独立进程；退出发生在首个 M4 页已持久提交之后。
func TestPricingUpgradeSubprocess(t *testing.T) {
	path := os.Getenv(pricingUpgradeChildPathEnv)
	if path == "" {
		return
	}
	backupDir := os.Getenv(pricingUpgradeChildBackupEnv)
	writer, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer closePublishedPricingPools(reader, writer)
	if _, err := repository.BootstrapPricingInitialization(context.Background(), writer); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsurePricingBootstrapInbox(context.Background(), writer); err != nil {
		t.Fatal(err)
	}
	// 接收与备份、旧 migration 和 M4 共用同一 writer；每次 Insert 提交后才进入下一轮。
	ingestReady := make(chan struct{})
	go func() {
		for sequence := 0; ; sequence++ {
			if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), writer, "redis_pull:usage", []string{`{"same":"message"}`}, time.Now()); err != nil {
				fmt.Fprintf(os.Stderr, "continuous receive during upgrade: %v\n", err)
				os.Exit(72)
			}
			if sequence == 0 {
				close(ingestReady)
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()
	<-ingestReady
	if err := reader.Callback().Query().Before("gorm:query").Register("test:interrupt_pricing_upgrade_after_committed_page", func(tx *gorm.DB) {
		if tx.Statement.Table != "usage_events" {
			return
		}
		var state entities.PricingMigrationState
		if err := reader.Where("id = ?", 1).Take(&state).Error; err != nil || state.BackupPath == nil || state.CursorsJSON == nil {
			return
		}
		var cursors repository.PricingMigrationEventCursors
		if err := json.Unmarshal([]byte(*state.CursorsJSON), &cursors); err != nil || cursors.HotAfterID < 1000 {
			return
		}
		if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), writer, "redis_pull:usage", []string{`{"same":"message"}`, `{"same":"message"}`}, time.Now()); err != nil {
			fmt.Fprintf(os.Stderr, "receive during upgrade: %v\n", err)
			os.Exit(72)
		}
		os.Exit(pricingUpgradeInterruptedCode)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MigrateLegacyPricingEvents(context.Background(), writer, reader, backupDir, time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Fatal("migration completed before the subprocess could interrupt a committed page")
}

// TestPricingUpgradeResumesCommittedPageAcrossProcess 验证同一旧库在进程骤停后复用唯一备份和固定冷热边界。
func TestPricingUpgradeResumesCommittedPageAcrossProcess(t *testing.T) {
	fixture := openPublishedPricingEventFixture(t)
	seedPublishedPricingCoveredArchiveAndLongHotTail(t, fixture.writer)
	if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), fixture.writer, "redis_pull:usage", []string{`{"same":"message"}`, `{"same":"message"}`}, time.Now()); err != nil {
		t.Fatal(err)
	}
	closePublishedPricingPools(fixture.reader, fixture.writer)

	childCtx, cancelChild := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelChild()
	child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestPricingUpgradeSubprocess$")
	child.Env = append(os.Environ(), pricingUpgradeChildPathEnv+"="+fixture.path, pricingUpgradeChildBackupEnv+"="+fixture.backupDir)
	output, err := child.CombinedOutput()
	var exitError *exec.ExitError
	if childCtx.Err() != nil || !errors.As(err, &exitError) || exitError.ExitCode() != pricingUpgradeInterruptedCode {
		t.Fatalf("upgrade subprocess did not stop after its first committed page: error=%v output=%s", err, output)
	}

	writer, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: fixture.path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(reader, writer) })
	var state entities.PricingMigrationState
	if err := reader.Where("id = ?", 1).Take(&state).Error; err != nil {
		t.Fatal(err)
	}
	if state.BackupPath == nil || state.BaselineJSON == nil || state.CursorsJSON == nil || !state.SchemaComplete || state.DataComplete {
		t.Fatalf("abrupt interruption lost protected migration state: %+v", state)
	}
	var baseline repository.PricingLegacyBaseline
	if err := json.Unmarshal([]byte(*state.BaselineJSON), &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.InboxMaxID < 3 || baseline.Fixed == nil || baseline.Fixed.OverviewCursor != 2 || baseline.Fixed.HotMaxID != 3003 || baseline.Fixed.ArchiveMaxID != 2 {
		t.Fatalf("fixed C/H or backup inbox boundary changed: %+v", baseline)
	}
	var cursors repository.PricingMigrationEventCursors
	if err := json.Unmarshal([]byte(*state.CursorsJSON), &cursors); err != nil || cursors.HotAfterID < 1000 || cursors.HotAfterID >= baseline.Fixed.HotMaxID {
		t.Fatalf("subprocess did not leave a partial committed page: %+v, %v", cursors, err)
	}
	backup := openPricingUpgradeBackup(t, *state.BackupPath)
	backupInboxCount := assertPricingUpgradeInbox(t, backup, 3)
	if int64(backupInboxCount) != baseline.InboxMaxID {
		t.Fatalf("persistent M1 inbox boundary = %d, backup has %d", baseline.InboxMaxID, backupInboxCount)
	}
	liveInboxCount := assertPricingUpgradeInbox(t, writer, backupInboxCount+2)
	assertPricingUpgradeBackupFacts(t, backup)

	// 同一磁盘库重开后的标准入口必须跳过已提交页，继续固定旧价和冷热边界。
	resumed, err := repository.MigrateLegacyPricingEvents(context.Background(), writer, reader, filepath.Join(t.TempDir(), "must-not-replace-backup"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), writer, reader, resumed); err != nil {
		t.Fatal(err)
	}
	var completed entities.PricingMigrationState
	if err := reader.Where("id = ?", 1).Take(&completed).Error; err != nil || !completed.DataComplete || completed.BackupPath == nil || *completed.BackupPath != *state.BackupPath {
		t.Fatalf("restart replaced backup or did not complete: %+v, %v", completed, err)
	}
	assertStoredPricingEvent(t, writer, "usage_events", 1, 0.00078, true)
	assertStoredPricingEvent(t, writer, "usage_events", 3003, 0.000003, true)
	assertStoredPricingEvent(t, writer, "usage_events_archive", 2, 0.00021, true)
	assertPricingUpgradeOverview(t, writer)
	if got := assertPricingUpgradeInbox(t, writer, liveInboxCount); got != liveInboxCount {
		t.Fatalf("received inbox rows changed on migration restart: %d to %d", liveInboxCount, got)
	}

	// 一次成功路径从 M1 原备份另开文件完成；对比全部冷热事件费用，排除断点重复或遗漏。
	copyPath := filepath.Join(t.TempDir(), "one-shot.db")
	contents, err := os.ReadFile(*state.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, contents, 0600); err != nil {
		t.Fatal(err)
	}
	oneWriter, oneReader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: copyPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(oneReader, oneWriter) })
	oneBaseline, err := repository.MigrateLegacyPricingEvents(context.Background(), oneWriter, oneReader, filepath.Join(t.TempDir(), "one-shot-backup"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), oneWriter, oneReader, oneBaseline); err != nil {
		t.Fatal(err)
	}
	assertPricingUpgradeCostParity(t, reader, oneReader)
}

// TestPricingUpgradeProtectsBeforePublishedReplay 验证旧五维清表失败时，M1 文件仍保存原桶和原水位供重启恢复。
func TestPricingUpgradeProtectsBeforePublishedReplay(t *testing.T) {
	fixture := openPublishedPricingEventFixture(t)
	seedPricingUpgradePendingFiveDimensions(t, fixture.writer)
	if err := fixture.writer.Exec(`CREATE TRIGGER interrupt_published_overview_replay
		BEFORE INSERT ON usage_overview_hourly_stats BEGIN SELECT RAISE(ABORT, 'stop old replay'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MigrateLegacyPricingEvents(context.Background(), fixture.writer, fixture.reader, fixture.backupDir, time.Now()); err == nil {
		t.Fatal("old destructive replay unexpectedly completed")
	}
	var failed entities.PricingMigrationState
	if err := fixture.reader.Where("id = ?", 1).Take(&failed).Error; err != nil || failed.BackupPath == nil || failed.BaselineJSON == nil || failed.DataComplete {
		t.Fatalf("M1 protection did not persist before old replay failure: %+v, %v", failed, err)
	}
	backup := openPricingUpgradeBackup(t, *failed.BackupPath)
	var oldRequestCount, oldCursor int64
	if err := backup.Table("usage_overview_hourly_stats").Select("COALESCE(SUM(request_count), 0)").Scan(&oldRequestCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := backup.Table("usage_overview_aggregation_checkpoints").Select("last_aggregated_usage_event_id").Where("name = ?", "overview").Scan(&oldCursor).Error; err != nil {
		t.Fatal(err)
	}
	if oldRequestCount != 1 || oldCursor != 1 || backup.Migrator().HasColumn("usage_events", "cost_usd") {
		t.Fatalf("M1 backup was taken after old replay: requests=%d cursor=%d", oldRequestCount, oldCursor)
	}
	var liveRequestCount int64
	if err := fixture.reader.Table("usage_overview_hourly_stats").Count(&liveRequestCount).Error; err != nil || liveRequestCount != 0 {
		t.Fatalf("failure did not occur after old replay cleared its live table: count=%d err=%v", liveRequestCount, err)
	}
	if err := fixture.writer.Exec("DROP TRIGGER interrupt_published_overview_replay").Error; err != nil {
		t.Fatal(err)
	}
	closePublishedPricingPools(fixture.reader, fixture.writer)
	writer, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: fixture.path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(reader, writer) })
	baseline, err := repository.MigrateLegacyPricingEvents(context.Background(), writer, reader, filepath.Join(t.TempDir(), "do-not-replace"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CompleteLegacyPricingData(context.Background(), writer, reader, baseline); err != nil {
		t.Fatal(err)
	}
	var completed entities.PricingMigrationState
	if err := reader.Where("id = ?", 1).Take(&completed).Error; err != nil || !completed.DataComplete || completed.BackupPath == nil || *completed.BackupPath != *failed.BackupPath {
		t.Fatalf("replayed legacy migration replaced original backup or failed: %+v, %v", completed, err)
	}
}

// seedPricingUpgradePendingFiveDimensions 恢复五维旧发布物理结构和旧 checkpoint，保持已有统计非空。
func seedPricingUpgradePendingFiveDimensions(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		"DROP TABLE usage_events_archive",
		"DROP TABLE usage_aggregation_checkpoints",
		"CREATE TABLE cpa_api_keys (id INTEGER PRIMARY KEY)",
		"CREATE TABLE usage_identities (id INTEGER PRIMARY KEY)",
		`CREATE TABLE usage_overview_aggregation_checkpoints (
			id INTEGER PRIMARY KEY, name TEXT NOT NULL, last_aggregated_usage_event_id INTEGER NOT NULL,
			stats_updated_at TEXT, created_at TEXT, updated_at TEXT)`,
		"INSERT INTO usage_overview_aggregation_checkpoints (id,name,last_aggregated_usage_event_id) VALUES (1,'overview',1)",
		"DELETE FROM schema_migrations WHERE version >= '20260723_usage_overview_five_dimensions'",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("restore old five-dimension physical schema: %v", err)
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		for _, column := range []string{"service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type"} {
			if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN " + column).Error; err != nil {
				t.Fatalf("remove future %s.%s: %v", table, column, err)
			}
		}
	}
}

// seedPublishedPricingCoveredArchiveAndLongHotTail 保持 C=2 的冷热旧统计，后续大于 C 的热事件只做明细回填。
func seedPublishedPricingCoveredArchiveAndLongHotTail(t *testing.T, db *gorm.DB) {
	t.Helper()
	hotTime := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC).In(time.Local)
	coldTime := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC).In(time.Local)
	for _, item := range []struct {
		table string
		stamp time.Time
	}{
		{"usage_overview_hourly_stats", hotTime.Truncate(time.Hour)},
		{"usage_overview_daily_stats", time.Date(hotTime.Year(), hotTime.Month(), hotTime.Day(), 0, 0, 0, 0, time.Local)},
	} {
		if err := db.Table(item.table).Where("id = ?", 1).Updates(map[string]any{"api_group_key": "unknown", "bucket_start": timeutil.FormatStorageTime(item.stamp)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		table string
		stamp time.Time
	}{
		{"usage_overview_hourly_stats", coldTime.Truncate(time.Hour)},
		{"usage_overview_daily_stats", time.Date(coldTime.Year(), coldTime.Month(), coldTime.Day(), 0, 0, 0, 0, time.Local)},
	} {
		if err := db.Table(item.table).Create(map[string]any{
			"id": 2, "bucket_start": timeutil.FormatStorageTime(item.stamp), "api_group_key": "unknown",
			"model": "model-a", "auth_index": "", "model_alias": "", "service_tier": "",
			"response_service_tier": "", "reasoning_effort": "", "endpoint": "", "executor_type": "",
			"request_count": 1, "success_count": 1, "failure_count": 0, "input_tokens": 50,
			"output_tokens": 10, "reasoning_tokens": 0, "cached_tokens": 0,
			"cache_read_tokens": 0, "cache_creation_tokens": 0, "total_tokens": 60,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("usage_aggregation_checkpoints").Where("name = ?", "overview").Update("last_aggregated_usage_event_id", 2).Error; err != nil {
		t.Fatal(err)
	}
	rows := make([]map[string]any, 0, 3000)
	for id := int64(4); id <= 3003; id++ {
		rows = append(rows, map[string]any{"id": id, "event_key": fmt.Sprintf("tail-%d", id), "model": "model-a",
			"timestamp": "2026-09-01T10:02:00Z", "input_tokens": 1, "total_tokens": 1, "failed": false})
	}
	if err := db.Table("usage_events").CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
}

// openPricingUpgradeBackup 以硬只读句柄检查 M1 文件，而不是从已变化的运行库推断备份内容。
func openPricingUpgradeBackup(t *testing.T, path string) *gorm.DB {
	t.Helper()
	backup, err := repository.OpenReadDatabase(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closePublishedPricingPools(backup, backup) })
	return backup
}

// assertPricingUpgradeBackupFacts 检查备份仍保留旧统计、水位及未增费列。
func assertPricingUpgradeBackupFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		if db.Migrator().HasColumn(table, "cost_usd") {
			t.Fatalf("M1 backup of %s was overwritten after schema migration", table)
		}
	}
	var cursor int64
	if err := db.Table("usage_aggregation_checkpoints").Select("last_aggregated_usage_event_id").Where("name = ?", "overview").Scan(&cursor).Error; err != nil || cursor != 2 {
		t.Fatalf("M1 backup overview cursor = %d, %v", cursor, err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var rowCount, requestCount int64
		if err := db.Table(table).Count(&rowCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table(table).Select("COALESCE(SUM(request_count), 0)").Scan(&requestCount).Error; err != nil || rowCount != 2 || requestCount != 2 {
			t.Fatalf("M1 backup %s rows/requests = %d/%d, %v", table, rowCount, requestCount, err)
		}
	}
}

// assertPricingUpgradeInbox 确认相同原文保留不同接收行，备份后新增消息没有被重启吞掉。
func assertPricingUpgradeInbox(t *testing.T, db *gorm.DB, minimum int) int {
	t.Helper()
	var rows []struct {
		ID         int64
		RawMessage string
		Status     string
	}
	if err := db.Table("redis_usage_inboxes").Select("id, raw_message, status").Order("id").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) < minimum {
		t.Fatalf("inbox row count = %d, want at least %d", len(rows), minimum)
	}
	for index, row := range rows {
		if row.ID != int64(index+1) || row.RawMessage != `{"same":"message"}` || row.Status != "pending" {
			t.Fatalf("inbox row %d changed across upgrade: %+v", index, row)
		}
	}
	return len(rows)
}

// assertPricingUpgradeOverview 保证 C 内冷热已计价，C 后大批热事件仍不重复计入旧统计。
func assertPricingUpgradeOverview(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var rows []struct {
			RequestCount         int64
			CostUSD              *float64
			UnavailableCostCount *int64
		}
		if err := db.Table(table).Select("request_count, cost_usd, unavailable_cost_count").Order("id").Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].CostUSD == nil || rows[1].CostUSD == nil || rows[0].UnavailableCostCount == nil || rows[1].UnavailableCostCount == nil {
			t.Fatalf("incomplete %s costs: %+v", table, rows)
		}
		if rows[0].RequestCount != 1 || rows[1].RequestCount != 1 || *rows[0].UnavailableCostCount != 0 || *rows[1].UnavailableCostCount != 0 ||
			!pricingUpgradeClose(*rows[0].CostUSD, 0.00078) || !pricingUpgradeClose(*rows[1].CostUSD, 0.00021) {
			t.Fatalf("%s C-covered rows changed: %+v", table, rows)
		}
	}
}

type pricingUpgradeEventCost struct {
	ID            int64
	CostUSD       *float64
	CostAvailable *bool
}

// assertPricingUpgradeCostParity 比较断点恢复与一次成功对全部冷热事件的持久费用结果。
func assertPricingUpgradeCostParity(t *testing.T, resumed, oneShot *gorm.DB) {
	t.Helper()
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		load := func(db *gorm.DB) []pricingUpgradeEventCost {
			var rows []pricingUpgradeEventCost
			if err := db.Table(table).Select("id, cost_usd, cost_available").Order("id").Scan(&rows).Error; err != nil {
				t.Fatal(err)
			}
			return rows
		}
		actual, want := load(resumed), load(oneShot)
		if len(actual) != len(want) {
			t.Fatalf("%s resumed/one-shot row count = %d/%d", table, len(actual), len(want))
		}
		for index := range actual {
			left, right := actual[index], want[index]
			if left.ID != right.ID || left.CostUSD == nil || right.CostUSD == nil || left.CostAvailable == nil || right.CostAvailable == nil ||
				*left.CostAvailable != *right.CostAvailable || !pricingUpgradeClose(*left.CostUSD, *right.CostUSD) {
				t.Fatalf("%s resumed cost differs from one-shot at %d: %+v vs %+v", table, index, left, right)
			}
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var actual, want []struct {
			CostUSD              *float64
			UnavailableCostCount *int64
		}
		if err := resumed.Table(table).Select("cost_usd, unavailable_cost_count").Order("id").Scan(&actual).Error; err != nil {
			t.Fatal(err)
		}
		if err := oneShot.Table(table).Select("cost_usd, unavailable_cost_count").Order("id").Scan(&want).Error; err != nil {
			t.Fatal(err)
		}
		if len(actual) != len(want) {
			t.Fatalf("%s resumed/one-shot overview rows = %d/%d", table, len(actual), len(want))
		}
		for index := range actual {
			left, right := actual[index], want[index]
			if left.CostUSD == nil || right.CostUSD == nil || left.UnavailableCostCount == nil || right.UnavailableCostCount == nil ||
				*left.UnavailableCostCount != *right.UnavailableCostCount || !pricingUpgradeClose(*left.CostUSD, *right.CostUSD) {
				t.Fatalf("%s resumed overview cost differs from one-shot at %d: %+v vs %+v", table, index, left, right)
			}
		}
	}
}

func pricingUpgradeClose(left, right float64) bool {
	return !math.IsNaN(left) && !math.IsNaN(right) && !math.IsInf(left, 0) && !math.IsInf(right, 0) && math.Abs(left-right) <= 1e-9
}
