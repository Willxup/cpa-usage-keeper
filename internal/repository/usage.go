package repository

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// usageEventProjectionColumns 限制 usage_events 查询列，避免 Overview 和列表页把 RawJSON 等大字段读入内存。
const usageEventProjectionColumns = "id, api_group_key, provider, auth_type, model, reasoning_effort, timestamp, source, auth_index, failed, latency_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens"

// usageEventProjection 是 usage_events 轻量投影，专门承接 select columns 的查询结果。
type usageEventProjection struct {
	ID                  int64
	APIGroupKey         string
	Provider            string
	AuthType            string
	Model               string
	ReasoningEffort     string
	Timestamp           time.Time
	Source              string
	AuthIndex           string
	Failed              bool
	LatencyMS           int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

// BuildUsageSnapshot 构建无筛选的旧版 usage snapshot，供仍需要全量快照的调用方使用。
func BuildUsageSnapshot(db *gorm.DB) (*dto.StatisticsSnapshot, error) {
	// 复用带筛选入口，空 filter 表示不限制时间和 API key。
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
	query = query.Select(usageEventProjectionColumns).Order("timestamp DESC, id DESC").Limit(pageSize).Offset(offset)

	var events []usageEventProjection
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}

	rows := make([]dto.UsageEventRecord, 0, len(events))
	for _, event := range events {
		rows = append(rows, usageEventProjectionToRecord(event))
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
	if err := query.Select("DISTINCT model").Where("model <> ''").Order("model ASC").Pluck("model", &values).Error; err != nil {
		return nil, fmt.Errorf("load usage event model filter options: %w", err)
	}
	return values, nil
}

// queryUsageEvents 统一 usage_events 的 GORM model 入口，方便后续追加通用 scope。
func queryUsageEvents(db *gorm.DB) *gorm.DB {
	return db.Model(&entities.UsageEvent{})
}

// usageEventProjectionToRecord 把数据库投影转换成 Request Event Log 的外部 DTO。
func usageEventProjectionToRecord(event usageEventProjection) dto.UsageEventRecord {
	// 对前端展示字段统一 trim，避免历史脏数据影响筛选和展示一致性。
	return dto.UsageEventRecord{
		ID:                  event.ID,
		Timestamp:           timeutil.NormalizeStorageTime(event.Timestamp),
		APIGroupKey:         strings.TrimSpace(event.APIGroupKey),
		Model:               strings.TrimSpace(event.Model),
		ReasoningEffort:     strings.TrimSpace(event.ReasoningEffort),
		AuthType:            strings.TrimSpace(event.AuthType),
		Provider:            strings.TrimSpace(event.Provider),
		Source:              strings.TrimSpace(event.Source),
		AuthIndex:           strings.TrimSpace(event.AuthIndex),
		Failed:              event.Failed,
		LatencyMS:           event.LatencyMS,
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CachedTokens:        event.CachedTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         event.TotalTokens,
	}
}

// usageEventProjectionToEntity 把轻量投影转回实体，供内存聚合复用原有事件处理逻辑。
func usageEventProjectionToEntity(event usageEventProjection) entities.UsageEvent {
	// 这里不 trim 原始维度，后续聚合入口会按各自语义统一 normalize。
	return entities.UsageEvent{
		ID:                  event.ID,
		APIGroupKey:         event.APIGroupKey,
		Provider:            event.Provider,
		AuthType:            event.AuthType,
		Model:               event.Model,
		ReasoningEffort:     event.ReasoningEffort,
		Timestamp:           event.Timestamp,
		Source:              event.Source,
		AuthIndex:           event.AuthIndex,
		Failed:              event.Failed,
		LatencyMS:           event.LatencyMS,
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CachedTokens:        event.CachedTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         event.TotalTokens,
	}
}

// applyUsageQueryWindow 给 usage 查询追加闭区间时间过滤。
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

// Overview Tab 第一步：应用时间窗口和全局 API-Key 条件，后续 Overview 专属条件也从这里加。
func applyUsageOverviewQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	query = applyUsageQueryWindow(query, filter)
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	return query
}

// Analysis Tab 第一步：应用时间窗口和全局 API-Key 条件，避免 Request Event Log 的筛选污染聚合。
func applyUsageAnalysisTabQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	query = applyUsageQueryWindow(query, filter)
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	return query
}

// Request Event Log 筛选项第一步：只应用时间窗口，不叠加当前列表筛选。
func applyUsageEventFilterOptionsQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	return applyUsageQueryWindow(query, filter)
}

// Request Event Log 列表第一步：在时间窗口上叠加 model/source/auth_index/result。
func applyUsageEventListQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	query = applyUsageQueryWindow(query, filter)
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		query = query.Where("model = ?", model)
	}
	if authIndex := strings.TrimSpace(filter.AuthIndex); authIndex != "" {
		// Source 下拉在 API 层已转换成 auth_index，仓储层只保留真实查询维度。
		query = query.Where("auth_index = ?", authIndex)
	}
	switch strings.TrimSpace(filter.Result) {
	case "success":
		query = query.Where("failed = ?", false)
	case "failed":
		query = query.Where("failed = ?", true)
	}
	return query
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

func BuildAnalysisWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (*dto.AnalysisRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		return nil, fmt.Errorf("analysis requires start_time and end_time")
	}
	windowMinutes := computeWindowMinutes(filter)
	bucketByDay := windowMinutes > 24*60
	record := &dto.AnalysisRecord{
		Granularity: func() dto.AnalysisGranularity {
			if bucketByDay {
				return dto.AnalysisGranularityDaily
			}
			return dto.AnalysisGranularityHourly
		}(),
		RangeStart: filter.StartTime,
		RangeEnd:   filter.EndTime,
	}

	fullStart, fullEnd := usageOverviewFullHourWindow(*filter.StartTime, *filter.EndTime)
	fullEnd = analysisHourlyStatsEnd(filter, fullEnd)
	if !fullEnd.After(fullStart) {
		return record, nil
	}
	if bucketByDay {
		fullDayStart, fullDayEnd := usageOverviewFullDayWindow(fullStart, fullEnd)
		var dailyRows []entities.UsageOverviewDailyStat
		if fullDayEnd.After(fullDayStart) {
			var err error
			dailyRows, err = loadAnalysisOverviewDailyStatsWithFilter(db, filter, fullDayStart, fullDayEnd)
			if err != nil {
				return nil, err
			}
		}
		hourlyRows, err := loadAnalysisDailyBoundaryHourlyStatsWithFilter(db, filter, fullStart, fullDayStart, fullDayEnd, fullEnd)
		if err != nil {
			return nil, err
		}
		dailyIdentityLookup, err := loadAnalysisDailyIdentityLookup(db, dailyRows)
		if err != nil {
			return nil, err
		}
		hourlyIdentityLookup, err := loadAnalysisHourlyIdentityLookup(db, hourlyRows)
		if err != nil {
			return nil, err
		}
		applyAnalysisDailyAndBoundaryHourlyRows(record, dailyRows, dailyIdentityLookup, hourlyRows, hourlyIdentityLookup)
		return record, nil
	}
	rows, err := loadAnalysisOverviewHourlyStatsWithFilter(db, filter, fullStart, fullEnd)
	if err != nil {
		return nil, err
	}
	identityLookup, err := loadAnalysisHourlyIdentityLookup(db, rows)
	if err != nil {
		return nil, err
	}
	applyAnalysisHourlyRows(record, rows, identityLookup)
	fillAnalysisFullDayHourlyBuckets(record, filter)
	return record, nil
}

func analysisHourlyStatsEnd(filter dto.UsageQueryFilter, fullEnd time.Time) time.Time {
	if filter.StartTime == nil || filter.EndTime == nil {
		return fullEnd
	}
	switch filter.Range {
	case "4h", "8h", "12h", "24h":
		if timeutil.NormalizeStorageTime(*filter.EndTime).After(fullEnd) {
			return fullEnd.Add(time.Hour)
		}
		return fullEnd
	case "today", "yesterday":
	default:
		return fullEnd
	}
	start := timeutil.NormalizeStorageTime(*filter.StartTime).Truncate(time.Hour)
	dayBoundaryEnd := start.Add(24 * time.Hour)
	if dayBoundaryEnd.After(fullEnd) {
		return dayBoundaryEnd
	}
	return fullEnd
}

