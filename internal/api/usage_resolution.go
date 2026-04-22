package api

import (
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

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
