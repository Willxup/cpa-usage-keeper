package repository

import (
	"database/sql"
	"fmt"
	"math"
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
const usageEventProjectionColumns = "id, api_group_key, provider, auth_type, request_id, model, model_alias, reasoning_effort, service_tier, response_service_tier, executor_type, endpoint, timestamp, source, auth_index, failed, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens, total_tokens"
const analysisLatencyMaxDisplayPoints = 2500

// usageOverviewRawEventProjectionColumns 是 Overview 边界补偿和 realtime DB 兜底的最小事件投影。
const usageOverviewRawEventProjectionColumns = "api_group_key, provider, auth_type, model, model_alias, timestamp, source, auth_index, failed, generate, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens, total_tokens"

// usageEventProjection 是 usage_events 轻量投影，专门承接 select columns 的查询结果。
type usageEventProjection struct {
	ID                  int64
	APIGroupKey         string
	Provider            string
	AuthType            string
	RequestID           string
	Model               string
	ModelAlias          *string `gorm:"column:model_alias"`
	ReasoningEffort     string
	ServiceTier         string
	ResponseServiceTier string
	ExecutorType        string
	Endpoint            string
	Timestamp           time.Time
	Source              string
	AuthIndex           string
	Failed              bool
	Generate            *bool
	LatencyMS           int64
	TTFTMS              *int64 `gorm:"column:ttft_ms"`
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
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

	rows, err := loadUsageEventRecordsForQuery(db, query)
	if err != nil {
		return nil, err
	}
	totalPages := 0
	if totalCount > 0 {
		totalPages = int((totalCount + int64(pageSize) - 1) / int64(pageSize))
	}
	return &dto.UsageEventsPageRecord{Events: rows, Models: modelOptions, TotalCount: totalCount, Page: page, PageSize: pageSize, TotalPages: totalPages}, nil
}

// ExportUsageEventsWithFilter 使用 Request Event Log 相同筛选，但不应用分页。
func ExportUsageEventsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) ([]dto.UsageEventRecord, error) {
	rows := []dto.UsageEventRecord{}
	if err := StreamUsageEventsWithFilter(db, filter, func(row dto.UsageEventRecord) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

// StreamUsageEventsWithFilter 使用 Request Event Log 相同筛选逐行导出，不应用分页。
func StreamUsageEventsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, emit func(dto.UsageEventRecord) error) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	query := applyUsageEventListQuery(db.Model(&entities.UsageEvent{}), filter)
	query = query.Select(usageEventProjectionColumns).Order("timestamp DESC, id DESC")
	return streamUsageEventRecordsForQuery(db, query, emit)
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

	// 第二步：只按 model 索引去重排序；空白值由内存兜底过滤，避免给索引扫描追加无意义条件。
	var values []string
	if err := query.Select("DISTINCT model").Order("model ASC").Pluck("model", &values).Error; err != nil {
		return nil, fmt.Errorf("load usage event model filter options: %w", err)
	}
	models := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		models = append(models, value)
	}
	return models, nil
}

// queryUsageEvents 统一 usage_events 的 GORM model 入口，方便后续追加通用 scope。
func queryUsageEvents(db *gorm.DB) *gorm.DB {
	return db.Model(&entities.UsageEvent{})
}

func FindUsageEventRequestIDByID(db *gorm.DB, id int64) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database is nil")
	}
	if id <= 0 {
		return "", gorm.ErrRecordNotFound
	}
	var event entities.UsageEvent
	if err := db.Select("request_id").Where("id = ?", id).First(&event).Error; err != nil {
		return "", err
	}
	return strings.TrimSpace(event.RequestID), nil
}

func loadUsageEventRecordsForQuery(db *gorm.DB, query *gorm.DB) ([]dto.UsageEventRecord, error) {
	var events []usageEventProjection
	if err := query.Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load usage events: %w", err)
	}
	costResolver, err := newUsageCostResolverForDB(db)
	if err != nil {
		return nil, fmt.Errorf("load usage event pricing settings: %w", err)
	}

	rows := make([]dto.UsageEventRecord, 0, len(events))
	for _, event := range events {
		record := usageEventProjectionToRecord(event)
		// Request Events cost 只在响应阶段按当前价格配置计算，不回写 usage_events。
		record.CostUSD, record.CostAvailable, record.PricingStyle = usageEventRecordCost(record, costResolver)
		rows = append(rows, record)
	}
	return rows, nil
}

func streamUsageEventRecordsForQuery(db *gorm.DB, query *gorm.DB, emit func(dto.UsageEventRecord) error) error {
	if emit == nil {
		return fmt.Errorf("usage event stream callback is nil")
	}
	costResolver, err := newUsageCostResolverForDB(db)
	if err != nil {
		return fmt.Errorf("load usage event pricing settings: %w", err)
	}
	rows, err := query.Rows()
	if err != nil {
		return fmt.Errorf("load usage events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var event usageEventProjection
		if err := db.ScanRows(rows, &event); err != nil {
			return fmt.Errorf("scan usage event: %w", err)
		}
		record := usageEventProjectionToRecord(event)
		// Request Events cost 只在响应阶段按当前价格配置计算，不回写 usage_events。
		record.CostUSD, record.CostAvailable, record.PricingStyle = usageEventRecordCost(record, costResolver)
		if err := emit(record); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate usage events: %w", err)
	}
	return nil
}

// usageEventProjectionToRecord 把数据库投影转换成 Request Event Log 的外部 DTO。
func usageEventProjectionToRecord(event usageEventProjection) dto.UsageEventRecord {
	// 对前端展示字段统一 trim，避免历史脏数据影响筛选和展示一致性。
	return dto.UsageEventRecord{
		ID:          event.ID,
		Timestamp:   timeutil.NormalizeStorageTime(event.Timestamp),
		APIGroupKey: strings.TrimSpace(event.APIGroupKey),
		Model:       strings.TrimSpace(event.Model),
		ModelAlias: func() string {
			if event.ModelAlias == nil {
				return ""
			}
			return strings.TrimSpace(*event.ModelAlias)
		}(),
		ReasoningEffort:     strings.TrimSpace(event.ReasoningEffort),
		ServiceTier:         strings.TrimSpace(event.ServiceTier),
		ResponseServiceTier: strings.TrimSpace(event.ResponseServiceTier),
		ExecutorType:        strings.TrimSpace(event.ExecutorType),
		Endpoint:            strings.TrimSpace(event.Endpoint),
		AuthType:            strings.TrimSpace(event.AuthType),
		RequestID:           strings.TrimSpace(event.RequestID),
		Provider:            strings.TrimSpace(event.Provider),
		Source:              strings.TrimSpace(event.Source),
		AuthIndex:           strings.TrimSpace(event.AuthIndex),
		Failed:              event.Failed,
		LatencyMS:           event.LatencyMS,
		TTFTMS:              event.TTFTMS,
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         event.TotalTokens,
	}
}

func usageEventRecordCost(record dto.UsageEventRecord, costResolver *UsageCostResolver) (float64, bool, string) {
	result := costResolver.Calculate(UsageCostSubject{
		Model:        record.Model,
		ModelAlias:   record.ModelAlias,
		ServiceTier:  record.ServiceTier,
		ExecutorType: record.ExecutorType,
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         record.InputTokens,
			OutputTokens:        record.OutputTokens,
			CacheReadTokens:     record.CacheReadTokens,
			CacheCreationTokens: record.CacheCreationTokens,
		},
	})
	return result.Cost.TotalCostUSD, result.Available, result.PricingStyle
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
		ModelAlias:          event.ModelAlias,
		ReasoningEffort:     event.ReasoningEffort,
		ServiceTier:         event.ServiceTier,
		ResponseServiceTier: event.ResponseServiceTier,
		ExecutorType:        event.ExecutorType,
		Endpoint:            event.Endpoint,
		Timestamp:           event.Timestamp,
		Source:              event.Source,
		AuthIndex:           event.AuthIndex,
		Failed:              event.Failed,
		Generate:            event.Generate,
		LatencyMS:           event.LatencyMS,
		TTFTMS:              event.TTFTMS,
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         event.TotalTokens,
	}
}

