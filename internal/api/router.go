package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const appBasePathPlaceholder = "__APP_BASE_PATH__"
const manualSyncRateLimitWindow = time.Second

type syncLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	lastSync time.Time
}

func (l *syncLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.lastSync.IsZero() && now.Sub(l.lastSync) < l.window {
		return false
	}
	l.lastSync = now
	return true
}

type StatusProvider interface {
	Status() poller.Status
}

type SyncRunner interface {
	SyncNow(ctx context.Context) error
}

func NewRouter(
	staticDir string,
	statusProvider StatusProvider,
	usageProvider service.UsageProvider,
	authFileProvider service.AuthFileProvider,
	providerMetadataProvider service.ProviderMetadataProvider,
	pricingProvider service.PricingProvider,
	authConfig AuthConfig,
	authHandler *authHandler,
	basePath string,
	db *gorm.DB,
) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	appGroup := router.Group(basePath)
	registerHealthRoutes(appGroup)

	apiV1 := appGroup.Group("/api/v1")
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
	registerSyncRoutes(protected, statusProvider, &syncLimiter{window: manualSyncRateLimitWindow})
	registerUsageOverviewRoute(protected, usageProvider)
	registerUsageAnalysisRoute(protected, usageProvider)
	registerUsageEventsRoute(protected, usageProvider, authFileProvider, providerMetadataProvider)
	registerUsageCredentialsRoute(protected, usageProvider, authFileProvider, providerMetadataProvider)
	registerAuthFileRoutes(protected, authFileProvider)
	registerProviderMetadataRoutes(protected, providerMetadataProvider)
	registerPricingRoutes(protected, pricingProvider)
	registerUsageImportRoute(protected, db)

	if staticDir != "" {
		if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
			indexPath := filepath.Join(staticDir, "index.html")
			serveIndex := func(c *gin.Context) {
				indexHTML, err := renderIndexHTML(indexPath, basePath)
				if err != nil {
					c.Status(http.StatusNotFound)
					return
				}
				c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
			}

			appGroup.GET("/", serveIndex)
			appGroup.Static("/assets", filepath.Join(staticDir, "assets"))
			router.NoRoute(func(c *gin.Context) {
				requestPath, ok := stripBasePath(basePath, c.Request.URL.Path)
				if !ok {
					c.Status(http.StatusNotFound)
					return
				}
				if strings.HasPrefix(requestPath, "/api/") {
					c.Status(http.StatusNotFound)
					return
				}

				relPath := strings.TrimPrefix(filepath.Clean(requestPath), "/")
				if relPath != "." && relPath != "" {
					assetPath := filepath.Join(staticDir, relPath)
					if assetInfo, err := os.Stat(assetPath); err == nil && !assetInfo.IsDir() {
						c.File(assetPath)
						return
					}
				}

				serveIndex(c)
			})
		}
	}

	return router
}

func renderIndexHTML(indexPath, basePath string) ([]byte, error) {
	indexHTML, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}

	return bytes.ReplaceAll(
		indexHTML,
		[]byte(strconv.Quote(appBasePathPlaceholder)),
		[]byte(strconv.Quote(basePath)),
	), nil
}

func stripBasePath(basePath, requestPath string) (string, bool) {
	cleaned := filepath.Clean(requestPath)
	if cleaned == "." {
		cleaned = "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	if basePath == "" {
		return cleaned, true
	}
	if cleaned == basePath {
		return "/", true
	}
	if !strings.HasPrefix(cleaned, basePath+"/") {
		return "", false
	}
	trimmed := strings.TrimPrefix(cleaned, basePath)
	if trimmed == "" {
		return "/", true
	}
	return trimmed, true
}

type statusResponse struct {
	Running     bool       `json:"running"`
	SyncRunning bool       `json:"sync_running"`
	Timezone    string     `json:"timezone"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	LastWarning string     `json:"last_warning,omitempty"`
	LastStatus  string     `json:"last_status,omitempty"`
}

func registerStatusRoutes(router gin.IRoutes, statusProvider StatusProvider) {
	router.GET("/status", func(c *gin.Context) {
		if statusProvider == nil {
			c.JSON(http.StatusOK, statusResponse{Timezone: time.Local.String()})
			return
		}

		c.JSON(http.StatusOK, buildStatusResponse(statusProvider.Status()))
	})
}

func registerSyncRoutes(router gin.IRoutes, statusProvider StatusProvider, limiter *syncLimiter) {
	router.POST("/sync", func(c *gin.Context) {
		if limiter != nil && !limiter.allow(time.Now()) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "sync rate limit exceeded"})
			return
		}

		syncRunner, ok := statusProvider.(SyncRunner)
		if !ok || syncRunner == nil {
			writeInternalError(c, "sync runner is not configured", nil)
			return
		}

		if err := syncRunner.SyncNow(c.Request.Context()); err != nil {
			if errors.Is(err, poller.ErrSyncAlreadyRunning) {
				c.JSON(http.StatusConflict, gin.H{"error": "sync already running"})
				return
			}
			if !errors.Is(err, poller.ErrSyncCompletedWithWarnings) {
				writeInternalError(c, "manual sync failed", err)
				return
			}
		}

		if statusProvider, ok := syncRunner.(StatusProvider); ok {
			c.JSON(http.StatusOK, buildStatusResponse(statusProvider.Status()))
			return
		}
		c.JSON(http.StatusOK, gin.H{"sync_running": false})
	})
}

func buildStatusResponse(status poller.Status) statusResponse {
	response := statusResponse{
		Running:     status.Running,
		SyncRunning: status.SyncRunning,
		Timezone:    time.Local.String(),
		LastError:   status.LastError,
		LastWarning: status.LastWarning,
		LastStatus:  status.LastStatus,
	}
	if !status.LastRunAt.IsZero() {
		lastRunAt := status.LastRunAt.UTC()
		response.LastRunAt = &lastRunAt
	}
	return response
}
