package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
)

type usedModelsResponse struct {
	Models []string `json:"models"`
}

type pricingEntryResponse struct {
	Model                string  `json:"model"`
	PricingStyle         string  `json:"pricing_style"`
	PromptPricePer1M     float64 `json:"prompt_price_per_1m"`
	CompletionPricePer1M float64 `json:"completion_price_per_1m"`
	CacheReadPricePer1M  float64 `json:"cache_read_price_per_1m"`
	CacheWritePricePer1M float64 `json:"cache_write_price_per_1m"`
	PriceMultiplier      float64 `json:"price_multiplier"`
}

type pricingListResponse struct {
	Pricing []pricingEntryResponse `json:"pricing"`
}

type updatePricingRequest struct {
	Model                         string          `json:"model"`
	PricingStyle                  string          `json:"pricing_style"`
	PromptPricePer1M              float64         `json:"prompt_price_per_1m"`
	CompletionPricePer1M          float64         `json:"completion_price_per_1m"`
	CacheReadPricePer1M           float64         `json:"cache_read_price_per_1m"`
	CacheWritePricePer1M          float64         `json:"cache_write_price_per_1m"`
	PriceMultiplier               *float64        `json:"price_multiplier"`
	LegacyCachePricePer1M         json.RawMessage `json:"cache_price_per_1m"`
	LegacyCacheCreationPricePer1M json.RawMessage `json:"cache_creation_price_per_1m"`
}

func registerPricingRoutes(router gin.IRoutes, pricingProvider service.PricingProvider) {
	router.GET("/models/used", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusOK, usedModelsResponse{Models: []string{}})
			return
		}

		models, err := pricingProvider.ListUsedModels(c.Request.Context())
		if err != nil {
			writeInternalError(c, "list used models failed", err)
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
			writeInternalError(c, "list pricing failed", err)
			return
		}

		response := make([]pricingEntryResponse, 0, len(settings))
		for _, setting := range settings {
			response = append(response, pricingEntryResponse{
				Model:                setting.Model,
				PricingStyle:         setting.PricingStyle,
				PromptPricePer1M:     setting.PromptPricePer1M,
				CompletionPricePer1M: setting.CompletionPricePer1M,
				CacheReadPricePer1M:  setting.CacheReadPricePer1M,
				CacheWritePricePer1M: setting.CacheWritePricePer1M,
				PriceMultiplier:      modelPriceMultiplierValue(setting.PriceMultiplier),
			})
		}
		c.JSON(http.StatusOK, pricingListResponse{Pricing: response})
	})

	router.GET("/pricing/sync/preview", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusOK, servicedto.PricingSyncPreview{
				Source:          "Models.dev",
				Matches:         []servicedto.PricingSyncMatch{},
				UnmatchedModels: []string{},
			})
			return
		}

		preview, err := pricingProvider.PreviewPricingSync(c.Request.Context())
		if err != nil {
			writeInternalError(c, "preview pricing sync failed", err)
			return
		}
		if preview.Matches == nil {
			preview.Matches = []servicedto.PricingSyncMatch{}
		}
		if preview.UnmatchedModels == nil {
			preview.UnmatchedModels = []string{}
		}
		c.JSON(http.StatusOK, preview)
	})

	router.PUT("/pricing", func(c *gin.Context) {
		updatePricing(c, pricingProvider, "")
	})

	router.PUT("/pricing/:model", func(c *gin.Context) {
		updatePricing(c, pricingProvider, c.Param("model"))
	})

	router.DELETE("/pricing", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "pricing provider is not configured"})
			return
		}
		model := strings.TrimSpace(c.Query("model"))
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
			return
		}
		if err := pricingProvider.DeletePricing(c.Request.Context(), model); err != nil {
			if strings.Contains(err.Error(), "required") {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			writeInternalError(c, "delete pricing failed", err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}

func updatePricing(c *gin.Context, pricingProvider service.PricingProvider, pathModel string) {
	if pricingProvider == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "pricing provider is not configured"})
		return
	}

	var request updatePricingRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(request.LegacyCachePricePer1M) > 0 || len(request.LegacyCacheCreationPricePer1M) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "legacy cache price fields are not supported"})
		return
	}

	model := strings.TrimSpace(pathModel)
	if model == "" {
		model = strings.TrimSpace(request.Model)
	}
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	setting, err := pricingProvider.UpdatePricing(c.Request.Context(), servicedto.UpdatePricingInput{
		Model:                model,
		PricingStyle:         request.PricingStyle,
		PromptPricePer1M:     request.PromptPricePer1M,
		CompletionPricePer1M: request.CompletionPricePer1M,
		CacheReadPricePer1M:  request.CacheReadPricePer1M,
		CacheWritePricePer1M: request.CacheWritePricePer1M,
		PriceMultiplier:      request.PriceMultiplier,
	})
	if err != nil {
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "non-negative") || strings.Contains(err.Error(), "pricing_style") || strings.Contains(err.Error(), "price_multiplier") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		writeInternalError(c, "update pricing failed", err)
		return
	}

	c.JSON(http.StatusOK, pricingEntryResponse{
		Model:                setting.Model,
		PricingStyle:         setting.PricingStyle,
		PromptPricePer1M:     setting.PromptPricePer1M,
		CompletionPricePer1M: setting.CompletionPricePer1M,
		CacheReadPricePer1M:  setting.CacheReadPricePer1M,
		CacheWritePricePer1M: setting.CacheWritePricePer1M,
		PriceMultiplier:      modelPriceMultiplierValue(setting.PriceMultiplier),
	})
}

func modelPriceMultiplierValue(multiplier *float64) float64 {
	if multiplier == nil {
		return 1
	}
	return *multiplier
}
