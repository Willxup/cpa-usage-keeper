package test

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/backup"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestNewWithConfigUsesIndependentFourConnectionFileReader(t *testing.T) {
	// 准备：文件数据库应在 writer 初始化完成后创建独立硬只读池。
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = application.Close()
		}
	})

	// 断言：文件库的 reader 与 writer 必须是两个不同 GORM 池。
	if application.ReadDB == nil || application.ReadDB == application.DB {
		t.Fatalf("expected independent file reader, write=%p read=%p", application.DB, application.ReadDB)
	}
	writeSQL, err := application.DB.DB()
	if err != nil {
		t.Fatalf("load write sql db: %v", err)
	}
	readSQL, err := application.ReadDB.DB()
	if err != nil {
		t.Fatalf("load read sql db: %v", err)
	}
	// 断言：App 对外 reader 使用固定四连接上限，writer 仍保持唯一连接。
	if stats := writeSQL.Stats(); stats.MaxOpenConnections != 1 {
		t.Fatalf("expected writer max open connections to be 1, got %+v", stats)
	}
	if stats := readSQL.Stats(); stats.MaxOpenConnections != 4 {
		t.Fatalf("expected reader max open connections to be 4, got %+v", stats)
	}

	// 执行：统一关闭入口必须依次释放两个不同的底层池。
	if err := application.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	closed = true
	// 断言：关闭后 writer 与 reader 都不能再接受连接。
	if err := writeSQL.Ping(); err == nil {
		t.Fatal("expected writer ping to fail after App.Close")
	}
	if err := readSQL.Ping(); err == nil {
		t.Fatal("expected reader ping to fail after App.Close")
	}
}

func TestOverviewBypassesOccupiedFileWriter(t *testing.T) {
	// 准备：构造文件数据库 App，并独占唯一 writer 模拟 usage 入库或聚合正在使用连接。
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer application.Close()
	writeSQL, err := application.DB.DB()
	if err != nil {
		t.Fatalf("load write sql db: %v", err)
	}
	heldWriter, err := writeSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold write database connection: %v", err)
	}
	defer heldWriter.Close()

	// 执行：Overview 请求设置一秒上限，错误接到 writer 时会等待到 context 超时。
	requestContext, cancelRequest := context.WithTimeout(context.Background(), time.Second)
	defer cancelRequest()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?range=24h", nil).WithContext(requestContext)
	application.Router.ServeHTTP(response, request)

	// 断言：主 Overview 必须从独立 reader 完成，不能排队等待被占用的 writer。
	if response.Code != http.StatusOK {
		t.Fatalf("expected Overview to bypass occupied writer, got %d %s", response.Code, response.Body.String())
	}
}

func TestAPIKeyListBypassesOccupiedFileWriter(t *testing.T) {
	// 准备：API Key service 同时包含查询和 alias 更新；纯列表查询必须由统一 DB 自动路由到 reader。
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer application.Close()
	if err := application.DB.Create(&entities.CPAAPIKey{APIKey: "sk-reader-route", DisplayKey: "sk-*********route"}).Error; err != nil {
		t.Fatalf("seed CPA API key: %v", err)
	}
	writeSQL, err := application.DB.DB()
	if err != nil {
		t.Fatalf("load write sql db: %v", err)
	}
	heldWriter, err := writeSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold write database connection: %v", err)
	}
	writerHeld := true
	defer func() {
		if writerHeld {
			_ = heldWriter.Close()
		}
	}()

	// 执行：请求仍使用原 API；若 App 把混合 service 整体绑定 writer，请求会一直等待唯一写连接。
	requestDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/usage/api-keys", nil)
		application.Router.ServeHTTP(response, request)
		requestDone <- response
	}()

	// 断言：纯 List 查询必须在 writer 仍被占用时从 reader 返回，不需要修改 API/service 查询代码。
	select {
	case response := <-requestDone:
		if response.Code != http.StatusOK {
			t.Fatalf("expected API Key list to bypass occupied writer, got %d %s", response.Code, response.Body.String())
		}
	case <-time.After(time.Second):
		if err := heldWriter.Close(); err != nil {
			t.Fatalf("release writer after route timeout: %v", err)
		}
		writerHeld = false
		t.Fatal("API Key list waited for occupied writer instead of using reader")
	}

	if err := heldWriter.Close(); err != nil {
		t.Fatalf("release writer connection: %v", err)
	}
	writerHeld = false
}

