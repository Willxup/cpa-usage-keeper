package test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

func TestAggregateUsageLatencyStatsKeepsHoursForThreeDaysAndDaysForThreeHundredSixtyFiveDays(t *testing.T) {
	// 小时只服务最长 24 小时窗口并保留三天余量；自然日继续覆盖页面允许的最近 365 天。
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.Local)
	hourCutoff := now.Add(-3 * 24 * time.Hour).Truncate(time.Hour)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	events := []entities.UsageEvent{
		validLatencyEvent(1, "recent", now.Add(-time.Hour), 100, 900),
		validLatencyEvent(2, "hour-boundary", hourCutoff.Add(15*time.Minute), 200, 1200),
		validLatencyEvent(3, "day-only-before-hour", hourCutoff.Add(-45*time.Minute), 300, 1500),
		validLatencyEvent(4, "day-boundary", today.AddDate(0, 0, -364).Add(12*time.Hour), 400, 1800),
		validLatencyEvent(5, "expired-day", today.AddDate(0, 0, -365).Add(12*time.Hour), 500, 2100),
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed latency retention events: %v", err)
	}

	if err := repository.AggregateUsageLatencyStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate latency retention events: %v", err)
	}
	var rows []entities.UsageLatencyStat
	if err := db.Order("api_group_key asc, bucket_type asc").Find(&rows).Error; err != nil {
		t.Fatalf("load retained latency rows: %v", err)
	}
	got := make(map[string][]entities.UsageLatencyBucketType)
	for _, row := range rows {
		got[row.APIGroupKey] = append(got[row.APIGroupKey], row.BucketType)
	}
	if len(got["recent"]) != 2 || len(got["hour-boundary"]) != 2 || len(got["day-only-before-hour"]) != 1 || got["day-only-before-hour"][0] != entities.UsageLatencyBucketDay || len(got["day-boundary"]) != 1 || got["day-boundary"][0] != entities.UsageLatencyBucketDay {
		t.Fatalf("unexpected retained hour/day rows: %+v", got)
	}
	if _, exists := got["expired-day"]; exists {
		t.Fatalf("expected event before the 365-day boundary to advance the cursor without recreating expired rows: %+v", got)
	}
	assertLatencyAggregationCheckpoint(t, db, 5)
}

func TestCleanupStorageRemovesExpiredLatencyHourAndDayRows(t *testing.T) {
	// 每日维护保留原有 Latency retention，并由统一条件式 VACUUM 评估空闲页。
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 30, 0, 0, time.Local)
	templates := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(1, "template", now, 100, 900)})
	templateByType := make(map[entities.UsageLatencyBucketType]entities.UsageLatencyStat, len(templates))
	for _, row := range templates {
		templateByType[row.BucketType] = row
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	rows := []entities.UsageLatencyStat{
		latencyRetentionRow(templateByType[entities.UsageLatencyBucketHour], "expired-hour", now.Add(-4*24*time.Hour).Truncate(time.Hour), now),
		latencyRetentionRow(templateByType[entities.UsageLatencyBucketHour], "kept-hour", now.Add(-2*24*time.Hour).Truncate(time.Hour), now),
		latencyRetentionRow(templateByType[entities.UsageLatencyBucketDay], "expired-day", today.AddDate(0, 0, -365), now),
		latencyRetentionRow(templateByType[entities.UsageLatencyBucketDay], "kept-day", today.AddDate(0, 0, -364), now),
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed latency retention rows: %v", err)
	}

	if _, err := repository.CleanupStorage(db, now); err != nil {
		t.Fatalf("cleanup latency retention rows: %v", err)
	}
	var keys []string
	if err := db.Model(&entities.UsageLatencyStat{}).Order("api_group_key asc").Pluck("api_group_key", &keys).Error; err != nil {
		t.Fatalf("load latency rows after cleanup: %v", err)
	}
	if len(keys) != 2 || keys[0] != "kept-day" || keys[1] != "kept-hour" {
		t.Fatalf("expected only in-retention latency rows, got %v", keys)
	}
}

