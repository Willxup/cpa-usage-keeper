package repository

import (
	"fmt"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func BuildUsageSnapshot(db *gorm.DB) (*cpa.StatisticsSnapshot, error) {
	return BuildUsageSnapshotWithFilter(db, UsageQueryFilter{})
}

func ListUsageEventsWithFilter(db *gorm.DB, filter UsageQueryFilter) ([]UsageEventRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	query := db.Model(&models.UsageEvent{}).Order("timestamp DESC")
	if filter.StartTime != nil {
		query = query.Where("timestamp >= ?", filter.StartTime.UTC())
	}
	if filter.EndTime != nil {
		query = query.Where("timestamp <= ?", filter.EndTime.UTC())
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultUsageEventsLimit
	}
	query = query.Limit(limit)

	var events []models.UsageEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}

	rows := make([]UsageEventRecord, 0, len(events))
	for _, event := range events {
		rows = append(rows, UsageEventRecord{
			Timestamp:       event.Timestamp.UTC(),
			APIGroupKey:     strings.TrimSpace(event.APIGroupKey),
			Model:           strings.TrimSpace(event.Model),
			Source:          strings.TrimSpace(event.Source),
			AuthIndex:       strings.TrimSpace(event.AuthIndex),
			Failed:          event.Failed,
			LatencyMS:       event.LatencyMS,
			InputTokens:     event.InputTokens,
			OutputTokens:    event.OutputTokens,
			ReasoningTokens: event.ReasoningTokens,
			CachedTokens:    event.CachedTokens,
			TotalTokens:     event.TotalTokens,
		})
	}
	return rows, nil
}

func ListUsageCredentialStatsWithFilter(db *gorm.DB, filter UsageQueryFilter) ([]UsageCredentialStatRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	query := db.Model(&models.UsageEvent{})
	if filter.StartTime != nil {
		query = query.Where("timestamp >= ?", filter.StartTime.UTC())
	}
	if filter.EndTime != nil {
		query = query.Where("timestamp <= ?", filter.EndTime.UTC())
	}
	query = query.Select("TRIM(source) AS source, TRIM(auth_index) AS auth_index, failed, COUNT(*) AS request_count")
	query = query.Group("TRIM(source), TRIM(auth_index), failed")
	query = query.Order("request_count DESC, source ASC, auth_index ASC, failed ASC")

	var rows []UsageCredentialStatRecord
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage credential stats: %w", err)
	}
	return rows, nil
}

