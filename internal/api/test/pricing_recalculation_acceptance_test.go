package test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

// TestPricingRecalculationHTTPWithRealStorage 验证真实接口、唯一任务、读写门禁、缓存和增量统计的完整连通。
// 用未结束的费用读取停住准备阶段，HTTP 断开后后台仍完成；期间新 CPA 消息只能落 inbox。
func TestPricingRecalculationHTTPWithRealStorage(t *testing.T) {
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "recalculation-http.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, handle := range []*gorm.DB{reader, db} {
			pool, err := handle.DB()
			if err != nil {
				t.Error(err)
				continue
			}
			if err := pool.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	start := time.Now().In(time.Local).Truncate(time.Hour).Add(-time.Hour)
	end := start.Add(30 * time.Minute)
	cost, available := 7.0, true
	events := []entities.UsageEvent{
		{EventKey: "before", Timestamp: start.Add(-time.Minute)},
		{EventKey: "inside", Timestamp: start.Add(time.Minute)},
		{EventKey: "at-end", Timestamp: end},
	}
	for index := range events {
		events[index].Model = "priced"
		events[index].APIGroupKey = "test-key"
		events[index].AuthType = "oauth"
		events[index].AuthIndex = "test-auth"
		events[index].InputTokens, events[index].TotalTokens = 1_000_000, 1_000_000
		events[index].CostUSD, events[index].CostAvailable = &cost, &available
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
		t.Fatal(err)
	}
	recent, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return end }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recent.Close)
	catalog := pricing.NewCatalog(pricing.EmptySnapshot())
	aggregation := poller.NewUsageAggregationRunner(db)
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		PricingCatalog: catalog, RecentUsageEvents: recent, UsageAggregationNotifier: aggregation,
		Now: func() time.Time { return end },
	})
	quotas := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(nil))
	t.Cleanup(quotas.StopRefreshTasks)
	reads := service.NewCostReadGate()
	lifecycle, stop := context.WithCancel(context.Background())
	provider := service.NewPricingServiceWithRecalculation(db, catalog, service.PricingRecalculationDependencies{
		LifecycleCtx: lifecycle, Sync: syncer, Aggregation: aggregation, CostReadGate: reads,
		Recent: recent, Quota: quotas, Now: func() time.Time { return end },
	})
	oldRead, ok := reads.Acquire()
	if !ok {
		t.Fatal("initial read was blocked")
	}
	t.Cleanup(func() { oldRead(); stop(); provider.WaitPricingRecalculation() })
	router := keeperapi.NewRouter(nil, nil, nil, provider, keeperapi.AuthConfig{}, nil, "/cpa", keeperapi.OptionalProviders{CostReadGate: reads})
	serve := func(method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, newPricingRequest(method, "/cpa/api/v1"+path, body))
		return response
	}
	modelBody := `{"model":"priced","pricing_style":"openai","base_prices":{"input":2,"output":0,"cache_read":0,"cache_write":0},"model_multiplier":1,"conditional_multipliers":[],"branches":[]}`
	saved := serve(http.MethodPut, "/pricing/models", modelBody)
	var configured servicedto.SavePricingModelResponse
	if saved.Code != http.StatusOK || json.Unmarshal(saved.Body.Bytes(), &configured) != nil {
		t.Fatalf("save configuration: %d %s", saved.Code, saved.Body.String())
	}
	body, err := json.Marshal(servicedto.StartRecalculationRequest{StartAt: start, ConfigRevision: configured.ConfigRevision})
	if err != nil {
		t.Fatal(err)
	}
	requestCtx, disconnect := context.WithCancel(context.Background())
	defer disconnect()
	response := httptest.NewRecorder()
	request := newPricingRequest(http.MethodPost, "/cpa/api/v1/pricing/recalculations", string(body)).WithContext(requestCtx)
	router.ServeHTTP(response, request)
	var accepted servicedto.StartRecalculationResponse
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &accepted) != nil || !accepted.Started {
		t.Fatalf("start recalculation: %d %s", response.Code, response.Body.String())
	}
	waitForAPICostGateBlocked(t, reads)
	disconnect()
	// 请求已结束并断开，current 仍可读取；统计读取则由共享门禁明确返回 busy。
	current := serve(http.MethodGet, "/pricing/recalculations/current", "")
	var running servicedto.RecalculationTask
	if current.Code != http.StatusOK || json.Unmarshal(current.Body.Bytes(), &running) != nil || running.TaskID != accepted.Task.TaskID || running.Status != servicedto.RecalculationRunning {
		t.Fatalf("current task during blocked read: %d %s", current.Code, current.Body.String())
	}
	busy := serve(http.MethodGet, "/usage/events", "")
	var busyError servicedto.PricingErrorResponse
	if busy.Code != http.StatusServiceUnavailable || json.Unmarshal(busy.Body.Bytes(), &busyError) != nil || busyError.Code != "costs_busy" {
		t.Fatalf("statistics were not gated: %d %s", busy.Code, busy.Body.String())
	}
	writeBusy := serve(http.MethodPut, "/pricing/models", modelBody)
	if writeBusy.Code != http.StatusConflict || json.Unmarshal(writeBusy.Body.Bytes(), &busyError) != nil || busyError.Code != "pricing_busy" {
		t.Fatalf("pricing write was not blocked: %d %s", writeBusy.Code, writeBusy.Body.String())
	}
	duplicateBody, _ := json.Marshal(servicedto.StartRecalculationRequest{StartAt: start.Add(-time.Hour), ConfigRevision: 0})
	duplicate := serve(http.MethodPost, "/pricing/recalculations", string(duplicateBody))
	var ignored servicedto.StartRecalculationResponse
	if duplicate.Code != http.StatusOK || json.Unmarshal(duplicate.Body.Bytes(), &ignored) != nil || ignored.Started || ignored.Task.TaskID != accepted.Task.TaskID || !ignored.Task.StartAt.Equal(start) {
		t.Fatalf("duplicate request replaced or queued a task: %d %s", duplicate.Code, duplicate.Body.String())
	}
	// 停稳等待不占 writer；新消息持久化成功，但尚不生成 usage_event。
	payload := fmt.Sprintf(`{"timestamp":%q,"provider":"codex","auth_type":"oauth","auth_index":"test-auth","model":"priced","request_id":"during-recalculation","tokens":{"input_tokens":1000000}}`, start.Add(2*time.Minute).Format(time.RFC3339Nano))
	inbox, err := repository.InsertRedisUsageInboxRawMessages(db, "redis_subscribe:usage", []string{payload}, end)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("CPA reception while paused: %+v %v", inbox, err)
	}
	var eventCount int64
	if err := db.Model(&entities.UsageEvent{}).Count(&eventCount).Error; err != nil || eventCount != 3 {
		t.Fatalf("events changed while paused: %d %v", eventCount, err)
	}
	oldRead()
	waitPricingRecalculationHTTPJob(t, provider)
	completed := serve(http.MethodGet, "/pricing/recalculations/current", "")
	var done servicedto.RecalculationTask
	if completed.Code != http.StatusOK || json.Unmarshal(completed.Body.Bytes(), &done) != nil || done.Status != servicedto.RecalculationCompleted || done.TotalCount == nil || *done.TotalCount != 1 || done.ProcessedCount != 1 || done.Error != nil {
		t.Fatalf("request disconnect canceled job or wrong progress: %d %s", completed.Code, completed.Body.String())
	}
	assertPricingRecalculationHTTPFees(t, db, []float64{7, 2, 7}, 16)
	if release, ok := reads.Acquire(); !ok {
		t.Fatal("completed task retained fee read gate")
	} else {
		release()
	}
	// 结束结果不充当幂等历史；下一次明确点击可以重算相同范围，金额不得双加。
	again := serve(http.MethodPost, "/pricing/recalculations", string(body))
	var repeated servicedto.StartRecalculationResponse
	if again.Code != http.StatusAccepted || json.Unmarshal(again.Body.Bytes(), &repeated) != nil || !repeated.Started || repeated.Task.TaskID == accepted.Task.TaskID {
		t.Fatalf("completed task prevented new run: %d %s", again.Code, again.Body.String())
	}
	waitPricingRecalculationHTTPJob(t, provider)
	assertPricingRecalculationHTTPFees(t, db, []float64{7, 2, 7}, 16)
	workCtx, cancelWork := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWork()
	processed, err := syncer.ProcessRedisUsageInbox(workCtx)
	if err != nil || processed.InsertedEvents != 1 {
		t.Fatalf("processing did not resume for persisted inbox: %+v %v", processed, err)
	}
	if _, err := aggregation.RunOnce(workCtx); err != nil {
		t.Fatalf("aggregation did not resume: %v", err)
	}
	assertPricingRecalculationHTTPFees(t, db, []float64{7, 2, 7, 2}, 18)
	var storedInbox entities.RedisUsageInbox
	if err := db.First(&storedInbox, inbox[0].ID).Error; err != nil || storedInbox.Status != repository.RedisUsageInboxStatusProcessed || storedInbox.AttemptCount != 0 {
		t.Fatalf("pause consumed or lost inbox: %+v %v", storedInbox, err)
	}
}

