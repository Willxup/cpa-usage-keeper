package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	webui "cpa-usage-keeper/web"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Runner 是 App 后台任务的最小接口，具体语义由字段名和实现方法表达。
type Runner interface {
	Run(ctx context.Context) error
}

// StatusProvider 只提供运行状态，不作为后台 runner 启动。
type StatusProvider interface {
	Status() poller.Status
}

type Options struct {
	EnvFile string
}

type QuotaRunner interface {
	SetRefreshContext(context.Context)
	StopRefreshTasks()
	WaitRefreshTasks()
	StartAutoRefresh(context.Context) error
}

type App struct {
	Config            *config.Config
	DB                *gorm.DB
	Router            *gin.Engine
	Poller            StatusProvider
	RedisIngest       Runner
	RedisProcess      Runner
	Maintenance       *StorageCleanupRunner
	MetadataSync      *MetadataSyncRunner
	QuotaService      QuotaRunner
	QuotaAutoRefresh  QuotaRunner
	BackupMaintenance *DatabaseBackupRunner
	RecentUsageCache  *repository.UsageRecentEventCache
	LogCloser         io.Closer

	backgroundCancel context.CancelFunc
	backgroundWG     sync.WaitGroup
}

// newUsageRecentEventCache 是最近事件缓存构造入口，测试可替换它来覆盖缓存初始化失败路径。
var newUsageRecentEventCache = repository.NewUsageRecentEventCache

func New() (*App, error) {
	return NewWithOptions(Options{})
}

func NewWithOptions(options Options) (*App, error) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: options.EnvFile})
	if err != nil {
		return nil, err
	}

	return NewWithConfig(*cfg)
}

