package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// registerStaticRoutes 让启动外壳和完整路由共享同一份 BasePath、资源缓存与 iframe CSP 规则。
func registerStaticRoutes(router *gin.Engine, appGroup *gin.RouterGroup, staticFS fs.FS, frameAncestorOrigins []string, basePath string, missingAPI gin.HandlerFunc) {
	var serveIndex gin.HandlerFunc
	if staticFS != nil {
		if indexFile, err := staticFS.Open("index.html"); err == nil {
			_ = indexFile.Close()
			httpFS := http.FS(staticFS)
			serveIndex = func(c *gin.Context) {
				indexHTML, err := renderIndexHTML(staticFS, basePath)
				if err != nil {
					c.Status(http.StatusNotFound)
					return
				}
				setHTMLCacheHeaders(c, frameAncestorOrigins)
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
		}
	}
	if serveIndex == nil && missingAPI == nil {
		return
	}
	router.NoRoute(func(c *gin.Context) {
		requestPath, ok := stripBasePath(basePath, c.Request.URL.Path)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		if strings.HasPrefix(requestPath, "/api/") || missingAPI != nil && requestPath == "/api" {
			if missingAPI != nil {
				missingAPI(c)
				return
			}
			c.Status(http.StatusNotFound)
			return
		}
		if serveIndex == nil {
			c.Status(http.StatusNotFound)
			return
		}
		if assetPath, ok := staticAssetPath(requestPath); ok {
			if assetFile, err := staticFS.Open(assetPath); err == nil {
				_ = assetFile.Close()
				setStaticAssetCacheHeaders(c)
				c.FileFromFS(assetPath, http.FS(staticFS))
				return
			}
		}
		serveIndex(c)
	})
}
