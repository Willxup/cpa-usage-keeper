package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const quotaWindowRawOnlyThreshold = 5 * time.Hour

type UsageWindowStats struct {
	Tokens        int64
	Cost          float64
	CostAvailable bool
}

// UsageWindowStatsGrouper 把 SQL 已聚合的真实模型映射到额度组；false 表示模型归属未知。
type UsageWindowStatsGrouper func(model string) (groupKey string, ok bool)

// UsageWindowGroupedStats 保留每个额度组的统计，并标记窗口内模型是否都能可靠归组。
type UsageWindowGroupedStats struct {
	Groups   map[string]UsageWindowStats
	Complete bool
}

type UsageWindowStatsCalculator struct {
	db           *gorm.DB
	costResolver pricing.Resolver
}

type usageWindowTokenStats struct {
	APIGroupKey         string `gorm:"column:api_group_key"`
	Model               string `gorm:"column:model"`
	AuthIndex           string `gorm:"column:auth_index"`
	ModelAlias          string `gorm:"column:model_alias"`
	ServiceTier         string `gorm:"column:service_tier"`
	ResponseServiceTier string `gorm:"column:response_service_tier"`
	ReasoningEffort     string `gorm:"column:reasoning_effort"`
	Endpoint            string `gorm:"column:endpoint"`
	ExecutorType        string `gorm:"column:executor_type"`
	TotalTokens         int64  `gorm:"column:total_tokens"`
	InputTokens         int64  `gorm:"column:input_tokens"`
	OutputTokens        int64  `gorm:"column:output_tokens"`
	CacheReadTokens     int64  `gorm:"column:cache_read_tokens"`
	CacheCreationTokens int64  `gorm:"column:cache_creation_tokens"`
}

type usageWindowTokenStatsKey struct {
	dimensions pricing.UsageDimensions
}

func NewUsageWindowStatsCalculator(_ context.Context, db *gorm.DB, costResolver pricing.Resolver) (*UsageWindowStatsCalculator, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	// 保存原始可克隆句柄；Reader clause 必须在每次 Sum 的独立 session 上绑定，不能保存可变 Statement。
	return &UsageWindowStatsCalculator{db: db, costResolver: costResolver}, nil
}

