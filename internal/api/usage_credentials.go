package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageCredentialsResponse struct {
	Credentials []usageCredentialPayload `json:"credentials"`
}

type usageCredentialPayload struct {
	Source       string `json:"source"`
	SourceType   string `json:"source_type,omitempty"`
	SourceKey    string `json:"source_key,omitempty"`
	SuccessCount int64  `json:"success_count"`
	FailureCount int64  `json:"failure_count"`
	TotalCount   int64  `json:"total_count"`
}

func registerUsageCredentialsRoute(
	router gin.IRoutes,
	usageProvider service.UsageProvider,
	authFileProvider service.AuthFileProvider,
	providerMetadataProvider service.ProviderMetadataProvider,
) {
	router.GET("/usage/credentials", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageCredentialsResponse{Credentials: []usageCredentialPayload{}})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, time.Now().UTC())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		rows, err := usageProvider.ListUsageCredentialStats(c.Request.Context(), filter)
		if err != nil {
			writeInternalError(c, "list usage credential stats failed", err)
			return
		}

		authFiles, providerMetadata, err := loadUsageResolutionData(c, authFileProvider, providerMetadataProvider)
		if err != nil {
			writeInternalError(c, "load usage resolution data failed", err)
			return
		}
		resolver := newUsageSourceResolver(authFiles, providerMetadata)
		c.JSON(http.StatusOK, usageCredentialsResponse{Credentials: buildUsageCredentialsPayload(rows, resolver)})
	})
}

func buildUsageCredentialsPayload(rows []service.UsageCredentialStat, resolver usageSourceResolver) []usageCredentialPayload {
	if len(rows) == 0 {
		return []usageCredentialPayload{}
	}

	buckets := make(map[string]*usageCredentialPayload, len(rows))
	orderedKeys := make([]string, 0, len(rows))
	for _, row := range rows {
		resolved := resolver.resolve(row.Source, row.AuthIndex)
		bucketKey := resolved.SourceKey
		if bucketKey == "" {
			bucketKey = resolved.DisplayName
		}
		payload, ok := buckets[bucketKey]
		if !ok {
			payload = &usageCredentialPayload{
				Source:     resolved.DisplayName,
				SourceType: resolved.SourceType,
				SourceKey:  resolved.SourceKey,
			}
			buckets[bucketKey] = payload
			orderedKeys = append(orderedKeys, bucketKey)
		}
		if row.Failed {
			payload.FailureCount += row.RequestCount
		} else {
			payload.SuccessCount += row.RequestCount
		}
		payload.TotalCount = payload.SuccessCount + payload.FailureCount
	}

	result := make([]usageCredentialPayload, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		result = append(result, *buckets[key])
	}
	return result
}
