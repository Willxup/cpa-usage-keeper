package api

import (
	"errors"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/quota"
	"github.com/gin-gonic/gin"
)

const quotaRefreshMaxAuthIndexes = 20

type quotaCheckRequest struct {
	AuthIndex string `json:"auth_index"`
}

type quotaRefreshRequest struct {
	AuthIndexes []string `json:"auth_indexes"`
	Limit       int      `json:"limit"`
}

func registerQuotaRoutes(router gin.IRoutes, provider QuotaProvider) {
	router.POST("/quota/check", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		// 先解析并校验 auth_index，避免空值进入后端身份解析流程。
		var request quotaCheckRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			return
		}
		request.AuthIndex = strings.TrimSpace(request.AuthIndex)
		if request.AuthIndex == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			return
		}

		// 统一把 service 层错误映射为前端可展示的 HTTP 状态和提示文案。
		response, err := provider.Check(c.Request.Context(), quota.CheckRequest{AuthIndex: request.AuthIndex})
		if err != nil {
			switch {
			case errors.Is(err, quota.ErrValidation):
				c.JSON(http.StatusBadRequest, gin.H{"error": "auth_index is required"})
			case errors.Is(err, quota.ErrNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "quota identity not found"})
			case errors.Is(err, quota.ErrUnsupportedType):
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "quota identity type is unsupported"})
			case errors.Is(err, quota.ErrProviderInput):
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": quotaProviderInputErrorMessage(err)})
			default:
				writeInternalError(c, "quota check failed", err)
			}
			return
		}

		c.JSON(http.StatusOK, response)
	})

	router.POST("/quota/cache", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		// 缓存读取只校验查询列表，不套用刷新队列的 20 条上限。
		var request quotaRefreshRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}
		if len(request.AuthIndexes) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}
		if request.Limit <= 0 {
			request.Limit = len(request.AuthIndexes)
		}

		response, err := provider.GetCachedQuota(c.Request.Context(), quota.CacheRequest{
			AuthIndexes: request.AuthIndexes,
			Limit:       request.Limit,
		})
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

	router.POST("/quota/refresh", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}

		// 手动刷新会真正触发 provider 请求，所以在入口层限制当前页最多 20 条。
		var request quotaRefreshRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}
		if len(request.AuthIndexes) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes are required"})
			return
		}
		if len(request.AuthIndexes) > quotaRefreshMaxAuthIndexes {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_indexes must not exceed 20"})
			return
		}
		if request.Limit <= 0 {
			request.Limit = quotaRefreshMaxAuthIndexes
		}
		if request.Limit > quotaRefreshMaxAuthIndexes {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must not exceed 20"})
			return
		}

		response, err := provider.Refresh(c.Request.Context(), quota.RefreshRequest{
			AuthIndexes: request.AuthIndexes,
			Limit:       request.Limit,
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

	router.GET("/quota/refresh/:task_id", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "quota provider is not configured", nil)
			return
		}
		taskID := strings.TrimSpace(c.Param("task_id"))
		if taskID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "task_id is required"})
			return
		}

		// 前端轮询只根据 task_id 查询任务状态，完成时直接带回缓存中的 quota。
		response, err := provider.GetRefreshTask(c.Request.Context(), taskID)
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

func quotaProviderInputErrorMessage(err error) string {
	return quota.ProviderInputErrorMessage(err, "quota provider input is invalid")
}
