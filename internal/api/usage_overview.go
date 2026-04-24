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
	Usage         usageOverviewPayload       `json:"usage"`
	Summary       usageOverviewSummary       `json:"summary"`
	Series        usageOverviewSeries        `json:"series"`
	HourlySeries  usageOverviewSeries        `json:"hourly_series"`
	DailySeries   usageOverviewSeries        `json:"daily_series"`
	ServiceHealth usageOverviewServiceHealth `json:"service_health"`
}

type usageOverviewPayload struct {
	TotalRequests  int64                               `json:"total_requests"`
	SuccessCount   int64                               `json:"success_count"`
	FailureCount   int64                               `json:"failure_count"`
	TotalTokens    int64                               `json:"total_tokens"`
	APIs           map[string]usageOverviewAPISnapshot `json:"apis"`
	RequestsByDay  map[string]int64                    `json:"requests_by_day"`
	RequestsByHour map[string]int64                    `json:"requests_by_hour"`
	TokensByDay    map[string]int64                    `json:"tokens_by_day"`
	TokensByHour   map[string]int64                    `json:"tokens_by_hour"`
}

type usageOverviewSummary struct {
	RequestCount    int64   `json:"request_count"`
	TokenCount      int64   `json:"token_count"`
	WindowMinutes   int64   `json:"window_minutes"`
	RPM             float64 `json:"rpm"`
	TPM             float64 `json:"tpm"`
	TotalCost       float64 `json:"total_cost"`
	CostAvailable   bool    `json:"cost_available"`
	CachedTokens    int64   `json:"cached_tokens"`
	ReasoningTokens int64   `json:"reasoning_tokens"`
}

type usageOverviewSeries struct {
	Requests        map[string]int64                   `json:"requests"`
	Tokens          map[string]int64                   `json:"tokens"`
	RPM             map[string]float64                 `json:"rpm"`
	TPM             map[string]float64                 `json:"tpm"`
	Cost            map[string]float64                 `json:"cost"`
	InputTokens     map[string]int64                   `json:"input_tokens"`
	OutputTokens    map[string]int64                   `json:"output_tokens"`
	CachedTokens    map[string]int64                   `json:"cached_tokens"`
	ReasoningTokens map[string]int64                   `json:"reasoning_tokens"`
	Models          map[string]usageOverviewSeriesLine `json:"models"`
}

type usageOverviewSeriesLine struct {
	Requests        map[string]int64   `json:"requests"`
	Tokens          map[string]int64   `json:"tokens"`
	RPM             map[string]float64 `json:"rpm"`
	TPM             map[string]float64 `json:"tpm"`
	Cost            map[string]float64 `json:"cost"`
	InputTokens     map[string]int64   `json:"input_tokens"`
	OutputTokens    map[string]int64   `json:"output_tokens"`
	CachedTokens    map[string]int64   `json:"cached_tokens"`
	ReasoningTokens map[string]int64   `json:"reasoning_tokens"`
}

type usageOverviewServiceHealth struct {
	TotalSuccess int64                             `json:"total_success"`
	TotalFailure int64                             `json:"total_failure"`
	SuccessRate  float64                           `json:"success_rate"`
	BlockDetails []usageOverviewServiceHealthBlock `json:"block_details"`
}

type usageOverviewServiceHealthBlock struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Success   int64     `json:"success"`
	Failure   int64     `json:"failure"`
	Rate      float64   `json:"rate"`
}

