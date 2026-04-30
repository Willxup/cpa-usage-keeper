package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type Runner interface {
	Run(ctx context.Context) error
	Status() poller.Status
	SyncNow(ctx context.Context) error
}

type App struct {
	Config                  *config.Config
	ConfiguredUsageSyncMode string
	DB                      *gorm.DB
	Router                  *gin.Engine
	Poller                  Runner
	Maintenance             *StorageCleanupRunner
	LogCloser               io.Closer
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
	cfg, err := config.LoadFromEnv()
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
		_ = logCloser.Close()
		return nil, err
	}

	configuredUsageSyncMode := cfg.UsageSyncMode
	cfg = resolveUsageSyncMode(context.Background(), cfg)
	syncService := service.NewSyncService(db, cfg)
	backgroundPoller := newBackgroundRunner(syncService, cfg)

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
		LogCloser:               logCloser,
		Router: api.NewRouter(
			filepath.Join("web", "dist"),
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

// runTemporaryStartupSnapshotRunsCleanup 是临时启动清理入口，后续删除 snapshot_runs 临时治理时可直接移除。
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
	if a == nil || a.LogCloser == nil {
		return nil
	}
	return a.LogCloser.Close()
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

	if a.Poller != nil {
		go func() {
			if err := a.Poller.Run(context.Background()); err != nil {
				logrus.Errorf("poller stopped: %v", err)
			}
		}()
	}
	if a.Maintenance != nil {
		go func() {
			if err := a.Maintenance.Run(context.Background()); err != nil {
				logrus.Errorf("maintenance cleanup stopped: %v", err)
			}
		}()
	}

	return a.Router.Run(":" + a.Config.AppPort)
}