// applyUsageQueryWindow 给 usage 查询追加时间过滤；Custom 使用半开区间避免带入下一时段边界。
func applyUsageQueryWindow(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB {
	// 查询参数和落库 timestamp 使用同一格式，避免 SQLite TEXT 范围比较失真。
	if filter.StartTime != nil {
		query = query.Where("timestamp >= ?", timeutil.FormatStorageTime(*filter.StartTime))
	}
	if filter.EndTime != nil {
		operator := "timestamp <= ?"
		if filter.EndExclusive {
			operator = "timestamp < ?"
		}
		query = query.Where(operator, timeutil.FormatStorageTime(*filter.EndTime))
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

// Request Event Log 列表第一步：在时间窗口上叠加 model/auth_index/result。
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

func BuildAnalysisWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (*dto.AnalysisRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		return nil, fmt.Errorf("analysis requires start_time and end_time")
	}
	costResolver, err := newUsageCostResolverForDB(db)
	if err != nil {
		return nil, err
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
		CostBreakdown: dto.AnalysisCostBreakdownRecord{
			CostAvailable: true,
		},
	}
	latencyDiagnostics, err := buildAnalysisLatencyDiagnosticsWithFilter(db, filter)
	if err != nil {
		return nil, err
	}
	record.LatencyDiagnostics = latencyDiagnostics

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
		applyAnalysisDailyAndBoundaryHourlyRows(record, dailyRows, dailyIdentityLookup, hourlyRows, hourlyIdentityLookup, costResolver)
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
	applyAnalysisHourlyRows(record, rows, identityLookup, costResolver)
	fillAnalysisFullDayHourlyBuckets(record, filter)
	return record, nil
}

func analysisHourlyStatsEnd(filter dto.UsageQueryFilter, fullEnd time.Time) time.Time {
	if filter.StartTime == nil || filter.EndTime == nil {
		return fullEnd
	}
	if timeutil.IsUsageRollingHourRange(filter.Range) || timeutil.IsUsageRollingDayRange(filter.Range) {
		if timeutil.NormalizeStorageTime(*filter.EndTime).After(fullEnd) {
			return fullEnd.Add(time.Hour)
		}
		return fullEnd
	}
	switch filter.Range {
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

func buildAnalysisLatencyDiagnosticsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (dto.AnalysisLatencyDiagnosticsRecord, error) {
	empty := emptyAnalysisLatencyDiagnosticsRecord()
	// 延迟诊断只统计要求实际生成的成功请求；TTFT/Latency 有效性仍在内存过滤，避免依赖未索引列。
	query := db.Model(&entities.UsageEvent{}).
		Select("latency_ms, ttft_ms").
		Where("failed = ?", false).
		Where("generate = ?", true)
	query = applyUsageAnalysisTabQuery(query, filter)

	rows, err := query.Rows()
	if err != nil {
		if isMissingUsageEventsTableError(err) {
			return empty, nil
		}
		return empty, fmt.Errorf("load analysis latency diagnostics: %w", err)
	}
	defer rows.Close()

	ttftValues := []int64{}
	latencyValues := []int64{}
	for rows.Next() {
		var latencyMS int64
		var ttftMS sql.NullInt64
		if err := rows.Scan(&latencyMS, &ttftMS); err != nil {
			return empty, fmt.Errorf("scan analysis latency diagnostics: %w", err)
		}
		if !ttftMS.Valid || ttftMS.Int64 <= 0 || latencyMS <= 0 {
			continue
		}
		// 保留原始 int64 值，避免为毫秒字段引入额外 int32 转换。
		ttftValues = append(ttftValues, ttftMS.Int64)
		latencyValues = append(latencyValues, latencyMS)
	}
	if err := rows.Err(); err != nil {
		return empty, fmt.Errorf("iterate analysis latency diagnostics: %w", err)
	}
	return buildAnalysisLatencyDiagnostics(ttftValues, latencyValues), nil
}

func emptyAnalysisLatencyDiagnosticsRecord() dto.AnalysisLatencyDiagnosticsRecord {
	return dto.AnalysisLatencyDiagnosticsRecord{
		Points:  []dto.AnalysisLatencyPointRecord{},
		Density: []dto.AnalysisLatencyDensityCellRecord{},
	}
}

func isMissingUsageEventsTableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "usage_events") && (strings.Contains(message, "no such table") || strings.Contains(message, "doesn't exist"))
}

func buildAnalysisLatencyDiagnostics(ttftValues, latencyValues []int64) dto.AnalysisLatencyDiagnosticsRecord {
	result := emptyAnalysisLatencyDiagnosticsRecord()
	if len(ttftValues) == 0 {
		return result
	}

	for index, ttft := range ttftValues {
		latency := latencyValues[index]
		if ttft > result.MaxTTFTMS {
			result.MaxTTFTMS = ttft
		}
		if latency > result.MaxLatencyMS {
			result.MaxLatencyMS = latency
		}
	}

	// p95 基于完整样本计算；前端散点只做确定性抽样，避免浏览器绘制过多点。
	result.TotalPoints = int64(len(ttftValues))
	result.P95TTFTMS = analysisNearestRankPercentile(ttftValues, 0.95)
	result.P95LatencyMS = analysisNearestRankPercentile(latencyValues, 0.95)
	result.Points, result.Sampled = sampleAnalysisLatencyPoints(ttftValues, latencyValues)
	return result
}

func analysisNearestRankPercentile(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sortedValues := append([]int64(nil), values...)
	sort.Slice(sortedValues, func(i, j int) bool { return sortedValues[i] < sortedValues[j] })
	index := int(math.Ceil(percentile*float64(len(sortedValues)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedValues) {
		index = len(sortedValues) - 1
	}
	return sortedValues[index]
}

func sampleAnalysisLatencyPoints(ttftValues, latencyValues []int64) ([]dto.AnalysisLatencyPointRecord, bool) {
	if len(ttftValues) <= analysisLatencyMaxDisplayPoints {
		points := make([]dto.AnalysisLatencyPointRecord, 0, len(ttftValues))
		for index, ttft := range ttftValues {
			points = append(points, dto.AnalysisLatencyPointRecord{TTFTMS: ttft, LatencyMS: latencyValues[index]})
		}
		return points, false
	}
	points := make([]dto.AnalysisLatencyPointRecord, 0, analysisLatencyMaxDisplayPoints)
	for index := 0; index < analysisLatencyMaxDisplayPoints; index++ {
		sourceIndex := int(math.Floor(float64(index) * float64(len(ttftValues)-1) / float64(analysisLatencyMaxDisplayPoints-1)))
		points = append(points, dto.AnalysisLatencyPointRecord{TTFTMS: ttftValues[sourceIndex], LatencyMS: latencyValues[sourceIndex]})
	}
	return points, true
}

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

func applyAnalysisHourlyRows(record *dto.AnalysisRecord, rows []entities.UsageOverviewHourlyStat, identityLookup analysisIdentityLookup, costResolver *UsageCostResolver) {
	bucketTotals := map[time.Time]*dto.AnalysisTokenUsageBucketRecord{}
	apiTotals := map[string]*dto.AnalysisCompositionRecord{}
	modelTotals := map[string]*dto.AnalysisCompositionRecord{}
	authFileTotals := map[string]*dto.AnalysisCompositionRecord{}
	aiProviderTotals := map[string]*dto.AnalysisCompositionRecord{}
	heatmapTotals := map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord{}
	for _, row := range rows {
		bucket := timeutil.NormalizeStorageTime(row.BucketStart).Truncate(time.Hour)
		cost, costAvailable := analysisRowCost(row.Model, row.ModelAlias, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, costResolver)
		applyAnalysisRow(record, bucketTotals, apiTotals, modelTotals, heatmapTotals, bucket, row.APIGroupKey, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
		applyAnalysisIdentityComposition(identityLookup, authFileTotals, aiProviderTotals, row.AuthIndex, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
	}
	finalizeAnalysisRecord(record, bucketTotals, apiTotals, modelTotals, authFileTotals, aiProviderTotals, heatmapTotals)
}

func applyAnalysisDailyAndBoundaryHourlyRows(record *dto.AnalysisRecord, dailyRows []entities.UsageOverviewDailyStat, dailyIdentityLookup analysisIdentityLookup, hourlyRows []entities.UsageOverviewHourlyStat, hourlyIdentityLookup analysisIdentityLookup, costResolver *UsageCostResolver) {
	bucketTotals := map[time.Time]*dto.AnalysisTokenUsageBucketRecord{}
	apiTotals := map[string]*dto.AnalysisCompositionRecord{}
	modelTotals := map[string]*dto.AnalysisCompositionRecord{}
	authFileTotals := map[string]*dto.AnalysisCompositionRecord{}
	aiProviderTotals := map[string]*dto.AnalysisCompositionRecord{}
	heatmapTotals := map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord{}
	for _, row := range dailyRows {
		bucket := timeutil.NormalizeStorageTime(row.BucketStart)
		cost, costAvailable := analysisRowCost(row.Model, row.ModelAlias, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, costResolver)
		applyAnalysisRow(record, bucketTotals, apiTotals, modelTotals, heatmapTotals, bucket, row.APIGroupKey, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
		applyAnalysisIdentityComposition(dailyIdentityLookup, authFileTotals, aiProviderTotals, row.AuthIndex, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
	}
	for _, row := range hourlyRows {
		bucketStart := timeutil.NormalizeStorageTime(row.BucketStart)
		bucket := time.Date(bucketStart.Year(), bucketStart.Month(), bucketStart.Day(), 0, 0, 0, 0, bucketStart.Location())
		cost, costAvailable := analysisRowCost(row.Model, row.ModelAlias, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, costResolver)
		applyAnalysisRow(record, bucketTotals, apiTotals, modelTotals, heatmapTotals, bucket, row.APIGroupKey, row.Model, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
		applyAnalysisIdentityComposition(hourlyIdentityLookup, authFileTotals, aiProviderTotals, row.AuthIndex, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
	}
	finalizeAnalysisRecord(record, bucketTotals, apiTotals, modelTotals, authFileTotals, aiProviderTotals, heatmapTotals)
}

func analysisRowCost(model string, modelAlias string, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int64, costResolver *UsageCostResolver) (helper.UsageTokenCostBreakdown, bool) {
	result := costResolver.Calculate(UsageCostSubject{
		Model:      model,
		ModelAlias: modelAlias,
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         inputTokens,
			OutputTokens:        outputTokens,
			CacheReadTokens:     cacheReadTokens,
			CacheCreationTokens: cacheCreationTokens,
		},
	})
	return result.Cost, result.Available
}

func applyAnalysisRow(record *dto.AnalysisRecord, bucketTotals map[time.Time]*dto.AnalysisTokenUsageBucketRecord, apiTotals, modelTotals map[string]*dto.AnalysisCompositionRecord, heatmapTotals map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord, bucket time.Time, apiGroupKey, model string, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens int64, cost helper.UsageTokenCostBreakdown, costAvailable bool) {
	apiKey := normalizeUsageOverviewDimension(apiGroupKey)
	modelName := normalizeUsageOverviewDimension(model)
	bucketTotal := bucketTotals[bucket]
	if bucketTotal == nil {
		bucketTotal = &dto.AnalysisTokenUsageBucketRecord{Bucket: bucket, CostAvailable: true}
		bucketTotals[bucket] = bucketTotal
	}
	bucketTotal.Requests += requests
	bucketTotal.InputTokens += inputTokens
	bucketTotal.OutputTokens += outputTokens
	bucketTotal.CacheReadTokens += cacheReadTokens
	bucketTotal.CacheCreationTokens += cacheCreationTokens
	bucketTotal.ReasoningTokens += reasoningTokens
	bucketTotal.TotalTokens += totalTokens
	bucketTotal.CostUSD += cost.TotalCostUSD
	if !costAvailable {
		bucketTotal.CostAvailable = false
	}

	apiTotal := apiTotals[apiKey]
	if apiTotal == nil {
		apiTotal = &dto.AnalysisCompositionRecord{Key: apiKey, CostAvailable: true}
		apiTotals[apiKey] = apiTotal
	}
	applyAnalysisCompositionTotals(apiTotal, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens, cost.TotalCostUSD, costAvailable)

	modelTotal := modelTotals[modelName]
	if modelTotal == nil {
		modelTotal = &dto.AnalysisCompositionRecord{Key: modelName, CostAvailable: true}
		modelTotals[modelName] = modelTotal
	}
	applyAnalysisCompositionTotals(modelTotal, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens, cost.TotalCostUSD, costAvailable)

	heatmapKey := analysisHeatmapKey{apiKey: apiKey, model: modelName}
	heatmapTotal := heatmapTotals[heatmapKey]
	if heatmapTotal == nil {
		heatmapTotal = &dto.AnalysisHeatmapRecord{APIKey: apiKey, Model: modelName, CostAvailable: true}
		heatmapTotals[heatmapKey] = heatmapTotal
	}
	heatmapTotal.Requests += requests
	heatmapTotal.InputTokens += inputTokens
	heatmapTotal.OutputTokens += outputTokens
	heatmapTotal.CacheReadTokens += cacheReadTokens
	heatmapTotal.CacheCreationTokens += cacheCreationTokens
	heatmapTotal.ReasoningTokens += reasoningTokens
	heatmapTotal.TotalTokens += totalTokens
	heatmapTotal.CostUSD += cost.TotalCostUSD
	if !costAvailable {
		heatmapTotal.CostAvailable = false
	}

	record.CostBreakdown.UncachedInputCostUSD += cost.UncachedInputCostUSD
	record.CostBreakdown.CacheReadCostUSD += cost.CacheReadCostUSD
	record.CostBreakdown.CacheWriteCostUSD += cost.CacheWriteCostUSD
	record.CostBreakdown.OutputCostUSD += cost.OutputCostUSD
	record.CostBreakdown.TotalCostUSD += cost.TotalCostUSD
	if !costAvailable {
		record.CostBreakdown.CostAvailable = false
	}
}

func applyAnalysisCompositionTotals(item *dto.AnalysisCompositionRecord, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens int64, costUSD float64, costAvailable bool) {
	item.Requests += requests
	item.InputTokens += inputTokens
	item.OutputTokens += outputTokens
	item.CacheReadTokens += cacheReadTokens
	item.CacheCreationTokens += cacheCreationTokens
	item.ReasoningTokens += reasoningTokens
	item.TotalTokens += totalTokens
	item.CostUSD += costUSD
	if !costAvailable {
		item.CostAvailable = false
	}
}

func applyAnalysisIdentityComposition(identityLookup analysisIdentityLookup, authFileTotals, aiProviderTotals map[string]*dto.AnalysisCompositionRecord, authIndex string, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens int64, cost helper.UsageTokenCostBreakdown, costAvailable bool) {
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return
	}
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAuthFile, authIndex); ok {
		applyAnalysisIdentityCompositionTotal(authFileTotals, identity, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens, cost.TotalCostUSD, costAvailable)
	}
	if identity, ok := identityLookup.find(entities.UsageIdentityAuthTypeAIProvider, authIndex); ok {
		applyAnalysisIdentityCompositionTotal(aiProviderTotals, identity, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens, cost.TotalCostUSD, costAvailable)
	}
}

func applyAnalysisIdentityCompositionTotal(totals map[string]*dto.AnalysisCompositionRecord, identity analysisIdentityInfo, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens int64, costUSD float64, costAvailable bool) {
	item := totals[identity.identity]
	if item == nil {
		item = &dto.AnalysisCompositionRecord{Key: identity.identity, Label: identity.label, CostAvailable: true}
		totals[identity.identity] = item
	}
	applyAnalysisCompositionTotals(item, requests, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens, totalTokens, costUSD, costAvailable)
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
			bucket = dto.AnalysisTokenUsageBucketRecord{Bucket: bucketTime, CostAvailable: true}
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
		record.ModelEfficiency = append(record.ModelEfficiency, buildAnalysisModelEfficiencyRecord(*item))
	}
	sortAnalysisComposition(record.ModelComposition)
	sort.Slice(record.ModelEfficiency, func(i, j int) bool {
		if record.ModelEfficiency[i].CostUSD == record.ModelEfficiency[j].CostUSD {
			if record.ModelEfficiency[i].TotalTokens == record.ModelEfficiency[j].TotalTokens {
				return record.ModelEfficiency[i].Model < record.ModelEfficiency[j].Model
			}
			return record.ModelEfficiency[i].TotalTokens > record.ModelEfficiency[j].TotalTokens
		}
		return record.ModelEfficiency[i].CostUSD > record.ModelEfficiency[j].CostUSD
	})
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

func buildAnalysisModelEfficiencyRecord(item dto.AnalysisCompositionRecord) dto.AnalysisModelEfficiencyRecord {
	result := dto.AnalysisModelEfficiencyRecord{
		Model:               item.Key,
		Requests:            item.Requests,
		InputTokens:         item.InputTokens,
		OutputTokens:        item.OutputTokens,
		CacheReadTokens:     item.CacheReadTokens,
		CacheCreationTokens: item.CacheCreationTokens,
		ReasoningTokens:     item.ReasoningTokens,
		TotalTokens:         item.TotalTokens,
		CostUSD:             item.CostUSD,
		CostAvailable:       item.CostAvailable,
	}
	if item.Requests > 0 {
		result.CostPerRequestUSD = item.CostUSD / float64(item.Requests)
		result.OutputTokensPerRequest = float64(item.OutputTokens) / float64(item.Requests)
	}
	if item.InputTokens > 0 {
		result.CacheReadRate = float64(item.CacheReadTokens) / float64(item.InputTokens)
	}
	return result
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
	return BuildUsageOverviewWithFilterAndRecentCache(db, filter, nil)
}

func BuildUsageOverviewWithFilterAndRecentCache(db *gorm.DB, filter dto.UsageQueryFilter, recentCache *UsageRecentEventCache) (*dto.UsageOverviewRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}

	// Overview 页面现在必须先由 API 层把 4h/8h/custom 等 range 解析成具体时间窗口。
	if filter.StartTime == nil || filter.EndTime == nil {
		return nil, fmt.Errorf("usage overview requires start_time and end_time")
	}

	// stats 表不保存价格，所有 cost 都按本次请求的 resolver 在查询阶段动态计算。
	costResolver, err := newUsageCostResolverForDB(db)
	if err != nil {
		return nil, err
	}

	overview, err := buildUsageOverviewFromStats(db, filter, costResolver, recentCache)
	if err != nil {
		return nil, err
	}
	return overview, nil
}

// BuildUsageOverviewRealtimeWithFilter 单独构建 Overview 实时运行态，避免主 Overview 查询承担短窗口 raw event 扫描。
func BuildUsageOverviewRealtimeWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (dto.UsageOverviewRealtimeRecord, error) {
	return BuildUsageOverviewRealtimeWithFilterAndRecentCache(db, filter, nil)
}

func BuildUsageOverviewRealtimeWithFilterAndRecentCache(db *gorm.DB, filter dto.UsageQueryFilter, recentCache *UsageRecentEventCache) (dto.UsageOverviewRealtimeRecord, error) {
	if db == nil {
		return dto.UsageOverviewRealtimeRecord{}, fmt.Errorf("database is nil")
	}
	costResolver, err := newUsageCostResolverForDB(db)
	if err != nil {
		return dto.UsageOverviewRealtimeRecord{}, err
	}
	return buildUsageOverviewRealtime(db, filter, costResolver, recentCache)
}

// newUsageOverviewRecord 初始化 Overview 返回结构中的 map，避免后续聚合写入 nil map。
func newUsageOverviewRecord(filter dto.UsageQueryFilter, windowMinutes int64) *dto.UsageOverviewRecord {
	return &dto.UsageOverviewRecord{
		Usage: &dto.StatisticsSnapshot{},
		Summary: dto.UsageOverviewSummaryRecord{
			WindowMinutes: windowMinutes,
			CostAvailable: true,
		},
		Series: newUsageOverviewSeriesRecord(),
		Health: buildUsageOverviewHealth(filter),
	}
}

// buildUsageOverviewFromStats 用预聚合表覆盖完整 bucket，用原始事件补偿窗口边界。
func buildUsageOverviewFromStats(db *gorm.DB, filter dto.UsageQueryFilter, costResolver *UsageCostResolver, recentCache *UsageRecentEventCache) (*dto.UsageOverviewRecord, error) {
	// queryNow 固定本次仓储查询的“当前时刻”，避免不同步骤各自 time.Now() 造成边界漂移。
	queryNow := usageOverviewQueryNow(filter)
	// currentRight 只描述范围语义：滚动范围和今天类范围要读取最新缓存，不用 end 截断。
	currentRight := usageOverviewCurrentRightBoundary(filter, queryNow)
	// effectiveFilter 只把未来结束时间压到 queryNow，避免 today/custom 当天查到未来 bucket。
	effectiveFilter := usageOverviewEffectiveFilter(filter, queryNow)

	// 先确定主序列粒度，后续 raw event 与 stats row 共用这一规则。
	windowMinutes := computeWindowMinutes(effectiveFilter)
	bucketByDay := shouldBucketUsageOverviewByDay(effectiveFilter, windowMinutes)
	overview := newUsageOverviewRecord(effectiveFilter, windowMinutes)

	// fullStart/fullEnd 是能被 hourly stats 完整覆盖的半开区间。
	fullStart, fullEnd := usageOverviewFullHourWindow(*effectiveFilter.StartTime, *effectiveFilter.EndTime)
	// 原始事件只补主统计的窄边界；Request Health 完全读取 Activity，不再扩大 raw 扫描窗口。
	rawEventWindows := usageOverviewRawEventWindows(effectiveFilter, fullStart, fullEnd, currentRight)

	// 非整点窗口的头尾不能用小时 stats，否则会把窗口外事件算进去。
	boundaryEvents, err := loadUsageOverviewRawEventWindowsWithFilter(db, effectiveFilter, rawEventWindows, recentCache)
	if err != nil {
		return nil, err
	}
	for _, event := range boundaryEvents {
		if usageOverviewEventInsideWindow(event, fullStart, fullEnd) {
			continue
		}
		applyUsageEventToOverviewSnapshot(overview.Usage, event)
		applyUsageEventToOverview(overview, event, bucketByDay, costResolver)
	}

	if fullEnd.After(fullStart) {
		// 短窗口的主序列和 snapshot 小时图必须保持小时粒度，不能因为内部包含完整天就压成 daily bucket。
		fullDayStart, fullDayEnd := usageOverviewFullDayWindow(fullStart, fullEnd)
		if !bucketByDay || !fullDayEnd.After(fullDayStart) {
			hourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, effectiveFilter, fullStart, fullEnd)
			if err != nil {
				return nil, err
			}
			for _, row := range hourlyRows {
				applyUsageOverviewHourlyStatToOverview(overview, row, bucketByDay, costResolver)
			}
		} else {
			// 长窗口中间的完整本地天用 daily stats，减少大量小时 row 累加。
			dailyRows, err := loadUsageOverviewDailyStatsWithFilter(db, effectiveFilter, fullDayStart, fullDayEnd)
			if err != nil {
				return nil, err
			}
			for _, row := range dailyRows {
				applyUsageOverviewDailyStatToOverview(overview, row, bucketByDay, costResolver)
			}

			// 完整天两侧剩余的完整小时仍走 hourly stats，避免回退到大范围事件扫描。
			for _, window := range []struct{ start, end time.Time }{{fullStart, fullDayStart}, {fullDayEnd, fullEnd}} {
				if !window.end.After(window.start) {
					continue
				}
				hourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, effectiveFilter, window.start, window.end)
				if err != nil {
					return nil, err
				}
				for _, row := range hourlyRows {
					applyUsageOverviewHourlyStatToOverview(overview, row, bucketByDay, costResolver)
				}
			}
		}
	}

	// Request Health 完整读取统一 Activity 网格，不再访问旧 Health 表或追加 raw 边界扫描。
	overview.Health, err = buildUsageOverviewHealthFromActivity(db, effectiveFilter)
	// Activity 查询失败时不能返回看似有效但为空的健康图。
	if err != nil {
		return nil, err
	}
	// PR1 保持 main 的精确筛选 totals；完整档位 Activity 只负责热力图格子。
	overview.Health.TotalSuccess = overview.Usage.SuccessCount
	// 失败总数同样复用已经按 effective filter 还原的 Overview usage 快照。
	overview.Health.TotalFailure = overview.Usage.FailureCount
	// summary 和 health 的各自累计完成后统一计算派生率。
	finalizeUsageOverview(overview)
	return overview, nil
}

// usageOverviewQueryNow 返回本次 Overview 仓储查询使用的稳定当前时间。
func usageOverviewQueryNow(filter dto.UsageQueryFilter) time.Time {
	// 测试和上层调用可以显式传 QueryNow，确保边界调度不受真实时钟影响。
	if filter.QueryNow != nil {
		return timeutil.NormalizeStorageTime(*filter.QueryNow)
	}
	// 未显式传入时使用项目时区归一化后的当前时间，保持与 API range 解析同一时区语义。
	return timeutil.NormalizeStorageTime(time.Now())
}

// usageOverviewEffectiveFilter 返回实际参与聚合的时间窗口。
func usageOverviewEffectiveFilter(filter dto.UsageQueryFilter, queryNow time.Time) dto.UsageQueryFilter {
	// 复制 filter，避免仓储内部为了压未来 end 修改调用方持有的时间指针。
	effective := filter
	// queryNow 已经是本次查询的统一当前时间，后续所有比较都围绕它展开。
	queryNow = timeutil.NormalizeStorageTime(queryNow)
	// StartTime 只做存储时区归一化，不主动移动左边界，避免改变用户选择的范围。
	if filter.StartTime != nil {
		start := timeutil.NormalizeStorageTime(*filter.StartTime)
		effective.StartTime = &start
	}
	// EndTime 为空时保留空值；调用方已有必填校验，这里只负责轻量归一化。
	if filter.EndTime == nil {
		effective.QueryNow = &queryNow
		return effective
	}
	// future end 只可能来自 today 或自定义当天结束，聚合时必须压到 queryNow。
	end := timeutil.NormalizeStorageTime(*filter.EndTime)
	if end.After(queryNow) {
		end = queryNow
	}
	// QueryNow 一起写回 filter，后续 cache 覆盖判断复用同一个稳定时刻。
	effective.EndTime = &end
	effective.QueryNow = &queryNow
	return effective
}

// usageOverviewCurrentRightBoundary 判断主查询右边界是否应该从缓存读到“现在之后已进入缓存的新事件”。
func usageOverviewCurrentRightBoundary(filter dto.UsageQueryFilter, queryNow time.Time) bool {
	// 滚动范围天然表示“截至当前”，不能被 API 层较早解析出的 end 截断。
	if _, ok := timeutil.ParseUsageRollingRange(strings.TrimSpace(filter.Range)); ok {
		return true
	}
	switch strings.TrimSpace(filter.Range) {
	case "today":
		return true
	case "custom":
		// 自定义范围只有结束时间落在 queryNow 之后时才代表当前进行中的当天查询。
		if filter.EndTime == nil {
			return false
		}
		return timeutil.NormalizeStorageTime(*filter.EndTime).After(timeutil.NormalizeStorageTime(queryNow))
	default:
		// yesterday 和历史自定义范围都必须按显式 end 截断。
		return false
	}
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
		fullStart = fullStart.AddDate(0, 0, 1)
	}
	fullEnd := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	if fullEnd.Before(fullStart) {
		fullEnd = fullStart
	}
	return fullStart, fullEnd
}

type usageOverviewRawEventWindow struct {
	// start 是需要 raw event 补偿的左边界，始终按闭区间读取。
	start time.Time
	// end 是历史/DB 查询的右边界；currentRight=true 时 cache 读取不会用它截断。
	end time.Time
	// includeEnd 只描述 DB/历史窗口是否包含 end 这一刻。
	includeEnd bool
	// currentRight 表示这是当前范围的主查询右边界，需要用 EventsSince 读取最新缓存。
	currentRight bool
}

// usageOverviewRawEventWindows 返回主 Overview 需要读取的 usage_events 窄边界，Health 完全交给 Activity。
func usageOverviewRawEventWindows(filter dto.UsageQueryFilter, fullHourStart, fullHourEnd time.Time, currentRight bool) []usageOverviewRawEventWindow {
	// Overview 必须已经解析出明确时间范围，否则无法计算边界补偿。
	if filter.StartTime == nil || filter.EndTime == nil {
		return nil
	}
	// 主查询窗口使用归一化后的存储时区，和 stats bucket 时间保持一致。
	windowStart := timeutil.NormalizeStorageTime(*filter.StartTime)
	windowEnd := timeutil.NormalizeStorageTime(*filter.EndTime)
	// 最多只包含主查询左右两个窄边界。
	windows := make([]usageOverviewRawEventWindow, 0, 2)
	// 主查询边界需要保留 includeEnd/currentRight 语义。
	windows = appendUsageOverviewRawEventBoundaryWindows(windows, windowStart, windowEnd, fullHourStart, fullHourEnd, !filter.EndExclusive, currentRight)
	// 主查询左右边界可能接触或重叠，合并后避免重复读取 raw event。
	return mergeUsageOverviewRawEventWindows(windows)
}

func appendUsageOverviewRawEventBoundaryWindows(windows []usageOverviewRawEventWindow, windowStart, windowEnd, coveredStart, coveredEnd time.Time, includeRightEnd bool, currentRightEnd bool) []usageOverviewRawEventWindow {
	// 空窗口没有补偿意义。
	if !windowStart.Before(windowEnd) {
		return windows
	}
	// coveredStart 左侧无法由 stats 完整覆盖，需要 raw event 左边界补偿。
	if windowStart.Before(coveredStart) {
		// 左边界最多补到 coveredStart；如果整个查询都在 coveredStart 左侧，则补到 windowEnd。
		leftEnd := coveredStart
		if windowEnd.Before(leftEnd) {
			leftEnd = windowEnd
		}
		// leftEnd 必须真正大于 windowStart，避免生成无意义半开窗口。
		if windowStart.Before(leftEnd) {
			windows = append(windows, usageOverviewRawEventWindow{
				start: windowStart,
				end:   leftEnd,
				// 当整个主窗口都落在左边界时，它同时也是右边界，需要继承 includeEnd。
				includeEnd: includeRightEnd && leftEnd.Equal(windowEnd),
				// 当整个当前范围都落在左边界时，它也应使用 open-ended cache。
				currentRight: currentRightEnd && leftEnd.Equal(windowEnd),
			})
		}
	}
	// coveredEnd 右侧无法由 stats 完整覆盖，需要 raw event 右边界补偿。
	if !windowEnd.Before(coveredEnd) {
		// 右边界从 coveredEnd 开始；如果 coveredEnd 在查询左侧，则从 windowStart 开始。
		rightStart := coveredEnd
		if rightStart.Before(windowStart) {
			rightStart = windowStart
		}
		// 当前范围在整点结束时仍生成零宽右边界，用 EventsSince 承接 API 解析后的新事件。
		if rightStart.Before(windowEnd) || (currentRightEnd && rightStart.Equal(windowEnd)) {
			windows = append(windows, usageOverviewRawEventWindow{start: rightStart, end: windowEnd, includeEnd: includeRightEnd, currentRight: currentRightEnd})
		}
	}
	return windows
}

func mergeUsageOverviewRawEventWindows(windows []usageOverviewRawEventWindow) []usageOverviewRawEventWindow {
	// 0/1 个窗口不需要排序和合并。
	if len(windows) < 2 {
		return windows
	}
	// 先按 start 再按 end 排序，确保后续只需要线性合并。
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].start.Equal(windows[j].start) {
			return windows[i].end.Before(windows[j].end)
		}
		return windows[i].start.Before(windows[j].start)
	})
	// 复用原切片作为结果，减少边界调度分配。
	merged := windows[:1]
	for _, window := range windows[1:] {
		// last 指向当前合并段，后续可能扩展它的 end 和语义标记。
		last := &merged[len(merged)-1]
		// 当前窗口和 last 完全不相交时，开启新的合并段。
		if window.start.After(last.end) {
			merged = append(merged, window)
			continue
		}
		// 当前窗口向右扩展 last 时，end/includeEnd/currentRight 都取更靠右窗口的语义。
		if window.end.After(last.end) {
			last.end = window.end
			last.includeEnd = window.includeEnd
			last.currentRight = window.currentRight
		} else if window.end.Equal(last.end) {
			// end 相同时，任一来源要求闭区间或当前右边界，都需要保留下来。
			last.includeEnd = last.includeEnd || window.includeEnd
			last.currentRight = last.currentRight || window.currentRight
		} else if window.currentRight {
			// 当前窗口被 last 包含但携带 currentRight 时，也不能丢掉 open-ended 语义。
			last.currentRight = true
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

func loadUsageOverviewRawEventWindowsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, windows []usageOverviewRawEventWindow, recentCache *UsageRecentEventCache) ([]entities.UsageEvent, error) {
	// 所有边界事件先汇总到一个切片，主统计和 health 统计后续复用同一批事件。
	events := make([]entities.UsageEvent, 0)
	// queryNow 来自 filter.QueryNow 或当前项目时区时间，覆盖判断只用这个稳定时刻。
	queryNow := usageOverviewQueryNow(filter)
	for _, window := range windows {
		// 缓存能覆盖该边界窗口时优先读取纯内存，避免最近边界再查 usage_events。
		if usageOverviewRecentCacheCoversWindow(recentCache, window, queryNow) {
			var cachedEvents []RecentUsageEvent
			var ok bool
			// 当前右边界使用 open-ended 读取，避免 API 解析 end 早于最新入缓存事件。
			if window.currentRight {
				cachedEvents, ok = recentCache.EventsSince(window.start, filter.APIGroupKey)
			} else {
				// 历史边界必须尊重 end/includeEnd，不能把结束后的事件算进来。
				cachedEvents, ok = recentCache.Events(window.start, window.end, window.includeEnd, filter.APIGroupKey)
			}
			// ok=false 只表示缓存对象不可用；缓存为空也会 ok=true 并返回空切片。
			if ok {
				for _, cachedEvent := range cachedEvents {
					// 下游聚合函数使用 entities.UsageEvent，这里把缓存投影转回最小实体。
					events = append(events, recentUsageEventToEntity(cachedEvent))
				}
				// 当前窗口已由缓存承接，不再访问 DB。
				continue
			}
		}
		// 缓存不存在或窗口早于 70 分钟覆盖范围时，回到原来的窄边界 DB 查询。
		windowEvents, err := loadUsageOverviewEventRangeWithFilter(db, filter, window.start, window.end, window.includeEnd)
		if err != nil {
			return nil, err
		}
		// DB 结果和缓存结果统一追加，后续按 fullStart/fullEnd 去重。
		events = append(events, windowEvents...)
	}
	return events, nil
}

// usageOverviewRecentCacheCoversWindow 判断某个边界窗口能否由最近事件缓存完整承接。
func usageOverviewRecentCacheCoversWindow(recentCache *UsageRecentEventCache, window usageOverviewRawEventWindow, queryNow time.Time) bool {
	// 没有缓存对象时只能走原有 DB 边界查询。
	if recentCache == nil {
		return false
	}
	// 窗口为空时不需要 DB；交给 cache 返回空结果，保持调用路径简单。
	if window.end.Before(window.start) || (!window.includeEnd && window.end.Equal(window.start) && !window.currentRight) {
		return true
	}
	// 覆盖起点由本次请求的 queryNow 推导，不读取 cache.now，避免请求过程中时间漂移。
	coveredStart := timeutil.NormalizeStorageTime(queryNow).Add(-recentCache.Window())
	// 只要边界窗口左端落在 70 分钟缓存内，就可以用纯缓存承接。
	return !timeutil.NormalizeStorageTime(window.start).Before(coveredStart)
}

// loadUsageOverviewEventRangeWithFilter 使用单段 timestamp 范围查询，避免 OR 影响 usage_events 时间索引。
func loadUsageOverviewEventRangeWithFilter(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time, includeEnd bool) ([]entities.UsageEvent, error) {
	if end.Before(start) || (!includeEnd && end.Equal(start)) {
		return nil, nil
	}
	// 单段范围让 SQLite 可以稳定使用 timestamp 索引，不把左右边界拼成 OR 查询。
	query := db.Model(&entities.UsageEvent{}).
		Where("timestamp >= ?", timeutil.FormatStorageTime(start)).
		Select(usageOverviewRawEventProjectionColumns).
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

// applyUsageOverviewHourlyStatToOverview 把小时 stats 同步写入 summary、snapshot 和主序列。
func applyUsageOverviewHourlyStatToOverview(overview *dto.UsageOverviewRecord, row entities.UsageOverviewHourlyStat, bucketByDay bool, costResolver *UsageCostResolver) {
	// 小时 stats 是完整小时事实，可直接累计到 snapshot totals。
	applyUsageOverviewHourlyStatToSnapshot(overview.Usage, row)
	// cost 不入 stats 表，必须在读取时按当前价格表重新计算。
	result := costResolver.Calculate(UsageCostSubject{
		Model:      row.Model,
		ModelAlias: row.ModelAlias,
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
		},
	})
	if !result.Available {
		overview.Summary.CostAvailable = false
	}
	rowCost := result.Cost.TotalCostUSD
	applyUsageOverviewStatToSummary(overview, row.RequestCount, row.InputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, rowCost)

	// 主序列按当前窗口选择小时或天粒度。
	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), bucketByDay)
	applyUsageOverviewStatToSeries(&overview.Series, row.RequestCount, row.InputTokens, row.CacheReadTokens, row.TotalTokens, rowCost, bucketKey, bucketMinutes)
}

