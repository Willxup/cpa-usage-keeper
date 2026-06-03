package api

import (
	"errors"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/quota"
	"github.com/gin-gonic/gin"
)

type quotaRequest struct {
	AuthIndexes []string `json:"auth_indexes"`
}

func registerQuotaRoutes(router gin.IRoutes, provider QuotaProvider) {
	router.POST("/quota/cache", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		// 缓存读取只校验查询列表；列表返回多少 auth_index，就按相同数量读取缓存。
		var request quotaRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}
		if len(request.AuthIndexes) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}

		response, err := provider.GetCachedQuota(c.Request.Context(), quota.CacheRequest{AuthIndexes: request.AuthIndexes})
		if err != nil {
			switch {
			case errors.Is(err, quota.ErrValidation):
				c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			default:
				writeInternalError(c, "quota cache lookup failed", err)
			}
			return
		}

		c.JSON(http.StatusOK, response)
	})

	router.GET("/quota/inspection", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		response, err := provider.GetInspectionStatus(c.Request.Context())
		if err != nil {
			writeInternalError(c, "quota inspection status lookup failed", err)
			return
		}

		c.JSON(http.StatusOK, response)
	})

	router.POST("/quota/inspection", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		response, err := provider.StartInspection(c.Request.Context())
		if err != nil {
			writeInternalError(c, "quota inspection start failed", err)
			return
		}

		c.JSON(http.StatusOK, response)
	})

	router.POST("/quota/refresh", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		var request quotaRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}
		if len(request.AuthIndexes) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}

		response, err := provider.Refresh(c.Request.Context(), quota.RefreshRequest{
			AuthIndexes: request.AuthIndexes,
			Source:      quota.RefreshSourceManual,
		})
		if err != nil {
			switch {
			case errors.Is(err, quota.ErrValidation):
				c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			default:
				writeInternalError(c, "quota refresh failed", err)
			}
			return
		}

		c.JSON(http.StatusOK, response)
	})

	router.GET("/quota/refresh/:auth_index", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}
		authIndex := strings.TrimSpace(c.Param("auth_index"))
		if authIndex == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			return
		}

		// 前端轮询直接以 auth_index 查询任务状态，避免维护额外 taskId 映射。
		response, err := provider.GetRefreshTaskByAuthIndex(c.Request.Context(), authIndex)
		if err != nil {
			switch {
			case errors.Is(err, quota.ErrTaskNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "quota refresh task not found"})
			default:
				writeInternalError(c, "quota refresh task lookup failed", err)
			}
			return
		}

		c.JSON(http.StatusOK, response)
	})
}