func ListUsageAnalysisWithFilter(db *gorm.DB, filter UsageQueryFilter) ([]UsageAnalysisAPIStatRecord, []UsageAnalysisModelStatRecord, error) {
	if db == nil {
		return nil, nil, fmt.Errorf("database is nil")
	}

	baseQuery := db.Model(&models.UsageEvent{})
	if filter.StartTime != nil {
		baseQuery = baseQuery.Where("timestamp >= ?", filter.StartTime.UTC())
	}
	if filter.EndTime != nil {
		baseQuery = baseQuery.Where("timestamp <= ?", filter.EndTime.UTC())
	}

	apiQuery := baseQuery.Session(&gorm.Session{})
	apiQuery = apiQuery.Select(strings.Join([]string{
		"TRIM(api_group_key) AS api_group_key",
		"COUNT(*) AS total_requests",
		"SUM(CASE WHEN failed THEN 0 ELSE 1 END) AS success_count",
		"SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failure_count",
		"SUM(input_tokens) AS input_tokens",
		"SUM(output_tokens) AS output_tokens",
		"SUM(reasoning_tokens) AS reasoning_tokens",
		"SUM(cached_tokens) AS cached_tokens",
		"SUM(total_tokens) AS total_tokens",
	}, ", "))
	apiQuery = apiQuery.Group("TRIM(api_group_key)")
	apiQuery = apiQuery.Order("total_requests DESC, api_group_key ASC")

	var apiRows []UsageAnalysisAPIStatRecord
	if err := apiQuery.Scan(&apiRows).Error; err != nil {
		return nil, nil, fmt.Errorf("load usage analysis api stats: %w", err)
	}

	modelQuery := baseQuery.Session(&gorm.Session{})
	modelQuery = modelQuery.Select(strings.Join([]string{
		"TRIM(model) AS model",
		"COUNT(*) AS total_requests",
		"SUM(CASE WHEN failed THEN 0 ELSE 1 END) AS success_count",
		"SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failure_count",
		"SUM(input_tokens) AS input_tokens",
		"SUM(output_tokens) AS output_tokens",
		"SUM(reasoning_tokens) AS reasoning_tokens",
		"SUM(cached_tokens) AS cached_tokens",
		"SUM(total_tokens) AS total_tokens",
		"SUM(latency_ms) AS total_latency_ms",
		"SUM(CASE WHEN latency_ms > 0 THEN 1 ELSE 0 END) AS latency_sample_count",
	}, ", "))
	modelQuery = modelQuery.Group("TRIM(model)")
	modelQuery = modelQuery.Order("total_requests DESC, model ASC")

	var modelRows []UsageAnalysisModelStatRecord
	if err := modelQuery.Scan(&modelRows).Error; err != nil {
		return nil, nil, fmt.Errorf("load usage analysis model stats: %w", err)
	}

	apiModelQuery := baseQuery.Session(&gorm.Session{})
	apiModelQuery = apiModelQuery.Select(strings.Join([]string{
		"TRIM(api_group_key) AS api_group_key",
		"TRIM(model) AS model",
		"COUNT(*) AS total_requests",
		"SUM(CASE WHEN failed THEN 0 ELSE 1 END) AS success_count",
		"SUM(CASE WHEN failed THEN 1 ELSE 0 END) AS failure_count",
		"SUM(input_tokens) AS input_tokens",
		"SUM(output_tokens) AS output_tokens",
		"SUM(reasoning_tokens) AS reasoning_tokens",
		"SUM(cached_tokens) AS cached_tokens",
		"SUM(total_tokens) AS total_tokens",
		"SUM(latency_ms) AS total_latency_ms",
		"SUM(CASE WHEN latency_ms > 0 THEN 1 ELSE 0 END) AS latency_sample_count",
	}, ", "))
	apiModelQuery = apiModelQuery.Group("TRIM(api_group_key), TRIM(model)")
	apiModelQuery = apiModelQuery.Order("api_group_key ASC, total_requests DESC, model ASC")

	var apiModelRows []struct {
		APIGroupKey        string
		Model              string
		TotalRequests      int64
		SuccessCount       int64
		FailureCount       int64
		InputTokens        int64
		OutputTokens       int64
		ReasoningTokens    int64
		CachedTokens       int64
		TotalTokens        int64
		TotalLatencyMS     int64
		LatencySampleCount int64
	}
	if err := apiModelQuery.Scan(&apiModelRows).Error; err != nil {
		return nil, nil, fmt.Errorf("load usage analysis api model stats: %w", err)
	}

	modelsByAPI := make(map[string][]UsageAnalysisModelStatRecord, len(apiRows))
	for _, row := range apiModelRows {
		modelsByAPI[row.APIGroupKey] = append(modelsByAPI[row.APIGroupKey], UsageAnalysisModelStatRecord{
			Model:              row.Model,
			TotalRequests:      row.TotalRequests,
			SuccessCount:       row.SuccessCount,
			FailureCount:       row.FailureCount,
			InputTokens:        row.InputTokens,
			OutputTokens:       row.OutputTokens,
			ReasoningTokens:    row.ReasoningTokens,
			CachedTokens:       row.CachedTokens,
			TotalTokens:        row.TotalTokens,
			TotalLatencyMS:     row.TotalLatencyMS,
			LatencySampleCount: row.LatencySampleCount,
		})
	}
	normalize := func(value string) string {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "unknown"
		}
		return trimmed
	}

	resultAPIs := make([]UsageAnalysisAPIStatRecord, 0, len(apiRows))
	for _, row := range apiRows {
		row.APIGroupKey = normalize(row.APIGroupKey)
		row.DisplayName = row.APIGroupKey
		models := modelsByAPI[strings.TrimSpace(row.APIGroupKey)]
		if len(models) == 0 {
			models = modelsByAPI[row.APIGroupKey]
		}
		for index := range models {
			models[index].Model = normalize(models[index].Model)
		}
		row.Models = models
		resultAPIs = append(resultAPIs, row)
	}
	for index := range modelRows {
		modelRows[index].Model = normalize(modelRows[index].Model)
	}

	return resultAPIs, modelRows, nil
}