// applyUsageOverviewDailyStatToOverview 把完整天 stats 写入长窗口 summary、snapshot 和主序列。
func applyUsageOverviewDailyStatToOverview(overview *dto.UsageOverviewRecord, row entities.UsageOverviewDailyStat, bucketByDay bool, costResolver *UsageCostResolver) {
	// 天 stats 只覆盖完整本地天，不能用于非整天边界。
	applyUsageOverviewDailyStatToSnapshot(overview.Usage, row)
	result := costResolver.Calculate(UsageCostSubject{
		Model:      row.Model,
		ModelAlias: row.ModelAlias,
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
		},
	})
	if !result.Available {
		overview.Summary.CostAvailable = false
	}
	rowCost := result.Cost.TotalCostUSD
	applyUsageOverviewStatToSummary(overview, row.RequestCount, row.InputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, rowCost)

	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(row.BucketStart), bucketByDay)
	applyUsageOverviewStatToSeries(&overview.Series, row.RequestCount, row.InputTokens, row.CacheReadTokens, row.TotalTokens, rowCost, bucketKey, bucketMinutes)
}

// applyUsageOverviewStatToSummary 写入 summary 中不在 StatisticsSnapshot 里的 token/cost 字段。
func applyUsageOverviewStatToSummary(overview *dto.UsageOverviewRecord, requestCount, inputTokens, cacheReadTokens, cacheCreationTokens, reasoningTokens int64, cost float64) {
	overview.Summary.InputTokens += inputTokens
	overview.Summary.CacheReadTokens += cacheReadTokens
	overview.Summary.CacheCreationTokens += cacheCreationTokens
	overview.Summary.ReasoningTokens += reasoningTokens
	overview.Summary.TotalCost += cost
}