type analysisHeatmapKey struct {
	apiKey string
	model  string
}

const analysisIdentityLookupBatchSize = 900

type analysisIdentityInfo struct {
	identity string
	label    string
	authType entities.UsageIdentityAuthType
}

type analysisIdentityLookup map[entities.UsageIdentityAuthType]map[string]analysisIdentityInfo

func loadAnalysisHourlyIdentityLookup(db *gorm.DB, rows []entities.UsageOverviewHourlyStat) (analysisIdentityLookup, error) {
	return loadAnalysisIdentityLookup(db, collectAnalysisAuthIndexes(len(rows), func(i int) string {
		return rows[i].AuthIndex
	}))
}

func loadAnalysisDailyIdentityLookup(db *gorm.DB, rows []entities.UsageOverviewDailyStat) (analysisIdentityLookup, error) {
	return loadAnalysisIdentityLookup(db, collectAnalysisAuthIndexes(len(rows), func(i int) string {
		return rows[i].AuthIndex
	}))
}

func collectAnalysisAuthIndexes(count int, authIndexAt func(int) string) []string {
	authIndexes := make([]string, 0, count)
	seen := map[string]struct{}{}
	for i := range count {
		authIndex := strings.TrimSpace(authIndexAt(i))
		if authIndex == "" {
			continue
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		authIndexes = append(authIndexes, authIndex)
	}
	return authIndexes
}

func loadAnalysisIdentityLookup(db *gorm.DB, authIndexes []string) (analysisIdentityLookup, error) {
	lookup := analysisIdentityLookup{
		entities.UsageIdentityAuthTypeAuthFile:   map[string]analysisIdentityInfo{},
		entities.UsageIdentityAuthTypeAIProvider: map[string]analysisIdentityInfo{},
	}
	if len(authIndexes) == 0 {
		return lookup, nil
	}
	for start := 0; start < len(authIndexes); start += analysisIdentityLookupBatchSize {
		end := min(start+analysisIdentityLookupBatchSize, len(authIndexes))
		var identities []entities.UsageIdentity
		if err := db.Where("identity IN ? AND auth_type IN ? AND is_deleted = ?", authIndexes[start:end], []entities.UsageIdentityAuthType{entities.UsageIdentityAuthTypeAuthFile, entities.UsageIdentityAuthTypeAIProvider}, false).Find(&identities).Error; err != nil {
			return nil, fmt.Errorf("load analysis usage identities: %w", err)
		}
		for _, identity := range identities {
			label := helper.UsageIdentityDisplayName(identity)
			lookup[identity.AuthType][identity.Identity] = analysisIdentityInfo{identity: identity.Identity, label: label, authType: identity.AuthType}
		}
	}
	return lookup, nil
}

func applyAnalysisHourlyRows(record *dto.AnalysisRecord, rows []entities.UsageOverviewHourlyStat, identityLookup analysisIdentityLookup) {
	bucketTotals := map[time.Time]*dto.AnalysisTokenUsageBucketRecord{}
	apiTotals := map[string]*dto.AnalysisCompositionRecord{}
	modelTotals := map[string]*dto.AnalysisCompositionRecord{}
	authFileTotals := map[string]*dto.AnalysisCompositionRecord{}
	aiProviderTotals := map[string]*dto.AnalysisCompositionRecord{}
	heatmapTotals := map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord{}
	for _, row := range rows {
		bucket := timeutil.NormalizeStorageTime(row.BucketStart).Truncate(time.Hour)
		applyAnalysisRow(record, bucketTotals, apiTotals, modelTotals, heatmapTotals, bucket, row.APIGroupKey, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens)
		applyAnalysisIdentityComposition(identityLookup, authFileTotals, aiProviderTotals, row.AuthIndex, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens)
	}
	finalizeAnalysisRecord(record, bucketTotals, apiTotals, modelTotals, authFileTotals, aiProviderTotals, heatmapTotals)
}

func applyAnalysisDailyRows(record *dto.AnalysisRecord, rows []entities.UsageOverviewDailyStat, identityLookup analysisIdentityLookup) {
	applyAnalysisDailyAndBoundaryHourlyRows(record, rows, identityLookup, nil, analysisIdentityLookup{})
}

func applyAnalysisDailyAndBoundaryHourlyRows(record *dto.AnalysisRecord, dailyRows []entities.UsageOverviewDailyStat, dailyIdentityLookup analysisIdentityLookup, hourlyRows []entities.UsageOverviewHourlyStat, hourlyIdentityLookup analysisIdentityLookup) {
	bucketTotals := map[time.Time]*dto.AnalysisTokenUsageBucketRecord{}
	apiTotals := map[string]*dto.AnalysisCompositionRecord{}
	modelTotals := map[string]*dto.AnalysisCompositionRecord{}
	authFileTotals := map[string]*dto.AnalysisCompositionRecord{}
	aiProviderTotals := map[string]*dto.AnalysisCompositionRecord{}
	heatmapTotals := map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord{}
	for _, row := range dailyRows {
		bucket := timeutil.NormalizeStorageTime(row.BucketStart)
		applyAnalysisRow(record, bucketTotals, apiTotals, modelTotals, heatmapTotals, bucket, row.APIGroupKey, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens)
		applyAnalysisIdentityComposition(dailyIdentityLookup, authFileTotals, aiProviderTotals, row.AuthIndex, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens)
	}
	for _, row := range hourlyRows {
		bucketStart := timeutil.NormalizeStorageTime(row.BucketStart)
		bucket := time.Date(bucketStart.Year(), bucketStart.Month(), bucketStart.Day(), 0, 0, 0, 0, bucketStart.Location())
		applyAnalysisRow(record, bucketTotals, apiTotals, modelTotals, heatmapTotals, bucket, row.APIGroupKey, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens)
		applyAnalysisIdentityComposition(hourlyIdentityLookup, authFileTotals, aiProviderTotals, row.AuthIndex, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens)
	}
	finalizeAnalysisRecord(record, bucketTotals, apiTotals, modelTotals, authFileTotals, aiProviderTotals, heatmapTotals)
}

func applyAnalysisRow(_ *dto.AnalysisRecord, bucketTotals map[time.Time]*dto.AnalysisTokenUsageBucketRecord, apiTotals, modelTotals map[string]*dto.AnalysisCompositionRecord, heatmapTotals map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord, bucket time.Time, apiGroupKey, model string, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens int64) {
	apiKey := normalizeUsageOverviewDimension(apiGroupKey)
	modelName := normalizeUsageOverviewDimension(model)
	bucketTotal := bucketTotals[bucket]
	if bucketTotal == nil {
		bucketTotal = &dto.AnalysisTokenUsageBucketRecord{Bucket: bucket}
		bucketTotals[bucket] = bucketTotal
	}
	bucketTotal.Requests += requests
	bucketTotal.InputTokens += inputTokens
	bucketTotal.OutputTokens += outputTokens
	bucketTotal.CachedTokens += cachedTokens
	bucketTotal.ReasoningTokens += reasoningTokens
	bucketTotal.TotalTokens += totalTokens

	apiTotal := apiTotals[apiKey]
	if apiTotal == nil {
		apiTotal = &dto.AnalysisCompositionRecord{Key: apiKey}
		apiTotals[apiKey] = apiTotal
	}
	applyAnalysisCompositionTotals(apiTotal, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens)

	modelTotal := modelTotals[modelName]
	if modelTotal == nil {
		modelTotal = &dto.AnalysisCompositionRecord{Key: modelName}
		modelTotals[modelName] = modelTotal
	}
	applyAnalysisCompositionTotals(modelTotal, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens)

	heatmapKey := analysisHeatmapKey{apiKey: apiKey, model: modelName}
	heatmapTotal := heatmapTotals[heatmapKey]
	if heatmapTotal == nil {
		heatmapTotal = &dto.AnalysisHeatmapRecord{APIKey: apiKey, Model: modelName}
		heatmapTotals[heatmapKey] = heatmapTotal
	}
	heatmapTotal.Requests += requests
	heatmapTotal.TotalTokens += totalTokens
}

func applyAnalysisCompositionTotals(item *dto.AnalysisCompositionRecord, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens int64) {
	item.Requests += requests
	item.InputTokens += inputTokens
	item.OutputTokens += outputTokens
	item.CachedTokens += cachedTokens
	item.ReasoningTokens += reasoningTokens
	item.TotalTokens += totalTokens
}

func applyAnalysisIdentityComposition(identityLookup analysisIdentityLookup, authFileTotals, aiProviderTotals map[string]*dto.AnalysisCompositionRecord, authIndex string, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens int64) {
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return
	}
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAuthFile, authIndex); ok {
		applyAnalysisIdentityCompositionTotal(authFileTotals, identity, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens)
	}
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAIProvider, authIndex); ok {
		applyAnalysisIdentityCompositionTotal(aiProviderTotals, identity, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens)
	}
}