func BuildUsageSnapshotWithFilter(db *gorm.DB, filter UsageQueryFilter) (*cpa.StatisticsSnapshot, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	query := db.Order("timestamp asc")
	if filter.StartTime != nil {
		query = query.Where("timestamp >= ?", filter.StartTime.UTC())
	}
	if filter.EndTime != nil {
		query = query.Where("timestamp <= ?", filter.EndTime.UTC())
	}

	var events []models.UsageEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}

	snapshot := &cpa.StatisticsSnapshot{
		APIs:           map[string]cpa.APISnapshot{},
		RequestsByDay:  map[string]int64{},
		RequestsByHour: map[string]int64{},
		TokensByDay:    map[string]int64{},
		TokensByHour:   map[string]int64{},
	}
	if len(events) == 0 {
		return snapshot, nil
	}

	for _, event := range events {
		apiKey := strings.TrimSpace(event.APIGroupKey)
		if apiKey == "" {
			apiKey = "unknown"
		}
		modelName := strings.TrimSpace(event.Model)
		if modelName == "" {
			modelName = "unknown"
		}

		apiSnapshot := snapshot.APIs[apiKey]
		if apiSnapshot.Models == nil {
			apiSnapshot.Models = map[string]cpa.ModelSnapshot{}
		}

		modelSnapshot := apiSnapshot.Models[modelName]
		detail := cpa.RequestDetail{
			Timestamp: event.Timestamp.UTC(),
			LatencyMS: event.LatencyMS,
			Source:    strings.TrimSpace(event.Source),
			AuthIndex: strings.TrimSpace(event.AuthIndex),
			Failed:    event.Failed,
			Tokens: cpa.TokenStats{
				InputTokens:     event.InputTokens,
				OutputTokens:    event.OutputTokens,
				ReasoningTokens: event.ReasoningTokens,
				CachedTokens:    event.CachedTokens,
				TotalTokens:     event.TotalTokens,
			},
		}
		modelSnapshot.Details = append(modelSnapshot.Details, detail)
		modelSnapshot.TotalRequests++
		modelSnapshot.TotalTokens += event.TotalTokens
		apiSnapshot.TotalRequests++
		apiSnapshot.TotalTokens += event.TotalTokens
		snapshot.TotalRequests++
		snapshot.TotalTokens += event.TotalTokens
		if event.Failed {
			modelSnapshot.FailureCount++
			apiSnapshot.FailureCount++
			snapshot.FailureCount++
		} else {
			modelSnapshot.SuccessCount++
			apiSnapshot.SuccessCount++
			snapshot.SuccessCount++
		}

		dayKey := event.Timestamp.UTC().Format("2006-01-02")
		hourKey := event.Timestamp.UTC().Format("2006-01-02T15:00:00Z")
		snapshot.RequestsByDay[dayKey]++
		snapshot.RequestsByHour[hourKey]++
		snapshot.TokensByDay[dayKey] += event.TotalTokens
		snapshot.TokensByHour[hourKey] += event.TotalTokens

		apiSnapshot.Models[modelName] = modelSnapshot
		snapshot.APIs[apiKey] = apiSnapshot
	}

	for apiKey, apiSnapshot := range snapshot.APIs {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			sort.Slice(modelSnapshot.Details, func(i, j int) bool {
				return modelSnapshot.Details[i].Timestamp.Before(modelSnapshot.Details[j].Timestamp)
			})
			apiSnapshot.Models[modelName] = modelSnapshot
		}
		snapshot.APIs[apiKey] = apiSnapshot
	}

	return snapshot, nil
}
