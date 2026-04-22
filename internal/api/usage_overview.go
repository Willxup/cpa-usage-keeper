package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/redact"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageOverviewResponse struct {
	Usage usageOverviewPayload `json:"usage"`
}

type usageOverviewPayload struct {
	TotalRequests  int64                                `json:"total_requests"`
	SuccessCount   int64                                `json:"success_count"`
	FailureCount   int64                                `json:"failure_count"`
	TotalTokens    int64                                `json:"total_tokens"`
	APIs           map[string]usageOverviewAPISnapshot  `json:"apis"`
	RequestsByDay  map[string]int64                     `json:"requests_by_day"`
	RequestsByHour map[string]int64                     `json:"requests_by_hour"`
	TokensByDay    map[string]int64                     `json:"tokens_by_day"`
	TokensByHour   map[string]int64                     `json:"tokens_by_hour"`
}

type usageOverviewAPISnapshot struct {
	DisplayName   string                                   `json:"display_name,omitempty"`
	TotalRequests int64                                    `json:"total_requests"`
	SuccessCount  int64                                    `json:"success_count"`
	FailureCount  int64                                    `json:"failure_count"`
	TotalTokens   int64                                    `json:"total_tokens"`
	Models        map[string]usageOverviewModelSnapshot    `json:"models"`
}

type usageOverviewModelSnapshot struct {
	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	FailureCount  int64 `json:"failure_count"`
	TotalTokens   int64 `json:"total_tokens"`
}

func registerUsageOverviewRoute(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage/overview", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageOverviewResponse{Usage: usageOverviewPayload{
				APIs:           map[string]usageOverviewAPISnapshot{},
				RequestsByDay:  map[string]int64{},
				RequestsByHour: map[string]int64{},
				TokensByDay:    map[string]int64{},
				TokensByHour:   map[string]int64{},
			}})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, time.Now().UTC())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		usage, err := usageProvider.GetUsageWithFilter(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		redactedUsage := redact.UsageSnapshot(usage)
		c.JSON(http.StatusOK, usageOverviewResponse{Usage: buildUsageOverviewPayload(redactedUsage)})
	})
}

func buildUsageOverviewPayload(snapshot *cpa.StatisticsSnapshot) usageOverviewPayload {
	if snapshot == nil {
		return usageOverviewPayload{
			APIs:           map[string]usageOverviewAPISnapshot{},
			RequestsByDay:  map[string]int64{},
			RequestsByHour: map[string]int64{},
			TokensByDay:    map[string]int64{},
			TokensByHour:   map[string]int64{},
		}
	}

	payload := usageOverviewPayload{
		TotalRequests:  snapshot.TotalRequests,
		SuccessCount:   snapshot.SuccessCount,
		FailureCount:   snapshot.FailureCount,
		TotalTokens:    snapshot.TotalTokens,
		RequestsByDay:  cloneInt64Map(snapshot.RequestsByDay),
		RequestsByHour: cloneInt64Map(snapshot.RequestsByHour),
		TokensByDay:    cloneInt64Map(snapshot.TokensByDay),
		TokensByHour:   cloneInt64Map(snapshot.TokensByHour),
		APIs:           map[string]usageOverviewAPISnapshot{},
	}

	for apiName, apiSnapshot := range snapshot.APIs {
		payloadAPI := usageOverviewAPISnapshot{
			DisplayName:   apiSnapshot.DisplayName,
			TotalRequests: apiSnapshot.TotalRequests,
			SuccessCount:  apiSnapshot.SuccessCount,
			FailureCount:  apiSnapshot.FailureCount,
			TotalTokens:   apiSnapshot.TotalTokens,
			Models:        map[string]usageOverviewModelSnapshot{},
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			payloadAPI.Models[modelName] = usageOverviewModelSnapshot{
				TotalRequests: modelSnapshot.TotalRequests,
				SuccessCount:  modelSnapshot.SuccessCount,
				FailureCount:  modelSnapshot.FailureCount,
				TotalTokens:   modelSnapshot.TotalTokens,
			}
		}
		payload.APIs[apiName] = payloadAPI
	}

	return payload
}

func cloneInt64Map(source map[string]int64) map[string]int64 {
	if len(source) == 0 {
		return map[string]int64{}
	}
	cloned := make(map[string]int64, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
