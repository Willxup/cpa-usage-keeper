package api

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type StatusProvider interface {
	Status() poller.Status
}

func NewRouter(
	staticDir string,
	statusProvider StatusProvider,
	usageProvider service.UsageProvider,
	pricingProvider service.PricingProvider,
	authConfig AuthConfig,
	authHandler *authHandler,
) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	registerHealthRoutes(router)

	apiV1 := router.Group("/api/v1")
	apiV1.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	authGroup := apiV1.Group("/auth")
	if authHandler == nil {
		authHandler = NewAuthHandler(authConfig, nil)
	}
	authHandler.registerRoutes(authGroup)

	protected := apiV1.Group("")
	protected.Use(authHandler.middleware())
	registerStatusRoutes(protected, statusProvider)
	registerUsageRoutes(protected, usageProvider)
	registerPricingRoutes(protected, pricingProvider)

	if staticDir != "" {
		if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
			indexPath := filepath.Join(staticDir, "index.html")
			router.GET("/", func(c *gin.Context) {
				c.File(indexPath)
			})
			router.Static("/assets", filepath.Join(staticDir, "assets"))
		}
	}

	return router
}

type statusResponse struct {
	Running     bool       `json:"running"`
	SyncRunning bool       `json:"sync_running"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

func registerStatusRoutes(router gin.IRoutes, statusProvider StatusProvider) {
	router.GET("/status", func(c *gin.Context) {
		if statusProvider == nil {
			c.JSON(http.StatusOK, statusResponse{})
			return
		}

		status := statusProvider.Status()
		response := statusResponse{
			Running:     status.Running,
			SyncRunning: status.SyncRunning,
			LastError:   status.LastError,
		}
		if !status.LastRunAt.IsZero() {
			lastRunAt := status.LastRunAt.UTC()
			response.LastRunAt = &lastRunAt
		}

		c.JSON(http.StatusOK, response)
	})
}