func applyAnalysisIdentityCompositionTotal(totals map[string]*dto.AnalysisCompositionRecord, identity analysisIdentityInfo, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens int64) {
	item := totals[identity.identity]
	if item == nil {
		item = &dto.AnalysisCompositionRecord{Key: identity.identity, Label: identity.label}
		totals[identity.identity] = item
	}
	applyAnalysisCompositionTotals(item, requests, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens)
}

func (lookup analysisIdentityLookup) find(authType entities.UsageIdentityAuthType, identity string) (analysisIdentityInfo, bool) {
	byIdentity := lookup[authType]
	if byIdentity == nil {
		return analysisIdentityInfo{}, false
	}
	item, ok := byIdentity[identity]
	return item, ok
}

func fillAnalysisFullDayHourlyBuckets(record *dto.AnalysisRecord, filter dto.UsageQueryFilter) {
	if record == nil || record.Granularity != dto.AnalysisGranularityHourly || filter.StartTime == nil {
		return
	}
	if filter.Range != "today" && filter.Range != "yesterday" {
		return
	}
	start := timeutil.NormalizeStorageTime(*filter.StartTime).Truncate(time.Hour)
	bucketByTime := make(map[time.Time]dto.AnalysisTokenUsageBucketRecord, len(record.TokenUsage)+24)
	for _, bucket := range record.TokenUsage {
		bucketByTime[timeutil.NormalizeStorageTime(bucket.Bucket).Truncate(time.Hour)] = bucket
	}
	record.TokenUsage = record.TokenUsage[:0]
	for hour := 0; hour <= 24; hour++ {
		bucketTime := start.Add(time.Duration(hour) * time.Hour)
		bucket, ok := bucketByTime[bucketTime]
		if !ok {
			bucket = dto.AnalysisTokenUsageBucketRecord{Bucket: bucketTime}
		}
		record.TokenUsage = append(record.TokenUsage, bucket)
	}
}

func finalizeAnalysisRecord(record *dto.AnalysisRecord, bucketTotals map[time.Time]*dto.AnalysisTokenUsageBucketRecord, apiTotals, modelTotals, authFileTotals, aiProviderTotals map[string]*dto.AnalysisCompositionRecord, heatmapTotals map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord) {
	for _, bucket := range bucketTotals {
		record.TokenUsage = append(record.TokenUsage, *bucket)
	}
	sort.Slice(record.TokenUsage, func(i, j int) bool { return record.TokenUsage[i].Bucket.Before(record.TokenUsage[j].Bucket) })
	for _, item := range apiTotals {
		record.APIKeyComposition = append(record.APIKeyComposition, *item)
	}
	sortAnalysisComposition(record.APIKeyComposition)
	for _, item := range modelTotals {
		record.ModelComposition = append(record.ModelComposition, *item)
	}
	sortAnalysisComposition(record.ModelComposition)
	for _, item := range authFileTotals {
		record.AuthFilesComposition = append(record.AuthFilesComposition, *item)
	}
	sortAnalysisComposition(record.AuthFilesComposition)
	for _, item := range aiProviderTotals {
		record.AIProviderComposition = append(record.AIProviderComposition, *item)
	}
	sortAnalysisComposition(record.AIProviderComposition)
	for _, cell := range heatmapTotals {
		record.Heatmap = append(record.Heatmap, *cell)
	}
	sort.Slice(record.Heatmap, func(i, j int) bool {
		if record.Heatmap[i].APIKey == record.Heatmap[j].APIKey {
			return record.Heatmap[i].Model < record.Heatmap[j].Model
		}
		return record.Heatmap[i].APIKey < record.Heatmap[j].APIKey
	})
}

func sortAnalysisComposition(items []dto.AnalysisCompositionRecord) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalTokens == items[j].TotalTokens {
			return items[i].Key < items[j].Key
		}
		return items[i].TotalTokens > items[j].TotalTokens
	})
}

// Overview 使用预聚合完整小时，并用原始事件补偿窗口边界以保持非整点查询精确。
func BuildUsageOverviewWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (*dto.UsageOverviewRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	// Overview 页面现在必须先由 API 层把 4h/8h/custom 等 range 解析成具体时间窗口。
	if filter.StartTime == nil || filter.EndTime == nil {
		return nil, fmt.Errorf("usage overview requires start_time and end_time")
	}

	// stats 表不保存价格，所有 cost 都按当前 model_price_settings 在查询阶段动态计算。
	pricingByModel, err := loadPriceSettingsByModel(db)
	if err != nil {
		return nil, err
	}

	return buildUsageOverviewFromStats(db, filter, pricingByModel)
}

// newUsageOverviewRecord 初始化 Overview 返回结构中的 map，避免后续聚合写入 nil map。
func newUsageOverviewRecord(filter dto.UsageQueryFilter, windowMinutes int64) *dto.UsageOverviewRecord {
	return &dto.UsageOverviewRecord{
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
}

// buildUsageOverviewFromStats 用预聚合表覆盖完整 bucket，用原始事件补偿窗口边界。
func buildUsageOverviewFromStats(db *gorm.DB, filter dto.UsageQueryFilter, pricingByModel map[string]entities.ModelPriceSetting) (*dto.UsageOverviewRecord, error) {
	// 先确定响应粒度和最近小时序列窗口，后续 raw event 与 stats row 共用这些规则。
	windowMinutes := computeWindowMinutes(filter)
	bucketByDay := shouldBucketUsageOverviewByDay(filter, windowMinutes)
	latestHourlyStart := latestHourlySeriesStart(filter)
	overview := newUsageOverviewRecord(filter, windowMinutes)

	// fullStart/fullEnd 是能被 hourly stats 完整覆盖的半开区间。
	fullStart, fullEnd := usageOverviewFullHourWindow(*filter.StartTime, *filter.EndTime)
	// 原始事件只补主统计和 health grid 各自的窄边界，避免长窗口被 health 7d 展示窗口扩大成大范围事件扫描。
	rawEventWindows := usageOverviewRawEventWindows(filter, overview.Health, fullStart, fullEnd)

	// 非整点窗口的头尾不能用小时 stats，否则会把窗口外事件算进去。
	boundaryEvents, err := loadUsageOverviewRawEventWindowsWithFilter(db, filter, rawEventWindows)
	if err != nil {
		return nil, err
	}
	for _, event := range boundaryEvents {
		if usageOverviewEventInsideWindow(event, fullStart, fullEnd) {
			continue
		}
		applyUsageEventToSnapshot(overview.Usage, event, false)
		applyUsageEventToOverview(overview, event, bucketByDay, latestHourlyStart, pricingByModel)
	}

	if fullEnd.After(fullStart) {
		// 短窗口的主序列和 snapshot 小时图必须保持小时粒度，不能因为内部包含完整天就压成 daily bucket。
		fullDayStart, fullDayEnd := usageOverviewFullDayWindow(fullStart, fullEnd)
		if !bucketByDay || !fullDayEnd.After(fullDayStart) {
			hourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, filter, fullStart, fullEnd)
			if err != nil {
				return nil, err
			}
			for _, row := range hourlyRows {
				applyUsageOverviewHourlyStatToOverview(overview, row, bucketByDay, latestHourlyStart, pricingByModel)
			}
		} else {
			// 长窗口中间的完整本地天用 daily stats，减少大量小时 row 累加。
			dailyRows, err := loadUsageOverviewDailyStatsWithFilter(db, filter, fullDayStart, fullDayEnd)
			if err != nil {
				return nil, err
			}
			for _, row := range dailyRows {
				applyUsageOverviewDailyStatToOverview(overview, row, bucketByDay, pricingByModel)
			}
			// Snapshot 的 RequestsByHour/TokensByHour 是旧响应结构，daily stats 不能直接还原小时图。
			snapshotHourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, filter, fullDayStart, fullDayEnd)
			if err != nil {
				return nil, err
			}
			for _, row := range snapshotHourlyRows {
				applyUsageOverviewHourlyStatToSnapshotHours(overview.Usage, row)
			}

			// 完整天两侧剩余的完整小时仍走 hourly stats，避免回退到大范围事件扫描。
			for _, window := range []struct{ start, end time.Time }{{fullStart, fullDayStart}, {fullDayEnd, fullEnd}} {
				if !window.end.After(window.start) {
					continue
				}
				hourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, filter, window.start, window.end)
				if err != nil {
					return nil, err
				}
				for _, row := range hourlyRows {
					applyUsageOverviewHourlyStatToOverview(overview, row, bucketByDay, latestHourlyStart, pricingByModel)
				}
			}

			// daily stats 无法还原 latest hourly series，最近 24 小时仍额外读取 hourly stats。
			if latestHourlyStart != nil {
				hourlySeriesStart := *latestHourlyStart
				if hourlySeriesStart.Before(fullDayStart) {
					hourlySeriesStart = fullDayStart
				}
				if fullDayEnd.After(hourlySeriesStart) {
					hourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, filter, hourlySeriesStart, fullDayEnd)
					if err != nil {
						return nil, err
					}
					for _, row := range hourlyRows {
						applyUsageOverviewHourlyStatToHourlySeries(overview, row, latestHourlyStart, pricingByModel)
					}
				}
			}
		}
	}

	healthSuccess, healthFailure, err := loadUsageOverviewHealthTotalsWithFilter(db, filter, boundaryEvents, fullStart, fullEnd)
	if err != nil {
		return nil, err
	}
	// Health 格子按展示窗口读取 health stats，总计仍按完整查询窗口覆盖，保持旧事件扫描语义。
	overview.Health = buildUsageOverviewHealth(filter)
	if err := applyUsageOverviewHealthStatsToOverview(db, overview, filter, boundaryEvents); err != nil {
		return nil, err
	}
	overview.Health.TotalSuccess = healthSuccess
	overview.Health.TotalFailure = healthFailure
	finalizeUsageOverview(overview, false)
	return overview, nil
}

