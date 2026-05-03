package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	webui "cpa-usage-keeper/web"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Runner interface {
	Run(ctx context.Context) error
	Status() poller.Status
	SyncNow(ctx context.Context) error
}

type Options struct {
	EnvFile string
}

type App struct {
	Config                  *config.Config
	ConfiguredUsageSyncMode string
	DB                      *gorm.DB
	Router                  *gin.Engine
	Poller                  Runner
	Maintenance             *StorageCleanupRunner
	BackupMaintenance       *DatabaseBackupRunner
	LogCloser               io.Closer

	backgroundCancel context.CancelFunc
	backgroundWG     sync.WaitGroup
}

var redisStartupProbe = func(ctx context.Context, cfg config.Config) error {
	client := cpa.NewRedisQueueClient(
		cfg.CPABaseURL,
		cfg.RedisQueueAddr,
		cfg.CPAManagementKey,
		cfg.RequestTimeout,
		cfg.RedisQueueKey,
		cfg.RedisQueueBatchSize,
	)
	return client.Probe(ctx)
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
	if err := runTemporaryStartupSnapshotRunsCleanup(db); err != nil {
		_ = closeGormDB(db)
		_ = logCloser.Close()
		return nil, err
	}

	configuredUsageSyncMode := cfg.UsageSyncMode
	cfg = resolveUsageSyncMode(context.Background(), cfg)
	syncService := service.NewSyncService(db, cfg)
	backgroundPoller := newBackgroundRunner(syncService, cfg)
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
	authFileService := service.NewAuthFileService(db)
	providerMetadataService := service.NewProviderMetadataService(db)
	pricingModelsClient := cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout)
	pricingService := service.NewPricingService(db, pricingModelsClient)
	sessionManager := auth.NewSessionManager(cfg.AuthSessionTTL)
	authHandler := api.NewAuthHandler(api.AuthConfig{
		Enabled:       cfg.AuthEnabled,
		LoginPassword: cfg.LoginPassword,
		SessionTTL:    cfg.AuthSessionTTL,
		BasePath:      cfg.AppBasePath,
	}, sessionManager)

	return &App{
		Config:                  &cfg,
		ConfiguredUsageSyncMode: configuredUsageSyncMode,
		DB:                      db,
		Poller:                  backgroundPoller,
		Maintenance:             NewStorageCleanupRunner(syncService),
		BackupMaintenance:       backupMaintenance,
		LogCloser:               logCloser,
		Router: api.NewRouter(
			webui.Static,
			backgroundPoller,
			usageService,
			authFileService,
			providerMetadataService,
			pricingService,
			api.AuthConfig{
				Enabled:       cfg.AuthEnabled,
				LoginPassword: cfg.LoginPassword,
				SessionTTL:    cfg.AuthSessionTTL,
				BasePath:      cfg.AppBasePath,
			},
			authHandler,
			cfg.AppBasePath,
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

func resolveUsageSyncMode(ctx context.Context, cfg config.Config) config.Config {
	if cfg.UsageSyncMode != "auto" {
		return cfg
	}
	if err := redisStartupProbe(ctx, cfg); err != nil {
		cfg.UsageSyncMode = "legacy_export"
		logrus.WithError(err).WithFields(logrus.Fields{
			"configured_mode": "auto",
			"effective_mode":  cfg.UsageSyncMode,
		}).Info("usage sync auto mode resolved")
		return cfg
	}
	cfg.UsageSyncMode = "redis"
	logrus.WithFields(logrus.Fields{
		"configured_mode": "auto",
		"effective_mode":  cfg.UsageSyncMode,
	}).Info("usage sync auto mode resolved")
	return cfg
}

func newBackgroundRunner(syncService *service.SyncService, cfg config.Config) Runner {
	if cfg.UsageSyncMode == "redis" {
		return poller.NewRedisDrain(syncService, poller.RedisDrainConfig{
			IdleInterval:     cfg.RedisQueueIdleInterval,
			ErrorBackoff:     cfg.RedisQueueErrorBackoff,
			MetadataInterval: cfg.RedisMetadataSyncInterval,
		})
	}
	return poller.New(syncService, cfg.PollInterval)
}

// runTemporaryStartupSnapshotRunsCleanup 是启动期额外执行的 snapshot_runs 治理入口，和每日清理共用 CleanupSnapshotRuns 语义。
// 它只处理 snapshot_runs 并执行 VACUUM，不包含每日 CleanupStorage 中的 redis_usage_inboxes 清理。
func runTemporaryStartupSnapshotRunsCleanup(db *gorm.DB) error {
	logrus.Info("temporary snapshot runs cleanup started")
	if _, err := repository.CleanupSnapshotRuns(db, time.Now()); err != nil {
		logrus.WithError(err).Error("temporary snapshot runs cleanup failed")
		return err
	}
	if err := repository.Vacuum(db); err != nil {
		logrus.WithError(err).Error("temporary snapshot runs cleanup failed")
		return err
	}
	logrus.Info("temporary snapshot runs cleanup completed")
	return nil
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

	configuredMode := a.ConfiguredUsageSyncMode
	if configuredMode == "" {
		configuredMode = a.Config.UsageSyncMode
	}
	logrus.WithFields(logrus.Fields{
		"configured_mode": configuredMode,
		"effective_mode":  a.Config.UsageSyncMode,
	}).Info("usage sync mode selected")

	ctx := a.startBackgroundContext()
	defer a.stopBackgroundTasks()
	if a.Poller != nil {
		a.startBackgroundTask(func() {
			if err := a.Poller.Run(ctx); err != nil {
				logrus.Errorf("poller stopped: %v", err)
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
	if a.BackupMaintenance != nil {
		a.startBackgroundTask(func() {
			if err := a.BackupMaintenance.Run(ctx); err != nil {
				logrus.Errorf("database backup stopped: %v", err)
			}
		})
	}

	return a.Router.Run(":" + a.Config.AppPort)
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
