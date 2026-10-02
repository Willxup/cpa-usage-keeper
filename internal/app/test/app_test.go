package test

import (
	"bytes"
	"context"
	. "cpa-usage-keeper/internal/app"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func TestAppCloseWaitsForQuotaRefreshTasksBeforeDatabaseClose(t *testing.T) {
	waitCalled := make(chan struct{}, 1)
	quotaService := &quotaContextRecorder{contextSet: make(chan context.Context, 1), waitCalled: waitCalled}
	app := &App{QuotaService: quotaService}

	if err := app.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	select {
	case <-waitCalled:
	case <-time.After(time.Second):
		t.Fatal("expected App.Close to wait for quota refresh goroutines")
	}
}

func TestAppCloseStopsRealQuotaRefreshTasksBeforeDatabaseClose(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "quota-close.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	if err := db.Create(&entities.UsageIdentity{Identity: "auth-1", Name: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile}).Error; err != nil {
		t.Fatalf("seed usage identity returned error: %v", err)
	}
	block := make(chan struct{})
	handler := &appQuotaHandlerStub{block: block}
	quotaService := quota.NewServiceWithRegistry(
		db,
		quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": handler}),
	)
	quotaService.SetRefreshContext(context.Background())
	app := &App{DB: db, QuotaService: quotaService}
	t.Cleanup(func() { _ = app.Close() })

	response, err := quotaService.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: quota.RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	waitForAppQuotaTaskStatus(t, quotaService, response.Tasks[0].AuthIndex, quota.RefreshTaskStatusRunning)

	if err := app.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	task, err := quotaService.GetRefreshTaskByAuthIndex(context.Background(), response.Tasks[0].AuthIndex)
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error after close: %v", err)
	}
	if task.Status != quota.RefreshTaskStatusFailed {
		t.Fatalf("expected app close to cancel and drain real quota worker before closing DB, got %+v", task)
	}
	if handler.calls != 0 {
		t.Fatalf("expected canceled quota worker not to complete provider call, got %d calls", handler.calls)
	}
}

func TestNewWithConfigBuildsAndClosesApplication(t *testing.T) {
	app := newInitializedApp(t, testAppConfig(t))
	defer app.Close()
	if runner, ok := app.Poller.(*poller.RedisPoller); !ok || runner == nil {
		t.Fatalf("expected Poller to be an initialized RedisPoller, got %T", app.Poller)
	}
	if runner, ok := app.RedisIngest.(*poller.RedisIngestRunner); !ok || runner == nil {
		t.Fatalf("expected RedisIngest to be an initialized RedisIngestRunner, got %T", app.RedisIngest)
	}
	if runner, ok := app.RedisProcess.(*poller.RedisProcessRunner); !ok || runner == nil {
		t.Fatalf("expected RedisProcess to be an initialized RedisProcessRunner, got %T", app.RedisProcess)
	}
	if app.UsageAggregation == nil {
		t.Fatal("expected usage aggregation runner to be initialized")
	}
	if app.Router == nil {
		t.Fatal("expected router to be initialized")
	}
	if app.LogCloser == nil {
		t.Fatal("expected log closer to be initialized")
	}
	if app.BackupMaintenance == nil {
		t.Fatal("expected database backup runner to be initialized")
	}
	if app.MetadataSync == nil {
		t.Fatal("expected metadata sync runner to be initialized")
	}
	if app.QuotaAutoRefresh == nil {
		t.Fatal("expected quota scheduled refresh runner to be initialized")
	}
	if app.QuotaService == nil {
		t.Fatal("expected quota service to remain available for manual refresh")
	}
	if app.Maintenance == nil {
		t.Fatal("expected maintenance cleanup runner to be initialized")
	}
	sqlDB, err := app.DB.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}
	if err := app.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected database ping to fail after app close")
	}
}

func TestNewWithConfigWiresMetadataRefreshControl(t *testing.T) {
	app := newInitializedApp(t, testAppConfig(t))
	defer app.Close()

	runner, ok := app.RedisIngest.(*poller.RedisIngestRunner)
	if !ok {
		t.Fatalf("expected redis ingest runner, got %T", app.RedisIngest)
	}
	runnerValue := reflect.ValueOf(runner).Elem()
	writer := runnerValue.FieldByName("writer")
	if writer.IsNil() {
		t.Fatal("expected redis ingest writer")
	}
	if got := writer.Elem().Type().String(); got != "*poller.ControlAwareRedisInboxWriter" {
		t.Fatalf("expected control-aware redis inbox writer, got %s", got)
	}
	observer := runnerValue.FieldByName("controlObserver")
	if observer.IsNil() {
		t.Fatal("expected metadata sync runner to observe redis ingest control messages")
	}
	if got := observer.Elem().Type().String(); got != "*app.UsageIngestBridge" {
		t.Fatalf("expected one startup-to-business ingest bridge, got %s", got)
	}
	writerObserver := writer.Elem().Elem().FieldByName("observer")
	if writerObserver.IsNil() {
		t.Fatal("expected redis inbox writer observer")
	}
	if got := writerObserver.Elem().Type().String(); got != "*app.UsageIngestBridge" {
		t.Fatalf("expected redis inbox writer to share the ingest bridge, got %s", got)
	}
	if got := writerObserver.Elem().Pointer(); got != observer.Elem().Pointer() {
		t.Fatalf("expected control and writer to share the same bridge, got %x want %x", got, observer.Elem().Pointer())
	}
}