// usageOverviewFullHourWindow 返回查询窗口内部可安全使用小时 stats 的半开区间。
func usageOverviewFullHourWindow(start, end time.Time) (time.Time, time.Time) {
	start = timeutil.NormalizeStorageTime(start)
	end = timeutil.NormalizeStorageTime(end)
	fullStart := start.Truncate(time.Hour)
	if !start.Equal(fullStart) {
		fullStart = fullStart.Add(time.Hour)
	}
	fullEnd := end.Truncate(time.Hour)
	if fullEnd.Before(fullStart) {
		fullEnd = fullStart
	}
	return fullStart, fullEnd
}

// usageOverviewFullDayWindow 返回完整小时窗口内部可安全使用 daily stats 的本地天区间。
func usageOverviewFullDayWindow(start, end time.Time) (time.Time, time.Time) {
	start = timeutil.NormalizeStorageTime(start)
	end = timeutil.NormalizeStorageTime(end)
	fullStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	if !start.Equal(fullStart) {
		fullStart = fullStart.Add(24 * time.Hour)
	}
	fullEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	if fullEnd.Before(fullStart) {
		fullEnd = fullStart
	}
	return fullStart, fullEnd
}

type usageOverviewRawEventWindow struct {
	start      time.Time
	end        time.Time
	includeEnd bool
}

// usageOverviewRawEventWindows 返回 Overview 需要读取 usage_events 的小窗口并集，完整小时和完整 health bucket 都交给 stats 表。
func usageOverviewRawEventWindows(filter dto.UsageQueryFilter, health dto.UsageOverviewHealthRecord, fullHourStart, fullHourEnd time.Time) []usageOverviewRawEventWindow {
	if filter.StartTime == nil || filter.EndTime == nil {
		return nil
	}
	windowStart := timeutil.NormalizeStorageTime(*filter.StartTime)
	windowEnd := timeutil.NormalizeStorageTime(*filter.EndTime)
	windows := make([]usageOverviewRawEventWindow, 0, 4)
	windows = appendUsageOverviewRawEventBoundaryWindows(windows, windowStart, windowEnd, fullHourStart, fullHourEnd, true)

	exactStart, exactEnd := usageOverviewHealthExactWindow(health, filter)
	if exactStart.Before(exactEnd) {
		span := time.Duration(health.BucketSeconds) * time.Second
		healthFullStart, healthFullEnd := usageOverviewFullHealthWindow(exactStart, exactEnd, span)
		windows = appendUsageOverviewRawEventBoundaryWindows(windows, exactStart, exactEnd, healthFullStart, healthFullEnd, false)
	}
	return mergeUsageOverviewRawEventWindows(windows)
}

func appendUsageOverviewRawEventBoundaryWindows(windows []usageOverviewRawEventWindow, windowStart, windowEnd, coveredStart, coveredEnd time.Time, includeRightEnd bool) []usageOverviewRawEventWindow {
	if !windowStart.Before(windowEnd) {
		return windows
	}
	if windowStart.Before(coveredStart) {
		leftEnd := coveredStart
		if windowEnd.Before(leftEnd) {
			leftEnd = windowEnd
		}
		if windowStart.Before(leftEnd) {
			windows = append(windows, usageOverviewRawEventWindow{start: windowStart, end: leftEnd})
		}
	}
	if !windowEnd.Before(coveredEnd) {
		rightStart := coveredEnd
		if rightStart.Before(windowStart) {
			rightStart = windowStart
		}
		if rightStart.Before(windowEnd) || (includeRightEnd && rightStart.Equal(windowEnd)) {
			windows = append(windows, usageOverviewRawEventWindow{start: rightStart, end: windowEnd, includeEnd: includeRightEnd})
		}
	}
	return windows
}

func mergeUsageOverviewRawEventWindows(windows []usageOverviewRawEventWindow) []usageOverviewRawEventWindow {
	if len(windows) < 2 {
		return windows
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].start.Equal(windows[j].start) {
			return windows[i].end.Before(windows[j].end)
		}
		return windows[i].start.Before(windows[j].start)
	})
	merged := windows[:1]
	for _, window := range windows[1:] {
		last := &merged[len(merged)-1]
		if window.start.After(last.end) {
			merged = append(merged, window)
			continue
		}
		if window.end.After(last.end) {
			last.end = window.end
			last.includeEnd = window.includeEnd
		} else if window.end.Equal(last.end) && window.includeEnd {
			last.includeEnd = true
		}
	}
	return merged
}

// usageOverviewEventInsideWindow 判断事件是否已由某个 stats 窗口覆盖。
func usageOverviewEventInsideWindow(event entities.UsageEvent, start, end time.Time) bool {
	timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
	return !timestamp.Before(start) && timestamp.Before(end)
}

// loadUsageOverviewHourlyStatsWithFilter 读取完整小时 stats，并复用 Overview 的 API key 过滤条件。
func loadUsageOverviewHourlyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]entities.UsageOverviewHourlyStat, error) {
	return loadUsageOverviewHourlyStats(db, filter, start, end, false)
}

func loadAnalysisOverviewHourlyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]entities.UsageOverviewHourlyStat, error) {
	return loadUsageOverviewHourlyStats(db, filter, start, end, true)
}

func loadAnalysisDailyBoundaryHourlyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, fullStart, fullDayStart, fullDayEnd, fullEnd time.Time) ([]entities.UsageOverviewHourlyStat, error) {
	windows := analysisDailyBoundaryHourlyWindows(fullStart, fullDayStart, fullDayEnd, fullEnd)
	rows := make([]entities.UsageOverviewHourlyStat, 0)
	for _, window := range windows {
		windowRows, err := loadAnalysisOverviewHourlyStatsWithFilter(db, filter, window.start, window.end)
		if err != nil {
			return nil, err
		}
		rows = append(rows, windowRows...)
	}
	return rows, nil
}

