package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const quotaWindowRawOnlyThreshold = 5 * time.Hour

type UsageWindowStats struct {
	Tokens int64
	Cost   float64
}

type UsageWindowStatsCalculator struct {
	db           *gorm.DB
	costResolver *UsageCostResolver
}

type usageWindowTokenStats struct {
	Model               string `gorm:"column:model"`
	ModelAlias          string `gorm:"column:model_alias"`
	TotalTokens         int64  `gorm:"column:total_tokens"`
	InputTokens         int64  `gorm:"column:input_tokens"`
	OutputTokens        int64  `gorm:"column:output_tokens"`
	CachedTokens        int64  `gorm:"column:cached_tokens"`
	CacheReadTokens     int64  `gorm:"column:cache_read_tokens"`
	CacheCreationTokens int64  `gorm:"column:cache_creation_tokens"`
}

type usageWindowTokenStatsKey struct {
	model      string
	modelAlias string
}

func NewUsageWindowStatsCalculator(ctx context.Context, db *gorm.DB) (*UsageWindowStatsCalculator, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	costResolver, err := NewUsageCostResolver(ctx, db)
	if err != nil {
		return nil, err
	}
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
	rows, err := loadUsageWindowTokenStats(c.db.WithContext(ctx), authIndex, start, end)
	if err != nil {
		return UsageWindowStats{}, err
	}
	return usageWindowStatsFromTokenStats(rows, c.costResolver), nil
}

func SumUsageWindowStatsByAuthIndex(ctx context.Context, db *gorm.DB, authIndex string, start time.Time, end *time.Time) (UsageWindowStats, error) {
	// 数据库句柄为空时直接返回错误，避免后续查询 panic。
	if db == nil {
		// 返回明确错误，调用方可以按普通统计失败处理。
		return UsageWindowStats{}, fmt.Errorf("database is nil")
	}
	// auth_index 是 quota 统计的唯一身份维度，先去掉外部传入的空白。
	authIndex = strings.TrimSpace(authIndex)
	// auth_index 为空时没有可查询对象，直接返回参数错误。
	if authIndex == "" {
		// 返回明确错误，避免误查全表。
		return UsageWindowStats{}, fmt.Errorf("auth_index is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryDB := db.WithContext(ctx)
	// 价格表按本次调用预加载一次，后续 raw/hourly 聚合结果都复用同一个 resolver。
	costResolver, err := NewUsageCostResolver(ctx, db)
	if err != nil {
		// 保留底层错误，方便定位数据库或迁移问题。
		return UsageWindowStats{}, err
	}
	// 根据窗口长度选择 raw-only 或 hourly-rollup 查询计划。
	rows, err := loadUsageWindowTokenStats(queryDB, authIndex, start, end)
	// 任意一段查询失败都返回错误，调用方会跳过本次窗口补充。
	if err != nil {
		// 给错误包上业务上下文，方便日志识别失败位置。
		return UsageWindowStats{}, err
	}
	// 把 model_alias/model 级 token 聚合结果换算成最终 token/cost 汇总。
	return usageWindowStatsFromTokenStats(rows, costResolver), nil
}

func loadUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end *time.Time) ([]usageWindowTokenStats, error) {
	// 空时间无法表达有效 quota 窗口，提前返回避免误构造超宽时间范围。
	if start.IsZero() || (end != nil && end.IsZero()) {
		// 返回空结果而不是错误，调用方会把它当作“该窗口暂无用量”。
		return nil, nil
	}
	// 没有结束时间时只能走 raw 查询，保持“从 start 到当前已有数据”的旧语义。
	if end == nil {
		// raw 查询本身会按 model group by，不再逐条读 usage_events。
		return sumRawUsageWindowTokenStats(db, authIndex, start, nil)
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
		return sumRawUsageWindowTokenStats(db, authIndex, windowStart, &windowEnd)
	}
	// 长窗口拆成边界 raw 和中间完整小时 rollup，降低真实高频数据下的扫描行数。
	return sumLongUsageWindowTokenStats(db, authIndex, windowStart, windowEnd)
}

func sumLongUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end time.Time) ([]usageWindowTokenStats, error) {
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
		rows, err := sumRawUsageWindowTokenStats(db, authIndex, start, &leftEnd)
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
		rows, err := sumHourlyUsageWindowTokenStats(db, authIndex, hourlyStart, hourlyEnd)
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
		rows, err := sumRawUsageWindowTokenStats(db, authIndex, rightStart, &end)
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

func sumRawUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end *time.Time) ([]usageWindowTokenStats, error) {
	// raw 查询只取 model_alias/model 级汇总字段，避免把大量 usage_events 行读进 Go 内存。
	query := db.Model(&entities.UsageEvent{}).
		// SELECT 中只聚合 token/cost 需要的字段，不读取 raw_json 等大字段。
		Select("model, COALESCE(model_alias, '') AS model_alias, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(cached_tokens), 0) AS cached_tokens, COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens, COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens").
		// auth_index 已经是唯一身份维度，这里不再额外按 auth_type 过滤。
		Where("auth_index = ? AND timestamp >= ?", authIndex, timeutil.FormatStorageTime(start)).
		// 按 model_alias/model 分组，后续按 alias 优先价格表计算 cost。
		Group("model_alias, model")
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
	// 返回 model_alias/model 级 token 汇总。
	return rows, nil
}

