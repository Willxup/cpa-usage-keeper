package test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"

	"gorm.io/gorm"
)

type stagedQuotaHandler struct {
	mu       sync.Mutex
	calls    int
	entered  chan int
	releases []chan struct{}
}

func (h *stagedQuotaHandler) Check(ctx context.Context, _ ProviderInput) (ProviderOutput, error) {
	h.mu.Lock()
	h.calls++
	call := h.calls
	h.mu.Unlock()
	h.entered <- call
	select {
	case <-ctx.Done():
		return ProviderOutput{}, ctx.Err()
	case <-h.releases[call-1]:
	}
	return ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: float64(call * 25)}}}}, nil
}

type keyedQuotaHandler struct {
	mu         sync.Mutex
	calls      map[string]int
	entered    chan string
	releaseOld <-chan struct{}
}

func (h *keyedQuotaHandler) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	authIndex := input.Identity.Identity
	h.mu.Lock()
	h.calls[authIndex]++
	h.mu.Unlock()
	h.entered <- authIndex
	if authIndex == "auth-a" {
		select {
		case <-ctx.Done():
			return ProviderOutput{}, ctx.Err()
		case <-h.releaseOld:
		}
	}
	return ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}, nil
}

func (h *keyedQuotaHandler) callCount(authIndex string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[authIndex]
}

func TestInvalidateStoredCostCacheClearsAllQuotaResultsAndTasks(t *testing.T) {
	service := &Service{}
	setRefreshTasks(service, map[string]*RefreshTaskRecord{
		"completed": {AuthIndex: "completed", Status: RefreshTaskStatusCompleted, Quota: &CheckResponse{ID: "completed"}},
		"running":   {AuthIndex: "running", Status: RefreshTaskStatusRunning},
		"queued":    {AuthIndex: "queued", Status: RefreshTaskStatusQueued},
		"failed":    {AuthIndex: "failed", Status: RefreshTaskStatusFailed, Error: "old error"},
	})
	service.InvalidateStoredCostCache()
	if got := refreshTaskCount(service); got != 0 {
		t.Fatalf("expected all quota result/task entries removed, got %d", got)
	}
	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"completed", "failed"}})
	if err != nil || len(cache.Items) != 0 {
		t.Fatalf("expected empty quota cache, got %+v err=%v", cache, err)
	}
	for _, authIndex := range []string{"running", "queued"} {
		if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), authIndex); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("%s still pending after invalidation: %v", authIndex, err)
		}
	}
}

