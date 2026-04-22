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
