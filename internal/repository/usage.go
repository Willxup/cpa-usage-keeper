package repository

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"gorm.io/gorm"
)

func BuildUsageSnapshot(db *gorm.DB) (*cpa.StatisticsSnapshot, error) {
	return BuildUsageSnapshotWithFilter(db, UsageQueryFilter{})
}

func ListUsageEventsWithFilter(db *gorm.DB, filter UsageQueryFilter) (*UsageEventsPageRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	baseQuery := db.Model(&models.UsageEvent{})
	baseQuery = applyUsageEventsListFilter(baseQuery, filter)

	var totalCount int64
	if err := baseQuery.Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("count usage events: %w", err)
	}
	modelFacets, err := listUsageEventFacetValues(db, filter, "model")
	if err != nil {
		return nil, err
	}
	sources, err := listUsageEventFacetValues(db, filter, "source")
	if err != nil {
		return nil, err
	}

	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = filter.Limit
	}
	if pageSize <= 0 {
		pageSize = DefaultUsageEventsLimit
	}
	offset := filter.Offset
	if offset <= 0 {
		offset = (page - 1) * pageSize
	}
	if offset < 0 {
		offset = 0
	}

	query := applyUsageEventsListFilter(db.Model(&models.UsageEvent{}), filter)
	query = query.Order("timestamp DESC, id DESC").Limit(pageSize).Offset(offset)

	var events []models.UsageEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}

	rows := make([]UsageEventRecord, 0, len(events))
	for _, event := range events {
		rows = append(rows, UsageEventRecord{
			ID:              event.ID,
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
	totalPages := 0
	if totalCount > 0 {
		totalPages = int((totalCount + int64(pageSize) - 1) / int64(pageSize))
	}
	return &UsageEventsPageRecord{Events: rows, Models: modelFacets, Sources: sources, TotalCount: totalCount, Page: page, PageSize: pageSize, TotalPages: totalPages}, nil
}

func ListUsageEventFilterOptionsWithFilter(db *gorm.DB, filter UsageQueryFilter) (*UsageEventFilterOptionsRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	models, err := listUsageEventFacetValues(db, filter, "model")
	if err != nil {
		return nil, err
	}
	sources, err := listUsageEventFacetValues(db, filter, "source")
	if err != nil {
		return nil, err
	}
	return &UsageEventFilterOptionsRecord{Models: models, Sources: sources}, nil
}

func listUsageEventFacetValues(db *gorm.DB, filter UsageQueryFilter, column string) ([]string, error) {
	query := applyUsageEventTimeFilter(db.Model(&models.UsageEvent{}), filter)
	var values []string
	if err := query.Select("DISTINCT TRIM("+column+")").Where("TRIM("+column+") <> ''").Order("TRIM("+column+") ASC").Pluck(column, &values).Error; err != nil {
		return nil, fmt.Errorf("load usage event %s facets: %w", column, err)
	}
	return values, nil
}

func applyUsageEventTimeFilter(query *gorm.DB, filter UsageQueryFilter) *gorm.DB {
	if filter.StartTime != nil {
		query = query.Where("timestamp >= ?", filter.StartTime.UTC())
	}
	if filter.EndTime != nil {
		query = query.Where("timestamp <= ?", filter.EndTime.UTC())
	}
	return query
}

func applyUsageEventsListFilter(query *gorm.DB, filter UsageQueryFilter) *gorm.DB {
	query = applyUsageEventTimeFilter(query, filter)
	if model := strings.TrimSpace(filter.Model); model != "" {
		query = query.Where("TRIM(model) = ?", model)
	}
	if source := strings.TrimSpace(filter.Source); source != "" {
		query = query.Where("TRIM(source) = ?", source)
	}
	if authIndex := strings.TrimSpace(filter.AuthIndex); authIndex != "" {
		query = query.Where("TRIM(auth_index) = ?", authIndex)
	}
	switch strings.TrimSpace(filter.Result) {
	case "success":
		query = query.Where("failed = ?", false)
	case "failed":
		query = query.Where("failed = ?", true)
	}
	return query
}

func ListUsageCredentialStatsWithFilter(db *gorm.DB, filter UsageQueryFilter) ([]UsageCredentialStatRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	query := applyUsageEventsListFilter(db.Model(&models.UsageEvent{}), filter)
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

	baseQuery := applyUsageEventsListFilter(db.Model(&models.UsageEvent{}), filter)

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

	events, err := loadUsageEventsWithFilter(db, filter)
	if err != nil {
		return nil, err
	}

	return buildUsageSnapshotFromEvents(events), nil
}

func BuildUsageOverviewWithFilter(db *gorm.DB, filter UsageQueryFilter) (*UsageOverviewRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	events, err := loadUsageEventsWithFilter(db, filter)
	if err != nil {
		return nil, err
	}
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		return nil, err
	}

	return buildUsageOverviewFromEvents(events, filter, pricingByModel), nil
}

func buildUsageOverviewFromEvents(events []models.UsageEvent, filter UsageQueryFilter, pricingByModel map[string]models.ModelPriceSetting) *UsageOverviewRecord {
	windowMinutes := computeWindowMinutes(filter)
	bucketByDay := shouldBucketUsageOverviewByDay(filter, windowMinutes)
	latestHourlyStart := latestHourlySeriesStart(filter)
	overview := &UsageOverviewRecord{
		Usage: &cpa.StatisticsSnapshot{
			APIs:           map[string]cpa.APISnapshot{},
			RequestsByDay:  map[string]int64{},
			RequestsByHour: map[string]int64{},
			TokensByDay:    map[string]int64{},
			TokensByHour:   map[string]int64{},
		},
		Summary: UsageOverviewSummaryRecord{
			WindowMinutes: windowMinutes,
			CostAvailable: true,
		},
		Series:       newUsageOverviewSeriesRecord(),
		HourlySeries: newUsageOverviewSeriesRecord(),
		DailySeries:  newUsageOverviewSeriesRecord(),
		Health:       buildUsageOverviewHealth(filter),
	}
	if len(events) == 0 {
		return overview
	}

	for _, event := range events {
		applyUsageEventToSnapshot(overview.Usage, event, false)
		applyUsageEventToOverview(overview, event, bucketByDay, latestHourlyStart, pricingByModel)
	}
	finalizeUsageOverview(overview, false)
	return overview
}

func loadUsageEventsWithFilter(db *gorm.DB, filter UsageQueryFilter) ([]models.UsageEvent, error) {
	query := applyUsageEventsListFilter(db.Model(&models.UsageEvent{}), filter).Order("timestamp asc")

	var events []models.UsageEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}
	return events, nil
}

