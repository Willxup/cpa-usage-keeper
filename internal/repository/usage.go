package repository

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func BuildUsageSnapshot(db *gorm.DB) (*dto.StatisticsSnapshot, error) {
	return BuildUsageSnapshotWithFilter(db, dto.UsageQueryFilter{})
}

// Request Event Log Tab：先按列表条件统计总数，再加载当前页和筛选项。
func ListUsageEventsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (*dto.UsageEventsPageRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	// 第一步：应用列表筛选，统计分页总数。
	baseQuery := queryUsageEvents(db)
	baseQuery = applyUsageEventListQuery(baseQuery, filter)

	var totalCount int64
	if err := baseQuery.Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("count usage events: %w", err)
	}

	// 第二步：model 筛选项只跟随时间窗口，不跟随当前列表筛选。
	modelOptions, err := listUsageEventModelFilterOptions(db, filter)
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
		pageSize = dto.DefaultUsageEventsLimit
	}
	offset := filter.Offset
	if offset <= 0 {
		offset = (page - 1) * pageSize
	}
	if offset < 0 {
		offset = 0
	}

	query := applyUsageEventListQuery(db.Model(&entities.UsageEvent{}), filter)
	query = query.Order("timestamp DESC, id DESC").Limit(pageSize).Offset(offset)

	var events []entities.UsageEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}

	rows := make([]dto.UsageEventRecord, 0, len(events))
	for _, event := range events {
		rows = append(rows, dto.UsageEventRecord{
			ID:              event.ID,
			Timestamp:       timeutil.NormalizeStorageTime(event.Timestamp),
			APIGroupKey:     strings.TrimSpace(event.APIGroupKey),
			Model:           strings.TrimSpace(event.Model),
			AuthType:        strings.TrimSpace(event.AuthType),
			Provider:        strings.TrimSpace(event.Provider),
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
	return &dto.UsageEventsPageRecord{Events: rows, Models: modelOptions, TotalCount: totalCount, Page: page, PageSize: pageSize, TotalPages: totalPages}, nil
}

// Request Event Log Filter Options：只按时间窗口收集 model 候选值。
func ListUsageEventFilterOptionsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (*dto.UsageEventFilterOptionsRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	models, err := listUsageEventModelFilterOptions(db, filter)
	if err != nil {
		return nil, err
	}
	return &dto.UsageEventFilterOptionsRecord{Models: models}, nil
}

func listUsageEventModelFilterOptions(db *gorm.DB, filter dto.UsageQueryFilter) ([]string, error) {
	// 第一步：model 候选值只来自 usage_events，并且只套用时间窗口。
	query := applyUsageEventFilterOptionsQuery(queryUsageEvents(db), filter)

	// 第二步：去重并排除空 model，保持下拉选项稳定排序。
	var values []string
	if err := query.Select("DISTINCT TRIM(model)").Where("TRIM(model) <> ''").Order("TRIM(model) ASC").Pluck("model", &values).Error; err != nil {
		return nil, fmt.Errorf("load usage event model filter options: %w", err)
	}
	return values, nil
}

func queryUsageEvents(db *gorm.DB) *gorm.DB {
	return db.Model(&entities.UsageEvent{})
}

func applyUsageQueryWindow(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	// 查询参数和落库 timestamp 使用同一格式，避免 SQLite TEXT 范围比较失真。
	if filter.StartTime != nil {
		query = query.Where("timestamp >= ?", timeutil.FormatStorageTime(*filter.StartTime))
	}
	if filter.EndTime != nil {
		query = query.Where("timestamp <= ?", timeutil.FormatStorageTime(*filter.EndTime))
	}
	return query
}

// Overview Tab 第一步：只应用时间窗口，后续 Overview 专属条件也从这里加。
func applyUsageOverviewQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	return applyUsageQueryWindow(query, filter)
}

// Analysis Tab 第一步：只应用时间窗口，避免 Request Event Log 的筛选污染聚合。
func applyUsageAnalysisTabQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	return applyUsageQueryWindow(query, filter)
}

