package app

import (
	"context"
	"fmt"
	"path/filepath"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type App struct {
	Config *config.Config
	DB     *gorm.DB
	Router *gin.Engine
	Poller *poller.Poller
}

func New() (*App, error) {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return nil, err
	}

	return NewWithConfig(*cfg)
}

func NewWithConfig(cfg config.Config) (*App, error) {
	logrus.SetLevel(parseLogLevel(cfg.LogLevel))

	db, err := repository.OpenDatabase(cfg)
	if err != nil {
		return nil, err
	}

	syncService := service.NewSyncService(db, cfg)
	backgroundPoller := poller.New(syncService, cfg.PollInterval)

	usageService := service.NewUsageService(db)
	authFileService := service.NewAuthFileService(db)
	providerMetadataService := service.NewProviderMetadataService(db)
	pricingService := service.NewPricingService(db)
	sessionManager := auth.NewSessionManager(cfg.AuthSessionTTL)
	authHandler := api.NewAuthHandler(api.AuthConfig{
		Enabled:       cfg.AuthEnabled,
		LoginPassword: cfg.LoginPassword,
		SessionTTL:    cfg.AuthSessionTTL,
	}, sessionManager)

	return &App{
		Config: &cfg,
		DB:     db,
		Poller: backgroundPoller,
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
			},
			authHandler,
		),
	}, nil
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

func parseLogLevel(level string) logrus.Level {
	parsed, err := logrus.ParseLevel(level)
	if err != nil {
		return logrus.InfoLevel
	}
	return parsed
}