func NewWithConfig(cfg config.Config) (*App, error) {
	logCloser, err := logging.Configure(cfg)
	if err != nil {
		return nil, err
	}

	db, err := repository.OpenDatabase(cfg)
	if err != nil {
		_ = logCloser.Close()
		return nil, err
	}
	// migrations 完成后、后台 runner 启动前先追平 Overview 增量表，避免首个 Overview 请求触发大批量聚合。
	logrus.Info("starting usage overview aggregation catch-up")
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, time.Now()); err != nil {
		_ = closeGormDB(db)
		_ = logCloser.Close()
		return nil, err
	}
	logrus.Info("completed usage overview aggregation catch-up")

	// 最近事件缓存只在增量表追平后创建，确保启动时能加载完整的最近 70 分钟事件投影。
	recentUsageCache, err := newUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{})
	if err != nil {
		// 缓存初始化失败会让 realtime/最近边界降级到 DB，但不影响核心写入和查询能力。
		logrus.WithError(err).Error("recent usage event cache initialization failed; falling back to database queries")
		recentUsageCache = nil
	}

	cpaClient := cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify)
	quotaService := quota.NewServiceWithOptions(db, cpaClient, quota.ServiceOptions{RefreshWorkerLimit: cfg.QuotaRefreshWorkerLimit})
	// syncService 仍然是 metadata 和 usage 处理共享的业务服务入口。
	syncService := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL:                   cfg.CPABaseURL,
		Client:                    cpaClient,
		CleanupUsageEventsEnabled: cfg.CleanupUsageEventsEnabled,
		// usage_events 事务提交后通过这个缓存做非阻塞增量追加，供 Overview realtime 和右边界补偿复用。
		RecentUsageEvents: recentUsageCache,
		// Redis usage response_headers 提交后异步 patch quota cache，不参与 usage_events 入库事务。
		UsageHeaderQuota: quotaService,
	})
	// metadataSyncRunner 提前创建，保证控制消息和后台任务使用同一个调度器实例。
	metadataSyncRunner := NewMetadataSyncRunner(syncService, cfg.MetadataSyncInterval)
	// redisPullSource 负责 Redis batch pull，并在 usage/queue 两个 key 间做一次兼容探测。
	redisPullSource := poller.NewRedisPullSource(cpa.RedisQueueOptions{
		BaseURL:       cfg.CPABaseURL,
		RedisAddr:     cfg.RedisQueueAddr,
		ManagementKey: cfg.CPAManagementKey,
		Timeout:       cfg.RequestTimeout,
		BatchSize:     cfg.RedisQueueBatchSize,
		TLS:           cfg.RedisQueueTLS,
		TLSSkipVerify: cfg.TLSSkipVerify,
	})
	// httpPullSource 保持 HTTP usage queue 兜底路径不变。
	httpPullSource := poller.NewHTTPPullSource(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify, cfg.RedisQueueBatchSize)
	// redisSubscribeSource 保持 Redis SUBSCRIBE 优先路径不变。
	redisSubscribeSource := poller.NewRedisSubscribeSource(poller.RedisSubscribeOptions{
		BaseURL:       cfg.CPABaseURL,
		RedisAddr:     cfg.RedisQueueAddr,
		ManagementKey: cfg.CPAManagementKey,
		Timeout:       cfg.RequestTimeout,
		TLS:           cfg.RedisQueueTLS,
		TLSSkipVerify: cfg.TLSSkipVerify,
	})
	// usage 通道可能混入 metadata 控制消息，落 inbox 前先过滤并转交 metadata runner。
	// inbox writer 不再接收 queue key，来源由 runner 传入并写入 redis_usage_inboxes.source。
	redisInboxWriter := poller.NewControlAwareRedisInboxWriter(poller.NewRedisInboxWriter(db), metadataSyncRunner)
	// redisIngestRunner 继续负责三种 usage 拉取方式的选择和降级。
	redisIngestRunner := poller.NewRedisIngestRunner(redisSubscribeSource, redisPullSource, httpPullSource, redisInboxWriter, poller.RedisIngestRunnerConfig{
		IdleInterval:       cfg.RedisQueueIdleInterval,
		BatchSize:          cfg.RedisQueueBatchSize,
		HTTPBackoffInitial: time.Second,
		HTTPBackoffMax:     30 * time.Second,
	})
	// usage 链路一旦降级或失败，metadata 同步回到轮询，直到下一条 CPA 控制消息重新启用通知模式。
	redisIngestRunner.SetControlMessageObserver(metadataSyncRunner)
	// redisProcessRunner 仍然只处理本地 inbox 到 usage_events 的消费。
	redisProcessRunner := poller.NewRedisProcessRunner(syncService)
	// backgroundPoller 继续组合远端 ingest 和本地 process 的状态展示。
	backgroundPoller := poller.NewRedisPoller(redisIngestRunner, redisProcessRunner)
	var backupMaintenance *DatabaseBackupRunner
	if cfg.BackupEnabled {
		sqlDB, err := db.DB()
		if err != nil {
			if recentUsageCache != nil {
				recentUsageCache.Close()
			}
			quotaService.StopRefreshTasks()
			_ = closeGormDB(db)
			_ = logCloser.Close()
			return nil, err
		}
		backupStore := newDatabaseBackupStore(sqlDB, cfg.BackupDir)
		backupMaintenance = NewDatabaseBackupRunner(backupStore, backupStore, cfg.BackupInterval, cfg.BackupRetentionDays)
	}

	usageService := service.NewUsageServiceWithRecentCache(db, recentUsageCache)
	requestLogService := service.NewRequestLogService(db, cpaClient)
	usageIdentityService := service.NewUsageIdentityServiceWithOptions(db, recentUsageCache, service.UsageIdentityServiceOptions{
		OnDisplayNameChanged: quotaService.UpdateUsageIdentityDisplayNameSnapshot,
	})
	cpaAPIKeyService := service.NewCPAAPIKeyService(db)
	authFilesManagementService := service.NewAuthFilesManagementService(cpaClient)
	if cfg.TLSSkipVerify {
		logrus.WithField("cpa_base_url", cfg.CPABaseURL).Warn("TLS certificate verification is disabled for CPA and Redis queue connections")
	}
	pricingService := service.NewPricingService(db, cpaClient)
	sessionManager := auth.NewSessionManager(cfg.AuthSessionTTL)
	if cfg.AuthEnabled {
		sessionManager = auth.NewPersistentSessionManager(cfg.AuthSessionTTL, auth.NewGormSessionStore(db))
	}
	authConfig := api.AuthConfig{
		Enabled:              cfg.AuthEnabled,
		LoginPassword:        cfg.LoginPassword,
		SessionTTL:           cfg.AuthSessionTTL,
		BasePath:             cfg.AppBasePath,
		FrameAncestorOrigins: frameAncestorOrigins(cfg),
	}
	authHandler := api.NewAuthHandler(authConfig, sessionManager)

	return &App{
		Config: &cfg,
		DB:     db,
		Poller: backgroundPoller,
		// Redis ingest/process 分成两个后台 runner，避免远端订阅拉取和本地 SQLite 处理互相等待。
		RedisIngest:       redisIngestRunner,
		RedisProcess:      redisProcessRunner,
		Maintenance:       NewStorageCleanupRunner(syncService),
		MetadataSync:      metadataSyncRunner,
		QuotaService:      quotaService,
		QuotaAutoRefresh:  quotaService,
		BackupMaintenance: backupMaintenance,
		RecentUsageCache:  recentUsageCache,
		LogCloser:         logCloser,
		Router: api.NewRouter(
			webui.Static,
			backgroundPoller,
			usageService,
			pricingService,
			authConfig,
			authHandler,
			cfg.AppBasePath,
			api.OptionalProviders{
				UsageIdentity: usageIdentityService,
				Quota:         quotaService,
				CPAAPIKeys:    cpaAPIKeyService,
				AuthFiles:     authFilesManagementService,
				RequestLogs:   requestLogService,
				Status: api.StatusRouteConfig{
					CPAPublicURL:               cfg.CPAPublicURL,
					CPARequestLogAccessEnabled: cfg.CPARequestLogAccessEnabled,
				},
			},
		),
	}, nil
}