// applyUsageOverviewHourlyStatToSnapshot 把小时 stats 合入 Overview 基础 usage 统计。
func applyUsageOverviewHourlyStatToSnapshot(snapshot *dto.StatisticsSnapshot, row entities.UsageOverviewHourlyStat) {
	applyUsageOverviewStatToSnapshotTotals(snapshot, row.RequestCount, row.SuccessCount, row.FailureCount, row.TotalTokens)
}

// applyUsageOverviewDailyStatToSnapshot 把天 stats 合入 Overview 基础 usage 统计。
func applyUsageOverviewDailyStatToSnapshot(snapshot *dto.StatisticsSnapshot, row entities.UsageOverviewDailyStat) {
	applyUsageOverviewStatToSnapshotTotals(snapshot, row.RequestCount, row.SuccessCount, row.FailureCount, row.TotalTokens)
}

// applyUsageOverviewStatToSnapshotTotals 复用 hourly/daily stats 的基础 totals 累计逻辑。
func applyUsageOverviewStatToSnapshotTotals(snapshot *dto.StatisticsSnapshot, requestCount, successCount, failureCount, totalTokens int64) {
	snapshot.TotalRequests += requestCount
	snapshot.TotalTokens += totalTokens
	snapshot.SuccessCount += successCount
	snapshot.FailureCount += failureCount
}

