package dto

import "time"

// CodexQuotaEfficiencyQuery 描述一次只读的 Codex 主额度效率回溯，不会把统计结果写回历史表。
type CodexQuotaEfficiencyQuery struct {
	// AuthIndex 是 Auth File 与 usage_events 之间唯一允许的账号关联键。
	AuthIndex string
	// Now 固定本次响应的“当前时间”，避免多条查询跨秒后对当前周期产生不同判断。
	Now time.Time
	// RangeStart 是已结束周期的最早 reset_at，同时限制本次 UsageEvent 回溯范围。
	RangeStart time.Time
	// WindowRole 可选地选择 Primary 或 Secondary；nil 表示由 repository 按当前周期决定。
	WindowRole *string
}

// CodexQuotaEfficiencyHistory 是图表、周期摘要和完整周期列表共同复用的规范化查询结果。
type CodexQuotaEfficiencyHistory struct {
	// GeneratedAt 是本次 pricing snapshot 与当前周期截点共同绑定的生成时间。
	GeneratedAt time.Time
	// RangeStart 是响应实际采用的历史下界，供调用层明确“最近 30 天”口径。
	RangeStart time.Time
	// Windows 只列最近一次账号响应存在的角色，每个角色最多一项并使用其最新真实周期。
	Windows []CodexQuotaEfficiencyWindow
	// SelectedWindow 是当前响应实际展开的单个窗口；没有历史时为 nil。
	SelectedWindow *CodexQuotaEfficiencyWindow
	// Cycles 由后端统一标记 current/completed；当前周期优先，其余按角色有效结束时间倒序。
	Cycles []CodexQuotaEfficiencyCycle
}

// CodexQuotaEfficiencyWindow 用上游角色稳定表达一个窗口，周期长度取该角色的最新观察值。
type CodexQuotaEfficiencyWindow struct {
	// WindowRole 是上游主额度中的 primary 或 secondary 位置。
	WindowRole string
	// WindowKind 是当前已知秒数的友好分类；未知正窗口保持 nil。
	WindowKind *string
	// WindowSeconds 是该角色最近观察到的真实窗口秒数，只用于展示当前周期类型。
	WindowSeconds int64
	// HasCurrentCycle 表示这个系列在 GeneratedAt 是否存在正在进行的周期。
	HasCurrentCycle bool
	// LastObservedAt 用于没有当前周期时稳定选择最近有数据的系列。
	LastObservedAt time.Time
}

// CodexQuotaEfficiencyCycle 保存一个周期边界、观察边界、总用量及其真实百分比变化区间。
type CodexQuotaEfficiencyCycle struct {
	// ID 是历史父行 ID，只用于稳定标识周期，不是 UsageEvent 外键。
	ID int64
	// Status 只允许 current/completed，前端不得自行用浏览器时间重算。
	Status string
	// WindowSeconds 是这个历史周期自身的上游真实秒数，不跟随角色最新周期变化。
	WindowSeconds int64
	// WindowStartedAt 是由 reset_at 减真实窗口秒数得到的周期开始边界。
	WindowStartedAt time.Time
	// ResetAt 是周期结束的半开右边界。
	ResetAt time.Time
	// EffectiveStartedAt 是角色在本周期真正生效的查询起点；重叠周期统一使用新周期理论起点切分。
	EffectiveStartedAt time.Time
	// EffectiveEndedAt 是角色在本周期真正结束的查询终点；窗口切换或角色消失时可早于 ResetAt。
	EffectiveEndedAt time.Time
	// FirstObservedAt 是 Keeper 首次看到该周期的时间，不能冒充周期开始。
	FirstObservedAt time.Time
	// LastObservedAt 是 Keeper 最近看到该周期的时间。
	LastObservedAt time.Time
	// FirstRemainingPercent 与 LastRemainingPercent 让单基线周期也能在列表中明确展示。
	FirstRemainingPercent *int
	LastRemainingPercent  *int
	// ObservationCount 是本周期所有整数百分比状态段的真实观察次数总和。
	ObservationCount int64
	// Usage 聚合整个周期边界内的 OAuth UsageEvent，包括百分比稳定期间的事件。
	Usage CodexQuotaEfficiencyUsage
	// Transitions 只包含真实相邻剩余百分比状态形成的效率样本。
	Transitions []CodexQuotaEfficiencyTransition
}

// CodexQuotaEfficiencyTransition 表示前一状态段首次观察之后、到后一状态段首次观察（含）的左开右闭区间。
type CodexQuotaEfficiencyTransition struct {
	// FromRemainingPercent 是变化前 Keeper 统一的整数剩余百分比。
	FromRemainingPercent int
	// ToRemainingPercent 是变化后 Keeper 统一的整数剩余百分比。
	ToRemainingPercent int
	// PercentagePoints 是真实下降百分点；跨档不会补造中间样本。
	PercentagePoints int
	// IsDirect 仅在恰好下降一个百分点时为 true。
	IsDirect bool
	// IntervalStartedAt 取前一状态段 first_observed_at，不属于区间。
	IntervalStartedAt time.Time
	// IntervalEndedAt 取后一状态段 first_observed_at，属于区间。
	IntervalEndedAt time.Time
	// Usage 是这个观察间隔内动态回溯出的 OAuth 用量。
	Usage CodexQuotaEfficiencyUsage
	// TokensPerPoint 是区间 TotalTokens 除以真实下降百分点后的平均值。
	TokensPerPoint float64
	// CostPerPoint 是区间可计价部分除以真实下降百分点后的平均值。
	CostPerPoint float64
	// CostPerPointAvailable 为 false 时调用层必须显示缺失，不能把 CostPerPoint 当作零成本。
	CostPerPointAvailable bool
}

// CodexQuotaEfficiencyUsage 是周期与变化区间共用的一份动态 UsageEvent 聚合事实。
type CodexQuotaEfficiencyUsage struct {
	// Requests 是范围内所有匹配请求数量。
	Requests int64
	// SuccessfulRequests 是 Failed=false 的请求数量。
	SuccessfulRequests int64
	// FailedRequests 是 Failed=true 的请求数量。
	FailedRequests int64
	// InputTokens 保留输入 Token 汇总，供当前 pricing snapshot 计算成本。
	InputTokens int64
	// OutputTokens 保留输出 Token 汇总。
	OutputTokens int64
	// ReasoningTokens 保留推理 Token 汇总，供用户理解 TotalTokens 构成。
	ReasoningTokens int64
	// CacheReadTokens 保留缓存读取 Token 汇总。
	CacheReadTokens int64
	// CacheCreationTokens 保留缓存写入 Token 汇总。
	CacheCreationTokens int64
	// TotalTokens 是页面展示和每百分点效率计算使用的总 Token。
	TotalTokens int64
	// TotalCostUSD 是按本次响应固定的当前 pricing snapshot 动态回算值。
	TotalCostUSD float64
	// CostAvailable 只有所有需要计价的分组都成功匹配价格时才为 true。
	CostAvailable bool
}