func TestInvalidateStoredCostCacheRejectsOldRefreshAndAllowsSameAuthIndexAgain(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &stagedQuotaHandler{entered: make(chan int, 2), releases: []chan struct{}{make(chan struct{}), make(chan struct{})}}
	service := newQuotaServiceWithRegistryAndOptions(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}), ServiceOptions{RefreshWorkerLimit: 2})
	setRefreshCooldown(service, func(time.Duration) {})
	first := queueManualQuotaRefresh(t, service, "auth-1")
	if first.Accepted != 1 {
		t.Fatalf("expected initial task, got %+v", first)
	}
	select {
	case call := <-handler.entered:
		if call != 1 {
			t.Fatalf("unexpected first provider call %d", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("old refresh did not start")
	}
	service.InvalidateStoredCostCache()
	second := queueManualQuotaRefresh(t, service, "auth-1")
	if second.Accepted != 1 {
		t.Fatalf("expected new manual refresh after clearing, got %+v", second)
	}
	select {
	case call := <-handler.entered:
		if call != 2 {
			t.Fatalf("unexpected second provider call %d", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new refresh did not start")
	}
	close(handler.releases[0])
	deadline := time.Now().Add(2 * time.Second)
	for len(refreshWorkerTokens(service)) > 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(refreshWorkerTokens(service)) > 1 {
		t.Fatal("old worker did not leave its slot")
	}
	current, err := service.GetRefreshTaskByAuthIndex(context.Background(), "auth-1")
	if err != nil || current.Status == RefreshTaskStatusCompleted || current.Quota != nil {
		t.Fatalf("old result wrote over the new pending task: %+v err=%v", current, err)
	}
	close(handler.releases[1])
	completed := waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	if completed.Quota == nil || len(completed.Quota.Quota) == 0 || completed.Quota.Quota[0].UsedPercent == nil || *completed.Quota.Quota[0].UsedPercent != 50 {
		t.Fatalf("expected only new refresh to publish, got %+v", completed)
	}
}

func TestInvalidateStoredCostCacheDoesNotLetOldDispatcherRunNewQueuedTask(t *testing.T) {
	db := openQuotaTestDatabase(t)
	for _, authIndex := range []string{"auth-a", "auth-b"} {
		seedUsageIdentity(t, db, entities.UsageIdentity{Identity: authIndex, Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	}
	releaseOld := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseOld) })
	defer release()
	handler := &keyedQuotaHandler{calls: map[string]int{}, entered: make(chan string, 3), releaseOld: releaseOld}
	service := newQuotaServiceWithRegistryAndOptions(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}), ServiceOptions{RefreshWorkerLimit: 1})
	setRefreshCooldown(service, func(time.Duration) {})
	first := queueManualQuotaRefresh(t, service, "auth-a", "auth-b")
	if first.Accepted != 2 {
		t.Fatalf("expected old running and queued tasks, got %+v", first)
	}
	select {
	case authIndex := <-handler.entered:
		if authIndex != "auth-a" {
			t.Fatalf("unexpected first call %s", authIndex)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("old auth-a did not start")
	}
	service.InvalidateStoredCostCache()
	newTask := queueManualQuotaRefresh(t, service, "auth-b")
	if newTask.Accepted != 1 {
		t.Fatalf("expected a fresh auth-b task, got %+v", newTask)
	}
	release()
	completed := waitForRefreshTask(t, service, "auth-b", RefreshTaskStatusCompleted)
	if completed.Quota == nil || handler.callCount("auth-b") != 1 {
		t.Fatalf("old dispatcher duplicated new auth-b work: task=%+v calls=%d", completed, handler.callCount("auth-b"))
	}
}

func TestInvalidateStoredCostCacheSkipsCooldownForInvalidatedQueuedTask(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &keyedQuotaHandler{calls: map[string]int{}, entered: make(chan string, 1)}
	service := newQuotaServiceWithRegistryAndOptions(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}), ServiceOptions{RefreshWorkerLimit: 1})
	var cooldownCalls atomic.Int64
	setRefreshCooldown(service, func(time.Duration) { cooldownCalls.Add(1) })
	releaseSlot := occupyRefreshWorkerToken(service)
	response := queueManualQuotaRefresh(t, service, "auth-1")
	if response.Accepted != 1 {
		releaseSlot()
		t.Fatalf("expected one queued refresh, got %+v", response)
	}
	service.InvalidateStoredCostCache()
	releaseSlot()
	service.WaitRefreshTasks()
	if got := cooldownCalls.Load(); got != 0 {
		t.Fatalf("invalidated queued work consumed %d cooldowns", got)
	}
	if got := handler.callCount("auth-1"); got != 0 {
		t.Fatalf("invalidated queued work called provider %d times", got)
	}
}

func TestInvalidateStoredCostCacheDropsPendingHeaderButAcceptsNewHeader(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := newQuotaServiceWithRegistryAndOptions(t, db, NewProviderRegistry(nil), ServiceOptions{UsageHeaderSnapshotFlushInterval: time.Hour})
	timers := newUsageHeaderManualTimers(service)
	old := codexUsageHeaderSnapshot("codex-auth", time.Now(), "4")
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(old)) {
		t.Fatal("old Header append rejected")
	}
	var firstTimer usageHeaderManualTimer
	select {
	case firstTimer = <-timers:
	case <-time.After(2 * time.Second):
		t.Fatal("old Header did not start a timer")
	}
	service.InvalidateStoredCostCache()
	firstTimer.fire <- time.Now()
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("old pending Header repopulated cache: %v", err)
	}
	newer := codexUsageHeaderSnapshot("codex-auth", old.ObservedAt.Add(time.Second), "8")
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(newer)) {
		t.Fatal("new Header append rejected")
	}
	// Stop 刷新 worker 会 flush 当前已接受的新窗口，不依赖真实分钟等待。
	service.StopRefreshTasks()
	current := waitForRefreshTask(t, service, "codex-auth", RefreshTaskStatusCompleted)
	if current.Quota == nil || len(current.Quota.Quota) == 0 || current.Quota.Quota[0].UsedPercent == nil || *current.Quota.Quota[0].UsedPercent != 8 {
		t.Fatalf("expected new Header only, got %+v", current)
	}
}

func TestInvalidateStoredCostCacheRejectsHeaderAlreadyInsideIdentityLookup(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	var once sync.Once
	callbackName := "test:block_header_during_invalidation"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_identities" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	result := make(chan bool, 1)
	go func() {
		result <- applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", time.Now(), "4"))
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Header identity lookup did not start")
	}
	service.InvalidateStoredCostCache()
	releaseOnce()
	select {
	case applied := <-result:
		if applied {
			t.Fatal("pre-clear Header reported published cache")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Header work did not finish")
	}
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("old Header restored cache after invalidation: %v", err)
	}
}

