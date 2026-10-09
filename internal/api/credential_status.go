package api

import (
	"errors"
	"net/http"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type credentialStatusRequest struct {
	Disabled *bool `json:"disabled"`
}

// registerCredentialStatusRoutes 注册认证文件与 AI 供应商的单条状态开关，前端只提供 auth_index。
func registerCredentialStatusRoutes(router gin.IRoutes, provider service.CredentialStatusProvider) {
	router.PATCH("/auth-files/:auth_index/status", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "credential status provider is not configured", nil)
			return
		}
		request, ok := bindCredentialStatusRequest(c)
		if !ok {
			return
		}
		response, err := provider.SetAuthFileDisabled(c.Request.Context(), c.Param("auth_index"), *request.Disabled)
		if err != nil {
			writeCredentialStatusError(c, err, "auth file status update failed")
			return
		}
		c.JSON(http.StatusOK, response)
	})

	router.PATCH("/ai-providers/:auth_index/status", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "credential status provider is not configured", nil)
			return
		}
		request, ok := bindCredentialStatusRequest(c)
		if !ok {
			return
		}
		response, err := provider.SetAIProviderDisabled(c.Request.Context(), c.Param("auth_index"), *request.Disabled)
		if err != nil {
			writeCredentialStatusError(c, err, "AI provider status update failed")
			return
		}
		c.JSON(http.StatusOK, response)
	})
}

func bindCredentialStatusRequest(c *gin.Context) (credentialStatusRequest, bool) {
	var request credentialStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Disabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "disabled is required"})
		return credentialStatusRequest{}, false
	}
	return request, true
}

// writeCredentialStatusError 把服务层哨兵错误映射为稳定的 HTTP 状态，其它错误保持 500。
func writeCredentialStatusError(c *gin.Context, err error, message string) {
	switch {
	case errors.Is(err, service.ErrCredentialStatusValidation):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credential status request"})
	case errors.Is(err, service.ErrCredentialStatusNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
	case errors.Is(err, service.ErrCredentialStatusUnsupported):
		c.JSON(http.StatusConflict, gin.H{"error": "credential status is not supported"})
	case errors.Is(err, service.ErrCredentialStatusConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "credential status target cannot be changed on its own"})
	default:
		writeInternalError(c, message, err)
	}
}