func analysisDailyBoundaryHourlyWindows(fullStart, fullDayStart, fullDayEnd, fullEnd time.Time) []usageOverviewRawEventWindow {
	windows := make([]usageOverviewRawEventWindow, 0, 2)
	leftEnd := fullDayStart
	if fullEnd.Before(leftEnd) {
		leftEnd = fullEnd
	}
	if fullStart.Before(leftEnd) {
		windows = append(windows, usageOverviewRawEventWindow{start: fullStart, end: leftEnd})
	}
	rightStart := fullDayEnd
	if rightStart.Before(fullStart) {
		rightStart = fullStart
	}
	if rightStart.Before(fullEnd) {
		windows = append(windows, usageOverviewRawEventWindow{start: rightStart, end: fullEnd})
	}
	return mergeUsageOverviewRawEventWindows(windows)
}

func loadUsageOverviewHourlyStats(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, activeCPAAPIKeysOnly bool) ([]entities.UsageOverviewHourlyStat, error) {
	var rows []entities.UsageOverviewHourlyStat
	query := db.Model(&entities.UsageOverviewHourlyStat{}).
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
		Order("bucket_start asc")
	if activeCPAAPIKeysOnly {
		query = query.Joins("INNER JOIN cpa_api_keys ON cpa_api_keys.api_key = usage_overview_hourly_stats.api_group_key AND cpa_api_keys.is_deleted = ?", false)
	}
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview hourly stats: %w", err)
	}
	return rows, nil
}

// loadUsageOverviewDailyStatsWithFilter 读取完整本地天 stats，并复用 Overview 的 API key 过滤条件。
func loadUsageOverviewDailyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]entities.UsageOverviewDailyStat, error) {
	return loadUsageOverviewDailyStats(db, filter, start, end, false)
}

func loadAnalysisOverviewDailyStatsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]entities.UsageOverviewDailyStat, error) {
	return loadUsageOverviewDailyStats(db, filter, start, end, true)
}

func loadUsageOverviewDailyStats(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, activeCPAAPIKeysOnly bool) ([]entities.UsageOverviewDailyStat, error) {
	var rows []entities.UsageOverviewDailyStat
	query := db.Model(&entities.UsageOverviewDailyStat{}).
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
		Order("bucket_start asc")
	if activeCPAAPIKeysOnly {
		query = query.Joins("INNER JOIN cpa_api_keys ON cpa_api_keys.api_key = usage_overview_daily_stats.api_group_key AND cpa_api_keys.is_deleted = ?", false)
	}
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview daily stats: %w", err)
	}
	return rows, nil
}

func loadUsageOverviewRawEventWindowsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, windows []usageOverviewRawEventWindow) ([]entities.UsageEvent, error) {
	events := make([]entities.UsageEvent, 0)
	for _, window := range windows {
		windowEvents, err := loadUsageOverviewEventRangeWithFilter(db, filter, window.start, window.end, window.includeEnd)
		if err != nil {
			return nil, err
		}
		events = append(events, windowEvents...)
	}
	return events, nil
}

// loadUsageOverviewBoundaryEventsWithFilter 只读取不能被完整小时 stats 覆盖的边界事件。
func loadUsageOverviewBoundaryEventsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, fullStart, fullEnd time.Time) ([]entities.UsageEvent, error) {
	if filter.StartTime == nil || filter.EndTime == nil {
		return loadUsageOverviewEventsWithFilter(db, filter)
	}
	// 边界补偿只覆盖完整小时之外的左段和右段，完整小时全部交给 stats 表。
	windowStart := timeutil.NormalizeStorageTime(*filter.StartTime)
	windowEnd := timeutil.NormalizeStorageTime(*filter.EndTime)
	events := make([]entities.UsageEvent, 0)
	if windowStart.Before(fullStart) {
		// 左边界用 [windowStart, fullStart) 半开区间，避免和第一个完整小时重复。
		leftEnd := fullStart
		if windowEnd.Before(leftEnd) {
			leftEnd = windowEnd
		}
		leftEvents, err := loadUsageOverviewEventRangeWithFilter(db, filter, windowStart, leftEnd, false)
		if err != nil {
			return nil, err
		}
		events = append(events, leftEvents...)
	}
	if !windowEnd.Before(fullEnd) {
		// 右边界用 [fullEnd, windowEnd]，保留原始闭区间 end_time 语义。
		rightStart := fullEnd
		if rightStart.Before(windowStart) {
			rightStart = windowStart
		}
		rightEvents, err := loadUsageOverviewEventRangeWithFilter(db, filter, rightStart, windowEnd, true)
		if err != nil {
			return nil, err
		}
		events = append(events, rightEvents...)
	}
	return events, nil
}

// loadUsageOverviewEventRangeWithFilter 使用单段 timestamp 范围查询，避免 OR 影响 usage_events 时间索引。
func loadUsageOverviewEventRangeWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, includeEnd bool) ([]entities.UsageEvent, error) {
	if end.Before(start) || (!includeEnd && end.Equal(start)) {
		return nil, nil
	}
	// 单段范围让 SQLite 可以稳定使用 timestamp 索引，不把左右边界拼成 OR 查询。
	query := db.Model(&entities.UsageEvent{}).
		Where("timestamp >= ?", timeutil.FormatStorageTime(start)).
		Select(usageEventProjectionColumns).
		Order("timestamp asc")
	if includeEnd {
		query = query.Where("timestamp <= ?", timeutil.FormatStorageTime(end))
	} else {
		query = query.Where("timestamp < ?", timeutil.FormatStorageTime(end))
	}
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	var rows []usageEventProjection
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage overview boundary event range: %w", err)
	}
	events := make([]entities.UsageEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, usageEventProjectionToEntity(row))
	}
	return events, nil
}

// loadUsageOverviewHealthTotalsWithFilter 用完整小时 stats 和边界事件还原旧 Overview health 总计语义。
func loadUsageOverviewHealthTotalsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, boundaryEvents []entities.UsageEvent, fullStart, fullEnd time.Time) (int64, int64, error) {
	// 总计口径覆盖完整查询窗口：边界事件来自 usage_events，完整小时来自 hourly stats。
	var successCount int64
	var failureCount int64
	for _, event := range boundaryEvents {
		if usageOverviewEventInsideWindow(event, fullStart, fullEnd) {
			continue
		}
		if event.Failed {
			failureCount++
		} else {
			successCount++
		}
	}
	if !fullEnd.After(fullStart) {
		return successCount, failureCount, nil
	}
	// health 总计不按 health grid 窗口截断，否则 7d/30d 查询会丢完整查询窗口内的数据。
	totalsQuery := db.Model(&entities.UsageOverviewHourlyStat{}).
		Select("COALESCE(SUM(success_count), 0) AS success_count, COALESCE(SUM(failure_count), 0) AS failure_count").
		Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(fullStart), timeutil.FormatStorageTime(fullEnd))
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		totalsQuery = totalsQuery.Where("api_group_key = ?", apiGroupKey)
	}
	var totals struct {
		SuccessCount int64
		FailureCount int64
	}
	if err := totalsQuery.Scan(&totals).Error; err != nil {
		return 0, 0, fmt.Errorf("load usage overview health totals: %w", err)
	}
	return successCount + totals.SuccessCount, failureCount + totals.FailureCount, nil
}

