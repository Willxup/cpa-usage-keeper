package api

import (
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

func loadUsageResolutionData(
	c *gin.Context,
	usageIdentityProvider service.UsageIdentityProvider,
) ([]models.UsageIdentity, error) {
	if usageIdentityProvider == nil {
		return []models.UsageIdentity{}, nil
	}
	return usageIdentityProvider.ListUsageIdentities(c.Request.Context())
}