func TestCleanupUsageLatencyStatsUsesStoredTimezoneAtExactRetentionBoundary(t *testing.T) {
	// Latency bucket_start 使用项目本地 RFC3339 字符串，清理参数必须使用同一格式，不能混入 UTC 文本比较。
	for _, zoneName := range []string{"America/New_York", "Asia/Shanghai"} {
		t.Run(zoneName, func(t *testing.T) {
			location, err := time.LoadLocation(zoneName)
			if err != nil {
				t.Fatalf("load timezone %s: %v", zoneName, err)
			}
			previousLocal := time.Local
			time.Local = location
			t.Cleanup(func() { time.Local = previousLocal })

			db := openTestDatabase(t)
			now := time.Date(2026, 7, 26, 12, 30, 0, 0, location)
			templates := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(1, "template", now, 100, 900)})
			templateByType := make(map[entities.UsageLatencyBucketType]entities.UsageLatencyStat, len(templates))
			for _, row := range templates {
				templateByType[row.BucketType] = row
			}

			// 这些边界由固定墙钟时间手工给出，避免用被测 retention helper 反推期望值。
			hourCutoff := time.Date(2026, 7, 23, 12, 0, 0, 0, location)
			dayCutoff := time.Date(2025, 7, 27, 0, 0, 0, 0, location)
			rows := []entities.UsageLatencyStat{
				latencyRetentionRow(templateByType[entities.UsageLatencyBucketHour], "expired-hour", hourCutoff.Add(-time.Hour), now),
				latencyRetentionRow(templateByType[entities.UsageLatencyBucketHour], "boundary-hour", hourCutoff, now),
				latencyRetentionRow(templateByType[entities.UsageLatencyBucketHour], "recent-hour", hourCutoff.Add(time.Hour), now),
				latencyRetentionRow(templateByType[entities.UsageLatencyBucketDay], "expired-day", dayCutoff.AddDate(0, 0, -1), now),
				latencyRetentionRow(templateByType[entities.UsageLatencyBucketDay], "boundary-day", dayCutoff, now),
				latencyRetentionRow(templateByType[entities.UsageLatencyBucketDay], "recent-day", dayCutoff.AddDate(0, 0, 1), now),
			}
			if err := db.Create(&rows).Error; err != nil {
				t.Fatalf("seed exact latency retention boundaries: %v", err)
			}

			if err := repository.CleanupUsageLatencyStats(db, now); err != nil {
				t.Fatalf("cleanup exact latency retention boundaries: %v", err)
			}
			var keys []string
			if err := db.Model(&entities.UsageLatencyStat{}).Order("api_group_key asc").Pluck("api_group_key", &keys).Error; err != nil {
				t.Fatalf("load exact latency retention rows: %v", err)
			}
			want := []string{"boundary-day", "boundary-hour", "recent-day", "recent-hour"}
			if len(keys) != len(want) {
				t.Fatalf("retained latency keys=%v, want %v", keys, want)
			}
			for index := range want {
				if keys[index] != want[index] {
					t.Fatalf("retained latency keys=%v, want %v", keys, want)
				}
			}
		})
	}
}

