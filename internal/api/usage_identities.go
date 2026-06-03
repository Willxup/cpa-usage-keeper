package api

import (
	"net/http"
	"strconv"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageIdentitiesResponse struct {
	Identities []usageIdentityResponse `json:"identities"`
}

type usageIdentitiesPageResponse struct {
	Identities []usageIdentityResponse  `json:"identities"`
	TotalCount int64                    `json:"total_count"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	TotalPages int                      `json:"total_pages"`
	TypeCounts []usageIdentityTypeCount `json:"type_counts"`
}

type usageIdentityTypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

type usageIdentityResponse struct {
	ID                         string                         `json:"id"`
	Name                       string                         `json:"name"`
	DisplayName                string                         `json:"displayName"`
	AuthType                   entities.UsageIdentityAuthType `json:"auth_type"`
	AuthTypeName               string                         `json:"auth_type_name"`
	Identity                   string                         `json:"identity"`
	Type                       string                         `json:"type"`
	Provider                   string                         `json:"provider"`
	Prefix                     string                         `json:"prefix"`
	FileName                   *string                        `json:"file_name,omitempty"`
	FilePath                   *string                        `json:"file_path,omitempty"`
	Priority                   *int                           `json:"priority,omitempty"`
	Disabled                   bool                           `json:"disabled"`
	Note                       *string                        `json:"note,omitempty"`
	PlanType                   *string                        `json:"plan_type,omitempty"`
	ActiveStart                *time.Time                     `json:"active_start,omitempty"`
	ActiveUntil                *time.Time                     `json:"active_until,omitempty"`
	TotalRequests              int64                          `json:"total_requests"`
	SuccessCount               int64                          `json:"success_count"`
	FailureCount               int64                          `json:"failure_count"`
	InputTokens                int64                          `json:"input_tokens"`
	OutputTokens               int64                          `json:"output_tokens"`
	ReasoningTokens            int64                          `json:"reasoning_tokens"`
	CachedTokens               int64                          `json:"cached_tokens"`
	TotalTokens                int64                          `json:"total_tokens"`
	LastAggregatedUsageEventID string                         `json:"last_aggregated_usage_event_id"`
	FirstUsedAt                *time.Time                     `json:"first_used_at,omitempty"`
	LastUsedAt                 *time.Time                     `json:"last_used_at,omitempty"`
	StatsUpdatedAt             *time.Time                     `json:"stats_updated_at,omitempty"`
	IsDeleted                  bool                           `json:"is_deleted"`
	CreatedAt                  time.Time                      `json:"created_at"`
	UpdatedAt                  time.Time                      `json:"updated_at"`
	DeletedAt                  *time.Time                     `json:"deleted_at,omitempty"`
}

func registerUsageIdentityRoutes(router gin.IRoutes, usageIdentityProvider service.UsageIdentityProvider) {
	router.GET("/usage/identities/page", func(c *gin.Context) {
		if usageIdentityProvider == nil {
			c.JSON(http.StatusOK, usageIdentitiesPageResponse{Identities: []usageIdentityResponse{}, Page: 1, PageSize: 10, TypeCounts: []usageIdentityTypeCount{}})
			return
		}

		// 分页接口专供 Credentials 分区使用，按 auth_type 在服务端过滤后再分页。
		request, ok := parseUsageIdentitiesPageRequest(c)
		if !ok {
			return
		}
		result, err := usageIdentityProvider.ListActiveUsageIdentitiesPage(c.Request.Context(), request)
		if err != nil {
			writeInternalError(c, "list active usage identities page failed", err)
			return
		}

		// 复用统一响应映射，保证分页接口和旧列表接口的字段/脱敏规则一致。
		response := make([]usageIdentityResponse, 0, len(result.Items))
		for _, item := range result.Items {
			response = append(response, mapUsageIdentityResponse(item))
		}
		typeCounts := make([]usageIdentityTypeCount, 0, len(result.TypeCounts))
		for _, item := range result.TypeCounts {
			typeCounts = append(typeCounts, usageIdentityTypeCount{Type: item.Type, Count: item.Count})
		}
		c.JSON(http.StatusOK, usageIdentitiesPageResponse{
			Identities: response,
			TotalCount: result.Total,
			Page:       request.Page,
			PageSize:   request.PageSize,
			TotalPages: totalPages(result.Total, request.PageSize),
			TypeCounts: typeCounts,
		})
	})

	router.GET("/usage/identities", func(c *gin.Context) {
		if usageIdentityProvider == nil {
			c.JSON(http.StatusOK, usageIdentitiesResponse{Identities: []usageIdentityResponse{}})
			return
		}

		items, err := usageIdentityProvider.ListActiveUsageIdentities(c.Request.Context())
		if err != nil {
			writeInternalError(c, "list active usage identities failed", err)
			return
		}

		response := make([]usageIdentityResponse, 0, len(items))
		for _, item := range items {
			response = append(response, mapUsageIdentityResponse(item))
		}
		c.JSON(http.StatusOK, usageIdentitiesResponse{Identities: response})
	})
}

func parseUsageIdentitiesPageRequest(c *gin.Context) (service.ListUsageIdentitiesRequest, bool) {
	// page/page_size 做宽松兜底，auth_type 做严格校验，避免前端分区拿到混合数据。
	page := positiveQueryInt(c, "page", 1)
	pageSize := positiveQueryInt(c, "page_size", 10)
	request := service.ListUsageIdentitiesRequest{Page: page, PageSize: pageSize, Sort: c.Query("sort"), Types: cleanUsageIdentityTypeFilters(c.QueryArray("type"))}
	if rawActiveOnly := c.Query("active_only"); rawActiveOnly != "" {
		activeOnly, err := strconv.ParseBool(rawActiveOnly)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "active_only must be true or false"})
			return service.ListUsageIdentitiesRequest{}, false
		}
		request.ActiveOnly = &activeOnly
	}
	if rawAuthType := c.Query("auth_type"); rawAuthType != "" {
		value, err := strconv.Atoi(rawAuthType)
		if err != nil || (value != int(entities.UsageIdentityAuthTypeAuthFile) && value != int(entities.UsageIdentityAuthTypeAIProvider)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_type must be 1 or 2"})
			return service.ListUsageIdentitiesRequest{}, false
		}
		authType := entities.UsageIdentityAuthType(value)
		request.AuthType = &authType
	}
	return request, true
}

func cleanUsageIdentityTypeFilters(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	types := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		types = append(types, value)
	}
	return types
}

func positiveQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func totalPages(total int64, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return int((total + int64(pageSize) - 1) / int64(pageSize))
}

func mapUsageIdentityResponse(item entities.UsageIdentity) usageIdentityResponse {
	// AI provider 的 identity 是 API Key，只在返回给前端时脱敏，数据库原值不改。
	identity := item.Identity
	if item.AuthType == entities.UsageIdentityAuthTypeAIProvider {
		identity = helper.RedactSensitiveValue(item.Identity)
	}
	var fileName *string
	var filePath *string
	if item.AuthType == entities.UsageIdentityAuthTypeAuthFile {
		fileName = item.FileName
		filePath = item.FilePath
	}

	disabled := false
	if item.Disabled != nil {
		disabled = *item.Disabled
	}

	return usageIdentityResponse{
		ID:                         strconv.FormatInt(item.ID, 10),
		Name:                       item.Name,
		DisplayName:                helper.UsageIdentityDisplayName(item),
		AuthType:                   item.AuthType,
		AuthTypeName:               item.AuthTypeName,
		Identity:                   identity,
		Type:                       item.Type,
		Provider:                   item.Provider,
		Prefix:                     item.Prefix,
		FileName:                   fileName,
		FilePath:                   filePath,
		Priority:                   item.Priority,
		Disabled:                   disabled,
		Note:                       item.Note,
		PlanType:                   item.PlanType,
		ActiveStart:                item.ActiveStart,
		ActiveUntil:                item.ActiveUntil,
		TotalRequests:              item.TotalRequests,
		SuccessCount:               item.SuccessCount,
		FailureCount:               item.FailureCount,
		InputTokens:                item.InputTokens,
		OutputTokens:               item.OutputTokens,
		ReasoningTokens:            item.ReasoningTokens,
		CachedTokens:               item.CachedTokens,
		TotalTokens:                item.TotalTokens,
		LastAggregatedUsageEventID: strconv.FormatInt(item.LastAggregatedUsageEventID, 10),
		FirstUsedAt:                item.FirstUsedAt,
		LastUsedAt:                 item.LastUsedAt,
		StatsUpdatedAt:             item.StatsUpdatedAt,
		IsDeleted:                  item.IsDeleted,
		CreatedAt:                  item.CreatedAt,
		UpdatedAt:                  item.UpdatedAt,
		DeletedAt:                  item.DeletedAt,
	}
}
