package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/models"
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
	usageIdentityProvider service.UsageIdentityProvider,
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
			writeInternalError(c, "list usage event filter options failed", err)
			return
		}

		identities, err := loadUsageResolutionData(c, usageIdentityProvider)
		if err != nil {
			writeInternalError(c, "load usage resolution data failed", err)
			return
		}
		c.JSON(http.StatusOK, usageEventFilterOptionsResponse{
			Models:  options.Models,
			Sources: buildUsageSourceFilterOptions(options.Sources, identities),
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
		if err := applyUsageEventsSourceFilter(&filter); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		rows, err := usageProvider.ListUsageEvents(c.Request.Context(), filter)
		if err != nil {
			writeInternalError(c, "list usage events failed", err)
			return
		}

		identities, err := loadUsageResolutionData(c, usageIdentityProvider)
		if err != nil {
			writeInternalError(c, "load usage resolution data failed", err)
			return
		}
		c.JSON(http.StatusOK, usageEventsResponse{
			Events:     buildUsageEventsPayload(rows.Events),
			Models:     rows.Models,
			Sources:    buildUsageSourceFilterOptions(rows.Sources, identities),
			TotalCount: rows.TotalCount,
			Page:       rows.Page,
			PageSize:   rows.PageSize,
			TotalPages: rows.TotalPages,
		})
	})
}

func applyUsageEventsSourceFilter(filter *service.UsageFilter) error {
	if filter == nil {
		return nil
	}
	source := strings.TrimSpace(filter.Source)
	if source == "" {
		return nil
	}
	if value, ok := strings.CutPrefix(source, "auth:"); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("source auth filter value is required")
		}
		filter.AuthType = "oauth"
		filter.AuthIndex = value
		filter.Source = value
		filter.Provider = ""
		return nil
	}
	if value, ok := strings.CutPrefix(source, "provider:"); ok {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("source provider filter value is required")
		}
		filter.AuthType = "apikey"
		filter.Provider = value
		filter.Source = ""
		filter.AuthIndex = ""
	}
	return nil
}

func buildUsageEventsPayload(rows []service.UsageEventRecord) []usageEventPayload {
	if len(rows) == 0 {
		return []usageEventPayload{}
	}
	payload := make([]usageEventPayload, 0, len(rows))
	for _, row := range rows {
		source, sourceKey := usageEventPublicSource(row)
		payload = append(payload, usageEventPayload{
			ID:        row.ID,
			Timestamp: row.Timestamp.UTC().Format(time.RFC3339),
			Model:     row.Model,
			Source:    source,
			SourceKey: sourceKey,
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

func usageEventPublicSource(row service.UsageEventRecord) (string, string) {
	switch strings.TrimSpace(row.AuthType) {
	case "apikey":
		provider := strings.TrimSpace(row.Provider)
		if provider == "" {
			provider = "AI Provider"
		}
		return provider, "provider:" + provider
	case "oauth":
		source := firstNonEmptyString(row.Source, row.AuthIndex, "unknown")
		return source, "auth:" + source
	default:
		if provider := strings.TrimSpace(row.Provider); provider != "" {
			return provider, "provider:" + provider
		}
		source := firstNonEmptyString(row.Source, row.AuthIndex, "unknown")
		return source, "auth:" + source
	}
}

func buildUsageSourceFilterOptions(sources []string, identities []models.UsageIdentity) []usageSourceFilterOption {
	if len(identities) == 0 {
		return []usageSourceFilterOption{}
	}
	options := make([]usageSourceFilterOption, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		option, ok := usageSourceFilterOptionFromIdentity(identity)
		if !ok {
			continue
		}
		if _, exists := seen[option.Value]; exists {
			continue
		}
		seen[option.Value] = struct{}{}
		options = append(options, option)
	}
	return options
}

func usageSourceFilterOptionFromIdentity(identity models.UsageIdentity) (usageSourceFilterOption, bool) {
	switch identity.AuthType {
	case models.UsageIdentityAuthTypeAuthFile:
		value := strings.TrimSpace(identity.Identity)
		if value == "" {
			return usageSourceFilterOption{}, false
		}
		label := firstNonEmptyString(identity.Name, value)
		return usageSourceFilterOption{Value: "auth:" + value, Label: label}, true
	case models.UsageIdentityAuthTypeAIProvider:
		provider := safeAIProviderDisplayValue(identity.Provider, identity.Identity, "")
		label := firstNonEmptyString(provider, safeAIProviderDisplayValue(identity.Name, identity.Identity, ""), safeAIProviderDisplayValue(identity.Type, identity.Identity, ""))
		if label == "" {
			return usageSourceFilterOption{}, false
		}
		return usageSourceFilterOption{Value: "provider:" + label, Label: label}, true
	default:
		return usageSourceFilterOption{}, false
	}
}