type usageOverviewAPISnapshot struct {
	DisplayName   string                                `json:"display_name,omitempty"`
	TotalRequests int64                                 `json:"total_requests"`
	SuccessCount  int64                                 `json:"success_count"`
	FailureCount  int64                                 `json:"failure_count"`
	TotalTokens   int64                                 `json:"total_tokens"`
	Models        map[string]usageOverviewModelSnapshot `json:"models"`
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
			c.JSON(http.StatusOK, usageOverviewResponse{
				Usage:         buildUsageOverviewPayload(nil),
				Summary:       usageOverviewSummary{},
				Series:        emptyUsageOverviewSeries(),
				HourlySeries:  emptyUsageOverviewSeries(),
				DailySeries:   emptyUsageOverviewSeries(),
				ServiceHealth: usageOverviewServiceHealth{BlockDetails: []usageOverviewServiceHealthBlock{}},
			})
			return
		}

		filter, err := parseUsageFilterQuery(c.Request, time.Now().UTC())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		overview, err := usageProvider.GetUsageOverview(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var usage *cpa.StatisticsSnapshot
		if overview != nil {
			usage = overview.Usage
		}
		redactedUsage := redact.UsageSnapshot(usage)
		c.JSON(http.StatusOK, usageOverviewResponse{
			Usage:         buildUsageOverviewPayload(redactedUsage),
			Summary:       buildUsageOverviewSummary(overview),
			Series:        buildUsageOverviewSeries(overview),
			HourlySeries:  buildUsageOverviewHourlySeries(overview),
			DailySeries:   buildUsageOverviewDailySeries(overview),
			ServiceHealth: buildUsageOverviewServiceHealth(overview),
		})
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

func buildUsageOverviewSummary(overview *service.UsageOverviewSnapshot) usageOverviewSummary {
	if overview == nil {
		return usageOverviewSummary{}
	}
	return usageOverviewSummary{
		RequestCount:    overview.Summary.RequestCount,
		TokenCount:      overview.Summary.TokenCount,
		WindowMinutes:   overview.Summary.WindowMinutes,
		RPM:             overview.Summary.RPM,
		TPM:             overview.Summary.TPM,
		TotalCost:       overview.Summary.TotalCost,
		CostAvailable:   overview.Summary.CostAvailable,
		CachedTokens:    overview.Summary.CachedTokens,
		ReasoningTokens: overview.Summary.ReasoningTokens,
	}
}

func emptyUsageOverviewSeries() usageOverviewSeries {
	return usageOverviewSeries{
		Requests:        map[string]int64{},
		Tokens:          map[string]int64{},
		RPM:             map[string]float64{},
		TPM:             map[string]float64{},
		Cost:            map[string]float64{},
		InputTokens:     map[string]int64{},
		OutputTokens:    map[string]int64{},
		CachedTokens:    map[string]int64{},
		ReasoningTokens: map[string]int64{},
		Models:          map[string]usageOverviewSeriesLine{},
	}
}

func mapUsageOverviewSeriesLine(series service.UsageOverviewSeries) usageOverviewSeriesLine {
	return usageOverviewSeriesLine{
		Requests:        cloneInt64Map(series.Requests),
		Tokens:          cloneInt64Map(series.Tokens),
		RPM:             cloneFloat64Map(series.RPM),
		TPM:             cloneFloat64Map(series.TPM),
		Cost:            cloneFloat64Map(series.Cost),
		InputTokens:     cloneInt64Map(series.InputTokens),
		OutputTokens:    cloneInt64Map(series.OutputTokens),
		CachedTokens:    cloneInt64Map(series.CachedTokens),
		ReasoningTokens: cloneInt64Map(series.ReasoningTokens),
	}
}

func mapUsageOverviewSeries(series service.UsageOverviewSeries) usageOverviewSeries {
	models := make(map[string]usageOverviewSeriesLine, len(series.Models))
	for model, modelSeries := range series.Models {
		models[model] = mapUsageOverviewSeriesLine(modelSeries)
	}
	return usageOverviewSeries{
		Requests:        cloneInt64Map(series.Requests),
		Tokens:          cloneInt64Map(series.Tokens),
		RPM:             cloneFloat64Map(series.RPM),
		TPM:             cloneFloat64Map(series.TPM),
		Cost:            cloneFloat64Map(series.Cost),
		InputTokens:     cloneInt64Map(series.InputTokens),
		OutputTokens:    cloneInt64Map(series.OutputTokens),
		CachedTokens:    cloneInt64Map(series.CachedTokens),
		ReasoningTokens: cloneInt64Map(series.ReasoningTokens),
		Models:          models,
	}
}

func buildUsageOverviewSeries(overview *service.UsageOverviewSnapshot) usageOverviewSeries {
	if overview == nil {
		return emptyUsageOverviewSeries()
	}
	return mapUsageOverviewSeries(overview.Series)
}

func buildUsageOverviewHourlySeries(overview *service.UsageOverviewSnapshot) usageOverviewSeries {
	if overview == nil {
		return emptyUsageOverviewSeries()
	}
	return mapUsageOverviewSeries(overview.HourlySeries)
}

func buildUsageOverviewDailySeries(overview *service.UsageOverviewSnapshot) usageOverviewSeries {
	if overview == nil {
		return emptyUsageOverviewSeries()
	}
	return mapUsageOverviewSeries(overview.DailySeries)
}

func buildUsageOverviewServiceHealth(overview *service.UsageOverviewSnapshot) usageOverviewServiceHealth {
	if overview == nil {
		return usageOverviewServiceHealth{BlockDetails: []usageOverviewServiceHealthBlock{}}
	}
	blocks := make([]usageOverviewServiceHealthBlock, 0, len(overview.Health.BlockDetails))
	for _, block := range overview.Health.BlockDetails {
		blocks = append(blocks, usageOverviewServiceHealthBlock{
			StartTime: block.StartTime,
			EndTime:   block.EndTime,
			Success:   block.Success,
			Failure:   block.Failure,
			Rate:      block.Rate,
		})
	}
	return usageOverviewServiceHealth{
		TotalSuccess: overview.Health.TotalSuccess,
		TotalFailure: overview.Health.TotalFailure,
		SuccessRate:  overview.Health.SuccessRate,
		BlockDetails: blocks,
	}
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

func cloneFloat64Map(source map[string]float64) map[string]float64 {
	if len(source) == 0 {
		return map[string]float64{}
	}
	cloned := make(map[string]float64, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