func (c *UsageWindowStatsCalculator) SumByAuthIndex(ctx context.Context, authIndex string, start time.Time, end *time.Time) (UsageWindowStats, error) {
	if c == nil || c.db == nil {
		return UsageWindowStats{}, fmt.Errorf("usage window stats calculator is nil")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return UsageWindowStats{}, fmt.Errorf("auth_index is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 先绑定 Reader，再创建 clone=2 的独立 session；长窗口三段查询不会互相累积 Model/Where。
	queryDB := c.db.Clauses(dbresolver.Read).Session(&gorm.Session{Context: ctx})
	rows, err := loadUsageWindowTokenStats(queryDB, authIndex, start, end, c.costResolver.ActiveFields())
	if err != nil {
		return UsageWindowStats{}, err
	}
	return usageWindowStatsFromTokenStats(rows, c.costResolver), nil
}

func (c *UsageWindowStatsCalculator) SumGroupsByAuthIndex(ctx context.Context, authIndex string, start time.Time, end *time.Time, grouper UsageWindowStatsGrouper) (UsageWindowGroupedStats, error) {
	if c == nil || c.db == nil {
		return UsageWindowGroupedStats{}, fmt.Errorf("usage window stats calculator is nil")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return UsageWindowGroupedStats{}, fmt.Errorf("auth_index is required")
	}
	if grouper == nil {
		return UsageWindowGroupedStats{}, fmt.Errorf("usage window stats grouper is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 分组统计与普通窗口统计共用相同 Reader、索引范围和 SQL 模型聚合，只改变少量结果行的最终合并方式。
	queryDB := c.db.Clauses(dbresolver.Read).Session(&gorm.Session{Context: ctx})
	rows, err := loadUsageWindowTokenStats(queryDB, authIndex, start, end, c.costResolver.ActiveFields())
	if err != nil {
		return UsageWindowGroupedStats{}, err
	}
	return usageWindowGroupedStatsFromTokenStats(rows, c.costResolver, grouper), nil
}

func SumUsageWindowStatsByAuthIndex(ctx context.Context, db *gorm.DB, authIndex string, start time.Time, end *time.Time, costResolver pricing.Resolver) (UsageWindowStats, error) {
	// 兼容函数只负责构造统一 calculator，禁止保留第二套窗口切分、查询和计价实现。
	calculator, err := NewUsageWindowStatsCalculator(ctx, db, costResolver)
	if err != nil {
		return UsageWindowStats{}, err
	}
	return calculator.SumByAuthIndex(ctx, authIndex, start, end)
}

func loadUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end *time.Time, activeFields pricing.ActiveFields) ([]usageWindowTokenStats, error) {
	// 空时间无法表达有效 quota 窗口，提前返回避免误构造超宽时间范围。
	if start.IsZero() || (end != nil && end.IsZero()) {
		// 返回空结果而不是错误，调用方会把它当作“该窗口暂无用量”。
		return nil, nil
	}
	// 没有结束时间时只能走 raw 查询，保持“从 start 到当前已有数据”的旧语义。
	if end == nil {
		// raw 查询本身会按 model group by，不再逐条读 usage_events。
		return sumRawUsageWindowTokenStats(db, authIndex, start, nil, activeFields)
	}
	// 结束时间归一化为存储时区，避免和 SQLite 文本时间比较口径不一致。
	windowEnd := timeutil.NormalizeStorageTime(*end)
	// 开始时间归一化为存储时区，确保后续整点切分与查询参数一致。
	windowStart := timeutil.NormalizeStorageTime(start)
	// 空窗口或反向窗口没有统计意义，直接返回空结果。
	if !windowStart.Before(windowEnd) {
		// 返回空 slice 而不是错误，方便调用方统一累加。
		return nil, nil
	}
	// 5 小时及以内直接查 raw，避免小窗口为了 rollup 多打几次数据库。
	if windowEnd.Sub(windowStart) <= quotaWindowRawOnlyThreshold {
		// raw 查询会使用 auth_index + timestamp 范围索引，并在 SQL 内完成 model 聚合。
		return sumRawUsageWindowTokenStats(db, authIndex, windowStart, &windowEnd, activeFields)
	}
	// 长窗口拆成边界 raw 和中间完整小时 rollup，降低真实高频数据下的扫描行数。
	return sumLongUsageWindowTokenStats(db, authIndex, windowStart, windowEnd, activeFields)
}

func sumLongUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end time.Time, activeFields pricing.ActiveFields) ([]usageWindowTokenStats, error) {
	// 左边界结束点取 start 之后的第一个整点，只有非整点部分才需要 raw 补偿。
	leftEnd := ceilUsageWindowHour(start)
	// 如果窗口不到左边界整点就结束，左边界最多只能补到 end。
	if end.Before(leftEnd) {
		// 把左边界裁剪到实际窗口结束，避免 raw 查询越过窗口。
		leftEnd = end
	}
	// 右边界开始点取 end 所在整点，整点之后的尾巴需要 raw 补偿。
	rightStart := end.Truncate(time.Hour)
	// 最近一个完整小时可能刚写入 usage_events 但还没进入 hourly rollup，所以再向前保守一小时。
	safeHourlyEnd := rightStart.Add(-time.Hour)
	// 如果保守后的 hourly 结束点比原右边界更早，就扩大右边界 raw 覆盖范围。
	if safeHourlyEnd.Before(rightStart) {
		// raw 右边界覆盖最近一到两小时，换取对聚合滞后的稳定兼容。
		rightStart = safeHourlyEnd
	}
	// 如果右边界起点落在窗口开始之前，说明没有完整小时可用。
	if rightStart.Before(start) {
		// 把右边界起点裁剪到 start，避免读取窗口外数据。
		rightStart = start
	}
	if rightStart.Before(leftEnd) {
		// 右边界不能早于左边界结束点，否则两个 raw 半开区间会重叠并重复计数。
		rightStart = leftEnd
	}
	// 完整小时开始于左边界之后的整点。
	hourlyStart := leftEnd
	// 完整小时结束于保守后的右边界起点。
	hourlyEnd := rightStart
	// 用 map 按 model_alias/model 合并 left raw、hourly、right raw 的结果。
	merged := make(map[usageWindowTokenStatsKey]usageWindowTokenStats)
	// 左边界存在时读取 usage_events 边界段。
	if start.Before(leftEnd) {
		// 查询左边界 raw 聚合，最多覆盖不足一小时的数据。
		rows, err := sumRawUsageWindowTokenStats(db, authIndex, start, &leftEnd, activeFields)
		// 左边界查询失败时直接返回，避免展示半截统计。
		if err != nil {
			// 包装左边界错误，便于测试和日志定位。
			return nil, fmt.Errorf("sum left raw usage window stats: %w", err)
		}
		// 把左边界 model 聚合结果合并到总结果。
		mergeUsageWindowTokenStats(merged, rows)
	}
	// 中间存在完整小时时读取 hourly rollup。
	if hourlyStart.Before(hourlyEnd) {
		// 查询完整小时 rollup 聚合，避免扫描 7 天 raw events。
		rows, err := sumHourlyUsageWindowTokenStats(db, authIndex, hourlyStart, hourlyEnd, activeFields)
		// hourly 查询失败时直接返回，避免展示半截统计。
		if err != nil {
			// 包装 hourly 错误，便于区分 raw 和 rollup 问题。
			return nil, fmt.Errorf("sum hourly usage window stats: %w", err)
		}
		// 把 hourly model 聚合结果合并到总结果。
		mergeUsageWindowTokenStats(merged, rows)
	}
	// 右边界存在时读取 usage_events 尾部段。
	if rightStart.Before(end) {
		// 查询右边界 raw 聚合，覆盖最后一个完整小时之后的数据。
		rows, err := sumRawUsageWindowTokenStats(db, authIndex, rightStart, &end, activeFields)
		// 右边界查询失败时直接返回，避免展示半截统计。
		if err != nil {
			// 包装右边界错误，便于测试和日志定位。
			return nil, fmt.Errorf("sum right raw usage window stats: %w", err)
		}
		// 把右边界 model 聚合结果合并到总结果。
		mergeUsageWindowTokenStats(merged, rows)
	}
	// 把 map 转回 slice，交给 cost 计算函数按 model 价格处理。
	return usageWindowTokenStatsValues(merged), nil
}

func sumRawUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end *time.Time, activeFields pricing.ActiveFields) ([]usageWindowTokenStats, error) {
	dimensions := UsagePricingDimensionColumns(activeFields)
	groupDimensions := append([]string{"model_alias", "model"}, dimensions[2:]...)
	selectDimensions := append([]string(nil), dimensions...)
	for index := range selectDimensions {
		if selectDimensions[index] == "model_alias" {
			selectDimensions[index] = "COALESCE(model_alias, '') AS model_alias"
		}
	}
	selectClause := strings.Join(selectDimensions, ", ") + ", COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens, COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens"
	// raw 查询只取 model_alias/model 级汇总字段，避免把大量 usage_events 行读进 Go 内存。
	query := db.Model(&entities.UsageEvent{}).
		// SELECT 中只聚合 token/cost 需要的字段，不读取 raw_json 等大字段。
		Select(selectClause).
		// auth_index 已经是唯一身份维度，这里不再额外按 auth_type 过滤。
		Where("auth_index = ? AND timestamp >= ?", authIndex, timeutil.FormatStorageTime(start)).
		// 按 model_alias/model 分组保留现有聚合边界，后续 cost 先按真实 model、再按 alias 回退。
		Group(strings.Join(groupDimensions, ", "))
	// 如果调用方传入结束时间，就用半开区间避免边界重复累计。
	if end != nil {
		// end 统一格式化为 storage time，确保 SQLite 文本比较稳定。
		query = query.Where("timestamp < ?", timeutil.FormatStorageTime(*end))
	}
	// rows 只承接聚合后的少量 model 行。
	var rows []usageWindowTokenStats
	// 执行 SQL 聚合查询。
	if err := query.Scan(&rows).Error; err != nil {
		// 包装 raw 查询错误，保留调用上下文。
		return nil, fmt.Errorf("sum raw usage window stats: %w", err)
	}
	for index := range rows {
		rows[index].AuthIndex = authIndex
	}
	// 返回 model_alias/model 级 token 汇总。
	return rows, nil
}

func sumHourlyUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end time.Time, activeFields pricing.ActiveFields) ([]usageWindowTokenStats, error) {
	dimensions := UsagePricingDimensionColumns(activeFields)
	groupDimensions := append([]string{"model_alias", "model"}, dimensions[2:]...)
	selectClause := strings.Join(dimensions, ", ") + ", COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens, COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens"
	// hourly 查询直接读取 overview 已经维护好的小时增量表。
	query := db.Model(&entities.UsageOverviewHourlyStat{}).
		// SELECT 中聚合 token/cost 需要的字段，保持和 raw 查询返回结构一致。
		Select(selectClause).
		// auth_index + bucket_start 范围可以使用现有 hourly auth_bucket 索引。
		Where("auth_index = ? AND bucket_start >= ? AND bucket_start < ?", authIndex, timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
		// 按 model_alias/model 分组保留现有聚合边界，后续 cost 先按真实 model、再按 alias 回退。
		Group(strings.Join(groupDimensions, ", "))
	// rows 只承接聚合后的少量 model 行。
	var rows []usageWindowTokenStats
	// 执行 hourly 聚合查询。
	if err := query.Scan(&rows).Error; err != nil {
		// 包装 hourly 查询错误，保留调用上下文。
		return nil, fmt.Errorf("sum hourly usage window stats: %w", err)
	}
	for index := range rows {
		rows[index].AuthIndex = authIndex
	}
	// 返回 model_alias/model 级 token 汇总。
	return rows, nil
}

func mergeUsageWindowTokenStats(merged map[usageWindowTokenStatsKey]usageWindowTokenStats, rows []usageWindowTokenStats) {
	// 遍历每个来源返回的 model_alias/model 汇总行。
	for _, row := range rows {
		// 维度先 trim，避免同一模型因为空白产生两个聚合桶。
		dimensions := newUsagePricingCostSubject(
			row.APIGroupKey,
			row.Model,
			row.AuthIndex,
			row.ModelAlias,
			row.ServiceTier,
			row.ResponseServiceTier,
			row.ReasoningEffort,
			row.Endpoint,
			row.ExecutorType,
			0, 0, 0, 0,
		).Dimensions
		key := usageWindowTokenStatsKey{dimensions: dimensions}
		// 从已有 map 中取出当前 model_alias/model 的累计值。
		current := merged[key]
		// 写回规范化后的维度，后续 resolver 也按 trim 后的值查找。
		current.APIGroupKey = dimensions.APIGroupKey
		current.Model = dimensions.Model
		current.AuthIndex = dimensions.AuthIndex
		current.ModelAlias = dimensions.ModelAlias
		current.ServiceTier = dimensions.ServiceTier
		current.ResponseServiceTier = dimensions.ResponseServiceTier
		current.ReasoningEffort = dimensions.ReasoningEffort
		current.Endpoint = dimensions.Endpoint
		current.ExecutorType = dimensions.ExecutorType
		// 累加 total_tokens，用于前端 token 展示。
		current.TotalTokens += row.TotalTokens
		// 累加 input_tokens，用于普通输入、缓存读取和缓存写入成本拆分。
		current.InputTokens += row.InputTokens
		// 累加 output_tokens，用于 completion 成本计算。
		current.OutputTokens += row.OutputTokens
		// 累加 cache read 明细，所有价格风格都按统一四段 token 事实计价。
		current.CacheReadTokens += row.CacheReadTokens
		// 累加 cache creation/write 明细，缺失时自然保持为 0。
		current.CacheCreationTokens += row.CacheCreationTokens
		// 把合并后的 model_alias/model 统计写回 map。
		merged[key] = current
	}
}

func usageWindowTokenStatsValues(merged map[usageWindowTokenStatsKey]usageWindowTokenStats) []usageWindowTokenStats {
	// 预分配 slice 容量，避免 model 数较多时反复扩容。
	rows := make([]usageWindowTokenStats, 0, len(merged))
	// 遍历 map 中已经合并好的 model 统计。
	for _, row := range merged {
		// 把单个 model 的累计统计追加到返回列表。
		rows = append(rows, row)
	}
	// 返回列表顺序不影响最终 token/cost 汇总。
	return rows
}

func usageWindowStatsFromTokenStats(rows []usageWindowTokenStats, costResolver pricing.Resolver) UsageWindowStats {
	// 空窗口的 cost 仍是完整的零值，只有存在无法计价的 Token 行时才降级为 unavailable。
	stats := UsageWindowStats{CostAvailable: true}
	// 遍历每个 model_alias/model 的聚合 token。
	for _, row := range rows {
		stats = addUsageWindowTokenStats(stats, row, costResolver)
	}
	// 返回最终窗口统计。
	return stats
}

func usageWindowGroupedStatsFromTokenStats(rows []usageWindowTokenStats, costResolver pricing.Resolver, grouper UsageWindowStatsGrouper) UsageWindowGroupedStats {
	result := UsageWindowGroupedStats{Groups: make(map[string]UsageWindowStats), Complete: true}
	for _, row := range rows {
		groupKey, ok := grouper(row.Model)
		groupKey = strings.TrimSpace(groupKey)
		if !ok || groupKey == "" {
			// 未知的零值聚合行不影响完整性；任何正 Token 事实都可能属于某个额度组，不能静默漏算。
			if usageWindowTokenStatsHasUsage(row) {
				result.Complete = false
			}
			continue
		}
		stats, exists := result.Groups[groupKey]
		if !exists {
			stats.CostAvailable = true
		}
		result.Groups[groupKey] = addUsageWindowTokenStats(stats, row, costResolver)
	}
	return result
}

func addUsageWindowTokenStats(stats UsageWindowStats, row usageWindowTokenStats, costResolver pricing.Resolver) UsageWindowStats {
	stats.Tokens += row.TotalTokens
	result := costResolver.Calculate(newUsagePricingCostSubject(
		row.APIGroupKey,
		row.Model,
		row.AuthIndex,
		row.ModelAlias,
		row.ServiceTier,
		row.ResponseServiceTier,
		row.ReasoningEffort,
		row.Endpoint,
		row.ExecutorType,
		row.InputTokens,
		row.OutputTokens,
		row.CacheReadTokens,
		row.CacheCreationTokens,
	))
	// 兼容极少数只有 total_tokens 的旧行：模型未匹配价格时不能把零成本冒充完整结果。
	if row.TotalTokens > 0 && result.MatchedModel == "" {
		result.Available = false
	}
	stats.Cost += result.Cost.TotalCostUSD
	if !result.Available {
		stats.CostAvailable = false
	}
	return stats
}

func usageWindowTokenStatsHasUsage(row usageWindowTokenStats) bool {
	return row.TotalTokens > 0 || row.InputTokens > 0 || row.OutputTokens > 0 || row.CacheReadTokens > 0 || row.CacheCreationTokens > 0
}

func ceilUsageWindowHour(value time.Time) time.Time {
	// 先把时间归一化，避免不同 location 下 Truncate 结果难以比较。
	value = timeutil.NormalizeStorageTime(value)
	// 取当前时间所在小时的整点。
	truncated := value.Truncate(time.Hour)
	// 如果本身已经是整点，就直接返回当前整点。
	if value.Equal(truncated) {
		// 整点窗口不需要左边界 raw 补偿。
		return truncated
	}
	// 非整点时返回下一个整点，作为完整小时 rollup 的开始。
	return truncated.Add(time.Hour)
}