func TestApplyUsageLatencyAggregationPageReadsOldRowsBeforeWaitingForWriter(t *testing.T) {
	// 旧 BLOB 必须由 Reader 读取和合并；唯一 Writer 被占用时，旧行查询仍应先完成再等待最终写入。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	db, reader, closePools := openLatencyReaderPoolTestDatabase(t)
	defer closePools()
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	firstRows := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(1, "reader-key", now.Add(-time.Minute), 100, 900)})
	secondRows := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(2, "reader-key", now, 200, 1200)})
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 0, 1, firstRows, now); err != nil {
		t.Fatalf("seed latency row before reader routing check: %v", err)
	}
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load latency writer pool: %v", err)
	}
	heldWriter, err := writerSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold latency writer connection: %v", err)
	}
	writerHeld := true
	defer func() {
		if writerHeld {
			_ = heldWriter.Close()
		}
	}()
	oldRowsLoaded := make(chan struct{}, 1)
	callbackName := "test:latency_old_rows_use_reader"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_latency_stats" {
			select {
			case oldRowsLoaded <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		t.Fatalf("register latency reader callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	applyDone := make(chan error, 1)
	go func() {
		// Runner 传入的句柄已经带 Write scope；Apply 内部的旧行读取仍必须显式覆盖到 Reader。
		applyDone <- repository.ApplyUsageLatencyAggregationPage(context.Background(), db.Clauses(dbresolver.Write), 1, 2, secondRows, now.Add(time.Minute))
	}()
	select {
	case <-oldRowsLoaded:
	case <-time.After(time.Second):
		_ = heldWriter.Close()
		writerHeld = false
		<-applyDone
		t.Fatal("old latency rows did not load while the writer was occupied")
	}
	select {
	case err := <-applyDone:
		t.Fatalf("expected final latency write to wait for occupied writer, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := heldWriter.Close(); err != nil {
		t.Fatalf("release latency writer: %v", err)
	}
	writerHeld = false
	select {
	case err := <-applyDone:
		if err != nil {
			t.Fatalf("apply latency page after writer release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("latency apply did not finish after writer release")
	}
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load latency reader pool: %v", err)
	}
	if stats := readerSQL.Stats(); stats.MaxOpenConnections != 8 {
		t.Fatalf("expected production reader pool for old latency rows: stats=%+v", stats)
	}
}

func TestApplyUsageLatencyAggregationPageMergesExistingBlobsAndAdvancesCursor(t *testing.T) {
	// 两个页面落入同一个 hour/day key，验证 store 合并而不是覆盖旧分布。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	firstRows := buildLatencyRowsForTest(t, []entities.UsageEvent{
		validLatencyEvent(1, "key-a", now.Add(-30*time.Minute), 100, 900),
		validLatencyEvent(2, "key-a", now.Add(-20*time.Minute), 200, 1200),
	})
	secondRows := buildLatencyRowsForTest(t, []entities.UsageEvent{
		validLatencyEvent(3, "key-a", now.Add(-10*time.Minute), 300, 1500),
	})

	// 每个 Apply 都由独立事务写 rows 并推进 Latency 自己的通用水位。
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 0, 2, firstRows, now); err != nil {
		t.Fatalf("apply first latency page: %v", err)
	}
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 2, 3, secondRows, now.Add(time.Minute)); err != nil {
		t.Fatalf("apply second latency page: %v", err)
	}

	// hour 和 day 两行都必须保留三个样本、精确 max 和可解码合并 BLOB。
	var rows []entities.UsageLatencyStat
	if err := db.Order("bucket_type asc").Find(&rows).Error; err != nil {
		t.Fatalf("load merged latency rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two merged latency rows, got %+v", rows)
	}
	for _, row := range rows {
		assertLatencyStoredRow(t, row, 3, 300, 1500, 3)
	}

	// cursor 只能在两行都成功合并后推进到第二页末尾。
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointLatency).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load latency checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 3 {
		t.Fatalf("expected latency checkpoint 3, got %+v", checkpoint)
	}
}

func TestApplyUsageLatencyAggregationPageRollsBackPreparedRowsWhenCheckpointChanged(t *testing.T) {
	// Reader 结果与最终写事务之间仍由 expected cursor 保护；过期页面写入必须和水位冲突一起回滚。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	firstRows := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(1, "conflict-key", now.Add(-time.Minute), 100, 900)})
	secondRows := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(2, "conflict-key", now, 200, 1200)})
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 0, 1, firstRows, now); err != nil {
		t.Fatalf("seed latency page before conflict: %v", err)
	}
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 0, 2, secondRows, now.Add(time.Minute)); err == nil {
		t.Fatal("expected stale latency checkpoint to reject prepared rows")
	}
	var rows []entities.UsageLatencyStat
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("load latency rows after checkpoint conflict: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected one hour and one day row after rollback, got %+v", rows)
	}
	for _, row := range rows {
		assertLatencyStoredRow(t, row, 1, 100, 900, 1)
	}
	assertLatencyAggregationCheckpoint(t, db, 1)
}

func TestApplyUsageLatencyAggregationPageLoadsCompositeKeysInTwoHundredKeyChunks(t *testing.T) {
	// 201 个不同 API key 强制跨过计划约定的 200-key 查询边界。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	firstEvents := make([]entities.UsageEvent, 0, 201)
	secondEvents := make([]entities.UsageEvent, 0, 201)
	for index := 0; index < 201; index++ {
		key := fmt.Sprintf("key-%03d", index)
		firstEvents = append(firstEvents, validLatencyEvent(int64(index+1), key, now.Add(-time.Minute), 100, 900))
		secondEvents = append(secondEvents, validLatencyEvent(int64(index+202), key, now.Add(-30*time.Second), 200, 1200))
	}
	firstRows := buildLatencyRowsForTest(t, firstEvents)
	secondRows := buildLatencyRowsForTest(t, secondEvents)

	// 先写入 402 行（每个 key 一行 hour、一行 day），再只统计第二次合并的旧行 SELECT。
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 0, 201, firstRows, now); err != nil {
		t.Fatalf("apply initial 201-key latency page: %v", err)
	}
	var latencySelects atomic.Int64
	callbackName := "test:count_latency_composite_key_loads"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_latency_stats" {
			latencySelects.Add(1)
		}
	}); err != nil {
		t.Fatalf("register latency query callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 201, 402, secondRows, now.Add(time.Minute)); err != nil {
		t.Fatalf("merge second 201-key latency page: %v", err)
	}
	// 402 composite keys 按每块 200 应恰好执行三次旧行批量加载，不能退化成逐行查询。
	if got := latencySelects.Load(); got != 3 {
		t.Fatalf("expected 3 latency key chunk queries, got %d", got)
	}
	var rowCount int64
	if err := db.Model(&entities.UsageLatencyStat{}).Count(&rowCount).Error; err != nil {
		t.Fatalf("count 201-key latency rows: %v", err)
	}
	if rowCount != 402 {
		t.Fatalf("expected 402 unique hour/day rows, got %d", rowCount)
	}
}