// applyUsageOverviewStatToSeries 维护主序列，并即时刷新 RPM/TPM/cache rate。
func applyUsageOverviewStatToSeries(series *dto.UsageOverviewSeriesRecord, requestCount, inputTokens, cacheReadTokens, totalTokens int64, cost float64, bucketKey string, bucketMinutes int64) {
	series.Requests[bucketKey] += requestCount
	series.Tokens[bucketKey] += totalTokens
	series.Cost[bucketKey] += cost
	series.RPM[bucketKey] = float64(series.Requests[bucketKey]) / float64(bucketMinutes)
	series.TPM[bucketKey] = float64(series.Tokens[bucketKey]) / float64(bucketMinutes)
	updateUsageOverviewSeriesCacheReadRate(series, bucketKey, inputTokens, cacheReadTokens)
}

// usageOverviewHealthBlockIndex 用 timestamp 定位 Activity 边界对应的展示格子。
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

const (
	usageOverviewRealtimeBucketCount                     = 30
	usageOverviewRealtimeDistributionMaxParticles        = 1000
	usageOverviewRealtimeDistributionDefaultParticleSize = 1
)

type usageOverviewRealtimeBucket struct {
	bucketStart         time.Time
	requests            int64
	tokens              int64
	inputTokens         int64
	cacheReadTokens     int64
	cacheCreationTokens int64
	costUSD             float64
	costAvailable       bool
	ttftSamples         []usageOverviewRealtimeResponseSample
	latencySamples      []usageOverviewRealtimeResponseSample
}

type usageOverviewRealtimeResponseSample struct {
	timestamp time.Time
	ms        int64
}

type usageOverviewRealtimeTopAccumulator struct {
	key           string
	label         string
	tokens        int64
	requests      int64
	costUSD       float64
	costAvailable bool
}

type usageOverviewRealtimeEvent struct {
	event                 entities.UsageEvent
	identityFallbackKind  RecentUsageIdentityKind
	identityFallbackLabel string
}

// buildUsageOverviewRealtime 从最近事件缓存聚合 Overview 下方实时图表；缓存对象不可用时回退到 usage_events 窄窗查询。
func buildUsageOverviewRealtime(db *gorm.DB, filter dto.UsageQueryFilter, costResolver *UsageCostResolver, recentCache *UsageRecentEventCache) (dto.UsageOverviewRealtimeRecord, error) {
	// window/span 由 15m/30m/60m 统一映射，前端所有 realtime 图共享同一窗口。
	window, span := usageOverviewRealtimeWindow(filter.RealtimeWindow)
	// 滑动聚合需要窗口左侧的少量预热 bucket，避免切换窗口时曲线从左边界重新爬坡。
	aggregationWindow := usageOverviewRealtimeAggregationWindow(window)
	aggregationBucketCount := usageOverviewRealtimeAggregationBucketCount(span, aggregationWindow)
	warmupBucketCount := usageOverviewRealtimeWarmupBucketCount(aggregationBucketCount)
	// 默认实时结束时间是当前项目时区时间，测试可以用 RealtimeEndTime 固定。
	end := timeutil.NormalizeStorageTime(time.Now())
	if filter.RealtimeEndTime != nil {
		end = timeutil.NormalizeStorageTime(*filter.RealtimeEndTime)
	}
	// realtime 只看最近窗口，不受 Overview 顶部 range 影响。
	start := end.Add(-window)
	// readStart 只用于图表平滑预热；右侧 current usage 仍从 start 开始统计。
	readStart := start.Add(-time.Duration(warmupBucketCount) * span)
	// 所有 realtime 图表共用一次缓存读取，缓存对象不存在时再回退到 DB 窄窗查询。
	events, cacheOK := loadUsageOverviewRealtimeEventsFromRecentCache(recentCache, filter, readStart, end)
	if !cacheOK {
		// 缓存对象不可用时，直接回退到 usage_events 的同窗口投影，不影响正常缓存命中语义。
		dbEvents, err := loadUsageOverviewRealtimeEventsFromDB(db, filter, readStart, end)
		if err != nil {
			return dto.UsageOverviewRealtimeRecord{}, err
		}
		events = dbEvents
	}

	// 无论数据来源是缓存还是 DB，都先创建完整 bucket 骨架，前端渲染结构保持一致。
	buckets := newUsageOverviewRealtimeBuckets(readStart, span, usageOverviewRealtimeBucketCount+warmupBucketCount)
	// 只有 current usage 的 Auth File / AI Provider 展示名需要身份表补全，隐藏预热事件不参与 Top5。
	authIndexes := collectRealtimeAuthIndexes(events, start)
	identityLookup, err := loadAnalysisIdentityLookup(db, authIndexes)
	if err != nil {
		return dto.UsageOverviewRealtimeRecord{}, err
	}
	// 四个 Top5 维度共用 accumulator，按 token 占比排序输出。
	modelUsage := map[string]*usageOverviewRealtimeTopAccumulator{}
	apiKeyUsage := map[string]*usageOverviewRealtimeTopAccumulator{}
	authFileUsage := map[string]*usageOverviewRealtimeTopAccumulator{}
	aiProviderUsage := map[string]*usageOverviewRealtimeTopAccumulator{}

	for _, realtimeEvent := range events {
		// 缓存事件已经是最小投影，这里转回 UsageEvent 复用现有 cost/token helper。
		event := realtimeEvent.event
		// bucket index 基于 realtime 窗口 start 和固定 span 计算。
		timestamp := timeutil.NormalizeStorageTime(event.Timestamp)
		index := usageOverviewRealtimeBucketIndex(timestamp, readStart, span, len(buckets))
		if index < 0 {
			continue
		}
		// visibleEvent 控制 Top5/当前占比统计范围，避免预热事件进入当前窗口语义。
		visibleEvent := !timestamp.Before(start)
		// 请求水平包含成功和失败请求。
		bucket := &buckets[index]
		bucket.requests++
		if visibleEvent {
			// current usage 的请求数同样包含成功和失败，token 后续只由成功请求累计。
			applyUsageOverviewRealtimeRequest(realtimeEvent, modelUsage, apiKeyUsage, authFileUsage, aiProviderUsage, identityLookup)
		}
		if !event.Failed && usageEventGenerateEnabled(event.Generate) && event.TTFTMS != nil && *event.TTFTMS > 0 && event.LatencyMS > 0 {
			// TTFT 和 Latency 共用同一有效请求样本，避免两张响应分布图的统计口径不一致。
			bucket.ttftSamples = append(bucket.ttftSamples, usageOverviewRealtimeResponseSample{timestamp: timestamp, ms: *event.TTFTMS})
			bucket.latencySamples = append(bucket.latencySamples, usageOverviewRealtimeResponseSample{timestamp: timestamp, ms: event.LatencyMS})
		}
		// 失败或无 token 的请求不参与 token velocity/cache/current token share。
		if event.Failed || event.TotalTokens <= 0 {
			continue
		}

		// cost 仍按本次请求的 resolver 动态计算，保持 Overview 和 Analysis 的价格口径一致。
		costResult := costResolver.CalculateEvent(event)
		cost := costResult.Cost.TotalCostUSD
		// token velocity/cache level 都从同一个 bucket accumulator 派生。
		bucket.tokens += event.TotalTokens
		bucket.inputTokens += event.InputTokens
		bucket.cacheReadTokens += event.CacheReadTokens
		bucket.cacheCreationTokens += event.CacheCreationTokens
		bucket.costUSD += cost
		if !costResult.Available {
			bucket.costAvailable = false
		}
		if visibleEvent {
			// current usage 的 token 占比只统计有 token 的成功请求。
			applyUsageOverviewRealtimeTokenUsage(realtimeEvent, cost, costResult.Available, modelUsage, apiKeyUsage, authFileUsage, aiProviderUsage, identityLookup)
		}
	}

	// 最后统一把 bucket、percentile 和 Top5 accumulator 映射成 API DTO。
	return finalizeUsageOverviewRealtime(window, span, start, end, buckets, warmupBucketCount, modelUsage, apiKeyUsage, authFileUsage, aiProviderUsage), nil
}

func usageEventGenerateEnabled(generate *bool) bool {
	return generate == nil || *generate
}

func usageOverviewRealtimeWindow(value string) (time.Duration, time.Duration) {
	switch strings.TrimSpace(value) {
	case "15m":
		return 15 * time.Minute, 30 * time.Second
	case "30m":
		return 30 * time.Minute, time.Minute
	case "60m":
		return 60 * time.Minute, 120 * time.Second
	default:
		return 15 * time.Minute, 30 * time.Second
	}
}

func usageOverviewRealtimeWindowLabel(window time.Duration) string {
	switch window {
	case 15 * time.Minute:
		return "15m"
	case 30 * time.Minute:
		return "30m"
	case 60 * time.Minute:
		return "60m"
	default:
		return "15m"
	}
}

func usageOverviewRealtimeAggregationWindow(window time.Duration) time.Duration {
	switch window {
	case 15 * time.Minute:
		return 3 * time.Minute
	case 30 * time.Minute:
		return 5 * time.Minute
	case 60 * time.Minute:
		return 10 * time.Minute
	default:
		return 3 * time.Minute
	}
}

func usageOverviewRealtimeAggregationBucketCount(span, aggregationWindow time.Duration) int {
	if span <= 0 || aggregationWindow <= 0 {
		return 1
	}
	count := int(aggregationWindow / span)
	if aggregationWindow%span != 0 {
		count++
	}
	if count < 1 {
		return 1
	}
	return count
}

func usageOverviewRealtimeWarmupBucketCount(aggregationBucketCount int) int {
	// 当前 bucket 本身也属于滑动窗口，所以只需要补前面的 N-1 个隐藏 bucket。
	if aggregationBucketCount <= 1 {
		return 0
	}
	return aggregationBucketCount - 1
}

func loadUsageOverviewRealtimeEventsFromRecentCache(recentCache *UsageRecentEventCache, filter dto.UsageQueryFilter, start, end time.Time) ([]usageOverviewRealtimeEvent, bool) {
	// realtime 的缓存对象为空时，调用方会回退到 usage_events 查询。
	if recentCache == nil {
		return nil, false
	}
	cachedEvents, ok := recentCache.Events(start, end, false, filter.APIGroupKey)
	if !ok {
		return nil, false
	}
	// 保留 fallback kind/label，后续 identity lookup 找不到时仍能展示 source/provider。
	events := make([]usageOverviewRealtimeEvent, 0, len(cachedEvents))
	for _, cachedEvent := range cachedEvents {
		events = append(events, usageOverviewRealtimeEvent{
			event:                 recentUsageEventToEntity(cachedEvent),
			identityFallbackKind:  cachedEvent.IdentityFallbackKind,
			identityFallbackLabel: cachedEvent.IdentityFallbackLabel,
		})
	}
	return events, true
}

