package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
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
	Config    *config.Config
	DB        *gorm.DB
	Router    *gin.Engine
	Poller    Runner
	LogCloser io.Closer
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

	syncService := service.NewSyncService(db, cfg)
	backgroundPoller := newBackgroundRunner(syncService, cfg)

	usageService := service.NewUsageService(db)
	authFileService := service.NewAuthFileService(db)
	providerMetadataService := service.NewProviderMetadataService(db)
	pricingService := service.NewPricingService(db)
	sessionManager := auth.NewSessionManager(cfg.AuthSessionTTL)
	authHandler := api.NewAuthHandler(api.AuthConfig{
		Enabled:       cfg.AuthEnabled,
		LoginPassword: cfg.LoginPassword,
		SessionTTL:    cfg.AuthSessionTTL,
		BasePath:      cfg.AppBasePath,
	}, sessionManager)

	return &App{
		Config:    &cfg,
		DB:        db,
		Poller:    backgroundPoller,
		LogCloser: logCloser,
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

func newBackgroundRunner(syncService *service.SyncService, cfg config.Config) Runner {
	if cfg.UsageSyncMode == "redis" || cfg.UsageSyncMode == "auto" {
		return poller.NewRedisDrain(syncService, poller.RedisDrainConfig{
			IdleInterval:           cfg.RedisQueueIdleInterval,
			ErrorBackoff:           cfg.RedisQueueErrorBackoff,
			MetadataInterval:       cfg.RedisMetadataSyncInterval,
			EnableLegacyFallback:   cfg.UsageSyncMode == "auto",
			LegacyFallbackInterval: cfg.PollInterval,
		})
	}
	return poller.New(syncService, cfg.PollInterval)
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

	if a.Poller != nil {
		go func() {
			if err := a.Poller.Run(context.Background()); err != nil {
				logrus.Errorf("poller stopped: %v", err)
			}
		}()
	}

	return a.Router.Run(":" + a.Config.AppPort)
}