func sumHourlyUsageWindowTokenStats(db *gorm.DB, authIndex string, start time.Time, end time.Time) ([]usageWindowTokenStats, error) {
	// hourly 查询直接读取 overview 已经维护好的小时增量表。
	query := db.Model(&entities.UsageOverviewHourlyStat{}).
		// SELECT 中聚合 token/cost 需要的字段，保持和 raw 查询返回结构一致。
		Select("model, model_alias, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(cached_tokens), 0) AS cached_tokens, COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens, COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens").
		// auth_index + bucket_start 范围可以使用现有 hourly auth_bucket 索引。
		Where("auth_index = ? AND bucket_start >= ? AND bucket_start < ?", authIndex, timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
		// 按 model_alias/model 分组，后续按 alias 优先价格表计算 cost。
		Group("model_alias, model")
	// rows 只承接聚合后的少量 model 行。
	var rows []usageWindowTokenStats
	// 执行 hourly 聚合查询。
	if err := query.Scan(&rows).Error; err != nil {
		// 包装 hourly 查询错误，保留调用上下文。
		return nil, fmt.Errorf("sum hourly usage window stats: %w", err)
	}
	// 返回 model_alias/model 级 token 汇总。
	return rows, nil
}

func mergeUsageWindowTokenStats(merged map[usageWindowTokenStatsKey]usageWindowTokenStats, rows []usageWindowTokenStats) {
	// 遍历每个来源返回的 model_alias/model 汇总行。
	for _, row := range rows {
		// 维度先 trim，避免同一模型因为空白产生两个聚合桶。
		model := strings.TrimSpace(row.Model)
		modelAlias := strings.TrimSpace(row.ModelAlias)
		key := usageWindowTokenStatsKey{model: model, modelAlias: modelAlias}
		// 从已有 map 中取出当前 model_alias/model 的累计值。
		current := merged[key]
		// 写回规范化后的维度，后续 resolver 也按 trim 后的值查找。
		current.Model = model
		current.ModelAlias = modelAlias
		// 累加 total_tokens，用于前端 token 展示。
		current.TotalTokens += row.TotalTokens
		// 累加 input_tokens，用于 prompt/cached 成本拆分。
		current.InputTokens += row.InputTokens
		// 累加 output_tokens，用于 completion 成本计算。
		current.OutputTokens += row.OutputTokens
		// 累加 cached_tokens，用于 cache 成本计算并从 prompt 中扣除。
		current.CachedTokens += row.CachedTokens
		// 累加 Claude cache read 明细，用于 Claude 风格价格计算。
		current.CacheReadTokens += row.CacheReadTokens
		// 累加 Claude cache creation/write 明细，用于 Claude 风格价格计算。
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

func usageWindowStatsFromTokenStats(rows []usageWindowTokenStats, costResolver *UsageCostResolver) UsageWindowStats {
	// 初始化最终返回的 token/cost 汇总。
	stats := UsageWindowStats{}
	// 遍历每个 model_alias/model 的聚合 token。
	for _, row := range rows {
		// total_tokens 直接累计到前端展示的窗口 token。
		stats.Tokens += row.TotalTokens
		// 使用统一 resolver 按 alias 优先规则计算该维度的 cost。
		result := costResolver.Calculate(UsageCostSubject{
			Model:      row.Model,
			ModelAlias: row.ModelAlias,
			Tokens: helper.UsageTokenCostInput{
				InputTokens:         row.InputTokens,
				OutputTokens:        row.OutputTokens,
				CachedTokens:        row.CachedTokens,
				CacheReadTokens:     row.CacheReadTokens,
				CacheCreationTokens: row.CacheCreationTokens,
			},
		})
		stats.Cost += result.Cost.TotalCostUSD
	}
	// 返回最终窗口统计。
	return stats
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