// loadUsageOverviewRealtimeEventsFromDB 在最近事件缓存完全不可用时，使用 usage_events 窄窗兜底。
func loadUsageOverviewRealtimeEventsFromDB(db *gorm.DB, filter dto.UsageQueryFilter, start, end time.Time) ([]usageOverviewRealtimeEvent, error) {
	// 兜底查询仍然只读实时窗口，不扩大成 Overview 的大范围扫描。
	rows, err := loadUsageOverviewEventRangeWithFilter(db, filter, start, end, false)
	if err != nil {
		return nil, err
	}
	events := make([]usageOverviewRealtimeEvent, 0, len(rows))
	for _, row := range rows {
		// DB 兜底也要预计算 fallback label，保证身份表缺失时展示一致。
		identityKind, fallbackLabel := usageRecentIdentityFallback(row.AuthType, row.Source, row.Provider)
		events = append(events, usageOverviewRealtimeEvent{
			event:                 row,
			identityFallbackKind:  identityKind,
			identityFallbackLabel: fallbackLabel,
		})
	}
	return events, nil
}

func newUsageOverviewRealtimeBuckets(start time.Time, span time.Duration, count int) []usageOverviewRealtimeBucket {
	// 内部 bucket 可以包含隐藏预热段，最终响应只输出固定 30 个可见 bucket。
	buckets := make([]usageOverviewRealtimeBucket, count)
	for index := range buckets {
		// 每个 bucket 默认 costAvailable=true，只有遇到缺价格事件才翻转。
		buckets[index] = usageOverviewRealtimeBucket{
			bucketStart:   start.Add(time.Duration(index) * span),
			costAvailable: true,
		}
	}
	return buckets
}

func usageOverviewRealtimeBucketIndex(timestamp, start time.Time, span time.Duration, bucketCount int) int {
	// 读取范围左侧的事件不进入 realtime 图表。
	if timestamp.Before(start) {
		return -1
	}
	// 通过时间差除以 bucket span 定位内部 bucket 索引。
	index := int(timestamp.Sub(start) / span)
	if index < 0 {
		return -1
	}
	// 边界上的极小漂移夹到最后一个 bucket，避免数组越界。
	if index >= bucketCount {
		return bucketCount - 1
	}
	return index
}

func collectRealtimeAuthIndexes(events []usageOverviewRealtimeEvent, visibleStart time.Time) []string {
	// usage_identities 查询只需要 auth_index，先去重减少 IN 参数数量。
	seen := map[string]struct{}{}
	result := make([]string, 0, len(events))
	for _, realtimeEvent := range events {
		// 预热事件只参与曲线平滑，不参与 current usage，因此无需查询身份展示名。
		if timeutil.NormalizeStorageTime(realtimeEvent.event.Timestamp).Before(visibleStart) {
			continue
		}
		// 空 auth_index 无法关联身份表，后续走 fallback。
		authIndex := strings.TrimSpace(realtimeEvent.event.AuthIndex)
		if authIndex == "" {
			continue
		}
		// 已见过的 auth_index 不重复追加。
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		result = append(result, authIndex)
	}
	return result
}

func applyUsageOverviewRealtimeRequest(realtimeEvent usageOverviewRealtimeEvent, modelUsage, apiKeyUsage, authFileUsage, aiProviderUsage map[string]*usageOverviewRealtimeTopAccumulator, identityLookup analysisIdentityLookup) {
	event := realtimeEvent.event
	// 模型维度的请求数不区分成功失败。
	applyUsageOverviewRealtimeRequestToTotals(modelUsage, normalizeUsageOverviewDimension(event.Model), normalizeUsageOverviewDimension(event.Model))
	// API Key 维度使用 api_group_key，KeyOverview 前端会隐藏这个 tab。
	applyUsageOverviewRealtimeRequestToTotals(apiKeyUsage, normalizeUsageOverviewDimension(event.APIGroupKey), normalizeUsageOverviewDimension(event.APIGroupKey))
	// Auth File / AI Provider 维度先走身份表，缺失时再用缓存 fallback。
	applyUsageOverviewRealtimeIdentityRequest(realtimeEvent, authFileUsage, aiProviderUsage, identityLookup)
}

func applyUsageOverviewRealtimeRequestToTotals(totals map[string]*usageOverviewRealtimeTopAccumulator, key, label string) {
	// 获取或创建 Top accumulator，保证 requests/tokens 累计到同一对象。
	item := usageOverviewRealtimeTopItem(totals, key, label)
	item.requests++
}

func applyUsageOverviewRealtimeTokenUsage(realtimeEvent usageOverviewRealtimeEvent, cost float64, costAvailable bool, modelUsage, apiKeyUsage, authFileUsage, aiProviderUsage map[string]*usageOverviewRealtimeTopAccumulator, identityLookup analysisIdentityLookup) {
	event := realtimeEvent.event
	// token share 的模型维度只统计成功且有 token 的请求。
	applyUsageOverviewRealtimeTokenUsageToTotals(modelUsage, normalizeUsageOverviewDimension(event.Model), normalizeUsageOverviewDimension(event.Model), event.TotalTokens, cost, costAvailable)
	// token share 的 API Key 维度同样按 api_group_key 聚合。
	applyUsageOverviewRealtimeTokenUsageToTotals(apiKeyUsage, normalizeUsageOverviewDimension(event.APIGroupKey), normalizeUsageOverviewDimension(event.APIGroupKey), event.TotalTokens, cost, costAvailable)
	// 身份维度 token 聚合保持和请求数相同的身份解析策略。
	applyUsageOverviewRealtimeIdentityTokenUsage(realtimeEvent, authFileUsage, aiProviderUsage, identityLookup, cost, costAvailable)
}

func applyUsageOverviewRealtimeTokenUsageToTotals(totals map[string]*usageOverviewRealtimeTopAccumulator, key, label string, tokens int64, cost float64, costAvailable bool) {
	// 同一个 key 的 token/cost 累加到同一 Top5 accumulator。
	item := usageOverviewRealtimeTopItem(totals, key, label)
	item.tokens += tokens
	item.costUSD += cost
	if !costAvailable {
		item.costAvailable = false
	}
}

func usageOverviewRealtimeTopItem(totals map[string]*usageOverviewRealtimeTopAccumulator, key, label string) *usageOverviewRealtimeTopAccumulator {
	// key 已存在时直接复用，避免重复 item 影响 Top5 排序。
	item, ok := totals[key]
	if !ok {
		// 新 item 默认 costAvailable=true，遇到缺价格事件时再置 false。
		item = &usageOverviewRealtimeTopAccumulator{key: key, label: label, costAvailable: true}
		totals[key] = item
	}
	return item
}

func applyUsageOverviewRealtimeIdentityRequest(realtimeEvent usageOverviewRealtimeEvent, authFileUsage, aiProviderUsage map[string]*usageOverviewRealtimeTopAccumulator, identityLookup analysisIdentityLookup) {
	// 一条事件最多归属 Auth File 或 AI Provider 其中一个身份维度。
	authFile, aiProvider := usageOverviewRealtimeIdentityTargets(realtimeEvent, identityLookup)
	if authFile != nil {
		applyUsageOverviewRealtimeRequestToTotals(authFileUsage, authFile.identity, authFile.label)
	}
	if aiProvider != nil {
		applyUsageOverviewRealtimeRequestToTotals(aiProviderUsage, aiProvider.identity, aiProvider.label)
	}
}

func applyUsageOverviewRealtimeIdentityTokenUsage(realtimeEvent usageOverviewRealtimeEvent, authFileUsage, aiProviderUsage map[string]*usageOverviewRealtimeTopAccumulator, identityLookup analysisIdentityLookup, cost float64, costAvailable bool) {
	event := realtimeEvent.event
	// token 累计使用和 request 累计相同的身份解析结果，避免两张 Top5 对不上。
	authFile, aiProvider := usageOverviewRealtimeIdentityTargets(realtimeEvent, identityLookup)
	if authFile != nil {
		applyUsageOverviewRealtimeTokenUsageToTotals(authFileUsage, authFile.identity, authFile.label, event.TotalTokens, cost, costAvailable)
	}
	if aiProvider != nil {
		applyUsageOverviewRealtimeTokenUsageToTotals(aiProviderUsage, aiProvider.identity, aiProvider.label, event.TotalTokens, cost, costAvailable)
	}
}

func usageOverviewRealtimeIdentityTargets(realtimeEvent usageOverviewRealtimeEvent, identityLookup analysisIdentityLookup) (*analysisIdentityInfo, *analysisIdentityInfo) {
	event := realtimeEvent.event
	// 优先用 auth_index 查 usage_identities，保证展示名和凭证页面一致。
	authIndex := strings.TrimSpace(event.AuthIndex)
	if authIndex != "" {
		if info, ok := identityLookup[entities.UsageIdentityAuthTypeAuthFile][authIndex]; ok {
			return &info, nil
		}
		if info, ok := identityLookup[entities.UsageIdentityAuthTypeAIProvider][authIndex]; ok {
			return nil, &info
		}
	}
	// 身份表找不到时，使用缓存里预先保存的 source/provider fallback。
	fallbackLabel := strings.TrimSpace(realtimeEvent.identityFallbackLabel)
	fallbackKey := authIndex
	if fallbackKey == "" {
		fallbackKey = fallbackLabel
	}
	if fallbackKey == "" {
		return nil, nil
	}
	if fallbackLabel == "" {
		fallbackLabel = fallbackKey
	}
	// fallback 的 key/label 都为空时，说明没有可展示的身份维度。
	fallback := analysisIdentityInfo{identity: fallbackKey, label: fallbackLabel}
	switch realtimeEvent.identityFallbackKind {
	case RecentUsageIdentityAuthFile:
		return &fallback, nil
	case RecentUsageIdentityAIProvider:
		return nil, &fallback
	default:
		return nil, nil
	}
}

func aggregateUsageOverviewRealtimeBucket(buckets []usageOverviewRealtimeBucket, index, bucketCount int) usageOverviewRealtimeBucket {
	if index < 0 || index >= len(buckets) {
		return usageOverviewRealtimeBucket{costAvailable: true}
	}
	start := index - bucketCount + 1
	if start < 0 {
		start = 0
	}
	// 实时图使用滑动窗口聚合，避免低频请求在单个小桶里被放大成尖峰。
	aggregated := usageOverviewRealtimeBucket{
		bucketStart:   buckets[index].bucketStart,
		costAvailable: true,
	}
	for bucketIndex := start; bucketIndex <= index; bucketIndex++ {
		bucket := buckets[bucketIndex]
		aggregated.requests += bucket.requests
		aggregated.tokens += bucket.tokens
		aggregated.inputTokens += bucket.inputTokens
		aggregated.cacheReadTokens += bucket.cacheReadTokens
		aggregated.cacheCreationTokens += bucket.cacheCreationTokens
		aggregated.costUSD += bucket.costUSD
		if !bucket.costAvailable {
			aggregated.costAvailable = false
		}
		aggregated.ttftSamples = append(aggregated.ttftSamples, bucket.ttftSamples...)
		aggregated.latencySamples = append(aggregated.latencySamples, bucket.latencySamples...)
	}
	return aggregated
}