func frameAncestorOrigins(cfg config.Config) []string {
	// 只信任显式浏览器公开地址；CPA_BASE_URL 可能是内网地址，不能进入 frame-ancestors。
	if origin, ok := publicOrigin(cfg.CPAPublicURL); ok {
		return []string{origin}
	}
	return nil
}

func publicOrigin(candidate string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(candidate))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + parsed.Host, true
}

func closeGormDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}

	a.stopBackgroundTasks()
	if a.QuotaService != nil {
		a.QuotaService.StopRefreshTasks()
	}
	if a.RecentUsageCache != nil {
		a.RecentUsageCache.Close()
		a.RecentUsageCache = nil
	}

	var closeErr error
	if a.DB != nil {
		closeErr = errors.Join(closeErr, closeGormDB(a.DB))
		a.DB = nil
	}
	if a.LogCloser != nil {
		closeErr = errors.Join(closeErr, a.LogCloser.Close())
		a.LogCloser = nil
	}
	return closeErr
}

func (a *App) Run() error {
	if a == nil || a.Router == nil || a.Config == nil {
		return fmt.Errorf("application is not initialized")
	}

	ctx := a.startBackgroundContext()
	defer a.stopBackgroundTasks()
	if a.RedisIngest != nil {
		a.startBackgroundTask(func() {
			if err := a.RedisIngest.Run(ctx); err != nil {
				logrus.Errorf("redis ingest stopped: %v", err)
			}
		})
	}
	if a.RedisProcess != nil {
		a.startBackgroundTask(func() {
			if err := a.RedisProcess.Run(ctx); err != nil {
				logrus.Errorf("redis process stopped: %v", err)
			}
		})
	}
	if a.Maintenance != nil {
		a.startBackgroundTask(func() {
			if err := a.Maintenance.Run(ctx); err != nil {
				logrus.Errorf("maintenance cleanup stopped: %v", err)
			}
		})
	}
	if a.MetadataSync != nil {
		a.startBackgroundTask(func() {
			if err := a.MetadataSync.Run(ctx); err != nil {
				logrus.Errorf("metadata sync stopped: %v", err)
			}
		})
	}
	if a.QuotaService != nil {
		a.QuotaService.SetRefreshContext(ctx)
	}
	if a.QuotaAutoRefresh != nil {
		a.startBackgroundTask(func() {
			// quota 自动刷新和手动刷新共用队列，但作为独立后台任务跟随 App 生命周期启动和停止。
			if err := a.QuotaAutoRefresh.StartAutoRefresh(ctx); err != nil {
				logrus.Errorf("quota auto refresh stopped: %v", err)
			}
		})
	}
	if a.BackupMaintenance != nil {
		a.startBackgroundTask(func() {
			if err := a.BackupMaintenance.Run(ctx); err != nil {
				logrus.Errorf("database backup stopped: %v", err)
			}
		})
	}

	server := &http.Server{
		Addr:    ":" + a.Config.AppPort,
		Handler: a.Router,
	}
	if a.Config.TLSEnabled {
		return server.ListenAndServeTLS(a.Config.TLSCertFile, a.Config.TLSKeyFile)
	}
	return server.ListenAndServe()
}

func (a *App) startBackgroundContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	a.backgroundCancel = cancel
	return ctx
}

func (a *App) startBackgroundTask(run func()) {
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		run()
	}()
}

func (a *App) stopBackgroundTasks() {
	if a.backgroundCancel != nil {
		a.backgroundCancel()
		a.backgroundCancel = nil
	}
	a.backgroundWG.Wait()
}