func waitPricingRecalculationHTTPJob(t *testing.T, provider service.PricingProvider) {
	t.Helper()
	done := make(chan struct{})
	go func() { provider.WaitPricingRecalculation(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("recalculation did not finish")
	}
}

func assertPricingRecalculationHTTPFees(t *testing.T, db *gorm.DB, expected []float64, total float64) {
	t.Helper()
	var events []entities.UsageEvent
	if err := db.Order("id").Find(&events).Error; err != nil || len(events) != len(expected) {
		t.Fatalf("persisted events: count=%d error=%v", len(events), err)
	}
	for index, event := range events {
		if event.CostUSD == nil || !(math.Abs(*event.CostUSD-expected[index]) <= 1e-12) || event.CostAvailable == nil || !*event.CostAvailable || event.InputTokens != 1_000_000 {
			t.Fatalf("event %d changed outside fee contract: %+v", index, event)
		}
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var summary struct {
			Cost     float64
			Requests int64
		}
		if err := db.Table(table).Select("SUM(cost_usd) AS cost, SUM(request_count) AS requests").Scan(&summary).Error; err != nil || !(math.Abs(summary.Cost-total) <= 1e-12) || summary.Requests != int64(len(expected)) {
			t.Fatalf("%s: %+v, error=%v", table, summary, err)
		}
	}
	state, err := repository.LoadUsageAggregationCheckpointSnapshot(context.Background(), db)
	if err != nil || state.OverviewCursor != events[len(events)-1].ID {
		t.Fatalf("overview checkpoint: %+v %v", state, err)
	}
}