func TestApplyUsageLatencyAggregationPageMergesEachKeyChunkBeforeLoadingTheNext(t *testing.T) {
	// 第一块必须完成真实 Sample merge 后才加载下一块，不能只解码后继续累积整页旧 BLOB。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	firstEvents := make([]entities.UsageEvent, 0, 201)
	secondEvents := make([]entities.UsageEvent, 0, 201)
	for index := 0; index < 201; index++ {
		key := fmt.Sprintf("chunk-key-%03d", index)
		firstEvents = append(firstEvents, validLatencyEvent(int64(index+1), key, now.Add(-time.Minute), 100, 900))
		secondEventID := int64(index + 202)
		if index == 0 {
			// 两边 BLOB 都可独立解码，但同一 event ID 的值不同，只会在 SampleSet.Merge 阶段报错。
			secondEventID = 1
		}
		secondEvents = append(secondEvents, validLatencyEvent(secondEventID, key, now.Add(-30*time.Second), 200, 1200))
	}
	firstRows := buildLatencyRowsForTest(t, firstEvents)
	secondRows := buildLatencyRowsForTest(t, secondEvents)
	if err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 0, 201, firstRows, now); err != nil {
		t.Fatalf("seed chunked latency rows: %v", err)
	}

	var latencySelects atomic.Int64
	callbackName := "test:count_latency_queries_before_chunk_merge_error"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_latency_stats" {
			latencySelects.Add(1)
		}
	}); err != nil {
		t.Fatalf("register latency chunk failure query callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 201, 402, secondRows, now.Add(time.Minute))
	if err == nil {
		t.Fatal("expected conflicting sample in first latency key chunk to fail")
	}
	if !strings.Contains(err.Error(), "conflicting sample for event 1") {
		t.Fatalf("expected SampleSet merge conflict, got %v", err)
	}
	// 即时合并只需第一条 SELECT；先解码整页、最后才 merge 的回退实现会执行全部三条 SELECT。
	if got := latencySelects.Load(); got != 1 {
		t.Fatalf("expected first conflicting chunk to stop after 1 query, got %d", got)
	}
	assertLatencyAggregationCheckpoint(t, db, 201)
}

func TestApplyUsageLatencyAggregationPageRollsBackOnCorruptStoredBlob(t *testing.T) {
	// 预置合法唯一键但损坏旧 Sketch，模拟磁盘或历史格式异常。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	rows := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(2, "key-a", now.Add(-time.Minute), 200, 1200)})
	corrupt := rows[0]
	corrupt.SampleCount = 1
	corrupt.TTFTSketch = []byte{latency.FormatVersion, 1}
	corrupt.CreatedAt = now
	corrupt.UpdatedAt = now
	if err := db.Create(&corrupt).Error; err != nil {
		t.Fatalf("seed corrupt latency row: %v", err)
	}
	if err := db.Create(&entities.UsageAggregationCheckpoint{Name: entities.UsageAggregationCheckpointLatency, LastAggregatedUsageEventID: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed latency checkpoint: %v", err)
	}

	// 解码旧 BLOB 失败必须让整个事务失败，不能推进水位或部分覆盖其它字段。
	err := repository.ApplyUsageLatencyAggregationPage(context.Background(), db, 1, 2, rows, now.Add(time.Minute))
	if err == nil {
		t.Fatal("expected corrupt latency blob to fail")
	}
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointLatency).Take(&checkpoint).Error; err != nil {
		t.Fatalf("reload latency checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 1 {
		t.Fatalf("corrupt latency apply advanced checkpoint: %+v", checkpoint)
	}
	var stored entities.UsageLatencyStat
	if err := db.Where("id = ?", corrupt.ID).Take(&stored).Error; err != nil {
		t.Fatalf("reload corrupt latency row: %v", err)
	}
	if string(stored.TTFTSketch) != string(corrupt.TTFTSketch) || stored.SampleCount != 1 {
		t.Fatalf("corrupt latency row changed after rollback: %+v", stored)
	}
}

func TestUsageLatencyStatsUniqueKeyRejectsDuplicateBucketAPI(t *testing.T) {
	// 数据库唯一键必须覆盖 bucket_type、bucket_start、api_group_key 三列。
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.Local)
	rows := buildLatencyRowsForTest(t, []entities.UsageEvent{validLatencyEvent(1, "key-a", now, 100, 900)})
	row := rows[0]
	row.CreatedAt = now
	row.UpdatedAt = now
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert first latency row: %v", err)
	}
	duplicate := row
	duplicate.ID = 0
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("expected duplicate latency bucket/API key to fail")
	}
}