func TestUsageEventExportBypassesOccupiedFileWriter(t *testing.T) {
	// 准备：导出属于纯查询；先写入一条当前事件，再独占唯一 writer。
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer application.Close()
	event := entities.UsageEvent{EventKey: "reader-export-event", Model: "reader-export-model", Timestamp: time.Now()}
	if err := application.DB.Create(&event).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	writeSQL, err := application.DB.DB()
	if err != nil {
		t.Fatalf("load write sql db: %v", err)
	}
	heldWriter, err := writeSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold write database connection: %v", err)
	}
	defer heldWriter.Close()

	// 执行：真实 JSON 导出会加载 identity、API Key 并流式读取 usage_events，全部应自动走 reader。
	requestContext, cancelRequest := context.WithTimeout(context.Background(), time.Second)
	defer cancelRequest()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/usage/events/export?range=24h&format=json", nil).WithContext(requestContext)
	application.Router.ServeHTTP(response, request)

	// 断言：writer 被占用时导出仍成功，且结果包含刚才已经提交的事件。
	if response.Code != http.StatusOK {
		t.Fatalf("expected export to bypass occupied writer, got %d %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(event.Model)) {
		t.Fatalf("expected export to contain committed event, got %s", response.Body.String())
	}
}

func TestAPIKeyAliasUpdateStaysOnWriterWhenReadersAreOccupied(t *testing.T) {
	// 准备：写命令会先 UPDATE 再回读最新 API Key；整个命令必须固定使用 writer。
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer application.Close()
	apiKey := entities.CPAAPIKey{APIKey: "sk-writer-command", DisplayKey: "sk-*********command"}
	if err := application.DB.Create(&apiKey).Error; err != nil {
		t.Fatalf("seed CPA API key: %v", err)
	}
	readSQL, err := application.ReadDB.DB()
	if err != nil {
		t.Fatalf("load read sql db: %v", err)
	}
	readers := make([]*sql.Conn, 0, 4)
	for index := 0; index < 4; index++ {
		reader, err := readSQL.Conn(context.Background())
		if err != nil {
			t.Fatalf("hold reader connection %d: %v", index, err)
		}
		readers = append(readers, reader)
	}
	readersHeld := true
	defer func() {
		if readersHeld {
			for _, reader := range readers {
				_ = reader.Close()
			}
		}
	}()

	// 执行：如果写后回读被自动切到 reader，请求会在 UPDATE 已提交后卡在被占满的读池。
	requestDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/usage/api-keys/"+strconv.FormatInt(apiKey.ID, 10), bytes.NewBufferString(`{"keyAlias":"primary"}`))
		request.Header.Set("X-CPA-Usage-Keeper-Request", "fetch")
		request.Header.Set("Content-Type", "application/json")
		application.Router.ServeHTTP(response, request)
		requestDone <- response
	}()

	// 断言：写命令及其回读应只使用 writer，不依赖任何可用 reader。
	select {
	case response := <-requestDone:
		if response.Code != http.StatusOK {
			t.Fatalf("expected API Key update to stay on writer, got %d %s", response.Code, response.Body.String())
		}
	case <-time.After(time.Second):
		for _, reader := range readers {
			_ = reader.Close()
		}
		readersHeld = false
		t.Fatal("API Key update switched to occupied reader during write command")
	}

	for _, reader := range readers {
		if err := reader.Close(); err != nil {
			t.Fatalf("release reader connection: %v", err)
		}
	}
	readersHeld = false
}

func TestRedisUsageProcessingStaysOnWriterWhenReadersAreOccupied(t *testing.T) {
	// 准备：写入需要 identity 查询的 inbox；该查询与后续事务共同属于一次 usage 写命令。
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer application.Close()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	identity := entities.UsageIdentity{
		Name: "writer-usage-identity", AuthType: entities.UsageIdentityAuthTypeAuthFile,
		Identity: "writer-usage-identity", Type: "codex",
	}
	if err := application.DB.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	inboxRows, err := repository.InsertRedisUsageInboxRawMessages(application.DB, "redis_pull:usage", []string{`{
		"timestamp":"2026-07-22T11:59:00Z",
		"provider":"codex",
		"auth_type":"oauth",
		"auth_index":"writer-usage-identity",
		"model":"gpt-5.5",
		"request_id":"writer-usage-processing",
		"tokens":{"input_tokens":10,"output_tokens":2,"total_tokens":12}
	}`}, now)
	if err != nil {
		t.Fatalf("seed redis usage inbox: %v", err)
	}
	notifier := &databasePoolUsageAggregationNotifier{}
	syncService := service.NewSyncServiceWithOptions(application.DB, service.SyncServiceOptions{
		BaseURL: "https://cpa.example.com", Now: func() time.Time { return now }, UsageAggregationNotifier: notifier,
	})
	readers, releaseReaders := holdDatabasePoolReaders(t, application.ReadDB)
	readersHeld := true
	defer func() {
		if readersHeld {
			releaseReaders()
		}
	}()

	// 执行：reader 全满时处理一批 usage；若列表或 identity 查询仍自动分流，命令无法到达 writer 事务。
	processDone := make(chan databasePoolUsageProcessResult, 1)
	go func() {
		result, processErr := syncService.ProcessRedisUsageInbox(context.Background())
		processDone <- databasePoolUsageProcessResult{result: result, err: processErr}
	}()

	// 断言：整个写命令不依赖任何 reader，并保持原有 completed、事件通知和 processed 状态。
	select {
	case processResult := <-processDone:
		if processResult.err != nil {
			t.Fatalf("ProcessRedisUsageInbox returned error: %v", processResult.err)
		}
		if processResult.result == nil || processResult.result.Status != "completed" || processResult.result.InsertedEvents != 1 {
			t.Fatalf("unexpected redis usage process result: %+v", processResult.result)
		}
	case <-time.After(time.Second):
		releaseReaders()
		readersHeld = false
		<-processDone
		t.Fatalf("redis usage processing waited for %d occupied readers", len(readers))
	}

	releaseReaders()
	readersHeld = false
	if notifier.usageCalls != 1 {
		t.Fatalf("expected one committed usage notification, got %d", notifier.usageCalls)
	}
	var storedInbox entities.RedisUsageInbox
	if err := application.DB.First(&storedInbox, inboxRows[0].ID).Error; err != nil {
		t.Fatalf("load processed redis usage inbox: %v", err)
	}
	if storedInbox.Status != repository.RedisUsageInboxStatusProcessed {
		t.Fatalf("expected processed inbox, got %+v", storedInbox)
	}
}