func TestInvalidateStoredCostCacheRejectsHeaderDuringWindowQuery(t *testing.T) {
	db, closePools := openQuotaReaderPoolTestDatabase(t)
	defer closePools()
	observedAt := time.Date(2026, 6, 22, 11, 0, 0, 0, time.Local)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageEvent(t, db, entities.UsageEvent{AuthType: "oauth", AuthIndex: "codex-auth", Model: "gpt-5.5", Timestamp: observedAt.Add(-time.Hour), TotalTokens: 12, CostUSD: floatPtr(1), CostAvailable: boolPtr(true)})
	service := newQuotaServiceWithRegistry(t, db, NewProviderRegistry(nil))
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	var blocked atomic.Bool
	callbackName := "test:block_header_window_query_during_invalidation"
	if err := db.Callback().Row().Before("gorm:row").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_events" && blocked.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Row().Remove(callbackName) })
	result := make(chan bool, 1)
	go func() {
		result <- applyUsageHeaderSnapshot(service, context.Background(), codexUsageHeaderSnapshot("codex-auth", observedAt, "4"))
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Header did not enter its local window query")
	}
	service.InvalidateStoredCostCache()
	releaseOnce()
	select {
	case applied := <-result:
		if applied {
			t.Fatal("old Header published after the local window query resumed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Header window query did not finish")
	}
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("old Header restored cache after invalidation: %v", err)
	}
}

func TestInvalidateStoredCostCacheStopsInspectionWithoutFalseCompletion(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &stagedQuotaHandler{entered: make(chan int, 2), releases: []chan struct{}{make(chan struct{}), make(chan struct{})}}
	service := newQuotaServiceWithRegistryAndOptions(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}), ServiceOptions{RefreshWorkerLimit: 2})
	setRefreshCooldown(service, func(time.Duration) {})
	if _, err := service.StartInspection(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handler.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("old inspection provider did not start")
	}
	service.InvalidateStoredCostCache()
	status, err := service.GetInspectionStatus(context.Background())
	if err != nil || status.Running || status.Completed || status.CompletedAt != nil {
		t.Fatalf("invalidated inspection must not finish: %+v err=%v", status, err)
	}
	close(handler.releases[0])
	if _, err := service.StartInspection(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-handler.entered:
		if call != 2 {
			t.Fatalf("unexpected new inspection call %d", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new inspection provider did not start")
	}
	close(handler.releases[1])
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	status, err = service.GetInspectionStatus(context.Background())
	if err != nil || !status.Completed || status.CompletedAt == nil {
		t.Fatalf("new inspection did not complete: %+v err=%v", status, err)
	}
}

func TestInvalidateStoredCostCacheDuringInspectionScanDoesNotRestartOldRound(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := newQuotaRefreshService(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": &refreshHandlerStub{}}))
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	var once sync.Once
	callbackName := "test:block_inspection_scan_during_invalidation"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_identities" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	type inspectionResult struct {
		status InspectionStatus
		err    error
	}
	result := make(chan inspectionResult, 1)
	go func() {
		status, err := service.StartInspection(context.Background())
		result <- inspectionResult{status: status, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("inspection scan did not block")
	}
	service.InvalidateStoredCostCache()
	releaseOnce()
	select {
	case got := <-result:
		if got.err != nil || got.status.Running || got.status.Completed || got.status.CompletedAt != nil {
			t.Fatalf("old inspection restarted after invalidation: %+v err=%v", got.status, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inspection start did not return")
	}
	if refreshTaskCount(service) != 0 {
		t.Fatalf("old inspection queued tasks after invalidation: %+v", refreshTasks(service))
	}
}

func TestInvalidateStoredCostCacheDuringAutoRefreshScanDoesNotQueueOldRound(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := newQuotaRefreshService(t, db, NewProviderRegistry(map[string]ProviderHandler{"claude": &refreshHandlerStub{}}))
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	var once sync.Once
	callbackName := "test:block_auto_refresh_scan_during_invalidation"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_identities" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })
	result := make(chan error, 1)
	go func() { result <- service.RunAutoRefresh(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("automatic refresh scan did not block")
	}
	service.InvalidateStoredCostCache()
	releaseOnce()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("invalidated automatic refresh returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("automatic refresh scan did not finish")
	}
	if got := refreshTaskCount(service); got != 0 {
		t.Fatalf("old automatic refresh queued %d tasks after invalidation", got)
	}
}
