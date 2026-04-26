package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/redact"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageAnalysisResponse struct {
	APIs   []usageAnalysisAPIPayload   `json:"apis"`
	Models []usageAnalysisModelPayload `json:"models"`
}

type usageAnalysisAPIPayload struct {
	APIKey         string                      `json:"api_key"`
	DisplayName    string                      `json:"display_name"`
	TotalRequests  int64                       `json:"total_requests"`
	SuccessCount   int64                       `json:"success_count"`
	FailureCount   int64                       `json:"failure_count"`
	InputTokens    int64                       `json:"input_tokens"`
	OutputTokens   int64                       `json:"output_tokens"`
	ReasoningTokens int64                      `json:"reasoning_tokens"`
	CachedTokens   int64                       `json:"cached_tokens"`
	TotalTokens    int64                       `json:"total_tokens"`
	Models         []usageAnalysisModelPayload `json:"models"`
}

type usageAnalysisModelPayload struct {
	Model              string `json:"model"`
	TotalRequests      int64  `json:"total_requests"`
	SuccessCount       int64  `json:"success_count"`
	FailureCount       int64  `json:"failure_count"`
	InputTokens        int64  `json:"input_tokens"`
	OutputTokens       int64  `json:"output_tokens"`
	ReasoningTokens    int64  `json:"reasoning_tokens"`
	CachedTokens       int64  `json:"cached_tokens"`
	TotalTokens        int64  `json:"total_tokens"`
	TotalLatencyMS     int64  `json:"total_latency_ms"`
	LatencySampleCount int64  `json:"latency_sample_count"`
}

func registerUsageAnalysisRoute(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage/analysis", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageAnalysisResponse{APIs: []usageAnalysisAPIPayload{}, Models: []usageAnalysisModelPayload{}})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, time.Now().UTC())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		analysis, err := usageProvider.GetUsageAnalysis(c.Request.Context(), filter)
		if err != nil {
			writeInternalError(c, "get usage analysis failed", err)
			return
		}

		c.JSON(http.StatusOK, buildUsageAnalysisPayload(analysis))
	})
}

func buildUsageAnalysisPayload(snapshot *service.UsageAnalysisSnapshot) usageAnalysisResponse {
	if snapshot == nil {
		return usageAnalysisResponse{APIs: []usageAnalysisAPIPayload{}, Models: []usageAnalysisModelPayload{}}
	}

	apis := make([]usageAnalysisAPIPayload, 0, len(snapshot.APIs))
	for _, api := range snapshot.APIs {
		models := make([]usageAnalysisModelPayload, 0, len(api.Models))
		for _, model := range api.Models {
			models = append(models, usageAnalysisModelPayload{
				Model:              model.Model,
				TotalRequests:      model.TotalRequests,
				SuccessCount:       model.SuccessCount,
				FailureCount:       model.FailureCount,
				InputTokens:        model.InputTokens,
				OutputTokens:       model.OutputTokens,
				ReasoningTokens:    model.ReasoningTokens,
				CachedTokens:       model.CachedTokens,
				TotalTokens:        model.TotalTokens,
				TotalLatencyMS:     model.TotalLatencyMS,
				LatencySampleCount: model.LatencySampleCount,
			})
		}
		apiKey := redact.APIAlias(api.APIKey)
		displayName := redact.APIKeyDisplayName(api.APIKey)
		apis = append(apis, usageAnalysisAPIPayload{
			APIKey:          apiKey,
			DisplayName:     displayName,
			TotalRequests:   api.TotalRequests,
			SuccessCount:    api.SuccessCount,
			FailureCount:    api.FailureCount,
			InputTokens:     api.InputTokens,
			OutputTokens:    api.OutputTokens,
			ReasoningTokens: api.ReasoningTokens,
			CachedTokens:    api.CachedTokens,
			TotalTokens:     api.TotalTokens,
			Models:          models,
		})
	}

	models := make([]usageAnalysisModelPayload, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		models = append(models, usageAnalysisModelPayload{
			Model:              model.Model,
			TotalRequests:      model.TotalRequests,
			SuccessCount:       model.SuccessCount,
			FailureCount:       model.FailureCount,
			InputTokens:        model.InputTokens,
			OutputTokens:       model.OutputTokens,
			ReasoningTokens:    model.ReasoningTokens,
			CachedTokens:       model.CachedTokens,
			TotalTokens:        model.TotalTokens,
			TotalLatencyMS:     model.TotalLatencyMS,
			LatencySampleCount: model.LatencySampleCount,
		})
	}

	return usageAnalysisResponse{APIs: apis, Models: models}
}