func TestUsageAggregationStaysOnWriterWhenReadersAreOccupied(t *testing.T) {
	// 准备：提交一条 usage event 和对应 header snapshot，覆盖聚合前门禁、写事务与提交后 cursor 回读。
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer application.Close()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	event := entities.UsageEvent{
		EventKey: "writer-aggregation-event", APIGroupKey: "provider-a", Model: "gpt-5.5",
		AuthType: "oauth", AuthIndex: "writer-aggregation-identity", Timestamp: now.Add(-time.Minute), TotalTokens: 12,
	}
	if err := application.DB.Create(&event).Error; err != nil {
		t.Fatalf("seed aggregation usage event: %v", err)
	}
	appender := &databasePoolUsageHeaderAppender{}
	runner := poller.NewUsageAggregationRunner(application.DB, appender)
	runner.NotifyUsageEventsCommitted([]entities.UsageEvent{event}, []quota.UsageHeaderSnapshot{{
		AuthType: "oauth", AuthIndex: event.AuthIndex, Provider: "codex", ObservedAt: now,
	}})
	readers, releaseReaders := holdDatabasePoolReaders(t, application.ReadDB)
	readersHeld := true
	defer func() {
		if readersHeld {
			releaseReaders()
		}
	}()

	// 执行并断言：reader 全满时依次运行三类聚合；普通自动路由会在门禁、事务查询或提交后 cursor 处等待。
	for _, expectedKind := range []poller.UsageAggregationKind{
		poller.UsageAggregationKindOverview,
		poller.UsageAggregationKindActivity,
		poller.UsageAggregationKindIdentity,
	} {
		runDone := make(chan databasePoolAggregationRunResult, 1)
		go func() {
			result, runErr := runner.RunOnce(context.Background())
			runDone <- databasePoolAggregationRunResult{result: result, err: runErr}
		}()
		select {
		case runResult := <-runDone:
			if runResult.err != nil {
				t.Fatalf("%s RunOnce returned error: %v", expectedKind, runResult.err)
			}
			if runResult.result.Kind != expectedKind {
				t.Fatalf("expected %s aggregation, got %+v", expectedKind, runResult.result)
			}
		case <-time.After(time.Second):
			releaseReaders()
			readersHeld = false
			<-runDone
			t.Fatalf("%s aggregation waited for %d occupied readers", expectedKind, len(readers))
		}
	}

	releaseReaders()
	readersHeld = false
	if appender.callCount != 1 {
		t.Fatalf("expected one header snapshot append after Overview commit, got %d", appender.callCount)
	}
}