// applyUsageOverviewHourlyStatToOverview 把小时 stats 同步写入 summary、snapshot、主序列、小时序列和天序列。
func applyUsageOverviewHourlyStatToOverview(overview *dto.UsageOverviewRecord, row entities.UsageOverviewHourlyStat, bucketByDay bool, latestHourlyStart *time.Time, pricingByModel map[string]entities.ModelPriceSetting) {
	// 小时 stats 是完整小时事实，可直接累计到 snapshot totals。
	applyUsageOverviewHourlyStatToSnapshot(overview.Usage, row)
	// cost 不入 stats 表，必须在读取时按当前价格表重新计算。
	rowCost := calculateUsageOverviewStatCost(row.InputTokens, row.OutputTokens, row.CachedTokens, pricingByModel[strings.TrimSpace(row.Model)])
	if _, ok := pricingByModel[strings.TrimSpace(row.Model)]; !ok && usageOverviewStatRequiresPricing(row.InputTokens, row.OutputTokens, row.CachedTokens) {
		overview.Summary.CostAvailable = false
	}
	applyUsageOverviewStatToSummary(overview, row.RequestCount, row.CachedTokens, row.ReasoningTokens, rowCost)

	// 主序列按当前窗口选择小时或天粒度。
	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), bucketByDay)
	applyUsageOverviewStatToSeries(&overview.Series, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens, rowCost, bucketKey, bucketMinutes)

	// 长窗口仍保留最近 24 小时 hourly_series。
	applyUsageOverviewHourlyStatToHourlySeries(overview, row, latestHourlyStart, pricingByModel)

	// daily_series 始终按天累计，供页面固定展示天趋势。
	dayKey, dayMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), true)
	applyUsageOverviewStatToSeries(&overview.DailySeries, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens, rowCost, dayKey, dayMinutes)
}

// applyUsageOverviewHourlyStatToHourlySeries 只补最近 24 小时 hourly_series，供长窗口页面仍展示小时趋势。
func applyUsageOverviewHourlyStatToHourlySeries(overview *dto.UsageOverviewRecord, row entities.UsageOverviewHourlyStat, latestHourlyStart *time.Time, pricingByModel map[string]entities.ModelPriceSetting) {
	// latestHourlyStart 非空时丢弃更早的小时，避免长窗口 hourly_series 过大。
	if latestHourlyStart != nil && timeutil.NormalizeStorageTime(row.BucketStart).Before(*latestHourlyStart) {
		return
	}
	rowCost := calculateUsageOverviewStatCost(row.InputTokens, row.OutputTokens, row.CachedTokens, pricingByModel[strings.TrimSpace(row.Model)])
	hourKey, hourMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), false)
	applyUsageOverviewStatToSeries(&overview.HourlySeries, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens, rowCost, hourKey, hourMinutes)
}

// applyUsageOverviewDailyStatToOverview 把完整天 stats 写入长窗口 summary、snapshot、主序列和天序列。
func applyUsageOverviewDailyStatToOverview(overview *dto.UsageOverviewRecord, row entities.UsageOverviewDailyStat, bucketByDay bool, pricingByModel map[string]entities.ModelPriceSetting) {
	// 天 stats 只覆盖完整本地天，不能用于非整天边界。
	applyUsageOverviewDailyStatToSnapshot(overview.Usage, row)
	rowCost := calculateUsageOverviewStatCost(row.InputTokens, row.OutputTokens, row.CachedTokens, pricingByModel[strings.TrimSpace(row.Model)])
	if _, ok := pricingByModel[strings.TrimSpace(row.Model)]; !ok && usageOverviewStatRequiresPricing(row.InputTokens, row.OutputTokens, row.CachedTokens) {
		overview.Summary.CostAvailable = false
	}
	applyUsageOverviewStatToSummary(overview, row.RequestCount, row.CachedTokens, row.ReasoningTokens, rowCost)

	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), bucketByDay)
	applyUsageOverviewStatToSeries(&overview.Series, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens, rowCost, bucketKey, bucketMinutes)

	dayKey, dayMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), true)
	applyUsageOverviewStatToSeries(&overview.DailySeries, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CachedTokens, row.ReasoningTokens, row.TotalTokens, rowCost, dayKey, dayMinutes)
}

// applyUsageOverviewStatToSummary 写入 summary 中不在 StatisticsSnapshot 里的 token/cost 字段。
func applyUsageOverviewStatToSummary(overview *dto.UsageOverviewRecord, requestCount, cachedTokens, reasoningTokens int64, cost float64) {
	overview.Summary.CachedTokens += cachedTokens
	overview.Summary.ReasoningTokens += reasoningTokens
	overview.Summary.TotalCost += cost
}

// applyUsageOverviewHourlyStatToSnapshot 把小时 stats 合入旧 snapshot 结构，保持 API response 兼容。
func applyUsageOverviewHourlyStatToSnapshot(snapshot *dto.StatisticsSnapshot, row entities.UsageOverviewHourlyStat) {
	applyUsageOverviewStatToSnapshotTotals(snapshot, row.APIGroupKey, row.Model, row.RequestCount, row.SuccessCount, row.FailureCount, row.TotalTokens)

	bucketStart := timeutil.NormalizeStorageTime(row.BucketStart)
	dayKey := bucketStart.Format("2006-01-02")
	hourKey := timeutil.FormatStorageTime(bucketStart.Truncate(time.Hour))
	snapshot.RequestsByDay[dayKey] += row.RequestCount
	snapshot.RequestsByHour[hourKey] += row.RequestCount
	snapshot.TokensByDay[dayKey] += row.TotalTokens
	snapshot.TokensByHour[hourKey] += row.TotalTokens
}

// applyUsageOverviewHourlyStatToSnapshotHours 用 hourly stats 补齐 snapshot 的小时图，不重复累计 totals/API/model。
func applyUsageOverviewHourlyStatToSnapshotHours(snapshot *dto.StatisticsSnapshot, row entities.UsageOverviewHourlyStat) {
	if snapshot.RequestsByHour == nil {
		snapshot.RequestsByHour = make(map[string]int64)
	}
	if snapshot.TokensByHour == nil {
		snapshot.TokensByHour = make(map[string]int64)
	}
	bucketKey := timeutil.NormalizeStorageTime(row.BucketStart).Format(time.RFC3339)
	snapshot.RequestsByHour[bucketKey] += row.RequestCount
	snapshot.TokensByHour[bucketKey] += row.TotalTokens
}

// applyUsageOverviewDailyStatToSnapshot 把天 stats 合入旧 snapshot 结构，完整小时明细由 hourly stats 负责。
func applyUsageOverviewDailyStatToSnapshot(snapshot *dto.StatisticsSnapshot, row entities.UsageOverviewDailyStat) {
	applyUsageOverviewStatToSnapshotTotals(snapshot, row.APIGroupKey, row.Model, row.RequestCount, row.SuccessCount, row.FailureCount, row.TotalTokens)

	dayKey := timeutil.NormalizeStorageTime(row.BucketStart).Format("2006-01-02")
	snapshot.RequestsByDay[dayKey] += row.RequestCount
	snapshot.TokensByDay[dayKey] += row.TotalTokens
}

// applyUsageOverviewStatToSnapshotTotals 复用 hourly/daily stats 的 API 和 model 维度累计逻辑。
func applyUsageOverviewStatToSnapshotTotals(snapshot *dto.StatisticsSnapshot, apiGroupKey, model string, requestCount, successCount, failureCount, totalTokens int64) {
	apiKey := normalizeUsageOverviewDimension(apiGroupKey)
	modelName := normalizeUsageOverviewDimension(model)
	apiSnapshot := snapshot.APIs[apiKey]
	if apiSnapshot.Models == nil {
		apiSnapshot.Models = map[string]dto.ModelSnapshot{}
	}
	modelSnapshot := apiSnapshot.Models[modelName]
	modelSnapshot.TotalRequests += requestCount
	modelSnapshot.TotalTokens += totalTokens
	modelSnapshot.SuccessCount += successCount
	modelSnapshot.FailureCount += failureCount
	apiSnapshot.TotalRequests += requestCount
	apiSnapshot.TotalTokens += totalTokens
	apiSnapshot.SuccessCount += successCount
	apiSnapshot.FailureCount += failureCount
	snapshot.TotalRequests += requestCount
	snapshot.TotalTokens += totalTokens
	snapshot.SuccessCount += successCount
	snapshot.FailureCount += failureCount
	apiSnapshot.Models[modelName] = modelSnapshot
	snapshot.APIs[apiKey] = apiSnapshot
}

