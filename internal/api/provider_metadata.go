package api

import (
	"net/http"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type providerMetadataListResponse struct {
	Items []providerMetadataResponse `json:"items"`
}

type providerMetadataResponse struct {
	LookupKey    string `json:"lookup_key"`
	ProviderType string `json:"provider_type,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	ProviderKey  string `json:"provider_key,omitempty"`
}

func registerProviderMetadataRoutes(router gin.IRoutes, provider service.ProviderMetadataProvider) {
	router.GET("/provider-metadata", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusOK, providerMetadataListResponse{Items: []providerMetadataResponse{}})
			return
		}

		items, err := provider.ListProviderMetadata(c.Request.Context())
		if err != nil {
			writeInternalError(c, "list provider metadata failed", err)
			return
		}

		response := make([]providerMetadataResponse, 0, len(items))
		for _, item := range items {
			response = append(response, mapProviderMetadataResponse(item))
		}
		c.JSON(http.StatusOK, providerMetadataListResponse{Items: response})
	})
}

func mapProviderMetadataResponse(item models.ProviderMetadata) providerMetadataResponse {
	resolved := usageSourceResolutionFromMetadata(item, item.LookupKey)
	return providerMetadataResponse{
		LookupKey:    resolved.SourceKey,
		ProviderType: item.ProviderType,
		DisplayName:  resolved.DisplayName,
		ProviderKey:  resolved.SourceKey,
	}
}