func buildLatencyRowsForTest(t *testing.T, events []entities.UsageEvent) []entities.UsageLatencyStat {
	// 测试只通过公开 BuildRows 生成有效 BLOB，避免复制生产编码逻辑。
	t.Helper()
	now := time.Time{}
	for _, event := range events {
		if event.Timestamp.After(now) {
			now = event.Timestamp
		}
	}
	rows, err := latency.BuildRows(events, now)
	if err != nil {
		t.Fatalf("build latency test rows: %v", err)
	}
	return rows
}

func latencyRetentionRow(template entities.UsageLatencyStat, apiGroupKey string, bucketStart, now time.Time) entities.UsageLatencyStat {
	template.ID = 0
	template.APIGroupKey = apiGroupKey
	template.BucketStart = bucketStart
	template.CreatedAt = now
	template.UpdatedAt = now
	return template
}

func assertLatencyAggregationCheckpoint(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointLatency).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load latency aggregation checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != want {
		t.Fatalf("expected latency aggregation checkpoint %d, got %+v", want, checkpoint)
	}
}

func openLatencyReaderPoolTestDatabase(t *testing.T) (*gorm.DB, *gorm.DB, func()) {
	t.Helper()
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "latency-reader.db")})
	if err != nil {
		t.Fatalf("open latency reader pools: %v", err)
	}
	closePools := func() {
		if reader != nil && reader != db {
			if sqlDB, dbErr := reader.DB(); dbErr == nil {
				_ = sqlDB.Close()
			}
		}
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	}
	return db, reader, closePools
}

func validLatencyEvent(id int64, apiGroupKey string, timestamp time.Time, ttftMS, latencyMS int64) entities.UsageEvent {
	// 每个测试事件显式设置 generate=true 和正延迟，确保进入 Latency 聚合。
	generate := true
	return entities.UsageEvent{ID: id, APIGroupKey: apiGroupKey, Timestamp: timestamp, Generate: &generate, TTFTMS: &ttftMS, LatencyMS: latencyMS}
}

func assertLatencyStoredRow(t *testing.T, row entities.UsageLatencyStat, count, maxTTFT, maxLatency int64, pointCount int) {
	// 表字段与三个二进制载荷必须表达同一批样本数。
	t.Helper()
	if row.SampleCount != count || row.MaxTTFTMS != maxTTFT || row.MaxLatencyMS != maxLatency || row.FormatVersion != latency.FormatVersion {
		t.Fatalf("unexpected stored latency row: %+v", row)
	}
	ttftSketch, err := latency.UnmarshalSketch(row.TTFTSketch)
	if err != nil {
		t.Fatalf("decode stored TTFT sketch: %v", err)
	}
	latencySketch, err := latency.UnmarshalSketch(row.LatencySketch)
	if err != nil {
		t.Fatalf("decode stored latency sketch: %v", err)
	}
	points, err := latency.UnmarshalSampleSet(row.SamplePoints)
	if err != nil {
		t.Fatalf("decode stored sample points: %v", err)
	}
	if ttftSketch.Count() != uint64(count) || latencySketch.Count() != uint64(count) || len(points.Points()) != pointCount {
		t.Fatalf("stored latency payload count mismatch: ttft=%d latency=%d points=%d", ttftSketch.Count(), latencySketch.Count(), len(points.Points()))
	}
}