func buildUsageSnapshotFromEvents(events []models.UsageEvent) *cpa.StatisticsSnapshot {
	snapshot := &cpa.StatisticsSnapshot{
		APIs:           map[string]cpa.APISnapshot{},
		RequestsByDay:  map[string]int64{},
		RequestsByHour: map[string]int64{},
		TokensByDay:    map[string]int64{},
		TokensByHour:   map[string]int64{},
	}
	if len(events) == 0 {
		return snapshot
	}

	for _, event := range events {
		applyUsageEventToSnapshot(snapshot, event, true)
	}
	finalizeUsageSnapshot(snapshot, true)
	return snapshot
}

func applyUsageEventToSnapshot(snapshot *cpa.StatisticsSnapshot, event models.UsageEvent, includeDetails bool) {
	apiKey := normalizeUsageOverviewDimension(event.APIGroupKey)
	modelName := normalizeUsageOverviewDimension(event.Model)

	apiSnapshot := snapshot.APIs[apiKey]
	if apiSnapshot.Models == nil {
		apiSnapshot.Models = map[string]cpa.ModelSnapshot{}
	}

	modelSnapshot := apiSnapshot.Models[modelName]
	if includeDetails {
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
	}
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

func finalizeUsageSnapshot(snapshot *cpa.StatisticsSnapshot, includeDetails bool) {
	if !includeDetails {
		return
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
}

func newUsageOverviewSeriesRecord() UsageOverviewSeriesRecord {
	return UsageOverviewSeriesRecord{
		Requests:        map[string]int64{},
		Tokens:          map[string]int64{},
		RPM:             map[string]float64{},
		TPM:             map[string]float64{},
		Cost:            map[string]float64{},
		InputTokens:     map[string]int64{},
		OutputTokens:    map[string]int64{},
		CachedTokens:    map[string]int64{},
		ReasoningTokens: map[string]int64{},
		Models:          map[string]UsageOverviewSeriesRecord{},
	}
}

func applyUsageEventToOverviewSeries(series *UsageOverviewSeriesRecord, event models.UsageEvent, cost float64, bucketKey string, bucketMinutes int64) {
	series.Requests[bucketKey]++
	series.Tokens[bucketKey] += event.TotalTokens
	series.Cost[bucketKey] += cost
	series.InputTokens[bucketKey] += event.InputTokens
	series.OutputTokens[bucketKey] += event.OutputTokens
	series.CachedTokens[bucketKey] += event.CachedTokens
	series.ReasoningTokens[bucketKey] += event.ReasoningTokens
	series.RPM[bucketKey] = float64(series.Requests[bucketKey]) / float64(bucketMinutes)
	series.TPM[bucketKey] = float64(series.Tokens[bucketKey]) / float64(bucketMinutes)

	modelName := normalizeUsageOverviewDimension(event.Model)
	modelSeries := series.Models[modelName]
	if modelSeries.Requests == nil {
		modelSeries = newUsageOverviewSeriesRecord()
	}
	modelSeries.Requests[bucketKey]++
	modelSeries.Tokens[bucketKey] += event.TotalTokens
	modelSeries.Cost[bucketKey] += cost
	modelSeries.InputTokens[bucketKey] += event.InputTokens
	modelSeries.OutputTokens[bucketKey] += event.OutputTokens
	modelSeries.CachedTokens[bucketKey] += event.CachedTokens
	modelSeries.ReasoningTokens[bucketKey] += event.ReasoningTokens
	modelSeries.RPM[bucketKey] = float64(modelSeries.Requests[bucketKey]) / float64(bucketMinutes)
	modelSeries.TPM[bucketKey] = float64(modelSeries.Tokens[bucketKey]) / float64(bucketMinutes)
	series.Models[modelName] = modelSeries
}

func applyUsageEventToOverview(overview *UsageOverviewRecord, event models.UsageEvent, bucketByDay bool, latestHourlyStart *time.Time, pricingByModel map[string]models.ModelPriceSetting) {
	overview.Summary.CachedTokens += event.CachedTokens
	overview.Summary.ReasoningTokens += event.ReasoningTokens
	if event.Failed {
		overview.Health.TotalFailure++
	} else {
		overview.Health.TotalSuccess++
	}
	pricing, ok := pricingByModel[strings.TrimSpace(event.Model)]
	if !ok {
		overview.Summary.CostAvailable = false
	}
	cost := calculateUsageEventCost(event, pricing)
	overview.Summary.TotalCost += cost

	bucketKey, bucketMinutes := usageOverviewBucket(event.Timestamp.UTC(), bucketByDay)
	applyUsageEventToOverviewSeries(&overview.Series, event, cost, bucketKey, bucketMinutes)

	hourKey, hourMinutes := usageOverviewBucket(event.Timestamp.UTC(), false)
	if latestHourlyStart == nil || !event.Timestamp.UTC().Before(*latestHourlyStart) {
		applyUsageEventToOverviewSeries(&overview.HourlySeries, event, cost, hourKey, hourMinutes)
	}

	dayKey, dayMinutes := usageOverviewBucket(event.Timestamp.UTC(), true)
	applyUsageEventToOverviewSeries(&overview.DailySeries, event, cost, dayKey, dayMinutes)
	updateUsageOverviewHealthBlock(overview.Health.BlockDetails, event)
}

func finalizeUsageOverview(overview *UsageOverviewRecord, includeDetails bool) {
	finalizeUsageSnapshot(overview.Usage, includeDetails)
	overview.Summary.RequestCount = overview.Usage.TotalRequests
	overview.Summary.TokenCount = overview.Usage.TotalTokens
	if overview.Summary.WindowMinutes > 0 {
		overview.Summary.RPM = float64(overview.Summary.RequestCount) / float64(overview.Summary.WindowMinutes)
		overview.Summary.TPM = float64(overview.Summary.TokenCount) / float64(overview.Summary.WindowMinutes)
	}
	if total := overview.Health.TotalSuccess + overview.Health.TotalFailure; total > 0 {
		overview.Health.SuccessRate = (float64(overview.Health.TotalSuccess) / float64(total)) * 100
	}
}

func normalizeUsageOverviewDimension(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func loadPriceSettingsByModel(db *gorm.DB) (map[string]models.ModelPriceSetting, error) {
	settings, err := ListModelPriceSettings(db)
	if err != nil {
		return nil, err
	}
	result := make(map[string]models.ModelPriceSetting, len(settings))
	for _, setting := range settings {
		result[strings.TrimSpace(setting.Model)] = setting
	}
	return result, nil
}

func calculateUsageEventCost(event models.UsageEvent, pricing models.ModelPriceSetting) float64 {
	inputTokens := event.InputTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	completionTokens := event.OutputTokens
	if completionTokens < 0 {
		completionTokens = 0
	}
	cachedTokens := event.CachedTokens
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	promptTokens := inputTokens - cachedTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	return (float64(promptTokens)/1_000_000.0)*pricing.PromptPricePer1M +
		(float64(completionTokens)/1_000_000.0)*pricing.CompletionPricePer1M +
		(float64(cachedTokens)/1_000_000.0)*pricing.CachePricePer1M
}

const usageOverviewDailyBucketThresholdMinutes int64 = 7 * 24 * 60

func computeWindowMinutes(filter UsageQueryFilter) int64 {
	if filter.StartTime == nil || filter.EndTime == nil {
		return 0
	}
	start := filter.StartTime.UTC()
	end := filter.EndTime.UTC()
	if end.Before(start) {
		return 0
	}
	minutes := int64(end.Sub(start) / time.Minute)
	if end.Sub(start)%time.Minute != 0 {
		minutes++
	}
	if minutes < 1 {
		return 1
	}
	return minutes
}

func shouldBucketUsageOverviewByDay(filter UsageQueryFilter, windowMinutes int64) bool {
	if filter.Range == "all" || filter.Range == "7d" {
		return true
	}
	return windowMinutes >= usageOverviewDailyBucketThresholdMinutes
}

func usageOverviewBucket(timestamp time.Time, byDay bool) (string, int64) {
	if byDay {
		return timestamp.Format("2006-01-02"), 24 * 60
	}
	return timestamp.Format("2006-01-02T15:00:00Z"), 60
}

func latestHourlySeriesStart(filter UsageQueryFilter) *time.Time {
	if filter.EndTime == nil {
		return nil
	}
	currentHour := filter.EndTime.UTC().Truncate(time.Hour)
	start := currentHour.Add(-23 * time.Hour)
	return &start
}

const (
	usageOverviewHealthRows           = 7
	usageOverviewHealthDefaultColumns = 96
	usageOverviewHealthDefaultSpan    = 15 * time.Minute
	usageOverviewHealthPresetWindow   = 24 * time.Hour
	usageOverviewHealthPresetSpan     = (usageOverviewHealthPresetWindow + time.Duration(usageOverviewHealthRows*usageOverviewHealthDefaultColumns) - 1) / time.Duration(usageOverviewHealthRows*usageOverviewHealthDefaultColumns)
)

func buildUsageOverviewHealth(filter UsageQueryFilter) UsageOverviewHealthRecord {
	rows := usageOverviewHealthRows
	columns, span := usageOverviewHealthGrid(filter)
	totalBlocks := rows * columns
	windowStart, windowEnd := usageOverviewHealthWindow(filter, totalBlocks, span)
	blocks := make([]UsageOverviewHealthBlockRecord, totalBlocks)
	for index := range blocks {
		startTime := windowStart.Add(time.Duration(index) * span)
		blocks[index] = UsageOverviewHealthBlockRecord{
			StartTime: startTime,
			EndTime:   startTime.Add(span),
			Rate:      -1,
		}
	}
	return UsageOverviewHealthRecord{
		Rows:          rows,
		Columns:       columns,
		BucketSeconds: int64((span + time.Second - 1) / time.Second),
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		BlockDetails:  blocks,
	}
}

func usageOverviewHealthGrid(filter UsageQueryFilter) (int, time.Duration) {
	if isUsageOverviewShortHealthRange(filter.Range) {
		return usageOverviewHealthDefaultColumns, usageOverviewHealthPresetSpan
	}
	return usageOverviewHealthDefaultColumns, usageOverviewHealthDefaultSpan
}

func isUsageOverviewShortHealthRange(value string) bool {
	switch value {
	case "4h", "8h", "12h", "24h":
		return true
	default:
		return false
	}
}

func usageOverviewHealthWindow(filter UsageQueryFilter, totalBlocks int, span time.Duration) (time.Time, time.Time) {
	end := time.Now().UTC()
	if filter.EndTime != nil {
		end = filter.EndTime.UTC()
	}
	if isUsageOverviewShortHealthRange(filter.Range) {
		return end.Add(-usageOverviewHealthPresetWindow), end
	}
	currentBucketStart := end.Truncate(span)
	windowEnd := currentBucketStart.Add(span)
	return windowEnd.Add(-time.Duration(totalBlocks) * span), windowEnd
}

func updateUsageOverviewHealthBlock(blocks []UsageOverviewHealthBlockRecord, event models.UsageEvent) {
	timestamp := event.Timestamp.UTC()
	for index := range blocks {
		block := &blocks[index]
		if timestamp.Before(block.StartTime) || !timestamp.Before(block.EndTime) {
			continue
		}
		if event.Failed {
			block.Failure++
		} else {
			block.Success++
		}
		total := block.Success + block.Failure
		if total > 0 {
			block.Rate = float64(block.Success) / float64(total)
		}
		return
	}
}
