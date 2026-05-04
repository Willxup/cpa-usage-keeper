package api

import (
	"net/http"
	"strings"
	"time"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/redact"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageIdentitiesResponse struct {
	Identities []usageIdentityResponse `json:"identities"`
}

type usageIdentityResponse struct {
	ID                         uint                         `json:"id"`
	Name                       string                       `json:"name"`
	AuthType                   models.UsageIdentityAuthType `json:"auth_type"`
	AuthTypeName               string                       `json:"auth_type_name"`
	Identity                   string                       `json:"identity"`
	Type                       string                       `json:"type"`
	Provider                   string                       `json:"provider"`
	TotalRequests              int64                        `json:"total_requests"`
	SuccessCount               int64                        `json:"success_count"`
	FailureCount               int64                        `json:"failure_count"`
	InputTokens                int64                        `json:"input_tokens"`
	OutputTokens               int64                        `json:"output_tokens"`
	ReasoningTokens            int64                        `json:"reasoning_tokens"`
	CachedTokens               int64                        `json:"cached_tokens"`
	TotalTokens                int64                        `json:"total_tokens"`
	LastAggregatedUsageEventID uint                         `json:"last_aggregated_usage_event_id"`
	FirstUsedAt                *time.Time                   `json:"first_used_at,omitempty"`
	LastUsedAt                 *time.Time                   `json:"last_used_at,omitempty"`
	StatsUpdatedAt             *time.Time                   `json:"stats_updated_at,omitempty"`
	IsDeleted                  bool                         `json:"is_deleted"`
	CreatedAt                  time.Time                    `json:"created_at"`
	UpdatedAt                  time.Time                    `json:"updated_at"`
	DeletedAt                  *time.Time                   `json:"deleted_at,omitempty"`
}

func registerUsageIdentityRoutes(router gin.IRoutes, usageIdentityProvider service.UsageIdentityProvider) {
	router.GET("/usage/identities", func(c *gin.Context) {
		if usageIdentityProvider == nil {
			c.JSON(http.StatusOK, usageIdentitiesResponse{Identities: []usageIdentityResponse{}})
			return
		}

		items, err := usageIdentityProvider.ListUsageIdentities(c.Request.Context())
		if err != nil {
			writeInternalError(c, "list usage identities failed", err)
			return
		}

		response := make([]usageIdentityResponse, 0, len(items))
		for _, item := range items {
			response = append(response, mapUsageIdentityResponse(item))
		}
		c.JSON(http.StatusOK, usageIdentitiesResponse{Identities: response})
	})
}

func mapUsageIdentityResponse(item models.UsageIdentity) usageIdentityResponse {
	identity := item.Identity
	name := item.Name
	identityType := item.Type
	provider := item.Provider
	if item.AuthType == models.UsageIdentityAuthTypeAIProvider {
		identity = redact.APIKeyDisplayName(item.Identity)
		identityType = safeAIProviderDisplayValue(item.Type, item.Identity, item.AuthTypeName)
		provider = safeAIProviderDisplayValue(item.Provider, item.Identity, firstNonEmptyString(identityType, identity))
		name = safeAIProviderDisplayValue(item.Name, item.Identity, firstNonEmptyString(provider, identityType, identity))
	}

	return usageIdentityResponse{
		ID:                         item.ID,
		Name:                       name,
		AuthType:                   item.AuthType,
		AuthTypeName:               item.AuthTypeName,
		Identity:                   identity,
		Type:                       identityType,
		Provider:                   provider,
		TotalRequests:              item.TotalRequests,
		SuccessCount:               item.SuccessCount,
		FailureCount:               item.FailureCount,
		InputTokens:                item.InputTokens,
		OutputTokens:               item.OutputTokens,
		ReasoningTokens:            item.ReasoningTokens,
		CachedTokens:               item.CachedTokens,
		TotalTokens:                item.TotalTokens,
		LastAggregatedUsageEventID: item.LastAggregatedUsageEventID,
		FirstUsedAt:                item.FirstUsedAt,
		LastUsedAt:                 item.LastUsedAt,
		StatsUpdatedAt:             item.StatsUpdatedAt,
		IsDeleted:                  item.IsDeleted,
		CreatedAt:                  item.CreatedAt,
		UpdatedAt:                  item.UpdatedAt,
		DeletedAt:                  item.DeletedAt,
	}
}

func safeAIProviderDisplayValue(value, rawIdentity, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	if isSensitiveUsageIdentityValue(trimmed, rawIdentity) {
		return fallback
	}
	return trimmed
}

func isSensitiveUsageIdentityValue(value, rawIdentity string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if raw := strings.TrimSpace(rawIdentity); raw != "" && strings.Contains(trimmed, raw) {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "sk-") || strings.Contains(lower, "aiza") || strings.Contains(lower, "cr_") || strings.Contains(lower, "cr-")
}