// applyUsageOverviewStatToSeries 同时维护总序列和按模型拆分序列，并即时刷新 RPM/TPM。
func applyUsageOverviewStatToSeries(series *dto.UsageOverviewSeriesRecord, model string, requestCount, inputTokens, outputTokens, cachedTokens, reasoningTokens, totalTokens int64, cost float64, bucketKey string, bucketMinutes int64) {
	series.Requests[bucketKey] += requestCount
	series.Tokens[bucketKey] += totalTokens
	series.Cost[bucketKey] += cost
	series.InputTokens[bucketKey] += inputTokens
	series.OutputTokens[bucketKey] += outputTokens
	series.CachedTokens[bucketKey] += cachedTokens
	series.ReasoningTokens[bucketKey] += reasoningTokens
	series.RPM[bucketKey] = float64(series.Requests[bucketKey]) / float64(bucketMinutes)
	series.TPM[bucketKey] = float64(series.Tokens[bucketKey]) / float64(bucketMinutes)

	modelName := normalizeUsageOverviewDimension(model)
	modelSeries := series.Models[modelName]
	if modelSeries.Requests == nil {
		modelSeries = newUsageOverviewSeriesRecord()
	}
	modelSeries.Requests[bucketKey] += requestCount
	modelSeries.Tokens[bucketKey] += totalTokens
	modelSeries.Cost[bucketKey] += cost
	modelSeries.InputTokens[bucketKey] += inputTokens
	modelSeries.OutputTokens[bucketKey] += outputTokens
	modelSeries.CachedTokens[bucketKey] += cachedTokens
	modelSeries.ReasoningTokens[bucketKey] += reasoningTokens
	modelSeries.RPM[bucketKey] = float64(modelSeries.Requests[bucketKey]) / float64(bucketMinutes)
	modelSeries.TPM[bucketKey] = float64(modelSeries.Tokens[bucketKey]) / float64(bucketMinutes)
	series.Models[modelName] = modelSeries
}

// applyUsageOverviewHealthStatsToOverview 用完整 health bucket 读 stats，边界 bucket 复用主查询已加载的事件。
func applyUsageOverviewHealthStatsToOverview(db *gorm.DB, overview *dto.UsageOverviewRecord, filter dto.UsageQueryFilter, boundaryEvents []entities.UsageEvent) error {
	spanSeconds := overview.Health.BucketSeconds
	span := time.Duration(spanSeconds) * time.Second
	// health grid 有自己的展示窗口，但统计不能越过用户查询窗口。
	exactStart, exactEnd := usageOverviewHealthExactWindow(overview.Health, filter)
	if !exactStart.Before(exactEnd) {
		return nil
	}

	// 完整 health bucket 走 health stats，边界 bucket 复用主边界事件。
	fullStart, fullEnd := usageOverviewFullHealthWindow(exactStart, exactEnd, span)
	if fullStart.Before(fullEnd) {
		query := db.Model(&entities.UsageOverviewHealthStat{}).
			Where("bucket_start >= ? AND bucket_start < ? AND span_seconds = ?", timeutil.FormatStorageTime(fullStart), timeutil.FormatStorageTime(fullEnd), spanSeconds)
		if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
			query = query.Where("api_group_key = ?", apiGroupKey)
		}
		var rows []entities.UsageOverviewHealthStat
		if err := query.Find(&rows).Error; err != nil {
			return fmt.Errorf("load usage overview health stats: %w", err)
		}
		for _, row := range rows {
			applyUsageOverviewHealthCountsToOverview(overview, timeutil.NormalizeStorageTime(row.BucketStart).Add(span/2), row.SuccessCount, row.FailureCount)
		}
	}

	// 已被完整 health bucket 覆盖的事件不能再次累计，否则会和 health stats 重复。
	for _, event := range boundaryEvents {
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		if timestamp.Before(exactStart) || !timestamp.Before(exactEnd) {
			continue
		}
		if fullStart.Before(fullEnd) && !timestamp.Before(fullStart) && timestamp.Before(fullEnd) {
			continue
		}
		updateUsageOverviewHealthBlock(overview.Health.BlockDetails, event)
		if event.Failed {
			overview.Health.TotalFailure++
		} else {
			overview.Health.TotalSuccess++
		}
	}
	return nil
}

// usageOverviewHealthExactWindow 返回 health grid 和查询条件相交后的精确统计窗口。
func usageOverviewHealthExactWindow(health dto.UsageOverviewHealthRecord, filter dto.UsageQueryFilter) (time.Time, time.Time) {
	exactStart := health.WindowStart
	exactEnd := health.WindowEnd
	if filter.StartTime != nil {
		filterStart := timeutil.NormalizeStorageTime(*filter.StartTime)
		if filterStart.After(exactStart) {
			exactStart = filterStart
		}
	}
	if filter.EndTime != nil {
		filterEnd := timeutil.NormalizeStorageTime(*filter.EndTime)
		if filterEnd.Before(exactEnd) {
			exactEnd = filterEnd
		}
	}
	return exactStart, exactEnd
}

// usageOverviewFullHealthWindow 返回可完全由 health stats 覆盖的半开 bucket 窗口。
func usageOverviewFullHealthWindow(exactStart, exactEnd time.Time, span time.Duration) (time.Time, time.Time) {
	fullStart := exactStart.Truncate(span)
	if fullStart.Before(exactStart) {
		fullStart = fullStart.Add(span)
	}
	fullEnd := exactEnd.Truncate(span)
	if fullEnd.Before(fullStart) {
		fullEnd = fullStart
	}
	return fullStart, fullEnd
}

// applyUsageOverviewHealthCountsToOverview 把单个 health stats bucket 写入展示格和总计。
func applyUsageOverviewHealthCountsToOverview(overview *dto.UsageOverviewRecord, timestamp time.Time, successCount, failureCount int64) {
	index := usageOverviewHealthBlockIndex(overview.Health.BlockDetails, timestamp)
	if index < 0 {
		return
	}
	block := &overview.Health.BlockDetails[index]
	block.Success += successCount
	block.Failure += failureCount
	if total := block.Success + block.Failure; total > 0 {
		block.Rate = float64(block.Success) / float64(total)
	}
	overview.Health.TotalSuccess += successCount
	overview.Health.TotalFailure += failureCount
}

// usageOverviewHealthBlockIndex 用桶中心点定位 health stat 应落入的展示格子。
func usageOverviewHealthBlockIndex(blocks []dto.UsageOverviewHealthBlockRecord, timestamp time.Time) int {
	for index := range blocks {
		block := blocks[index]
		if timestamp.Before(block.StartTime) || !timestamp.Before(block.EndTime) {
			continue
		}
		return index
	}
	return -1
}

// usageOverviewStatRequiresPricing 判断 stats row 是否需要价格表才能给出可信 cost。
func usageOverviewStatRequiresPricing(inputTokens, outputTokens, cachedTokens int64) bool {
	return inputTokens > 0 || outputTokens > 0 || cachedTokens > 0
}

// calculateUsageOverviewStatCost 按当前价格表计算聚合 row 成本，不读取历史价格快照。
func calculateUsageOverviewStatCost(inputTokens, outputTokens, cachedTokens int64, pricing entities.ModelPriceSetting) float64 {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	// cached_tokens 已单独计价，prompt 费用只计算非缓存输入 token。
	promptTokens := inputTokens - cachedTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	return (float64(promptTokens)/1_000_000.0)*pricing.PromptPricePer1M +
		(float64(outputTokens)/1_000_000.0)*pricing.CompletionPricePer1M +
		(float64(cachedTokens)/1_000_000.0)*pricing.CachePricePer1M
}

// Overview 第二步：按时间窗口读事件，再交给内存汇总。
func loadUsageOverviewEventsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) ([]entities.UsageEvent, error) {
	query := applyUsageOverviewQuery(db.Model(&entities.UsageEvent{}), filter).Select(usageEventProjectionColumns).Order("timestamp asc")

	var rows []usageEventProjection
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}
	events := make([]entities.UsageEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, usageEventProjectionToEntity(row))
	}
	return events, nil
}