func TestNewWithConfigExposesConfiguredCPAPublicURL(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.CPAPublicURL = "https://cpa.public.example.com/"
	app := newInitializedApp(t, cfg)
	defer app.Close()

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	app.Router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"cpa_public_url":"https://cpa.public.example.com/"`) {
		t.Fatalf("expected CPA public URL in status response, got %s", body)
	}
	if strings.Contains(body, "cpa_management_url") {
		t.Fatalf("expected status response to use cpa_public_url instead of cpa_management_url, got %s", body)
	}
}

func TestNewWithConfigLeavesExistingUsageForBackgroundAggregationRunner(t *testing.T) {
	// 准备：创建包含未聚合历史事件的旧数据库，并关闭 seed 连接模拟真实重启。
	dbPath := filepath.Join(t.TempDir(), "app-startup-overview-catchup.db")
	seedDB, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(seedDB, []entities.UsageEvent{
		{EventKey: "legacy-event", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), TotalTokens: 150},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	seedSQL, err := seedDB.DB()
	if err != nil {
		t.Fatalf("load seed sql db: %v", err)
	}
	if err := seedSQL.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	logDir := t.TempDir()

	// 执行：构造 App，但不启动后台 Runner。
	cfg := testAppConfig(t)
	cfg.SQLitePath = dbPath
	cfg.LogFileEnabled = true
	cfg.LogDir = logDir
	app := newInitializedApp(t, cfg)
	defer app.Close()

	// 断言：构造阶段不做同步 catch-up，工作保留给 App.Run 启动的后台任务。
	var checkpointCount int64
	if err := app.DB.Model(&entities.UsageAggregationCheckpoint{}).Where("name = ?", entities.UsageAggregationCheckpointOverview).Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count overview checkpoints returned error: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("expected constructor not to block on overview catch-up, got %d checkpoints", checkpointCount)
	}
	logContent := readAppLogFile(t, logDir)
	if strings.Contains(logContent, "usage overview aggregation catch-up") {
		t.Fatalf("expected constructor log without synchronous catch-up, got %s", logContent)
	}
}

func TestInitializeRejectsRecentUsageCacheFailureWithoutClosingDatabase(t *testing.T) {
	cacheErr := errors.New("recent cache unavailable")
	previousNewRecentUsageCache := newUsageRecentEventCache
	newUsageRecentEventCache = func(*gorm.DB, repository.UsageRecentEventCacheOptions) (*repository.UsageRecentEventCache, error) {
		return nil, cacheErr
	}
	t.Cleanup(func() { newUsageRecentEventCache = previousNewRecentUsageCache })

	cfg := testAppConfig(t)
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if err := app.Initialize(context.Background()); !errors.Is(err, cacheErr) {
		t.Fatalf("expected cache initialization failure, got %v", err)
	}
	if app.RecentUsageCache != nil {
		t.Fatalf("expected recent usage cache to be nil after initialization failure, got %T", app.RecentUsageCache)
	}
	if app.DB == nil {
		t.Fatal("failed startup closed durable inbox database before App.Close")
	}
	sqlDB, err := app.DB.DB()
	if err != nil || sqlDB.Ping() != nil {
		t.Fatalf("failed startup database is unavailable: %v", err)
	}
}

func TestNewWithConfigSkipsBackupRunnerWhenDisabled(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.BackupEnabled = false
	app := newInitializedApp(t, cfg)
	defer app.Close()
	if app.BackupMaintenance != nil {
		t.Fatal("expected database backup runner to be skipped when backups are disabled")
	}
}

func TestRunDoesNotStartWorkersBeforeShellListens(t *testing.T) {
	// 无法监听时不能先让业务 worker 使用尚未迁移的库。
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	pullStarted := make(chan struct{})
	processStarted := make(chan struct{})
	aggregationStarted := make(chan struct{})
	maintenanceStarted := make(chan struct{})
	metadataStarted := make(chan struct{}, 1)
	backupStarted := make(chan struct{})
	maintenance := NewStorageCleanupRunner(&maintenanceSyncStub{})
	(*appTestField[func(context.Context, time.Duration) bool](maintenance, "sleep")) = func(context.Context, time.Duration) bool {
		close(maintenanceStarted)
		return false
	}
	metadataRunner := NewMetadataSyncRunner(&metadataSyncStub{}, time.Second)
	(*appTestField[func()](metadataRunner, "onStart")) = func() {
		select {
		case metadataStarted <- struct{}{}:
		default:
		}
	}
	backupRunner := NewDatabaseBackupRunner(&databaseBackupWriterStub{}, nil, time.Second, 0)
	(*appTestField[func(context.Context, time.Duration) bool](backupRunner, "sleep")) = func(context.Context, time.Duration) bool {
		close(backupStarted)
		return false
	}
	statusProvider := &appRunStub{started: make(chan struct{})}
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Poller, app.RedisIngest, app.RedisProcess = statusProvider, &appRunStub{started: pullStarted}, &appRunStub{started: processStarted}
	app.UsageAggregation, app.Maintenance, app.MetadataSync, app.BackupMaintenance = &appRunStub{started: aggregationStarted}, maintenance, metadataRunner, backupRunner

	// 监听失败先于初始化，所有 worker 均不得启动。
	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	for name, started := range map[string]<-chan struct{}{
		"redis ingest":      pullStarted,
		"redis process":     processStarted,
		"usage aggregation": aggregationStarted,
		"maintenance":       maintenanceStarted,
		"metadata sync":     metadataStarted,
		"backup":            backupStarted,
	} {
		select {
		case <-started:
			t.Fatalf("%s runner started before the startup shell", name)
		default:
		}
	}
	select {
	case <-statusProvider.started:
		t.Fatal("expected poller status provider not to be started as a background runner")
	default:
	}
}

func TestRunSetsQuotaServiceContext(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	quotaService := &quotaContextRecorder{contextSet: make(chan context.Context, 1)}
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.QuotaService = quotaService

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-quotaService.contextSet:
		t.Fatal("quota context was installed before the startup shell listened")
	default:
	}
}

func TestRunCancelsBackgroundTasksWhenRouterStops(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	backupStarted := make(chan struct{})
	backupCanceled := make(chan struct{})
	backupRunner := NewDatabaseBackupRunner(&databaseBackupWriterStub{}, nil, time.Second, 0)
	(*appTestField[func(context.Context, time.Duration) bool](backupRunner, "sleep")) = func(ctx context.Context, _ time.Duration) bool {
		close(backupStarted)
		<-ctx.Done()
		close(backupCanceled)
		return false
	}
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.BackupMaintenance = backupRunner

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-backupStarted:
		t.Fatal("backup runner started before the startup shell listened")
	default:
	}
	_ = backupCanceled
}

type quotaContextRecorder struct {
	contextSet chan context.Context
	waitCalled chan struct{}
}

func (r *quotaContextRecorder) SetRefreshContext(ctx context.Context) {
	r.contextSet <- ctx
}

func (r *quotaContextRecorder) StartAutoRefresh(context.Context) error {
	return nil
}

func (r *quotaContextRecorder) WaitRefreshTasks() {
	if r.waitCalled != nil {
		r.waitCalled <- struct{}{}
	}
}

func (r *quotaContextRecorder) StopRefreshTasks() {
	r.WaitRefreshTasks()
}

type appQuotaHandlerStub struct {
	block <-chan struct{}
	calls int
}

func (s *appQuotaHandlerStub) Check(ctx context.Context, input quota.ProviderInput) (quota.ProviderOutput, error) {
	select {
	case <-ctx.Done():
		return quota.ProviderOutput{}, ctx.Err()
	case <-s.block:
	}
	s.calls++
	return quota.ProviderOutput{Result: quota.ClaudeResult{Usage: &quota.ClaudeUsagePayload{FiveHour: &quota.ClaudeUsageWindow{Utilization: 25}}}}, nil
}

func waitForAppQuotaTaskStatus(t *testing.T, service *quota.Service, authIndex string, status quota.RefreshTaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var task quota.RefreshTaskResponse
	var err error
	for time.Now().Before(deadline) {
		task, err = service.GetRefreshTaskByAuthIndex(context.Background(), authIndex)
		if err == nil && task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("auth_index %s did not reach status %s, last task=%+v err=%v", authIndex, status, task, err)
}

type appRunStub struct {
	started chan struct{}
}

func (s *appRunStub) Run(context.Context) error {
	close(s.started)
	return nil
}

func (s *appRunStub) Status() poller.Status {
	return poller.Status{}
}

func captureAppInfoLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func readAppLogFile(t *testing.T, logDir string) string {
	t.Helper()
	path := filepath.Join(logDir, "cpa-usage-keeper-"+time.Now().Format("2006-01-02")+".log")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read app log file: %v", err)
	}
	return string(content)
}

func testAppConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		AppPort:                "8080",
		CPABaseURL:             "https://cpa.example.com",
		CPAManagementKey:       "secret",
		RedisQueueIdleInterval: time.Second,
		MetadataSyncInterval:   30 * time.Second,
		SQLitePath:             t.TempDir() + "/app.db",
		BackupEnabled:          true,
		BackupDir:              t.TempDir() + "/backups",
		BackupRetentionDays:    7,
		RequestTimeout:         5 * time.Second,
		LogLevel:               "info",
		LogFileEnabled:         false,
		LogRetentionDays:       7,
	}
}
