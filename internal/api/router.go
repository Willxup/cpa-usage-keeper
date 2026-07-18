package api

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/service"
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
	GetRefreshTaskByAuthIndex(context.Context, string) (quota.RefreshTaskResponse, error)
	GetInspectionStatus(context.Context) (quota.InspectionStatus, error)
	StartInspection(context.Context) (quota.InspectionStatus, error)
	GetAutoRefreshSettings(context.Context) (quota.AutoRefreshSettings, error)
	UpdateAutoRefreshSettings(context.Context, quota.AutoRefreshSettings) (quota.AutoRefreshSettings, error)
	GetResetCredits(context.Context, quota.ResetCreditsRequest) (quota.ResetCreditsResponse, error)
	Reset(context.Context, quota.ResetRequest) (quota.ResetResponse, error)
}

type StatusRouteConfig struct {
	CPAPublicURL               string
	CPARequestLogAccessEnabled bool
}

type OptionalProviders struct {
	UsageIdentity service.UsageIdentityProvider
	Quota         QuotaProvider
	RelayUsage    RelayUsageProvider
	CPAAPIKeys    service.CPAAPIKeyProvider
	AuthFiles     service.AuthFilesManagementProvider
	RequestLogs   service.RequestLogProvider
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
	apiV1.Use(requestIntentMiddleware())
	if debugAPIRoutesEnabled() {
		registerPingRoutes(apiV1)
	}

	authGroup := apiV1.Group("/auth")
	if authHandler == nil {
		authHandler = NewAuthHandler(authConfig, nil)
	}
	authHandler.registerRoutes(authGroup)

	var usageIdentityProvider service.UsageIdentityProvider
	var quotaProvider QuotaProvider
	var relayUsageProvider RelayUsageProvider
	var cpaAPIKeyProvider service.CPAAPIKeyProvider
	var authFilesProvider service.AuthFilesManagementProvider
	var requestLogProvider service.RequestLogProvider
	var statusConfig StatusRouteConfig
	if len(optionalProviders) > 0 {
		usageIdentityProvider = optionalProviders[0].UsageIdentity
		quotaProvider = optionalProviders[0].Quota
		relayUsageProvider = optionalProviders[0].RelayUsage
		cpaAPIKeyProvider = optionalProviders[0].CPAAPIKeys
		authFilesProvider = optionalProviders[0].AuthFiles
		requestLogProvider = optionalProviders[0].RequestLogs
		statusConfig = optionalProviders[0].Status
	}
	authHandler.setCPAAPIKeyProvider(cpaAPIKeyProvider)
	requestLogDownloadTokens := newRequestLogDownloadTokenStore()

	registerUsageEventRequestLogDownloadTokenRoutes(apiV1, requestLogProvider, requestLogDownloadTokens, statusConfig.CPARequestLogAccessEnabled)

	versionProtected := apiV1.Group("")
	versionProtected.Use(authHandler.roleMiddleware(auth.RoleAdmin, auth.RoleAPIKeyViewer))
	registerVersionRoutes(versionProtected)

	adminProtected := apiV1.Group("")
	adminProtected.Use(authHandler.adminMiddleware())
	registerStatusRoutes(adminProtected, statusProvider, statusConfig)
	registerUpdateRoutes(adminProtected, nil)
	registerUsageOverviewRoute(adminProtected, usageProvider, cpaAPIKeyProvider)
	registerUsageAnalysisRoute(adminProtected, usageProvider, cpaAPIKeyProvider)
	registerUsageEventsRoute(adminProtected, usageProvider, usageIdentityProvider, cpaAPIKeyProvider, requestLogProvider, requestLogDownloadTokens, statusConfig.CPARequestLogAccessEnabled)
	registerUsageIdentityRoutes(adminProtected, usageIdentityProvider)
	registerAuthFileManagementRoutes(adminProtected, authFilesProvider)
	registerAuthSessionManagementRoutes(adminProtected, authHandler)
	registerCPAAPIKeyRoutes(adminProtected, cpaAPIKeyProvider)
	registerPricingRoutes(adminProtected, pricingProvider)
	registerQuotaRoutes(adminProtected, quotaProvider)
	registerRelayUsageRoutes(adminProtected, relayUsageProvider)

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
				setHTMLCacheHeaders(c, authConfig.FrameAncestorOrigins)
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

func debugAPIRoutesEnabled() bool {
	return version.Version == "dev" || os.Getenv("GIN_MODE") == gin.DebugMode
}

func registerPingRoutes(router gin.IRoutes) {
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
}

func setHTMLCacheHeaders(c *gin.Context, frameAncestorOrigins []string) {
	setNoStoreHeaders(c)
	setFrameAncestorsCSP(c, frameAncestorOrigins)
}

func setNoStoreHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

func setStaticAssetCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
}

func setFrameAncestorsCSP(c *gin.Context, origins []string) {
	values := []string{"frame-ancestors", "'self'"}
	seen := map[string]struct{}{"'self'": {}}
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		values = append(values, trimmed)
	}
	c.Header("Content-Security-Policy", strings.Join(values, " "))
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
	Running                    bool   `json:"running"`
	SyncRunning                bool   `json:"sync_running"`
	Timezone                   string `json:"timezone"`
	CPAPublicURL               string `json:"cpa_public_url,omitempty"`
	CPARequestLogAccessEnabled bool   `json:"cpa_request_log_access_enabled"`
	LastError                  string `json:"last_error,omitempty"`
	LastWarning                string `json:"last_warning,omitempty"`
	LastStatus                 string `json:"last_status,omitempty"`
}

type versionResponse struct {
	Version            string `json:"version"`
	UpdateCheckEnabled bool   `json:"updateCheckEnabled"`
}

func registerVersionRoutes(router gin.IRoutes) {
	router.GET("/version", func(c *gin.Context) {
		setNoStoreHeaders(c)
		c.JSON(http.StatusOK, buildVersionResponse())
	})
}

func buildVersionResponse() versionResponse {
	return versionResponse{
		Version:            version.Version,
		UpdateCheckEnabled: updatecheck.IsStableVersion(version.Version),
	}
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
		Running:                    status.Running,
		SyncRunning:                status.SyncRunning,
		Timezone:                   time.Local.String(),
		CPAPublicURL:               config.CPAPublicURL,
		CPARequestLogAccessEnabled: config.CPARequestLogAccessEnabled,
		LastError:                  status.LastError,
		LastWarning:                status.LastWarning,
		LastStatus:                 status.LastStatus,
	}
	return response
}
