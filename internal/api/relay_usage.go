package api

import (
	"context"
	"errors"
	"net/http"

	"cpa-usage-keeper/internal/relayusage"
	"github.com/gin-gonic/gin"
)

// RelayUsageProvider 是中转商用量查询服务在 API 层的抽象，便于测试注入。
type RelayUsageProvider interface {
	GetUsage(context.Context, relayusage.UsageRequest) (relayusage.UsageResponse, error)
	GetPlatformAssignments(context.Context, []string) ([]relayusage.PlatformAssignment, error)
	GetPlatformOverrides(context.Context) (map[string]string, error)
	UpdatePlatformOverrides(context.Context, relayusage.PlatformOverridesRequest) (map[string]string, error)
}

func registerRelayUsageRoutes(router gin.IRoutes, provider RelayUsageProvider) {
	router.POST("/usage/relay-provider/usage", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "relay usage provider is not configured", nil)
			return
		}
		var request relayusage.UsageRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "identity_ids are required"})
			return
		}
		if len(request.IdentityIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "identity_ids are required"})
			return
		}
		response, err := provider.GetUsage(c.Request.Context(), request)
		if err != nil {
			if errors.Is(err, relayusage.ErrValidation) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "identity_ids are required"})
				return
			}
			writeInternalError(c, "relay usage lookup failed", err)
			return
		}
		c.JSON(http.StatusOK, response)
	})

	router.POST("/usage/relay-provider/assignments", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "relay usage provider is not configured", nil)
			return
		}
		var request relayusage.UsageRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "identity_ids are required"})
			return
		}
		if len(request.IdentityIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "identity_ids are required"})
			return
		}
		assignments, err := provider.GetPlatformAssignments(c.Request.Context(), request.IdentityIDs)
		if err != nil {
			writeInternalError(c, "relay platform assignment lookup failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"assignments": assignments})
	})

	router.GET("/usage/relay-provider/platform-overrides", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "relay usage provider is not configured", nil)
			return
		}
		overrides, err := provider.GetPlatformOverrides(c.Request.Context())
		if err != nil {
			writeInternalError(c, "relay platform overrides lookup failed", err)
			return
		}
		if overrides == nil {
			overrides = map[string]string{}
		}
		c.JSON(http.StatusOK, gin.H{"overrides": overrides})
	})

	router.PUT("/usage/relay-provider/platform-overrides", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "relay usage provider is not configured", nil)
			return
		}
		var request relayusage.PlatformOverridesRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "overrides are required"})
			return
		}
		overrides, err := provider.UpdatePlatformOverrides(c.Request.Context(), request)
		if err != nil {
			writeInternalError(c, "relay platform overrides update failed", err)
			return
		}
		if overrides == nil {
			overrides = map[string]string{}
		}
		c.JSON(http.StatusOK, gin.H{"overrides": overrides})
	})
}
