package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/gin-gonic/gin"
)

type usageEventsResponse struct {
	Events     []usageEventPayload `json:"events"`
	TotalCount int64               `json:"total_count"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	TotalPages int                 `json:"total_pages"`
}

type usageSourceFilterOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	DisplayName string `json:"displayName"`
}

type usageEventFilterOptionsResponse struct {
	Models  []string                  `json:"models"`
	Sources []usageSourceFilterOption `json:"sources"`
}

type usageEventPayload struct {
	ID              string                 `json:"id,omitempty"`
	Timestamp       string                 `json:"timestamp"`
	Model           string                 `json:"model"`
	ReasoningEffort string                 `json:"reasoning_effort,omitempty"`
	Source          string                 `json:"source"`
	SourceRaw       string                 `json:"source_raw,omitempty"`
	SourceType      string                 `json:"source_type,omitempty"`
	AuthIndex       string                 `json:"auth_index,omitempty"`
	IsDelete        bool                   `json:"isDelete,omitempty"`
	Failed          bool                   `json:"failed"`
	LatencyMS       int64                  `json:"latency_ms"`
	Tokens          usageEventTokenPayload `json:"tokens"`
}

type usageEventTokenPayload struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

func registerUsageEventsRoute(
	router gin.IRoutes,
	usageProvider service.UsageProvider,
	usageIdentityProvider service.UsageIdentityProvider,
) {
	router.GET("/usage/events/filters/models", func(c *gin.Context) {
		models, err := loadUsageEventModelFilterOptions(c, usageProvider)
		if err != nil {
			writeInternalError(c, "list usage event model filter options failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": models})
	})

	router.GET("/usage/events/filters/sources", func(c *gin.Context) {
		sources, err := loadUsageEventSourceFilterOptions(c, usageIdentityProvider)
		if err != nil {
			writeInternalError(c, "list usage event source filter options failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"sources": sources})
	})

	router.GET("/usage/events", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageEventsResponse{Events: []usageEventPayload{}, Page: 1, PageSize: servicedto.DefaultUsageEventsLimit})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
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
		resolver := newUsageIdentityResolver(identities)
		c.JSON(http.StatusOK, usageEventsResponse{
			Events:     buildUsageEventsPayload(rows.Events, resolver),
			TotalCount: rows.TotalCount,
			Page:       rows.Page,
			PageSize:   rows.PageSize,
			TotalPages: rows.TotalPages,
		})
	})
}

// Source 下拉提交的是 usage identity，进入仓储前转换成 auth_index 查询。
func applyUsageEventsSourceFilter(filter *servicedto.UsageFilter) error {
	if filter == nil {
		return nil
	}
	source := strings.TrimSpace(filter.Source)
	if source == "" {
		return nil
	}
	filter.AuthIndex = source
	filter.Source = ""
	return nil
}

// 列表结果先按 auth_index 解析展示名，再组装前端需要的事件 payload。
func buildUsageEventsPayload(rows []servicedto.UsageEventRecord, resolver usageIdentityResolver) []usageEventPayload {
	if len(rows) == 0 {
		return []usageEventPayload{}
	}
	payload := make([]usageEventPayload, 0, len(rows))
	for _, row := range rows {
		identity, matched := resolver.resolveByAuthIndex(row.AuthIndex)
		source, isDelete := usageEventPublicSource(row, identity, matched)
		id := ""
		if row.ID != 0 {
			id = strconv.FormatInt(row.ID, 10)
		}
		payload = append(payload, usageEventPayload{
			ID:              id,
			Timestamp:       timeutil.FormatStorageTime(row.Timestamp),
			Model:           row.Model,
			ReasoningEffort: strings.TrimSpace(row.ReasoningEffort),
			Source:          source,
			SourceType:      identity.Type,
			AuthIndex:       row.AuthIndex,
			IsDelete:        isDelete,
			Failed:          row.Failed,
			LatencyMS:       row.LatencyMS,
			Tokens: usageEventTokenPayload{
				InputTokens:         row.InputTokens,
				OutputTokens:        row.OutputTokens,
				ReasoningTokens:     row.ReasoningTokens,
				CachedTokens:        row.CachedTokens,
				CacheReadTokens:     row.CacheReadTokens,
				CacheCreationTokens: row.CacheCreationTokens,
				TotalTokens:         row.TotalTokens,
			},
		})
	}
	return payload
}

func usageEventPublicSource(row servicedto.UsageEventRecord, identity resolvedUsageIdentity, matched bool) (string, bool) {
	if matched {
		return identity.DisplayName, false
	}
	isDelete := strings.TrimSpace(row.AuthIndex) != ""
	switch strings.TrimSpace(row.AuthType) {
	case "apikey":
		return strings.TrimSpace(row.Provider), isDelete
	case "oauth":
		return strings.TrimSpace(row.Source), isDelete
	default:
		return strings.TrimSpace(row.Provider), isDelete
	}
}

func loadUsageEventModelFilterOptions(c *gin.Context, usageProvider service.UsageProvider) ([]string, error) {
	if usageProvider == nil {
		return []string{}, nil
	}
	options, err := usageProvider.ListUsageEventFilterOptions(c.Request.Context(), servicedto.UsageFilter{})
	if err != nil {
		return nil, err
	}
	return options.Models, nil
}

func loadUsageEventSourceFilterOptions(c *gin.Context, usageIdentityProvider service.UsageIdentityProvider) ([]usageSourceFilterOption, error) {
	identities, err := loadUsageResolutionData(c, usageIdentityProvider)
	if err != nil {
		return nil, err
	}
	return buildUsageSourceFilterOptions(identities), nil
}

// Source 筛选项从活跃身份生成，避免把 usage_events.source 当成可选项暴露给页面。
func buildUsageSourceFilterOptions(identities []entities.UsageIdentity) []usageSourceFilterOption {
	if len(identities) == 0 {
		return []usageSourceFilterOption{}
	}
	options := make([]usageSourceFilterOption, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		// Source 下拉只展示活跃且有流量的身份，避免已删除身份继续出现在筛选项里。
		if identity.IsDeleted || identity.TotalRequests == 0 {
			continue
		}
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

func usageSourceFilterOptionFromIdentity(identity entities.UsageIdentity) (usageSourceFilterOption, bool) {
	switch identity.AuthType {
	case entities.UsageIdentityAuthTypeAuthFile, entities.UsageIdentityAuthTypeAIProvider:
		value := strings.TrimSpace(identity.Identity)
		if value == "" {
			return usageSourceFilterOption{}, false
		}
		label := strings.TrimSpace(identity.Name)
		displayName := helper.UsageIdentityDisplayName(identity)
		return usageSourceFilterOption{Value: value, Label: label, DisplayName: displayName}, true
	default:
		return usageSourceFilterOption{}, false
	}
}