func finalizeUsageOverviewRealtime(window, span time.Duration, windowStart, windowEnd time.Time, buckets []usageOverviewRealtimeBucket, visibleStartIndex int, modelUsage, apiKeyUsage, authFileUsage, aiProviderUsage map[string]*usageOverviewRealtimeTopAccumulator) dto.UsageOverviewRealtimeRecord {
	visibleBucketCount := len(buckets) - visibleStartIndex
	tokenVelocity := make([]dto.RealtimeTokenVelocityPointRecord, 0, visibleBucketCount)
	responseLevel := make([]dto.RealtimeResponseLevelPointRecord, 0, visibleBucketCount)
	responseDistribution := dto.RealtimeResponseDistributionRecord{
		TTFT: dto.RealtimeResponseDistributionSeriesRecord{
			AverageLine: make([]dto.RealtimeResponseAveragePointRecord, 0, visibleBucketCount),
			Particles:   []dto.RealtimeResponseParticleRecord{},
		},
		Latency: dto.RealtimeResponseDistributionSeriesRecord{
			AverageLine: make([]dto.RealtimeResponseAveragePointRecord, 0, visibleBucketCount),
			Particles:   []dto.RealtimeResponseParticleRecord{},
		},
	}
	requestLevel := make([]dto.RealtimeRequestLevelPointRecord, 0, visibleBucketCount)
	cacheLevel := make([]dto.RealtimeCacheLevelPointRecord, 0, visibleBucketCount)
	aggregationWindow := usageOverviewRealtimeAggregationWindow(window)
	aggregationBucketCount := usageOverviewRealtimeAggregationBucketCount(span, aggregationWindow)
	aggregationMinutes := aggregationWindow.Minutes()
	for index := visibleStartIndex; index < len(buckets); index++ {
		rollingBucket := aggregateUsageOverviewRealtimeBucket(buckets, index, aggregationBucketCount)
		rawBucket := buckets[index]
		bucketKey := timeutil.FormatStorageTime(rollingBucket.bucketStart)
		ttftP50, ttftP95 := usageOverviewRealtimePercentilePair(rollingBucket.ttftSamples, 0.50, 0.95)
		latencyP50, latencyP95 := usageOverviewRealtimePercentilePair(rollingBucket.latencySamples, 0.50, 0.95)
		tokenVelocity = append(tokenVelocity, dto.RealtimeTokenVelocityPointRecord{
			Bucket:          bucketKey,
			TokensPerMinute: float64(rollingBucket.tokens) / aggregationMinutes,
			Tokens:          rollingBucket.tokens,
			CostUSD:         usageOverviewRealtimeCostPtr(rollingBucket.costUSD, rollingBucket.costAvailable),
		})
		responseLevel = append(responseLevel, dto.RealtimeResponseLevelPointRecord{
			Bucket:       bucketKey,
			TTFTP50MS:    ttftP50,
			TTFTP95MS:    ttftP95,
			LatencyP50MS: latencyP50,
			LatencyP95MS: latencyP95,
		})
		responseDistribution.TTFT.AverageLine = append(responseDistribution.TTFT.AverageLine, dto.RealtimeResponseAveragePointRecord{
			Bucket: bucketKey,
			AvgMS:  usageOverviewRealtimeAverage(rollingBucket.ttftSamples),
		})
		responseDistribution.TTFT.Particles = appendUsageOverviewRealtimeDistributionParticles(responseDistribution.TTFT.Particles, bucketKey, rawBucket.ttftSamples)
		responseDistribution.Latency.AverageLine = append(responseDistribution.Latency.AverageLine, dto.RealtimeResponseAveragePointRecord{
			Bucket: bucketKey,
			AvgMS:  usageOverviewRealtimeAverage(rollingBucket.latencySamples),
		})
		responseDistribution.Latency.Particles = appendUsageOverviewRealtimeDistributionParticles(responseDistribution.Latency.Particles, bucketKey, rawBucket.latencySamples)
		requestLevel = append(requestLevel, dto.RealtimeRequestLevelPointRecord{
			Bucket:            bucketKey,
			RequestsPerMinute: float64(rollingBucket.requests) / aggregationMinutes,
			Requests:          rollingBucket.requests,
		})
		cacheLevel = append(cacheLevel, dto.RealtimeCacheLevelPointRecord{
			Bucket:              bucketKey,
			CacheReadRate:       usageOverviewRealtimeCacheReadRate(rollingBucket.cacheReadTokens, rollingBucket.inputTokens),
			CacheReadTokens:     rollingBucket.cacheReadTokens,
			CacheCreationTokens: rollingBucket.cacheCreationTokens,
			InputTokens:         rollingBucket.inputTokens,
		})
	}
	responseDistribution.TTFT = finalizeUsageOverviewRealtimeDistributionSeries(responseDistribution.TTFT)
	responseDistribution.Latency = finalizeUsageOverviewRealtimeDistributionSeries(responseDistribution.Latency)
	return dto.UsageOverviewRealtimeRecord{
		Window:               usageOverviewRealtimeWindowLabel(window),
		BucketSeconds:        int64(span / time.Second),
		WindowStart:          windowStart,
		WindowEnd:            windowEnd,
		TokenVelocity:        tokenVelocity,
		ResponseLevel:        responseLevel,
		ResponseDistribution: responseDistribution,
		CurrentUsage: dto.RealtimeCurrentUsageRecord{
			Models:      finalizeUsageOverviewRealtimeTopItems(modelUsage),
			APIKeys:     finalizeUsageOverviewRealtimeTopItems(apiKeyUsage),
			AuthFiles:   finalizeUsageOverviewRealtimeTopItems(authFileUsage),
			AIProviders: finalizeUsageOverviewRealtimeTopItems(aiProviderUsage),
		},
		RequestLevel: requestLevel,
		CacheLevel:   cacheLevel,
	}
}

func finalizeUsageOverviewRealtimeDistributionSeries(series dto.RealtimeResponseDistributionSeriesRecord) dto.RealtimeResponseDistributionSeriesRecord {
	series.TotalParticles = usageOverviewRealtimeParticleCountTotal(series.Particles)
	series.MaxParticles = usageOverviewRealtimeDistributionMaxParticles
	if len(series.Particles) <= usageOverviewRealtimeDistributionMaxParticles {
		return series
	}
	series.Sampled = true
	series.Particles = sampleUsageOverviewRealtimeDistributionParticles(series.Particles, usageOverviewRealtimeDistributionMaxParticles)
	return series
}

func sampleUsageOverviewRealtimeDistributionParticles(particles []dto.RealtimeResponseParticleRecord, maxParticles int) []dto.RealtimeResponseParticleRecord {
	if len(particles) <= maxParticles || maxParticles <= 0 {
		return particles
	}
	sortedParticles := append([]dto.RealtimeResponseParticleRecord(nil), particles...)
	sort.SliceStable(sortedParticles, func(i, j int) bool {
		leftTime := usageOverviewRealtimeParticleTimeKey(sortedParticles[i])
		rightTime := usageOverviewRealtimeParticleTimeKey(sortedParticles[j])
		if leftTime != rightTime {
			return leftTime < rightTime
		}
		if sortedParticles[i].MS != sortedParticles[j].MS {
			return sortedParticles[i].MS < sortedParticles[j].MS
		}
		return sortedParticles[i].Count < sortedParticles[j].Count
	})

	sampled := make([]dto.RealtimeResponseParticleRecord, 0, maxParticles)
	for index := 0; index < maxParticles; index++ {
		start, end := usageOverviewRealtimeDistributionParticleRange(index, len(sortedParticles), maxParticles)
		if end <= start {
			end = start + 1
		}
		group := sortedParticles[start:end]
		// 代表点保持为真实事件坐标，只把该组真实样本数汇总到 count。
		representative := group[len(group)/2]
		representative.Count = usageOverviewRealtimeParticleCountTotal(group)
		sampled = append(sampled, representative)
	}
	return sampled
}

func usageOverviewRealtimeDistributionParticleRange(index, particleCount, maxParticles int) (int, int) {
	if maxParticles <= 0 {
		return 0, 0
	}
	start := int(int64(index) * int64(particleCount) / int64(maxParticles))
	end := int(int64(index+1) * int64(particleCount) / int64(maxParticles))
	return start, end
}

func usageOverviewRealtimeParticleTimeKey(particle dto.RealtimeResponseParticleRecord) string {
	if particle.Timestamp != "" {
		return particle.Timestamp
	}
	return particle.Bucket
}

func usageOverviewRealtimeParticleCountTotal(particles []dto.RealtimeResponseParticleRecord) int64 {
	var total int64
	for _, particle := range particles {
		count := particle.Count
		if count <= 0 {
			count = usageOverviewRealtimeDistributionDefaultParticleSize
		}
		total += count
	}
	return total
}

func usageOverviewRealtimeAverage(samples []usageOverviewRealtimeResponseSample) *float64 {
	if len(samples) == 0 {
		return nil
	}
	var sum int64
	for _, sample := range samples {
		sum += sample.ms
	}
	value := float64(sum) / float64(len(samples))
	return &value
}

func appendUsageOverviewRealtimeDistributionParticles(dst []dto.RealtimeResponseParticleRecord, bucket string, samples []usageOverviewRealtimeResponseSample) []dto.RealtimeResponseParticleRecord {
	for _, sample := range samples {
		dst = append(dst, dto.RealtimeResponseParticleRecord{
			Bucket:    bucket,
			Timestamp: timeutil.FormatStorageTime(sample.timestamp),
			MS:        sample.ms,
			Count:     1,
		})
	}
	return dst
}

func usageOverviewRealtimeCostPtr(cost float64, available bool) *float64 {
	if !available {
		return nil
	}
	value := cost
	return &value
}

func usageOverviewRealtimeCacheReadRate(cacheReadTokens, inputTokens int64) *float64 {
	if inputTokens <= 0 {
		return nil
	}
	value := (float64(cacheReadTokens) / float64(inputTokens)) * 100
	return &value
}

func usageOverviewRealtimePercentilePair(samples []usageOverviewRealtimeResponseSample, first, second float64) (*int64, *int64) {
	if len(samples) == 0 {
		return nil, nil
	}
	sorted := make([]int64, 0, len(samples))
	for _, sample := range samples {
		sorted = append(sorted, sample.ms)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return usageOverviewRealtimeSortedPercentile(sorted, first), usageOverviewRealtimeSortedPercentile(sorted, second)
}

func usageOverviewRealtimeSortedPercentile(sorted []int64, percentile float64) *int64 {
	if len(sorted) == 0 {
		return nil
	}
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	value := sorted[index]
	return &value
}

func finalizeUsageOverviewRealtimeTopItems(totals map[string]*usageOverviewRealtimeTopAccumulator) []dto.RealtimeUsageTopItemRecord {
	items := make([]*usageOverviewRealtimeTopAccumulator, 0, len(totals))
	totalTokens := int64(0)
	for _, item := range totals {
		if item.tokens <= 0 {
			continue
		}
		items = append(items, item)
		totalTokens += item.tokens
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].tokens == items[j].tokens {
			return items[i].key < items[j].key
		}
		return items[i].tokens > items[j].tokens
	})
	if len(items) > 5 {
		items = items[:5]
	}
	result := make([]dto.RealtimeUsageTopItemRecord, 0, len(items))
	for _, item := range items {
		share := 0.0
		if totalTokens > 0 {
			share = (float64(item.tokens) / float64(totalTokens)) * 100
		}
		result = append(result, dto.RealtimeUsageTopItemRecord{
			Key:      item.key,
			Label:    item.label,
			Tokens:   item.tokens,
			Requests: item.requests,
			CostUSD:  usageOverviewRealtimeCostPtr(item.costUSD, item.costAvailable),
			Share:    share,
		})
	}
	return result
}

// applyUsageEventToOverviewSnapshot 把边界 raw event 累计到 Overview 基础 usage 统计。
func applyUsageEventToOverviewSnapshot(snapshot *dto.StatisticsSnapshot, event entities.UsageEvent) {
	snapshot.TotalRequests++
	snapshot.TotalTokens += event.TotalTokens
	if event.Failed {
		snapshot.FailureCount++
	} else {
		snapshot.SuccessCount++
	}
}

