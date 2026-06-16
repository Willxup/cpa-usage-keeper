package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
)

type usedModelsResponse struct {
	Models []string `json:"models"`
}

type pricingEntryResponse struct {
	Model                   string  `json:"model"`
	PricingStyle            string  `json:"pricing_style"`
	PromptPricePer1M        float64 `json:"prompt_price_per_1m"`
	CompletionPricePer1M    float64 `json:"completion_price_per_1m"`
	CachePricePer1M         float64 `json:"cache_price_per_1m"`
	CacheCreationPricePer1M float64 `json:"cache_creation_price_per_1m"`
	Source                  string  `json:"source,omitempty"`
	SourceURL               string  `json:"source_url,omitempty"`
	SyncedAt                *string `json:"synced_at,omitempty"`
}

type pricingListResponse struct {
	Pricing []pricingEntryResponse `json:"pricing"`
}

type updatePricingRequest struct {
	Model                   string  `json:"model"`
	PricingStyle            string  `json:"pricing_style"`
	PromptPricePer1M        float64 `json:"prompt_price_per_1m"`
	CompletionPricePer1M    float64 `json:"completion_price_per_1m"`
	CachePricePer1M         float64 `json:"cache_price_per_1m"`
	CacheCreationPricePer1M float64 `json:"cache_creation_price_per_1m"`
}

type syncPricingRequest struct {
	OverwriteManual bool `json:"overwrite_manual"`
}

type pricingSyncResponse struct {
	Source              string   `json:"source"`
	SourceURL           string   `json:"source_url"`
	SyncedAt            string   `json:"synced_at"`
	ModelsChecked       int      `json:"models_checked"`
	CreatedModels       []string `json:"created_models"`
	UpdatedModels       []string `json:"updated_models"`
	MissingModels       []string `json:"missing_models"`
	SkippedManualModels []string `json:"skipped_manual_models"`
}

type pricingSyncStatusResponse struct {
	Running      bool                 `json:"running"`
	LastSyncedAt *string              `json:"last_synced_at,omitempty"`
	LastError    string               `json:"last_error,omitempty"`
	LastResult   *pricingSyncResponse `json:"last_result,omitempty"`
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
				Model:                   setting.Model,
				PricingStyle:            setting.PricingStyle,
				PromptPricePer1M:        setting.PromptPricePer1M,
				CompletionPricePer1M:    setting.CompletionPricePer1M,
				CachePricePer1M:         setting.CachePricePer1M,
				CacheCreationPricePer1M: setting.CacheCreationPricePer1M,
				Source:                  setting.Source,
				SourceURL:               setting.SourceURL,
				SyncedAt:                optionalTimeString(setting.SyncedAt),
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

	router.GET("/pricing/sync", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusOK, pricingSyncStatusResponse{Running: false})
			return
		}
		status := pricingProvider.GetPricingSyncStatus(c.Request.Context())
		response := pricingSyncStatusResponse{
			Running:      status.Running,
			LastSyncedAt: optionalTimeString(status.LastSyncedAt),
			LastError:    status.LastError,
		}
		if status.LastResult != nil {
			mapped := mapPricingSyncResult(*status.LastResult)
			response.LastResult = &mapped
		}
		c.JSON(http.StatusOK, response)
	})

	router.POST("/pricing/sync", func(c *gin.Context) {
		if pricingProvider == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "pricing provider is not configured"})
			return
		}
		var request syncPricingRequest
		if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		result, err := pricingProvider.SyncPricing(c.Request.Context(), servicedto.SyncPricingInput{OverwriteManual: request.OverwriteManual})
		if err != nil {
			writeInternalError(c, "sync pricing failed", err)
			return
		}
		c.JSON(http.StatusOK, mapPricingSyncResult(result))
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

	model := strings.TrimSpace(pathModel)
	if model == "" {
		model = strings.TrimSpace(request.Model)
	}
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	setting, err := pricingProvider.UpdatePricing(c.Request.Context(), servicedto.UpdatePricingInput{
		Model:                   model,
		PricingStyle:            request.PricingStyle,
		PromptPricePer1M:        request.PromptPricePer1M,
		CompletionPricePer1M:    request.CompletionPricePer1M,
		CachePricePer1M:         request.CachePricePer1M,
		CacheCreationPricePer1M: request.CacheCreationPricePer1M,
	})
	if err != nil {
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "non-negative") || strings.Contains(err.Error(), "pricing_style") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		writeInternalError(c, "update pricing failed", err)
		return
	}

	c.JSON(http.StatusOK, pricingEntryResponse{
		Model:                   setting.Model,
		PricingStyle:            setting.PricingStyle,
		PromptPricePer1M:        setting.PromptPricePer1M,
		CompletionPricePer1M:    setting.CompletionPricePer1M,
		CachePricePer1M:         setting.CachePricePer1M,
		CacheCreationPricePer1M: setting.CacheCreationPricePer1M,
		Source:                  setting.Source,
		SourceURL:               setting.SourceURL,
		SyncedAt:                optionalTimeString(setting.SyncedAt),
	})
}

func mapPricingSyncResult(result servicedto.PricingSyncResult) pricingSyncResponse {
	return pricingSyncResponse{
		Source:              result.Source,
		SourceURL:           result.SourceURL,
		SyncedAt:            result.SyncedAt.Format(time.RFC3339Nano),
		ModelsChecked:       result.ModelsChecked,
		CreatedModels:       result.CreatedModels,
		UpdatedModels:       result.UpdatedModels,
		MissingModels:       result.MissingModels,
		SkippedManualModels: result.SkippedManualModels,
	}
}

func optionalTimeString(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.Format(time.RFC3339Nano)
	return &formatted
}
