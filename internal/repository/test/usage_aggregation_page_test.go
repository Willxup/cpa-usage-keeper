package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestLoadUsageAggregationEventPageUsesBoundsLimitProjectionAndReader(t *testing.T) {
	// 文件数据库才能证明统一业务句柄把事件页 SELECT 路由到独立 Reader。
	dbPath := filepath.Join(t.TempDir(), "aggregation-page.db")
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabasePools returned error: %v", err)
	}
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer pool: %v", err)
	}
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader pool: %v", err)
	}
	closeResolverTestPools(t, writerSQL, readerSQL)

	// 第二条事件填满三类 BuildRows 所需字段，同时给非投影字段写 sentinel。
	alias := "alias-a"
	generate := true
	ttft := int64(120)
	now := time.Date(2026, 7, 26, 11, 0, 0, 0, time.Local)
	events := []entities.UsageEvent{
		{EventKey: "event-1", APIGroupKey: "key-1", Model: "model-1", Timestamp: now},
		{EventKey: "must-not-project", Provider: "must-not-project", APIGroupKey: "key-2", Model: "model-2", ModelAlias: &alias, AuthIndex: "auth-2", ServiceTier: "priority", ResponseServiceTier: "default", ReasoningEffort: "high", Endpoint: "/v1/responses", ExecutorType: "redis", Timestamp: now.Add(time.Minute), Failed: true, Generate: &generate, LatencyMS: 900, TTFTMS: &ttft, InputTokens: 10, OutputTokens: 20, ReasoningTokens: 3, CachedTokens: 4, CacheReadTokens: 5, CacheCreationTokens: 6, TotalTokens: 33},
		{EventKey: "event-3", APIGroupKey: "key-3", Model: "model-3", Timestamp: now.Add(2 * time.Minute)},
		{EventKey: "event-4", APIGroupKey: "key-4", Model: "model-4", Timestamp: now.Add(3 * time.Minute)},
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert aggregation page events: %v", err)
	}

	// 占住唯一 writer；事件页若错误走 writer，会在一秒 context 到期前无法返回。
	heldWriter, err := writerSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold writer connection: %v", err)
	}
	defer heldWriter.Close()
	queryContext, cancelQuery := context.WithTimeout(context.Background(), time.Second)
	defer cancelQuery()

	// after=1、target=4、limit=2 必须只返回 ID 2 和 3，并保持升序。
	page, err := repository.LoadUsageAggregationEventPage(queryContext, db, 1, 4, 2)
	if err != nil {
		t.Fatalf("LoadUsageAggregationEventPage returned error: %v", err)
	}
	if len(page) != 2 || page[0].ID != 2 || page[1].ID != 3 {
		t.Fatalf("unexpected bounded aggregation page: %+v", page)
	}

	// 第一行逐字段验证固定投影；EventKey/Provider 不属于三类聚合并应保持零值。
	got := page[0]
	if got.APIGroupKey != "key-2" || got.Model != "model-2" || got.ModelAlias == nil || *got.ModelAlias != alias || got.AuthIndex != "auth-2" || got.ServiceTier != "priority" || got.ResponseServiceTier != "default" || got.ReasoningEffort != "high" || got.Endpoint != "/v1/responses" || got.ExecutorType != "redis" || !got.Timestamp.Equal(now.Add(time.Minute)) || !got.Failed || got.Generate == nil || !*got.Generate || got.LatencyMS != 900 || got.TTFTMS == nil || *got.TTFTMS != ttft || got.InputTokens != 10 || got.OutputTokens != 20 || got.ReasoningTokens != 3 || got.CachedTokens != 4 || got.CacheReadTokens != 5 || got.CacheCreationTokens != 6 || got.TotalTokens != 33 {
		t.Fatalf("aggregation page lost required projection fields: %+v", got)
	}
	if got.EventKey != "" || got.Provider != "" {
		t.Fatalf("aggregation page loaded fields outside fixed projection: event_key=%q provider=%q", got.EventKey, got.Provider)
	}
}