// newUsageOverviewSeriesRecord 初始化 Overview 趋势序列中的所有指标 map。
func newUsageOverviewSeriesRecord() dto.UsageOverviewSeriesRecord {
	return dto.UsageOverviewSeriesRecord{
		Requests:                 map[string]int64{},
		Tokens:                   map[string]int64{},
		RPM:                      map[string]float64{},
		TPM:                      map[string]float64{},
		Cost:                     map[string]float64{},
		CacheReadRate:            map[string]*float64{},
		CacheReadRateInputTokens: map[string]int64{},
		CacheReadRateReadTokens:  map[string]int64{},
	}
}

// applyUsageEventToOverviewSeries 把单条事件写入主序列。
func applyUsageEventToOverviewSeries(series *dto.UsageOverviewSeriesRecord, event entities.UsageEvent, cost float64, bucketKey string, bucketMinutes int64) {
	// 主序列按 bucket 累计请求、token、成本，并同步刷新 RPM/TPM/cache rate。
	series.Requests[bucketKey]++
	series.Tokens[bucketKey] += event.TotalTokens
	series.Cost[bucketKey] += cost
	series.RPM[bucketKey] = float64(series.Requests[bucketKey]) / float64(bucketMinutes)
	series.TPM[bucketKey] = float64(series.Tokens[bucketKey]) / float64(bucketMinutes)
	updateUsageOverviewSeriesCacheReadRate(series, bucketKey, event.InputTokens, event.CacheReadTokens)
}

// applyUsageEventToOverview 把边界 raw event 合并进 Overview，语义必须和 stats row 合并保持一致。
func applyUsageEventToOverview(overview *dto.UsageOverviewRecord, event entities.UsageEvent, bucketByDay bool, costResolver *UsageCostResolver) {
	overview.Summary.InputTokens += event.InputTokens
	overview.Summary.CacheReadTokens += event.CacheReadTokens
	overview.Summary.CacheCreationTokens += event.CacheCreationTokens
	overview.Summary.ReasoningTokens += event.ReasoningTokens
	if event.Failed {
		overview.Health.TotalFailure++
	} else {
		overview.Health.TotalSuccess++
	}
	// 边界事件也按当前价格表计算 cost；缺价格且有计费 token 时标记 cost 不完整。
	result := costResolver.CalculateEvent(event)
	if !result.Available {
		overview.Summary.CostAvailable = false
	}
	cost := result.Cost.TotalCostUSD
	overview.Summary.TotalCost += cost

	// 主序列使用页面当前粒度，缓存率同桶累计后即时刷新。
	bucketKey, bucketMinutes := usageOverviewBucket(timeutil.NormalizeStorageTime(event.Timestamp), bucketByDay)
	applyUsageEventToOverviewSeries(&overview.Series, event, cost, bucketKey, bucketMinutes)
	updateUsageOverviewHealthBlock(overview.Health.BlockDetails, event)
}

func updateUsageOverviewSeriesCacheReadRate(series *dto.UsageOverviewSeriesRecord, bucketKey string, inputTokens, cacheReadTokens int64) {
	series.CacheReadRateInputTokens[bucketKey] += inputTokens
	series.CacheReadRateReadTokens[bucketKey] += cacheReadTokens
	input := series.CacheReadRateInputTokens[bucketKey]
	if input <= 0 {
		series.CacheReadRate[bucketKey] = nil
		return
	}
	value := (float64(series.CacheReadRateReadTokens[bucketKey]) / float64(input)) * 100
	series.CacheReadRate[bucketKey] = &value
}

// finalizeUsageOverview 从累计后的 usage/health 数据反推 summary 派生指标。
func finalizeUsageOverview(overview *dto.UsageOverviewRecord) {
	overview.Summary.RequestCount = overview.Usage.TotalRequests
	overview.Summary.TokenCount = overview.Usage.TotalTokens
	if overview.Summary.WindowMinutes > 0 {
		overview.Summary.RPM = float64(overview.Summary.RequestCount) / float64(overview.Summary.WindowMinutes)
		overview.Summary.TPM = float64(overview.Summary.TokenCount) / float64(overview.Summary.WindowMinutes)
	}
	if overview.Summary.WindowMinutes > usageOverviewDailyAverageDayMinutes {
		days := float64(overview.Summary.WindowMinutes) / float64(usageOverviewDailyAverageDayMinutes)
		overview.Summary.DailyAverageRequests = usageOverviewFloat64Ptr(float64(overview.Summary.RequestCount) / days)
		overview.Summary.DailyAverageTokens = usageOverviewFloat64Ptr(float64(overview.Summary.TokenCount) / days)
		overview.Summary.DailyAverageCost = usageOverviewFloat64Ptr(overview.Summary.TotalCost / days)
		overview.Summary.DailyAverageRangeDays = usageOverviewFloat64Ptr(days)
	}
	if total := overview.Health.TotalSuccess + overview.Health.TotalFailure; total > 0 {
		overview.Health.SuccessRate = (float64(overview.Health.TotalSuccess) / float64(total)) * 100
	}
}

func usageOverviewFloat64Ptr(value float64) *float64 {
	return &value
}

// normalizeUsageOverviewDimension 统一 usage 统计中的空维度 key。
func normalizeUsageOverviewDimension(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

const (
	usageOverviewDailyAverageDayMinutes      int64 = 24 * 60
	usageOverviewDailyBucketThresholdMinutes int64 = 7 * 24 * 60
)

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

const (
	// usageOverviewHealthRows 保持兼容响应的 7 行布局。
	usageOverviewHealthRows = 7
	// usageOverviewHealthColumns 与 Activity 364 格契约共同固定为 52 列。
	usageOverviewHealthColumns = 52
)

// buildUsageOverviewHealth 用 Activity 唯一边界初始化空 service health 网格。
func buildUsageOverviewHealth(filter dto.UsageQueryFilter) dto.UsageOverviewHealthRecord {
	// range 在 PR1 只选择 short 或 medium，PR2 再接入 long。
	grain := usageOverviewHealthActivityGrain(filter)
	// 参考结束时间沿用本次 API 已解析并压到 queryNow 的有效结束时间。
	referenceEnd := usageOverviewHealthReferenceEnd(filter)
	// 纯内存 builder 与查询路径调用同一个 Activity 边界 helper。
	buckets, err := UsageActivityWindowEndingAt(grain, referenceEnd)
	// 合法固定 grain 理论上不会失败；防御性返回明确固定形状的空记录。
	if err != nil || len(buckets) != UsageActivityHeatmapBlocks {
		return dto.UsageOverviewHealthRecord{Rows: usageOverviewHealthRows, Columns: usageOverviewHealthColumns, BlockDetails: []dto.UsageOverviewHealthBlockRecord{}}
	}
	// blocks 固定分配 364 个槽位，不因数据稀疏减少格子。
	blocks := make([]dto.UsageOverviewHealthBlockRecord, len(buckets))
	// bucketSeconds 兼容旧字段并取交替跨度中的最大秒数。
	bucketSeconds := int64(0)
	// 逐格复制 Activity 的真实半开边界。
	for index, bucket := range buckets {
		// 空格 Rate=-1 保持现有前端“无数据”语义。
		blocks[index] = dto.UsageOverviewHealthBlockRecord{StartTime: bucket.Start, EndTime: bucket.End, Rate: -1}
		// 当前 bucket 真实秒数来自 start/end，不按 grain 名称反推。
		spanSeconds := int64(bucket.End.Sub(bucket.Start) / time.Second)
		// 最大跨度作为兼容 bucket_seconds 值。
		if spanSeconds > bucketSeconds {
			bucketSeconds = spanSeconds
		}
	}
	// 返回实际对齐窗口及固定网格元数据。
	return dto.UsageOverviewHealthRecord{
		Rows:          usageOverviewHealthRows,
		Columns:       usageOverviewHealthColumns,
		BucketSeconds: bucketSeconds,
		WindowStart:   buckets[0].Start,
		WindowEnd:     buckets[len(buckets)-1].End,
		BlockDetails:  blocks,
	}
}

// buildUsageOverviewHealthFromActivity 把统一 Activity 查询结果适配到现有 service_health DTO。
func buildUsageOverviewHealthFromActivity(db *gorm.DB, filter dto.UsageQueryFilter) (dto.UsageOverviewHealthRecord, error) {
	// PR1 保持短范围 short、其它现有范围 medium 的迁移边界。
	grain := usageOverviewHealthActivityGrain(filter)
	// referenceEnd 使用本次固定 effective filter 结束时间。
	referenceEnd := usageOverviewHealthReferenceEnd(filter)
	// Activity 查询负责补齐 364 格并应用 API group scope，同时沿用 service 传入的请求 context。
	grid, err := QueryUsageActivityGrid(gormDBContext(db), db, grain, referenceEnd, filter.APIGroupKey)
	// 查询失败时把仓储错误原样返回给 Overview。
	if err != nil {
		return dto.UsageOverviewHealthRecord{}, err
	}
	// 兼容 Health DTO 复制网格元数据和真实窗口。
	health := dto.UsageOverviewHealthRecord{
		Rows:          grid.Rows,
		Columns:       grid.Columns,
		BucketSeconds: grid.BucketSeconds,
		WindowStart:   grid.WindowStart,
		WindowEnd:     grid.WindowEnd,
		BlockDetails:  make([]dto.UsageOverviewHealthBlockRecord, len(grid.Blocks)),
	}
	// 每个 Activity block 机械转换为已有 Request Health block。
	for index, activityBlock := range grid.Blocks {
		// 默认无请求时 rate=-1。
		rate := -1.0
		// 请求总数只由 success+failure 派生，不读取冗余字段。
		total := activityBlock.SuccessCount + activityBlock.FailureCount
		// 有请求时 rate 保持现有 0..1 block 语义。
		if total > 0 {
			rate = float64(activityBlock.SuccessCount) / float64(total)
		}
		// block 的 start/end 直接透传 Activity 查询边界。
		health.BlockDetails[index] = dto.UsageOverviewHealthBlockRecord{
			StartTime: activityBlock.StartTime,
			EndTime:   activityBlock.EndTime,
			Success:   activityBlock.SuccessCount,
			Failure:   activityBlock.FailureCount,
			Rate:      rate,
		}
	}
	// header totals 由调用方按精确筛选窗口填入，PR2 才会改为与完整 Activity 档位对齐。
	return health, nil
}

// usageOverviewHealthActivityGrain 在 PR1 只完成现有短/默认 Health 数据源迁移。
func usageOverviewHealthActivityGrain(filter dto.UsageQueryFilter) entities.UsageActivityGrain {
	// 24h 及以下滚动范围、Today 和 Yesterday 使用 short。
	if isUsageOverviewShortHealthRange(filter.Range) {
		return entities.UsageActivityGrainShort
	}
	// 其它当前范围先保持默认 7d medium；PR2 再按 duration 选择 long。
	return entities.UsageActivityGrainMedium
}

// usageOverviewHealthReferenceEnd 返回本次 Activity 对齐使用的稳定结束时间。
func usageOverviewHealthReferenceEnd(filter dto.UsageQueryFilter) time.Time {
	// API 已解析结束时间时直接使用并归一化到存储时区。
	if filter.EndTime != nil {
		return timeutil.NormalizeStorageTime(*filter.EndTime)
	}
	// 防御性回退到本次 queryNow，避免未解析测试构造出零时间窗口。
	if filter.QueryNow != nil {
		return timeutil.NormalizeStorageTime(*filter.QueryNow)
	}
	// 旧纯内存 helper 没有边界时才使用当前项目时间。
	return timeutil.NormalizeStorageTime(time.Now())
}

// isUsageOverviewShortHealthRange 判断 health grid 是否使用 short Activity。
func isUsageOverviewShortHealthRange(value string) bool {
	if timeutil.IsUsageRollingHourRange(value) {
		return true
	}
	switch value {
	case "today", "yesterday":
		return true
	default:
		return false
	}
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
