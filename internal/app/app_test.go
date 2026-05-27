package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestAppCloseClosesDatabase(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
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

func TestNewWithConfigBuildsQuotaAutoRefreshWhenEnabled(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.QuotaAutoRefresh == nil {
		t.Fatal("expected quota auto refresh runner when enabled")
	}
	if app.QuotaService == nil {
		t.Fatal("expected quota service to remain available for manual refresh")
	}
}

func TestNewWithConfigSkipsQuotaAutoRefreshWhenDisabled(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.QuotaAutoRefreshEnabled = false
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.QuotaAutoRefresh != nil {
		t.Fatal("expected quota auto refresh runner to be skipped when disabled")
	}
	if app.QuotaService == nil {
		t.Fatal("expected quota service to remain available for manual refresh when auto refresh is disabled")
	}
}

func TestQuotaActiveRecorderIsDisabledWithAutoRefresh(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.QuotaAutoRefreshEnabled = false
	if recorder := quotaActiveRecorder(cfg, nil); recorder != nil {
		t.Fatalf("expected disabled quota auto refresh to avoid active recorder, got %T", recorder)
	}
}

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
	quotaService := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": handler}))
	quotaService.SetRefreshContext(context.Background())
	app := &App{DB: db, QuotaService: quotaService}

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
	if handler.callCount() != 0 {
		t.Fatalf("expected canceled quota worker not to complete provider call, got %d calls", handler.callCount())
	}
}

