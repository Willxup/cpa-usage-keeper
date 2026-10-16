package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerHealthRoutes(router gin.IRoutes, startupStatus func() StartupStatus) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	// 存活检查不因迁移失败停止；部署流量就绪检查必须等完整业务路由启用。
	router.GET("/readyz", func(c *gin.Context) {
		setNoStoreHeaders(c)
		status := startupStatus()
		code := http.StatusServiceUnavailable
		if status.Phase == StartupPhaseReady {
			code = http.StatusOK
		}
		c.JSON(code, status)
	})
}
