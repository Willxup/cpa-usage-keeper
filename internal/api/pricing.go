package api

import (
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usedModelsResponse struct {
	Models []string `json:"models"`
}

type pricingEntryResponse struct {
	Model                string  `json:"model"`
	PromptPricePer1M     float64 `json:"prompt_price_per_1m"`
	CompletionPricePer1M float64 `json:"completion_price_per_1m"`
	CachePricePer1M      float64 `json:"cache_price_per_1m"`
}

type pricingListResponse struct {
	Pricing []pricingEntryResponse `json:"pricing"`
}

type updatePricingRequest struct {
	PromptPricePer1M     float64 `json:"prompt_price_per_1m"`
	CompletionPricePer1M float64 `json:"completion_price_per_1m"`
	CachePricePer1M      float64 `json:"cache_price_per_1m"`
}

func registerPricingRoutes(router gin.IRoutes, pricingProvider service.PricingProvider) {
	router.GET("/models/used", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusOK, usedModelsResponse{Models: []string{}})
			return
		}

		models, err := pricingProvider.ListUsedModels(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, usedModelsResponse{Models: models})
	})

	router.GET("/pricing", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusOK, pricingListResponse{Pricing: []pricingEntryResponse{}})
			return
		}

		settings, err := pricingProvider.ListPricing(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		response := make([]pricingEntryResponse, 0, len(settings))
		for _, setting := range settings {
			response = append(response, pricingEntryResponse{
				Model:                setting.Model,
				PromptPricePer1M:     setting.PromptPricePer1M,
				CompletionPricePer1M: setting.CompletionPricePer1M,
				CachePricePer1M:      setting.CachePricePer1M,
			})
		}
		c.JSON(http.StatusOK, pricingListResponse{Pricing: response})
	})

	router.PUT("/pricing/:model", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "pricing provider is not configured"})
			return
		}

		model := strings.TrimSpace(c.Param("model"))
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
			return
		}

		var request updatePricingRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		setting, err := pricingProvider.UpdatePricing(c.Request.Context(), service.UpdatePricingInput{
			Model:                model,
			PromptPricePer1M:     request.PromptPricePer1M,
			CompletionPricePer1M: request.CompletionPricePer1M,
			CachePricePer1M:      request.CachePricePer1M,
		})
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "has not been used") || strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "non-negative") {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, pricingEntryResponse{
			Model:                setting.Model,
			PromptPricePer1M:     setting.PromptPricePer1M,
			CompletionPricePer1M: setting.CompletionPricePer1M,
			CachePricePer1M:      setting.CachePricePer1M,
		})
	})
}
