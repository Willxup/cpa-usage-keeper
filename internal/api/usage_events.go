package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageEventsResponse struct {
	Events     []usageEventPayload       `json:"events"`
	Models     []string                  `json:"models"`
	Sources    []usageSourceFilterOption `json:"sources"`
	TotalCount int64                     `json:"total_count"`
	Page       int                       `json:"page"`
	PageSize   int                       `json:"page_size"`
	TotalPages int                       `json:"total_pages"`
}

type usageSourceFilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type usageEventFilterOptionsResponse struct {
	Models  []string                  `json:"models"`
	Sources []usageSourceFilterOption `json:"sources"`
}

type usageEventPayload struct {
	ID         uint                   `json:"id,omitempty"`
	Timestamp  string                 `json:"timestamp"`
	Model      string                 `json:"model"`
	Source     string                 `json:"source"`
	SourceRaw  string                 `json:"source_raw,omitempty"`
	SourceType string                 `json:"source_type,omitempty"`
	SourceKey  string                 `json:"source_key,omitempty"`
	AuthIndex  string                 `json:"auth_index,omitempty"`
	Failed     bool                   `json:"failed"`
	LatencyMS  int64                  `json:"latency_ms"`
	Tokens     usageEventTokenPayload `json:"tokens"`
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
	router.GET("/usage/events/filters", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageEventFilterOptionsResponse{Models: []string{}, Sources: []usageSourceFilterOption{}})
			return
		}

		filter, err := parseUsageTimeFilterQuery(c.Request, time.Now().UTC())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		options, err := usageProvider.ListUsageEventFilterOptions(c.Request.Context(), filter)
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
		c.JSON(http.StatusOK, usageEventFilterOptionsResponse{
			Models:  options.Models,
			Sources: buildUsageSourceFilterOptions(options.Sources, resolver),
		})
	})

	router.GET("/usage/events", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageEventsResponse{Events: []usageEventPayload{}, Models: []string{}, Sources: []usageSourceFilterOption{}, Page: 1, PageSize: service.DefaultUsageEventsLimit})
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
		c.JSON(http.StatusOK, usageEventsResponse{
			Events:     buildUsageEventsPayload(rows.Events, resolver),
			Models:     rows.Models,
			Sources:    buildUsageSourceFilterOptions(rows.Sources, resolver),
			TotalCount: rows.TotalCount,
			Page:       rows.Page,
			PageSize:   rows.PageSize,
			TotalPages: rows.TotalPages,
		})
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
			ID:         row.ID,
			Timestamp:  row.Timestamp.UTC().Format(time.RFC3339),
			Model:      row.Model,
			Source:     resolved.DisplayName,
			SourceRaw:  row.Source,
			SourceType: resolved.SourceType,
			SourceKey:  resolved.SourceKey,
			AuthIndex:  row.AuthIndex,
			Failed:     row.Failed,
			LatencyMS:  row.LatencyMS,
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

func buildUsageSourceFilterOptions(sources []string, resolver usageSourceResolver) []usageSourceFilterOption {
	if len(sources) == 0 {
		return []usageSourceFilterOption{}
	}
	options := make([]usageSourceFilterOption, 0, len(sources))
	for _, source := range sources {
		resolved := resolver.resolve(source, "")
		options = append(options, usageSourceFilterOption{Value: source, Label: resolved.DisplayName})
	}
	return options
}
