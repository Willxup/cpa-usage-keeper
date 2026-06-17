package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/quota"
	"github.com/gin-gonic/gin"
)

type quotaRequest struct {
	AuthIndexes []string `json:"auth_indexes"`
}

const quotaResetErrorFailed = "quota_reset_failed"

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
	router.POST("/quota/reset", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		var request struct {
			AuthIndex string `json:"auth_index"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			return
		}
		authIndex := strings.TrimSpace(request.AuthIndex)
		if authIndex == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			return
		}

		response, err := provider.Reset(c.Request.Context(), quota.ResetRequest{AuthIndex: authIndex})
		if err != nil {
			switch {
			case errors.Is(err, quota.ErrValidation):
				c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			case errors.Is(err, quota.ErrNotFound):
				writeQuotaResetError(c, http.StatusNotFound, err)
			case errors.Is(err, quota.ErrUnsupportedType):
				writeQuotaResetError(c, http.StatusBadRequest, err)
			case errors.Is(err, quota.ErrResetInProgress):
				writeQuotaResetError(c, http.StatusConflict, err)
			default:
				var httpErr quota.ProviderHTTPError
				if errors.As(err, &httpErr) && httpErr.StatusCode >= 100 && httpErr.StatusCode <= 599 {
					statusCode := httpErr.StatusCode
					if statusCode == http.StatusUnauthorized {
						// 这里的 401 来自 Codex 官方接口，不代表 dashboard 登录态失效，前端应按 reset 失败提示处理。
						statusCode = http.StatusBadGateway
					}
					writeQuotaResetError(c, statusCode, err)
					return
				}
				slog.Error("quota reset failed", "error", err)
				writeQuotaResetError(c, http.StatusInternalServerError, err)
			}
			return
		}

		c.JSON(http.StatusOK, response)
	})

}

func writeQuotaResetError(c *gin.Context, statusCode int, err error) {
	payload := gin.H{"error": quotaResetErrorFailed}
	if err != nil {
		// detail 仅用于浏览器 Network/F12 排查，不作为前端展示文案。
		payload["detail"] = err.Error()
	}
	c.JSON(statusCode, payload)
}
