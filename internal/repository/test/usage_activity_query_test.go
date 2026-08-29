package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

func TestQueryUsageActivityGridReturnsFixedSparseBlocksAndAPIGroupScope(t *testing.T) {
	// 准备：两个 API group 在同一 medium bucket 内分别写入不同成功失败请求。
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		{EventKey: "activity-query-a-success", APIGroupKey: "provider-a", Timestamp: now.Add(-time.Hour), InputTokens: 10, CacheReadTokens: 3, TotalTokens: 13},
		{EventKey: "activity-query-a-failure", APIGroupKey: "provider-a", Timestamp: now.Add(-time.Hour), Failed: true, InputTokens: 20, CachedTokens: 99, CacheReadTokens: 4, TotalTokens: 24},
		{EventKey: "activity-query-b-success", APIGroupKey: "provider-b", Timestamp: now.Add(-time.Hour), InputTokens: 30, CacheReadTokens: 5, TotalTokens: 35},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert activity query events: %v", err)
	}
	if err := repository.AggregateUsageActivityStats(context.Background(), db, now); err != nil {
		t.Fatalf("aggregate activity query events: %v", err)
	}
	storedBucket, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainMedium, events[0].Timestamp)
	if err != nil {
		t.Fatalf("resolve stored medium bucket: %v", err)
	}

	// 执行：按 provider-a 查询固定 7×52 Activity 网格。
	grid, err := repository.QueryUsageActivityGrid(context.Background(), db, entities.UsageActivityGrainMedium, now, time.Time{}, "provider-a")
	if err != nil {
		t.Fatalf("QueryUsageActivityGrid returned error: %v", err)
	}

	// 断言：网格数量和真实边界固定，稀疏空桶补零且只累计目标 API group。
	if grid.Rows != 7 || grid.Columns != 52 || len(grid.Blocks) != repository.UsageActivityHeatmapBlocks {
		t.Fatalf("unexpected activity grid shape: %+v", grid)
	}
	if !grid.WindowStart.Equal(grid.Blocks[0].StartTime) || !grid.WindowEnd.Equal(grid.Blocks[len(grid.Blocks)-1].EndTime) {
		t.Fatalf("unexpected activity grid window: %+v", grid)
	}
	var populated *repositorydto.UsageActivityBlockRecord
	for index := range grid.Blocks {
		block := &grid.Blocks[index]
		if block.StartTime.Equal(storedBucket.Start) {
			populated = block
			break
		}
	}
	if populated == nil {
		t.Fatalf("expected stored bucket %s in activity grid", storedBucket.Start)
	}
	if !populated.EndTime.Equal(storedBucket.End) || populated.SuccessCount != 1 || populated.FailureCount != 1 {
		t.Fatalf("unexpected provider-a request counts: %+v", populated)
	}
	if populated.InputTokens != 30 || populated.CacheReadTokens != 7 || populated.TotalTokens != 37 {
		t.Fatalf("unexpected provider-a token counts: %+v", populated)
	}
}

func TestQueryUsageActivityGridIncludesBothDSTFallbackOffsets(t *testing.T) {
	// 准备：切换到存在秋季回拨的项目时区，并选择第二个 01:15 作为窗口参考时间。
	previousLocal := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load DST location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	referenceInstant, err := time.Parse(time.RFC3339, "2026-11-01T01:15:00-05:00")
	if err != nil {
		t.Fatalf("parse fallback reference: %v", err)
	}
	referenceEnd := referenceInstant.In(location)
	buckets, err := repository.UsageActivityWindowEndingAt(entities.UsageActivityGrainShort, referenceEnd)
	if err != nil {
		t.Fatalf("resolve fallback Activity window: %v", err)
	}
	localWindowEnd := buckets[len(buckets)-1].End.In(location)
	windowEndWallClock := localWindowEnd.Format("15:04:05")
	var firstOffsetBucket repository.UsageActivityBucket
	for _, bucket := range buckets {
		localStart := bucket.Start.In(location)
		_, offset := localStart.Zone()
		sameLocalDate := localStart.Year() == localWindowEnd.Year() && localStart.YearDay() == localWindowEnd.YearDay()
		if sameLocalDate && offset == -4*60*60 && localStart.Format("15:04:05") > windowEndWallClock {
			firstOffsetBucket = bucket
			break
		}
	}
	if firstOffsetBucket.Start.IsZero() {
		t.Fatal("expected first-offset bucket later in wall-clock text but earlier as an instant")
	}
	db := openTestDatabase(t)
	if err := db.Create(&entities.UsageActivityStat{
		Grain: entities.UsageActivityGrainShort, BucketStart: firstOffsetBucket.Start, BucketEnd: firstOffsetBucket.End,
		APIGroupKey: "fallback-provider", SuccessCount: 1,
	}).Error; err != nil {
		t.Fatalf("seed fallback Activity row: %v", err)
	}

	// 执行：查询跨越两个 01 时段的完整 short Activity 网格。
	grid, err := repository.QueryUsageActivityGrid(context.Background(), db, entities.UsageActivityGrainShort, referenceEnd, time.Time{}, "fallback-provider")
	if err != nil {
		t.Fatalf("query fallback Activity grid: %v", err)
	}

	// 断言：实际 instant 位于窗口内的 EDT 行不能因 RFC3339 本地文本排序被漏掉。
	var successCount int64
	for _, block := range grid.Blocks {
		successCount += block.SuccessCount
	}
	if successCount != 1 {
		t.Fatalf("expected both DST offsets to remain queryable, got success_count=%d", successCount)
	}
}

