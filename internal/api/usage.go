package api

import (
	"net/http"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/redact"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageResponse struct {
	Usage any `json:"usage"`
}

func registerUsageRoutes(
	router gin.IRoutes,
	usageProvider service.UsageProvider,
	authFileProvider service.AuthFileProvider,
	providerMetadataProvider service.ProviderMetadataProvider,
) {
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

		redactedUsage := redact.UsageSnapshot(usage)
		authFiles, providerMetadata, err := loadUsageResolutionData(c, authFileProvider, providerMetadataProvider)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		resolver := newUsageSourceResolver(authFiles, providerMetadata)
		applyUsageSourceResolution(redactedUsage, resolver)
		c.JSON(http.StatusOK, usageResponse{Usage: redactedUsage})
	})
}

func loadUsageResolutionData(
	c *gin.Context,
	authFileProvider service.AuthFileProvider,
	providerMetadataProvider service.ProviderMetadataProvider,
) ([]models.AuthFile, []models.ProviderMetadata, error) {
	authFiles := []models.AuthFile{}
	providerMetadata := []models.ProviderMetadata{}
	var err error
	if authFileProvider != nil {
		authFiles, err = authFileProvider.ListAuthFiles(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
	}
	if providerMetadataProvider != nil {
		providerMetadata, err = providerMetadataProvider.ListProviderMetadata(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
	}
	return authFiles, providerMetadata, nil
}