// Request Event Log 筛选项第一步：只应用时间窗口，不叠加当前列表筛选。
func applyUsageEventFilterOptionsQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	return applyUsageQueryWindow(query, filter)
}

// Request Event Log 列表第一步：在时间窗口上叠加 model/source/auth_index/result。
func applyUsageEventListQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	query = applyUsageQueryWindow(query, filter)
	if model := strings.TrimSpace(filter.Model); model != "" {
		query = query.Where("TRIM(model) = ?", model)
	}
	if source := strings.TrimSpace(filter.Source); source != "" {
		if authIndex := strings.TrimSpace(filter.AuthIndex); authIndex != "" {
			// 第二步：API 层会把 Source 下拉转成 auth_index，这里兼容直接传 source 的仓储调用。
			query = query.Where("(TRIM(auth_index) = ? OR TRIM(source) = ?)", authIndex, source)
		} else {
			query = query.Where("TRIM(source) = ?", source)
		}
	} else if authIndex := strings.TrimSpace(filter.AuthIndex); authIndex != "" {
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

// Analysis 第一步：按时间窗口做 API / model / API+model 聚合。
func ListUsageAnalysisWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) ([]dto.UsageAnalysisAPIStatRecord, []dto.UsageAnalysisModelStatRecord, error) {
	if db == nil {
		return nil, nil, fmt.Errorf("database is nil")
	}

	baseQuery := applyUsageAnalysisTabQuery(db.Model(&entities.UsageEvent{}), filter)

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

	var apiRows []dto.UsageAnalysisAPIStatRecord
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

	var modelRows []dto.UsageAnalysisModelStatRecord
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

	modelsByAPI := make(map[string][]dto.UsageAnalysisModelStatRecord, len(apiRows))
	for _, row := range apiModelRows {
		modelsByAPI[row.APIGroupKey] = append(modelsByAPI[row.APIGroupKey], dto.UsageAnalysisModelStatRecord{
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

	resultAPIs := make([]dto.UsageAnalysisAPIStatRecord, 0, len(apiRows))
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

// Snapshot 先读事件，再按时间窗口在内存里汇总。
func BuildUsageSnapshotWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (*dto.StatisticsSnapshot, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	events, err := loadUsageOverviewEventsWithFilter(db, filter)
	if err != nil {
		return nil, err
	}

	return buildUsageSnapshotFromEvents(events), nil
}

// Overview 先读事件，再组合窗口、系列和价格信息。
func BuildUsageOverviewWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (*dto.UsageOverviewRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	events, err := loadUsageOverviewEventsWithFilter(db, filter)
	if err != nil {
		return nil, err
	}
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		return nil, err
	}

	return buildUsageOverviewFromEvents(events, filter, pricingByModel), nil
}

func buildUsageOverviewFromEvents(events []entities.UsageEvent, filter dto.UsageQueryFilter, pricingByModel map[string]entities.ModelPriceSetting) *dto.UsageOverviewRecord {
	windowMinutes := computeWindowMinutes(filter)
	bucketByDay := shouldBucketUsageOverviewByDay(filter, windowMinutes)
	latestHourlyStart := latestHourlySeriesStart(filter)
	overview := &dto.UsageOverviewRecord{
		Usage: &dto.StatisticsSnapshot{
			APIs:           map[string]dto.APISnapshot{},
			RequestsByDay:  map[string]int64{},
			RequestsByHour: map[string]int64{},
			TokensByDay:    map[string]int64{},
			TokensByHour:   map[string]int64{},
		},
		Summary: dto.UsageOverviewSummaryRecord{
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

// Overview 第二步：按时间窗口读事件，再交给内存汇总。
func loadUsageOverviewEventsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) ([]entities.UsageEvent, error) {
	query := applyUsageOverviewQuery(db.Model(&entities.UsageEvent{}), filter).Order("timestamp asc")

	var events []entities.UsageEvent
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}
	return events, nil
}

func buildUsageSnapshotFromEvents(events []entities.UsageEvent) *dto.StatisticsSnapshot {
	snapshot := &dto.StatisticsSnapshot{
		APIs:           map[string]dto.APISnapshot{},
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

func applyUsageEventToSnapshot(snapshot *dto.StatisticsSnapshot, event entities.UsageEvent, includeDetails bool) {
	apiKey := normalizeUsageOverviewDimension(event.APIGroupKey)
	modelName := normalizeUsageOverviewDimension(event.Model)

	apiSnapshot := snapshot.APIs[apiKey]
	if apiSnapshot.Models == nil {
		apiSnapshot.Models = map[string]dto.ModelSnapshot{}
	}

	modelSnapshot := apiSnapshot.Models[modelName]
	if includeDetails {
		detail := dto.RequestDetail{
			Timestamp: timeutil.NormalizeStorageTime(event.Timestamp),
			LatencyMS: event.LatencyMS,
			Source:    strings.TrimSpace(event.Source),
			AuthIndex: strings.TrimSpace(event.AuthIndex),
			Failed:    event.Failed,
			Tokens: dto.TokenStats{
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

	eventTime := timeutil.NormalizeStorageTime(event.Timestamp)
	dayKey := eventTime.Format("2006-01-02")
	hourKey := timeutil.FormatStorageTime(eventTime.Truncate(time.Hour))
	snapshot.RequestsByDay[dayKey]++
	snapshot.RequestsByHour[hourKey]++
	snapshot.TokensByDay[dayKey] += event.TotalTokens
	snapshot.TokensByHour[hourKey] += event.TotalTokens

	apiSnapshot.Models[modelName] = modelSnapshot
	snapshot.APIs[apiKey] = apiSnapshot
}

func finalizeUsageSnapshot(snapshot *dto.StatisticsSnapshot, includeDetails bool) {
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

func newUsageOverviewSeriesRecord() dto.UsageOverviewSeriesRecord {
	return dto.UsageOverviewSeriesRecord{
		Requests:        map[string]int64{},
		Tokens:          map[string]int64{},
		RPM:             map[string]float64{},
		TPM:             map[string]float64{},
		Cost:            map[string]float64{},
		InputTokens:     map[string]int64{},
		OutputTokens:    map[string]int64{},
		CachedTokens:    map[string]int64{},
		ReasoningTokens: map[string]int64{},
		Models:          map[string]dto.UsageOverviewSeriesRecord{},
	}
}

func applyUsageEventToOverviewSeries(series *dto.UsageOverviewSeriesRecord, event entities.UsageEvent, cost float64, bucketKey string, bucketMinutes int64) {
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

func usageEventRequiresPricing(event entities.UsageEvent) bool {
	return event.InputTokens > 0 || event.OutputTokens > 0 || event.CachedTokens > 0
}

func applyUsageEventToOverview(overview *dto.UsageOverviewRecord, event entities.UsageEvent, bucketByDay bool, latestHourlyStart *time.Time, pricingByModel map[string]entities.ModelPriceSetting) {
	overview.Summary.CachedTokens += event.CachedTokens
	overview.Summary.ReasoningTokens += event.ReasoningTokens
	if event.Failed {
		overview.Health.TotalFailure++
	} else {
		overview.Health.TotalSuccess++
	}
	pricing, ok := pricingByModel[strings.TrimSpace(event.Model)]
	if !ok && usageEventRequiresPricing(event) {
		overview.Summary.CostAvailable = false
	}
	cost := calculateUsageEventCost(event, pricing)
	overview.Summary.TotalCost += cost

	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(event.Timestamp), bucketByDay)
	applyUsageEventToOverviewSeries(&overview.Series, event, cost, bucketKey, bucketMinutes)

	hourKey, hourMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(event.Timestamp), false)
	if latestHourlyStart == nil || !timeutil.NormalizeStorageTime(event.Timestamp).Before(*latestHourlyStart) {
		applyUsageEventToOverviewSeries(&overview.HourlySeries, event, cost, hourKey, hourMinutes)
	}

	dayKey, dayMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(event.Timestamp), true)
	applyUsageEventToOverviewSeries(&overview.DailySeries, event, cost, dayKey, dayMinutes)
	updateUsageOverviewHealthBlock(overview.Health.BlockDetails, event)
}

func finalizeUsageOverview(overview *dto.UsageOverviewRecord, includeDetails bool) {
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

func loadPriceSettingsByModel(db *gorm.DB) (map[string]entities.ModelPriceSetting, error) {
	settings, err := ListModelPriceSettings(db)
	if err != nil {
		return nil, err
	}
	result := make(map[string]entities.ModelPriceSetting, len(settings))
	for _, setting := range settings {
		result[strings.TrimSpace(setting.Model)] = setting
	}
	return result, nil
}

func calculateUsageEventCost(event entities.UsageEvent, pricing entities.ModelPriceSetting) float64 {
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

func computeWindowMinutes(filter dto.UsageQueryFilter) int64 {
	if filter.StartTime == nil || filter.EndTime == nil {
		return 0
	}
	start := timeutil.NormalizeStorageTime(*filter.StartTime)
	end := timeutil.NormalizeStorageTime(*filter.EndTime)
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

func shouldBucketUsageOverviewByDay(filter dto.UsageQueryFilter, windowMinutes int64) bool {
	if filter.Range == "all" || filter.Range == "7d" {
		return true
	}
	return windowMinutes >= usageOverviewDailyBucketThresholdMinutes
}

func usageOverviewBucket(timestamp time.Time, byDay bool) (string, int64) {
	if byDay {
		return timeutil.NormalizeStorageTime(timestamp).Format("2006-01-02"), 24 * 60
	}
	return timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(timestamp).Truncate(time.Hour)), 60
}

func latestHourlySeriesStart(filter dto.UsageQueryFilter) *time.Time {
	if filter.EndTime == nil {
		return nil
	}
	currentHour := timeutil.NormalizeStorageTime(*filter.EndTime).Truncate(time.Hour)
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

func buildUsageOverviewHealth(filter dto.UsageQueryFilter) dto.UsageOverviewHealthRecord {
	rows := usageOverviewHealthRows
	columns, span := usageOverviewHealthGrid(filter)
	totalBlocks := rows * columns
	windowStart, windowEnd := usageOverviewHealthWindow(filter, totalBlocks, span)
	blocks := make([]dto.UsageOverviewHealthBlockRecord, totalBlocks)
	for index := range blocks {
		startTime := windowStart.Add(time.Duration(index) * span)
		blocks[index] = dto.UsageOverviewHealthBlockRecord{
			StartTime: startTime,
			EndTime:   startTime.Add(span),
			Rate:      -1,
		}
	}
	return dto.UsageOverviewHealthRecord{
		Rows:          rows,
		Columns:       columns,
		BucketSeconds: int64((span + time.Second - 1) / time.Second),
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		BlockDetails:  blocks,
	}
}

func usageOverviewHealthGrid(filter dto.UsageQueryFilter) (int, time.Duration) {
	if isUsageOverviewShortHealthRange(filter.Range) {
		return usageOverviewHealthDefaultColumns, usageOverviewHealthPresetSpan
	}
	return usageOverviewHealthDefaultColumns, usageOverviewHealthDefaultSpan
}

func isUsageOverviewShortHealthRange(value string) bool {
	switch value {
	case "4h", "8h", "12h", "24h", "today":
		return true
	default:
		return false
	}
}

func usageOverviewHealthWindow(filter dto.UsageQueryFilter, totalBlocks int, span time.Duration) (time.Time, time.Time) {
	end := timeutil.NormalizeStorageTime(time.Now())
	if filter.EndTime != nil {
		end = timeutil.NormalizeStorageTime(*filter.EndTime)
	}
	if isUsageOverviewShortHealthRange(filter.Range) {
		return end.Add(-usageOverviewHealthPresetWindow), end
	}
	currentBucketStart := end.Truncate(span)
	windowEnd := currentBucketStart.Add(span)
	return windowEnd.Add(-time.Duration(totalBlocks) * span), windowEnd
}

func updateUsageOverviewHealthBlock(blocks []dto.UsageOverviewHealthBlockRecord, event entities.UsageEvent) {
	timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
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