func TestDatabaseBackupWaitsForFileWriterAndPreservesCommittedData(t *testing.T) {
	// 准备：开启真实备份并写入一条已提交事件，随后独占 writer 复现旧版串行备份语义。
	backupDir := filepath.Join(t.TempDir(), "backups")
	cfg := databasePoolTestConfig(filepath.Join(t.TempDir(), "app.db"))
	cfg.BackupEnabled = true
	cfg.BackupDir = backupDir
	cfg.BackupInterval = time.Hour
	cfg.BackupRetentionDays = 7
	application, err := keeperapp.NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer application.Close()
	if application.BackupMaintenance == nil {
		t.Fatal("expected backup maintenance runner")
	}
	event := entities.UsageEvent{EventKey: "writer-backup-event", Model: "model-a", Timestamp: time.Now()}
	if err := application.DB.Create(&event).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	writeSQL, err := application.DB.DB()
	if err != nil {
		t.Fatalf("load write sql db: %v", err)
	}
	heldWriter, err := writeSQL.Conn(context.Background())
	if err != nil {
		t.Fatalf("hold write database connection: %v", err)
	}
	writerHeld := true
	defer func() {
		if writerHeld {
			_ = heldWriter.Close()
		}
	}()

	// 执行：真实 runner 启动后先保持 writer 被占用；备份不得改用可并发重启的 reader 源。
	runnerContext, cancelRunner := context.WithCancel(context.Background())
	defer cancelRunner()
	runnerDone := make(chan error, 1)
	waitCountBefore := writeSQL.Stats().WaitCount
	go func() {
		runnerDone <- application.BackupMaintenance.Run(runnerContext)
	}()
	// 等待 database/sql 记录备份对 writer 的真实排队；若错误使用 reader，备份文件会先出现并立即使测试失败。
	waitDeadline := time.Now().Add(time.Second)
	for writeSQL.Stats().WaitCount == waitCountBefore {
		files, listErr := backup.ListFiles(backupDir)
		if listErr != nil {
			t.Fatalf("list backup files while writer is occupied: %v", listErr)
		}
		if len(files) != 0 {
			t.Fatalf("expected backup to wait for writer, got files %v", files)
		}
		if time.Now().After(waitDeadline) {
			t.Fatal("backup did not begin waiting for the occupied writer")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := heldWriter.Close(); err != nil {
		t.Fatalf("release write database connection: %v", err)
	}
	writerHeld = false
	backupPath := waitForDatabaseBackupFile(t, backupDir)
	cancelRunner()
	select {
	case runErr := <-runnerDone:
		if runErr != nil {
			t.Fatalf("backup runner returned error: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("backup runner did not stop after cancellation")
	}

	// 断言：最终备份文件可以通过硬只读入口打开，并包含 writer 已提交的数据。
	backupDB, err := repository.OpenReadDatabase(config.Config{SQLitePath: backupPath})
	if err != nil {
		t.Fatalf("open backup database: %v", err)
	}
	defer closeDatabasePoolTestDB(t, backupDB)
	var count int64
	if err := backupDB.Model(&entities.UsageEvent{}).Where("event_key = ?", event.EventKey).Count(&count).Error; err != nil {
		t.Fatalf("read backup usage event: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected backup to contain committed usage event, got %d rows", count)
	}
}

type databasePoolUsageProcessResult struct {
	result *servicedto.RedisBatchSyncResult
	err    error
}

type databasePoolAggregationRunResult struct {
	result poller.UsageAggregationRunResult
	err    error
}

type databasePoolUsageAggregationNotifier struct {
	usageCalls int
}

func (n *databasePoolUsageAggregationNotifier) NotifyUsageEventsCommitted([]entities.UsageEvent, []quota.UsageHeaderSnapshot) {
	n.usageCalls++
}

func (*databasePoolUsageAggregationNotifier) NotifyUsageIdentitiesChanged() {}

type databasePoolUsageHeaderAppender struct {
	callCount int
}

func (a *databasePoolUsageHeaderAppender) TryAppendUsageHeaderSnapshots([]quota.UsageHeaderSnapshot) bool {
	a.callCount++
	return true
}

func holdDatabasePoolReaders(t *testing.T, readDB interface{ DB() (*sql.DB, error) }) ([]*sql.Conn, func()) {
	// helper 只占用 reader 底层连接，不执行事务或改写任何数据库状态。
	t.Helper()
	readSQL, err := readDB.DB()
	if err != nil {
		t.Fatalf("load read sql db: %v", err)
	}
	readers := make([]*sql.Conn, 0, 4)
	for index := 0; index < 4; index++ {
		reader, openErr := readSQL.Conn(context.Background())
		if openErr != nil {
			for _, openedReader := range readers {
				_ = openedReader.Close()
			}
			t.Fatalf("hold reader connection %d: %v", index, openErr)
		}
		readers = append(readers, reader)
	}
	release := func() {
		for _, reader := range readers {
			if closeErr := reader.Close(); closeErr != nil {
				t.Errorf("release reader connection: %v", closeErr)
			}
		}
	}
	return readers, release
}

func waitForDatabaseBackupFile(t *testing.T, backupDir string) string {
	// 用有界短轮询等待 runner 完成原子 rename，只接受最终 .db 文件而不是临时文件。
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		files, err := backup.ListFiles(backupDir)
		if err != nil {
			t.Fatalf("list backup files: %v", err)
		}
		if len(files) > 0 {
			return files[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("database backup file was not created before timeout")
	return ""
}

func closeDatabasePoolTestDB(t *testing.T, db interface{ DB() (*sql.DB, error) }) {
	// 测试通过统一 helper 关闭 GORM 底层池，避免备份文件在 cleanup 前仍被占用。
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
}
