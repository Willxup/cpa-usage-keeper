package api

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
	"cpa-usage-keeper/internal/updatecheck"
	"cpa-usage-keeper/internal/version"
	"github.com/gin-gonic/gin"
)

const appBasePathPlaceholder = "__APP_BASE_PATH__"

type StatusProvider interface {
	Status() poller.Status
}

type QuotaProvider interface {
	GetCachedQuota(context.Context, quota.CacheRequest) (quota.CacheResponse, error)
	Refresh(context.Context, quota.RefreshRequest) (quota.RefreshResponse, error)
	GetRefreshTask(context.Context, string) (quota.RefreshTaskResponse, error)
}

type StatusRouteConfig struct {
	CPAPublicURL string
}

type OptionalProviders struct {
	UsageIdentity service.UsageIdentityProvider
	Quota         QuotaProvider
	CPAAPIKeys    service.CPAAPIKeyProvider
	Status        StatusRouteConfig
}

func NewRouter(
	staticFS fs.FS,
	statusProvider StatusProvider,
	usageProvider service.UsageProvider,
	pricingProvider service.PricingProvider,
	authConfig AuthConfig,
	authHandler *authHandler,
	basePath string,
	optionalProviders ...OptionalProviders,
) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
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

	var usageIdentityProvider service.UsageIdentityProvider
	var quotaProvider QuotaProvider
	var cpaAPIKeyProvider service.CPAAPIKeyProvider
	var statusConfig StatusRouteConfig
	if len(optionalProviders) > 0 {
		usageIdentityProvider = optionalProviders[0].UsageIdentity
		quotaProvider = optionalProviders[0].Quota
		cpaAPIKeyProvider = optionalProviders[0].CPAAPIKeys
		statusConfig = optionalProviders[0].Status
	}
	authHandler.setCPAAPIKeyProvider(cpaAPIKeyProvider)

	adminProtected := apiV1.Group("")
	adminProtected.Use(authHandler.adminMiddleware())
	registerStatusRoutes(adminProtected, statusProvider, statusConfig)
	registerUpdateRoutes(adminProtected, nil)
	registerUsageOverviewRoute(adminProtected, usageProvider)
	registerUsageAnalysisRoute(adminProtected, usageProvider, cpaAPIKeyProvider)
	registerUsageEventsRoute(adminProtected, usageProvider, usageIdentityProvider)
	registerUsageIdentityRoutes(adminProtected, usageIdentityProvider)
	registerCPAAPIKeyRoutes(adminProtected, cpaAPIKeyProvider)
	registerPricingRoutes(adminProtected, pricingProvider)
	registerQuotaRoutes(adminProtected, quotaProvider)

	keyViewerProtected := apiV1.Group("")
	keyViewerProtected.Use(authHandler.apiKeyViewerMiddleware())
	registerKeyOverviewRoute(keyViewerProtected, usageProvider, cpaAPIKeyProvider, authHandler)

	if staticFS != nil {
		if indexFile, err := staticFS.Open("index.html"); err == nil {
			_ = indexFile.Close()
			httpFS := http.FS(staticFS)
			serveIndex := func(c *gin.Context) {
				indexHTML, err := renderIndexHTML(staticFS, basePath)
				if err != nil {
					c.Status(http.StatusNotFound)
					return
				}
				setHTMLCacheHeaders(c)
				c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
			}
			serveAsset := func(c *gin.Context) {
				assetPath := "assets/" + strings.TrimPrefix(c.Param("filepath"), "/")
				if assetFile, err := staticFS.Open(assetPath); err == nil {
					_ = assetFile.Close()
					setStaticAssetCacheHeaders(c)
					c.FileFromFS(assetPath, httpFS)
					return
				}
				c.Status(http.StatusNotFound)
			}

			appGroup.GET("/", serveIndex)
			appGroup.GET("/assets/*filepath", serveAsset)
			appGroup.HEAD("/assets/*filepath", serveAsset)
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

				if assetPath, ok := staticAssetPath(requestPath); ok {
					if assetFile, err := staticFS.Open(assetPath); err == nil {
						_ = assetFile.Close()
						setStaticAssetCacheHeaders(c)
						c.FileFromFS(assetPath, httpFS)
						return
					}
				}

				serveIndex(c)
			})
		}
	}

	return router
}

func setHTMLCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

func setStaticAssetCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
}

func renderIndexHTML(staticFS fs.FS, basePath string) ([]byte, error) {
	indexFile, err := staticFS.Open("index.html")
	if err != nil {
		return nil, err
	}
	defer indexFile.Close()
	indexHTML, err := io.ReadAll(indexFile)
	if err != nil {
		return nil, err
	}

	return bytes.ReplaceAll(
		indexHTML,
		[]byte(strconv.Quote(appBasePathPlaceholder)),
		[]byte(strconv.Quote(basePath)),
	), nil
}

func cleanURLPath(requestPath string) string {
	cleaned := path.Clean(requestPath)
	if cleaned == "." {
		return "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "/" + cleaned
	}
	return cleaned
}

func staticAssetPath(requestPath string) (string, bool) {
	cleaned := cleanURLPath(requestPath)
	if strings.Contains(cleaned, "\\") {
		return "", false
	}
	relPath := strings.TrimPrefix(cleaned, "/")
	if relPath == "" {
		return "", false
	}
	return relPath, true
}

func stripBasePath(basePath, requestPath string) (string, bool) {
	cleaned := cleanURLPath(requestPath)
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
	Running            bool       `json:"running"`
	SyncRunning        bool       `json:"sync_running"`
	Timezone           string     `json:"timezone"`
	Version            string     `json:"version"`
	UpdateCheckEnabled bool       `json:"updateCheckEnabled"`
	CPAPublicURL       string     `json:"cpa_public_url,omitempty"`
	LastRunAt          *time.Time `json:"last_run_at,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	LastWarning        string     `json:"last_warning,omitempty"`
	LastStatus         string     `json:"last_status,omitempty"`
}

func registerStatusRoutes(router gin.IRoutes, statusProvider StatusProvider, config StatusRouteConfig) {
	router.GET("/status", func(c *gin.Context) {
		if statusProvider == nil {
			c.JSON(http.StatusOK, buildStatusResponse(poller.Status{}, config))
			return
		}

		c.JSON(http.StatusOK, buildStatusResponse(statusProvider.Status(), config))
	})
}

func buildStatusResponse(status poller.Status, config StatusRouteConfig) statusResponse {
	response := statusResponse{
		Running:            status.Running,
		SyncRunning:        status.SyncRunning,
		Timezone:           time.Local.String(),
		Version:            version.Version,
		UpdateCheckEnabled: updatecheck.IsStableVersion(version.Version),
		CPAPublicURL:       config.CPAPublicURL,
		LastError:          status.LastError,
		LastWarning:        status.LastWarning,
		LastStatus:         status.LastStatus,
	}
	if !status.LastRunAt.IsZero() {
		lastRunAt := timeutil.NormalizeStorageTime(status.LastRunAt)
		response.LastRunAt = &lastRunAt
	}
	return response
}
