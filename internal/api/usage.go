package api

import (
	"net/http"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageResponse struct {
	Usage any `json:"usage"`
}

func registerUsageRoutes(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageResponse{Usage: gin.H{
				"total_requests":   0,
				"success_count":    0,
				"failure_count":    0,
				"total_tokens":     0,
				"apis":             gin.H{},
				"requests_by_day":  gin.H{},
				"requests_by_hour": gin.H{},
				"tokens_by_day":    gin.H{},
				"tokens_by_hour":   gin.H{},
			}})
			return
		}

		usage, err := usageProvider.GetUsage(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, usageResponse{Usage: usage})
	})
}
