package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageEventsResponse struct {
	Events []usageEventPayload `json:"events"`
}

type usageEventPayload struct {
	Timestamp string                 `json:"timestamp"`
	Model     string                 `json:"model"`
	Source    string                 `json:"source"`
	SourceRaw string                 `json:"source_raw,omitempty"`
	SourceType string                `json:"source_type,omitempty"`
	SourceKey string                 `json:"source_key,omitempty"`
	AuthIndex string                 `json:"auth_index,omitempty"`
	Failed    bool                   `json:"failed"`
	LatencyMS int64                  `json:"latency_ms"`
	Tokens    usageEventTokenPayload `json:"tokens"`
}

type usageEventTokenPayload struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

func registerUsageEventsRoute(
	router gin.IRoutes,
	usageProvider service.UsageProvider,
	authFileProvider service.AuthFileProvider,
	providerMetadataProvider service.ProviderMetadataProvider,
) {
	router.GET("/usage/events", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageEventsResponse{Events: []usageEventPayload{}})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, time.Now().UTC())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		rows, err := usageProvider.ListUsageEvents(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		authFiles, providerMetadata, err := loadUsageResolutionData(c, authFileProvider, providerMetadataProvider)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		resolver := newUsageSourceResolver(authFiles, providerMetadata)
		c.JSON(http.StatusOK, usageEventsResponse{Events: buildUsageEventsPayload(rows, resolver)})
	})
}

func buildUsageEventsPayload(rows []service.UsageEventRecord, resolver usageSourceResolver) []usageEventPayload {
	if len(rows) == 0 {
		return []usageEventPayload{}
	}
	payload := make([]usageEventPayload, 0, len(rows))
	for _, row := range rows {
		resolved := resolver.resolve(row.Source, row.AuthIndex)
		payload = append(payload, usageEventPayload{
			Timestamp: row.Timestamp.UTC().Format(time.RFC3339),
			Model:     row.Model,
			Source:    resolved.DisplayName,
			SourceRaw: row.Source,
			SourceType: resolved.SourceType,
			SourceKey: resolved.SourceKey,
			AuthIndex: row.AuthIndex,
			Failed:    row.Failed,
			LatencyMS: row.LatencyMS,
			Tokens: usageEventTokenPayload{
				InputTokens:     row.InputTokens,
				OutputTokens:    row.OutputTokens,
				ReasoningTokens: row.ReasoningTokens,
				CachedTokens:    row.CachedTokens,
				TotalTokens:     row.TotalTokens,
			},
		})
	}
	return payload
}

