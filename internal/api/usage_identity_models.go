package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type usageIdentityModelTotalsResponse struct {
	RequestCount         int64   `json:"request_count"`
	InputTokens          int64   `json:"input_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	ReasoningTokens      int64   `json:"reasoning_tokens"`
	CacheReadTokens      int64   `json:"cache_read_tokens"`
	CacheCreationTokens  int64   `json:"cache_creation_tokens"`
	TotalTokens          int64   `json:"total_tokens"`
	CostUSD              float64 `json:"cost_usd"`
	UnavailableCostCount int64   `json:"unavailable_cost_count"`
	CostAvailable        bool    `json:"cost_available"`
}

type usageIdentityModelResponse struct {
	Model string `json:"model"`
	usageIdentityModelTotalsResponse
}

type usageIdentityModelsResponse struct {
	IdentityID string                           `json:"identity_id"`
	Range      string                           `json:"range"`
	Timezone   string                           `json:"timezone"`
	RangeStart time.Time                        `json:"range_start"`
	RangeEnd   time.Time                        `json:"range_end"`
	Models     []usageIdentityModelResponse     `json:"models"`
	Summary    usageIdentityModelTotalsResponse `json:"summary"`
}

func registerUsageIdentityModelsRoute(router gin.IRoutes, provider service.UsageIdentityProvider) {
	router.GET("/usage/identities/:id/models", func(c *gin.Context) {
		reader, ok := provider.(service.UsageIdentityModelsReader)
		if !ok {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "usage identity models are not configured"})
			return
		}
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid usage identity id"})
			return
		}
		result, err := reader.GetUsageIdentityModels(c.Request.Context(), id, c.Query("range"))
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidUsageIdentityModelsRange):
				c.JSON(http.StatusBadRequest, gin.H{"error": "range must be today, 7d, or 30d"})
			case errors.Is(err, gorm.ErrRecordNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": "usage identity not found"})
			default:
				writeInternalError(c, "get usage identity models failed", err)
			}
			return
		}
		models := make([]usageIdentityModelResponse, 0, len(result.Models))
		for _, model := range result.Models {
			models = append(models, usageIdentityModelResponse{
				Model: model.Model,
				usageIdentityModelTotalsResponse: mapUsageIdentityModelTotals(service.UsageIdentityModelTotals{
					RequestCount: model.RequestCount, InputTokens: model.InputTokens, OutputTokens: model.OutputTokens,
					ReasoningTokens: model.ReasoningTokens, CacheReadTokens: model.CacheReadTokens,
					CacheCreationTokens: model.CacheCreationTokens, TotalTokens: model.TotalTokens,
					CostUSD: model.CostUSD, UnavailableCostCount: model.UnavailableCostCount,
				}),
			})
		}
		c.JSON(http.StatusOK, usageIdentityModelsResponse{
			IdentityID: strconv.FormatInt(id, 10), Range: result.Range, Timezone: result.Timezone,
			RangeStart: result.RangeStart, RangeEnd: result.RangeEnd, Models: models,
			Summary: mapUsageIdentityModelTotals(result.Summary),
		})
	})
}

func mapUsageIdentityModelTotals(totals service.UsageIdentityModelTotals) usageIdentityModelTotalsResponse {
	return usageIdentityModelTotalsResponse{
		RequestCount: totals.RequestCount, InputTokens: totals.InputTokens, OutputTokens: totals.OutputTokens,
		ReasoningTokens: totals.ReasoningTokens, CacheReadTokens: totals.CacheReadTokens,
		CacheCreationTokens: totals.CacheCreationTokens, TotalTokens: totals.TotalTokens,
		CostUSD: totals.CostUSD, UnavailableCostCount: totals.UnavailableCostCount,
		CostAvailable: totals.UnavailableCostCount == 0,
	}
}
