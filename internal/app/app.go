package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	LogCloser         io.Closer

	backgroundCancel context.CancelFunc
	backgroundWG     sync.WaitGroup
}

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

	syncService := service.NewSyncService(db, cfg)
	redisPullSource := poller.NewRedisPullSource(cpa.RedisQueueOptions{
		BaseURL:       cfg.CPABaseURL,
		RedisAddr:     cfg.RedisQueueAddr,
		ManagementKey: cfg.CPAManagementKey,
		Timeout:       cfg.RequestTimeout,
		QueueKey:      cfg.RedisQueueKey,
		BatchSize:     cfg.RedisQueueBatchSize,
		TLS:           cfg.RedisQueueTLS,
		TLSSkipVerify: cfg.TLSSkipVerify,
	})
	httpPullSource := poller.NewHTTPPullSource(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify, cfg.RedisQueueBatchSize)
	redisSubscribeSource := poller.NewRedisSubscribeSource(poller.RedisSubscribeOptions{
		BaseURL:       cfg.CPABaseURL,
		RedisAddr:     cfg.RedisQueueAddr,
		ManagementKey: cfg.CPAManagementKey,
		Timeout:       cfg.RequestTimeout,
		TLS:           cfg.RedisQueueTLS,
		TLSSkipVerify: cfg.TLSSkipVerify,
	})
	redisIngestRunner := poller.NewRedisIngestRunner(redisSubscribeSource, redisPullSource, httpPullSource, poller.NewRedisInboxWriter(db, cfg.RedisQueueKey), poller.RedisIngestRunnerConfig{
		IdleInterval:       cfg.RedisQueueIdleInterval,
		BatchSize:          cfg.RedisQueueBatchSize,
		HTTPBackoffInitial: time.Second,
		HTTPBackoffMax:     30 * time.Second,
	})
	redisProcessRunner := poller.NewRedisProcessRunner(syncService)
	backgroundPoller := poller.NewRedisPoller(redisIngestRunner, redisProcessRunner)
	var backupMaintenance *DatabaseBackupRunner
	if cfg.BackupEnabled {
		sqlDB, err := db.DB()
		if err != nil {
			_ = closeGormDB(db)
			_ = logCloser.Close()
			return nil, err
		}
		backupStore := newDatabaseBackupStore(sqlDB, cfg.BackupDir)
		backupMaintenance = NewDatabaseBackupRunner(backupStore, backupStore, cfg.BackupInterval, cfg.BackupRetentionDays)
	}

	usageService := service.NewUsageService(db)
	usageIdentityService := service.NewUsageIdentityService(db)
	cpaAPIKeyService := service.NewCPAAPIKeyService(db)
	cpaClient := cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify)
	if cfg.TLSSkipVerify {
		logrus.WithField("cpa_base_url", cfg.CPABaseURL).Warn("TLS certificate verification is disabled for CPA and Redis queue connections")
	}
	pricingService := service.NewPricingService(db, cpaClient)
	quotaService := quota.NewServiceWithOptions(db, cpaClient, quota.ServiceOptions{RefreshWorkerLimit: cfg.QuotaRefreshWorkerLimit, AutoRefreshInterval: cfg.QuotaAutoRefreshInterval})
	cycleCostService := service.NewCycleCostService(db)
	sessionManager := auth.NewSessionManager(cfg.AuthSessionTTL)
	authHandler := api.NewAuthHandler(api.AuthConfig{
		Enabled:       cfg.AuthEnabled,
		LoginPassword: cfg.LoginPassword,
		SessionTTL:    cfg.AuthSessionTTL,
		BasePath:      cfg.AppBasePath,
	}, sessionManager)

	return &App{
		Config: &cfg,
		DB:     db,
		Poller: backgroundPoller,
		// Redis ingest/process 分成两个后台 runner，避免远端订阅拉取和本地 SQLite 处理互相等待。
		RedisIngest:       redisIngestRunner,
		RedisProcess:      redisProcessRunner,
		Maintenance:       NewStorageCleanupRunner(syncService),
		MetadataSync:      NewMetadataSyncRunner(syncService, cfg.MetadataSyncInterval),
		QuotaService:      quotaService,
		QuotaAutoRefresh:  quotaAutoRefreshService(cfg, quotaService),
		BackupMaintenance: backupMaintenance,
		LogCloser:         logCloser,
		Router: api.NewRouter(
			webui.Static,
			backgroundPoller,
			usageService,
			pricingService,
			api.AuthConfig{
				Enabled:       cfg.AuthEnabled,
				LoginPassword: cfg.LoginPassword,
				SessionTTL:    cfg.AuthSessionTTL,
				BasePath:      cfg.AppBasePath,
			},
			authHandler,
			cfg.AppBasePath,
			api.OptionalProviders{
				UsageIdentity: usageIdentityService,
				Quota:         quotaService,
				CycleCost:     cycleCostService,
				CPAAPIKeys:    cpaAPIKeyService,
				Status:        api.StatusRouteConfig{CPAPublicURL: cfg.CPAPublicURL, ActiveRecorder: quotaActiveRecorder(cfg, quotaService)},
			},
		),
	}, nil
}

func quotaActiveRecorder(cfg config.Config, service *quota.Service) api.ActiveStatusRecorder {
	if !cfg.QuotaAutoRefreshEnabled {
		return nil
	}
	return service
}

func quotaAutoRefreshService(cfg config.Config, service *quota.Service) QuotaRunner {
	if !cfg.QuotaAutoRefreshEnabled {
		return nil
	}
	return service
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
