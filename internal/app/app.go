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

// StatusProvider 只提供前端状态和手动同步入口，不作为后台 runner 启动。
type StatusProvider interface {
	Status() poller.Status
	SyncNow(ctx context.Context) error
}

type Options struct {
	EnvFile string
}

type App struct {
	Config            *config.Config
	DB                *gorm.DB
	Router            *gin.Engine
	Poller            StatusProvider
	RedisPull         Runner
	RedisProcess      Runner
	Maintenance       *StorageCleanupRunner
	MetadataSync      *MetadataSyncRunner
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
	// migrations 完成后、后台 runner 启动前先追平 Overview 增量表，避免首个页面请求触发大批量聚合。
	logrus.Info("starting usage overview aggregation catch-up")
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, time.Now()); err != nil {
		_ = closeGormDB(db)
		_ = logCloser.Close()
		return nil, err
	}
	logrus.Info("completed usage overview aggregation catch-up")

	syncService := service.NewSyncService(db, cfg)
	backgroundPoller := poller.NewRedisDrain(syncService, poller.RedisDrainConfig{
		IdleInterval: cfg.RedisQueueIdleInterval,
		ErrorBackoff: cfg.RedisQueueErrorBackoff,
	})
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
	pricingService := service.NewPricingService(db)
	quotaService := quota.NewService(db, cpaClient)
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
		// Redis pull/process 分成两个后台 runner，避免远端拉取和本地 SQLite 处理互相等待。
		RedisPull:         poller.NewRedisPullRunner(backgroundPoller),
		RedisProcess:      poller.NewRedisProcessRunner(backgroundPoller),
		Maintenance:       NewStorageCleanupRunner(syncService),
		MetadataSync:      NewMetadataSyncRunner(syncService, cfg.MetadataSyncInterval),
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
				CPAAPIKeys:    cpaAPIKeyService,
				Status:        api.StatusRouteConfig{CPAManagementURL: api.BuildCPAManagementURL(cfg.CPABaseURL)},
			},
		),
	}, nil
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
	if a.RedisPull != nil {
		a.startBackgroundTask(func() {
			if err := a.RedisPull.Run(ctx); err != nil {
				logrus.Errorf("redis pull stopped: %v", err)
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
