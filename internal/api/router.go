package api

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const appBasePathPlaceholder = "__APP_BASE_PATH__"

type StatusProvider interface {
	Status() poller.Status
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
	registerUsageOverviewRoute(protected, usageProvider)
	registerUsageAnalysisRoute(protected, usageProvider)
	registerUsageEventsRoute(protected, usageProvider, authFileProvider, providerMetadataProvider)
	registerUsageCredentialsRoute(protected, usageProvider, authFileProvider, providerMetadataProvider)
	registerAuthFileRoutes(protected, authFileProvider)
	registerProviderMetadataRoutes(protected, providerMetadataProvider)
	registerPricingRoutes(protected, pricingProvider)

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
