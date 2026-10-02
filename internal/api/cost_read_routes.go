package api

import (
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

// costReadMiddleware 只在已有权限检查之后，为精确列出的费用结果路由占用读取许可。
// 许可覆盖整个 handler，包括 CSV/JSON 最后一段写入；非费用路由直接继续。
func costReadMiddleware(gate *service.CostReadGate, basePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if gate == nil || !isCostReadRoute(c.Request.Method, strings.TrimPrefix(c.FullPath(), basePath)) {
			c.Next()
			return
		}
		release, ok := gate.Acquire()
		if !ok {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"code":    "costs_busy",
				"message": "Cost statistics are being updated. Please try again shortly.",
			})
			return
		}
		defer release()
		c.Next()
	}
}

// isCostReadRoute 按 Gin FullPath 精确识别费用结果入口，不按 /usage 或 /quota 前缀封锁非费用请求。
func isCostReadRoute(method, path string) bool {
	switch method {
	case http.MethodGet:
		switch path {
		case "/api/v1/usage/overview", "/api/v1/usage/overview/comparisons", "/api/v1/usage/overview/realtime",
			"/api/v1/usage/analysis", "/api/v1/usage/events", "/api/v1/usage/events/export",
			"/api/v1/key-overview", "/api/v1/key-overview/comparisons", "/api/v1/key-overview/realtime",
			"/api/v1/key-analysis", "/api/v1/quota/history/:auth_index", "/api/v1/quota/refresh/:auth_index":
			return true
		}
	case http.MethodPost:
		return path == "/api/v1/quota/cache" || path == "/api/v1/quota/refresh"
	}
	return false
}