// buildUsageSnapshotFromEvents 用原始事件构建旧 snapshot 响应，保留详情列表能力。
func buildUsageSnapshotFromEvents(events []entities.UsageEvent) *dto.StatisticsSnapshot {
	// Snapshot 仍按原始事件聚合，因为 Request Details 需要逐条事件明细。
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

// applyUsageEventToSnapshot 把单条 usage_event 累计到旧 snapshot 的 API/model/day/hour 结构。
func applyUsageEventToSnapshot(snapshot *dto.StatisticsSnapshot, event entities.UsageEvent, includeDetails bool) {
	// API key 和 model 维度都需要统一 unknown 兜底，否则空值会生成不可读 map key。
	apiKey := normalizeUsageOverviewDimension(event.APIGroupKey)
	modelName := normalizeUsageOverviewDimension(event.Model)

	apiSnapshot := snapshot.APIs[apiKey]
	if apiSnapshot.Models == nil {
		apiSnapshot.Models = map[string]dto.ModelSnapshot{}
	}

	modelSnapshot := apiSnapshot.Models[modelName]
	// Overview 不需要 Details，只有旧 Snapshot 页面才保留逐条请求详情。
	if includeDetails {
		detail := dto.RequestDetail{
			Timestamp: timeutil.NormalizeStorageTime(event.Timestamp),
			LatencyMS: event.LatencyMS,
			Source:    strings.TrimSpace(event.Source),
			AuthIndex: strings.TrimSpace(event.AuthIndex),
			Failed:    event.Failed,
			Tokens: dto.TokenStats{
				InputTokens:         event.InputTokens,
				OutputTokens:        event.OutputTokens,
				ReasoningTokens:     event.ReasoningTokens,
				CachedTokens:        event.CachedTokens,
				CacheReadTokens:     event.CacheReadTokens,
				CacheCreationTokens: event.CacheCreationTokens,
				TotalTokens:         event.TotalTokens,
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

// finalizeUsageSnapshot 做 snapshot 后处理，目前只在带 Details 时稳定详情排序。
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

// newUsageOverviewSeriesRecord 初始化 Overview 趋势序列中的所有指标 map。
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

// applyUsageEventToOverviewSeries 把单条事件写入总序列和 model 子序列。
func applyUsageEventToOverviewSeries(series *dto.UsageOverviewSeriesRecord, event entities.UsageEvent, cost float64, bucketKey string, bucketMinutes int64) {
	// 总序列按 bucket 累计请求、token、成本，并同步刷新 RPM/TPM。
	series.Requests[bucketKey]++
	series.Tokens[bucketKey] += event.TotalTokens
	series.Cost[bucketKey] += cost
	series.InputTokens[bucketKey] += event.InputTokens
	series.OutputTokens[bucketKey] += event.OutputTokens
	series.CachedTokens[bucketKey] += event.CachedTokens
	series.ReasoningTokens[bucketKey] += event.ReasoningTokens
	series.RPM[bucketKey] = float64(series.Requests[bucketKey]) / float64(bucketMinutes)
	series.TPM[bucketKey] = float64(series.Tokens[bucketKey]) / float64(bucketMinutes)

	// model 子序列结构和总序列一致，方便前端按模型切换同一套指标。
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

// usageEventRequiresPricing 判断单条事件是否需要价格表才能给出可信 cost。
func usageEventRequiresPricing(event entities.UsageEvent) bool {
	return event.InputTokens > 0 || event.OutputTokens > 0 || event.CachedTokens > 0
}

// applyUsageEventToOverview 把边界 raw event 合并进 Overview，语义必须和 stats row 合并保持一致。
func applyUsageEventToOverview(overview *dto.UsageOverviewRecord, event entities.UsageEvent, bucketByDay bool, latestHourlyStart *time.Time, pricingByModel map[string]entities.ModelPriceSetting) {
	overview.Summary.CachedTokens += event.CachedTokens
	overview.Summary.ReasoningTokens += event.ReasoningTokens
	if event.Failed {
		overview.Health.TotalFailure++
	} else {
		overview.Health.TotalSuccess++
	}
	// 边界事件也按当前价格表计算 cost；缺价格且有计费 token 时标记 cost 不完整。
	pricing, ok := pricingByModel[strings.TrimSpace(event.Model)]
	if !ok && usageEventRequiresPricing(event) {
		overview.Summary.CostAvailable = false
	}
	cost := calculateUsageEventCost(event, pricing)
	overview.Summary.TotalCost += cost

	// 主序列使用页面当前粒度，hourly/daily 辅助序列固定按各自粒度累计。
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

// finalizeUsageOverview 从累计后的 snapshot/health 数据反推 summary 派生指标。
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

// normalizeUsageOverviewDimension 统一 Overview/Snapshot 空维度的展示 key。
func normalizeUsageOverviewDimension(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

// loadPriceSettingsByModel 把当前价格配置转成按 model 查找的 map。
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

// calculateUsageEventCost 按当前价格表计算单条事件成本。
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
	// cached_tokens 已单独计价，prompt 费用只计算非缓存输入 token。
	promptTokens := inputTokens - cachedTokens
	if promptTokens < 0 {
		promptTokens = 0
	}
	return (float64(promptTokens)/1_000_000.0)*pricing.PromptPricePer1M +
		(float64(completionTokens)/1_000_000.0)*pricing.CompletionPricePer1M +
		(float64(cachedTokens)/1_000_000.0)*pricing.CachePricePer1M
}

const usageOverviewDailyBucketThresholdMinutes int64 = 7 * 24 * 60

// computeWindowMinutes 计算 Overview 窗口分钟数，非整分钟向上取整。
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

// shouldBucketUsageOverviewByDay 决定主 series 使用小时桶还是天桶。
func shouldBucketUsageOverviewByDay(filter dto.UsageQueryFilter, windowMinutes int64) bool {
	if filter.Range == "all" || filter.Range == "7d" {
		return true
	}
	return windowMinutes >= usageOverviewDailyBucketThresholdMinutes
}

// usageOverviewBucket 返回序列 bucket key 以及该 bucket 对应的分钟数。
func usageOverviewBucket(timestamp time.Time, byDay bool) (string, int64) {
	if byDay {
		return timeutil.NormalizeStorageTime(timestamp).Format("2006-01-02"), 24 * 60
	}
	return timeutil.FormatStorageTime(timeutil.NormalizeStorageTime(timestamp).Truncate(time.Hour)), 60
}

// latestHourlySeriesStart 返回长窗口里 hourly_series 只保留最近 24 小时的起点。
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

// buildUsageOverviewHealth 初始化 service health 网格，不在这里写入任何统计值。
func buildUsageOverviewHealth(filter dto.UsageQueryFilter) dto.UsageOverviewHealthRecord {
	rows := usageOverviewHealthRows
	columns, span := usageOverviewHealthGrid(filter)
	totalBlocks := rows * columns
	windowStart, windowEnd := usageOverviewHealthWindow(filter, totalBlocks, span)
	// 每个 block 先标记 Rate=-1，表示这个时间桶暂无请求样本。
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

// usageOverviewHealthGrid 根据 range 选择 health bucket 粒度。
func usageOverviewHealthGrid(filter dto.UsageQueryFilter) (int, time.Duration) {
	if isUsageOverviewShortHealthRange(filter.Range) {
		return usageOverviewHealthDefaultColumns, usageOverviewHealthPresetSpan
	}
	return usageOverviewHealthDefaultColumns, usageOverviewHealthDefaultSpan
}

// isUsageOverviewShortHealthRange 判断 health grid 是否使用 24h 专用细粒度窗口。
func isUsageOverviewShortHealthRange(value string) bool {
	switch value {
	case "4h", "8h", "12h", "24h", "today", "yesterday":
		return true
	default:
		return false
	}
}

// usageOverviewHealthWindow 返回 health grid 的展示窗口，可能和查询窗口不同。
func usageOverviewHealthWindow(filter dto.UsageQueryFilter, totalBlocks int, span time.Duration) (time.Time, time.Time) {
	end := timeutil.NormalizeStorageTime(time.Now())
	if filter.EndTime != nil {
		end = timeutil.NormalizeStorageTime(*filter.EndTime)
	}
	if isUsageOverviewShortHealthRange(filter.Range) {
		return end.Add(-usageOverviewHealthPresetWindow), end
	}
	// 长窗口按固定 15 分钟桶对齐到下一个 bucket 边界，保证网格列宽稳定。
	currentBucketStart := end.Truncate(span)
	windowEnd := currentBucketStart.Add(span)
	return windowEnd.Add(-time.Duration(totalBlocks) * span), windowEnd
}

// updateUsageOverviewHealthBlock 把单条事件落到对应 health block 并刷新成功率。
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