func TestQueryUsageActivityGridReadsRequestHealthWithoutLegacyTable(t *testing.T) {
	// 准备：同一窗口内写入成功、失败和其它 API group 事件，并追平 Activity。
	db := openTestDatabase(t)
	end := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		{EventKey: "activity-success", APIGroupKey: "provider-a", Model: "model-a", Timestamp: end.Add(-time.Hour), InputTokens: 10, TotalTokens: 10},
		{EventKey: "activity-failure", APIGroupKey: "provider-a", Model: "model-a", Timestamp: end.Add(-30 * time.Minute), Failed: true, InputTokens: 20, TotalTokens: 20},
		{EventKey: "activity-other", APIGroupKey: "provider-b", Model: "model-a", Timestamp: end.Add(-15 * time.Minute), InputTokens: 30, TotalTokens: 30},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert activity events: %v", err)
	}
	if err := repository.AggregateUsageActivityStats(context.Background(), db, end); err != nil {
		t.Fatalf("aggregate Activity stats: %v", err)
	}
	if db.Migrator().HasTable("usage_overview_health_stats") {
		if err := db.Migrator().DropTable("usage_overview_health_stats"); err != nil {
			t.Fatalf("drop legacy health table: %v", err)
		}
	}

	// 执行：独立查询在旧 Health 表不存在时读取 medium Activity。
	grid, err := repository.QueryUsageActivityGrid(context.Background(), db, entities.UsageActivityGrainMedium, end, time.Time{}, "provider-a")
	if err != nil {
		t.Fatalf("QueryUsageActivityGrid returned error: %v", err)
	}

	// 断言：Activity 固定为 364 格且隔离其它 API group。
	if grid.Rows != 7 || grid.Columns != 52 || len(grid.Blocks) != 364 {
		t.Fatalf("unexpected Activity shape: %+v", grid)
	}
	if grid.TotalSuccess != 1 || grid.TotalFailure != 1 {
		t.Fatalf("unexpected Activity totals: %+v", grid)
	}
	populatedBlocks := 0
	for _, block := range grid.Blocks {
		if block.SuccessCount+block.FailureCount == 0 {
			continue
		}
		populatedBlocks++
	}
	if populatedBlocks == 0 {
		t.Fatal("expected at least one populated activity-backed health block")
	}
}

func TestQueryUsageActivityGridPreservesRequestContext(t *testing.T) {
	// 准备：给仓储查询注入请求级 context，并只在 Activity 查询回调中检查它。
	db := openTestDatabase(t)
	type requestContextKey struct{}
	requestContext := context.WithValue(context.Background(), requestContextKey{}, "activity-request")
	activityQuerySawRequestContext := false
	if err := db.Callback().Query().Before("gorm:query").Register("test:require_activity_request_context", func(tx *gorm.DB) {
		// 非 Activity 查询保持原样，只观察 Activity 数据源读取。
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "usage_activity_stats" {
			return
		}
		// Activity 查询必须沿用 service 传入的请求 context，不能覆盖为 Background。
		if tx.Statement.Context.Value(requestContextKey{}) != "activity-request" {
			tx.AddError(context.Canceled)
			return
		}
		// 记录回调确实观察到了目标 Activity 查询。
		activityQuerySawRequestContext = true
	}); err != nil {
		t.Fatalf("register Activity context callback: %v", err)
	}
	end := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	// 执行：通过带请求 context 的 GORM session 查询独立 Activity。
	_, err := repository.QueryUsageActivityGrid(requestContext, db.WithContext(requestContext), entities.UsageActivityGrainMedium, end, time.Time{}, "")

	// 断言：Activity 查询应成功且回调必须看到原请求 context。
	if err != nil {
		t.Fatalf("QueryUsageActivityGrid returned error: %v", err)
	}
	if !activityQuerySawRequestContext {
		t.Fatal("expected Activity query to preserve request context")
	}
}