func TestNewWithConfigBuildsRedisIngestAndRouter(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.Poller == nil {
		t.Fatal("expected poller status provider to be initialized")
	}
	if app.RedisIngest == nil {
		t.Fatal("expected redis ingest runner to be initialized")
	}
	if app.RedisProcess == nil {
		t.Fatal("expected redis process runner to be initialized")
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
}

func TestNewWithConfigExposesConfiguredCPAPublicURL(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.CPAPublicURL = "https://cpa.public.example.com/"
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
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

func TestNewWithConfigAggregatesExistingOverviewStatsBeforeRunnersStart(t *testing.T) {
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

	cfg := testAppConfig(t)
	cfg.SQLitePath = dbPath
	cfg.LogFileEnabled = true
	cfg.LogDir = logDir
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := app.DB.Where("name = ?", "overview").First(&checkpoint).Error; err != nil {
		t.Fatalf("load overview checkpoint returned error: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID == 0 {
		t.Fatalf("expected startup catch-up to aggregate legacy usage events, got checkpoint %+v", checkpoint)
	}
	logContent := readAppLogFile(t, logDir)
	if !strings.Contains(logContent, "starting usage overview aggregation catch-up") {
		t.Fatalf("expected startup catch-up start log, got %s", logContent)
	}
	if !strings.Contains(logContent, "completed usage overview aggregation catch-up") {
		t.Fatalf("expected startup catch-up completion log, got %s", logContent)
	}
}

func TestNewWithConfigSkipsBackupRunnerWhenDisabled(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.BackupEnabled = false
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.BackupMaintenance != nil {
		t.Fatal("expected database backup runner to be skipped when backups are disabled")
	}
}

func TestNewWithConfigSelectsRedisIngestRunners(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if _, ok := app.Poller.(*poller.RedisPoller); !ok {
		t.Fatalf("expected redis status provider to use redis poller, got %T", app.Poller)
	}
	if _, ok := app.RedisIngest.(*poller.RedisIngestRunner); !ok {
		t.Fatalf("expected redis ingest runner, got %T", app.RedisIngest)
	}
	if _, ok := app.RedisProcess.(*poller.RedisProcessRunner); !ok {
		t.Fatalf("expected redis process runner, got %T", app.RedisProcess)
	}
	if app.Maintenance == nil {
		t.Fatal("expected maintenance cleanup runner to be initialized")
	}
}

func TestNewWithConfigCreatesIndependentMaintenanceRunner(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.Poller == nil {
		t.Fatal("expected sync status provider to be initialized")
	}
	if app.RedisIngest == nil {
		t.Fatal("expected independent redis ingest runner to be initialized")
	}
	if app.RedisProcess == nil {
		t.Fatal("expected independent redis process runner to be initialized")
	}
	if app.Maintenance == nil {
		t.Fatal("expected independent maintenance runner to be initialized")
	}
}

func TestRunStartsPollerAndMaintenanceIndependently(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	pullStarted := make(chan struct{})
	processStarted := make(chan struct{})
	maintenanceStarted := make(chan struct{})
	metadataStarted := make(chan struct{})
	backupStarted := make(chan struct{})
	maintenance := NewStorageCleanupRunner(&maintenanceSyncStub{})
	maintenance.sleep = func(context.Context, time.Duration) bool {
		close(maintenanceStarted)
		return false
	}
	metadataRunner := NewMetadataSyncRunner(&metadataSyncStub{}, time.Second)
	metadataRunner.sleep = func(context.Context, time.Duration) bool {
		close(metadataStarted)
		return false
	}
	backupRunner := NewDatabaseBackupRunner(&databaseBackupWriterStub{}, nil, time.Second, 0)
	backupRunner.sleep = func(context.Context, time.Duration) bool {
		close(backupStarted)
		return false
	}
	statusProvider := &appRunStub{started: make(chan struct{})}
	app := &App{
		Config:            &cfg,
		Router:            gin.New(),
		Poller:            statusProvider,
		RedisIngest:       &appRunStub{started: pullStarted},
		RedisProcess:      &appRunStub{started: processStarted},
		Maintenance:       maintenance,
		MetadataSync:      metadataRunner,
		BackupMaintenance: backupRunner,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-pullStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis ingest runner to start")
	}
	select {
	case <-processStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis process runner to start")
	}
	select {
	case <-statusProvider.started:
		t.Fatal("expected poller status provider not to be started as a background runner")
	default:
	}
	select {
	case <-maintenanceStarted:
	case <-time.After(time.Second):
		t.Fatal("expected maintenance runner to start")
	}
	select {
	case <-metadataStarted:
	case <-time.After(time.Second):
		t.Fatal("expected metadata sync runner to start")
	}
	select {
	case <-backupStarted:
	case <-time.After(time.Second):
		t.Fatal("expected database backup runner to start")
	}
}

func TestRunSetsQuotaServiceContextEvenWhenAutoRefreshDisabled(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	cfg.QuotaAutoRefreshEnabled = false
	quotaService := &quotaContextRecorder{contextSet: make(chan context.Context, 1)}
	app := &App{
		Config:       &cfg,
		Router:       gin.New(),
		QuotaService: quotaService,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case ctx := <-quotaService.contextSet:
		if ctx == nil {
			t.Fatal("expected quota service context to be non-nil")
		}
	case <-time.After(time.Second):
		t.Fatal("expected quota service context to be set")
	}
}

func TestRunCancelsBackgroundTasksWhenRouterStops(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	backupStarted := make(chan struct{})
	backupCanceled := make(chan struct{})
	backupRunner := NewDatabaseBackupRunner(&databaseBackupWriterStub{}, nil, time.Second, 0)
	backupRunner.sleep = func(ctx context.Context, _ time.Duration) bool {
		close(backupStarted)
		<-ctx.Done()
		close(backupCanceled)
		return false
	}
	app := &App{
		Config:            &cfg,
		Router:            gin.New(),
		BackupMaintenance: backupRunner,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-backupStarted:
	case <-time.After(time.Second):
		t.Fatal("expected database backup runner to start")
	}
	select {
	case <-backupCanceled:
	case <-time.After(time.Second):
		t.Fatal("expected database backup runner context to be canceled")
	}
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

func (s *appQuotaHandlerStub) callCount() int {
	return s.calls
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
		AppPort:                 "8080",
		CPABaseURL:              "https://cpa.example.com",
		CPAManagementKey:        "secret",
		RedisQueueIdleInterval:  time.Second,
		RedisQueueErrorBackoff:  10 * time.Second,
		MetadataSyncInterval:    30 * time.Second,
		SQLitePath:              t.TempDir() + "/app.db",
		BackupEnabled:           true,
		BackupDir:               t.TempDir() + "/backups",
		BackupRetentionDays:     7,
		RequestTimeout:          5 * time.Second,
		QuotaAutoRefreshEnabled: true,
		LogLevel:                "info",
		LogFileEnabled:          false,
		LogRetentionDays:        7,
	}
}
