package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

const codexQuotaHistoryRange = 30 * 24 * time.Hour

// CodexQuotaHistoryRequest 选择一个 Codex Auth File 及可选的上游主额度角色。
type CodexQuotaHistoryRequest struct {
	// AuthIndex 来自详情抽屉当前 Auth File，不接受 AI Provider 身份。
	AuthIndex string `json:"-"`
	// WindowRole 可选地选择 primary 或 secondary；nil 表示自动选择当前角色。
	WindowRole *string `json:"-"`
	// Now 仅供测试固定本次响应截点；生产请求保持零值并使用 time.Now。
	Now time.Time `json:"-"`
}

// CodexQuotaHistoryResponse 是额度历史标签一次请求即可复用的完整响应。
type CodexQuotaHistoryResponse struct {
	// GeneratedAt 固定当前周期统计截点和 pricing snapshot 的响应生成时间。
	GeneratedAt time.Time `json:"generated_at"`
	// RangeStart 明确已结束周期只回溯最近三十天。
	RangeStart time.Time `json:"range_start"`
	// Windows 只列最近一次账号响应存在的角色，每个角色最多一项且标题取其最新周期。
	Windows []CodexQuotaHistoryWindow `json:"windows"`
	// SelectedWindow 是本响应实际展示的单个窗口；无历史时为 nil。
	SelectedWindow *CodexQuotaHistoryWindow `json:"selected_window"`
	// Cycles 包含当前及已结束周期；当前周期由后端标记并排在第一位。
	Cycles []CodexQuotaHistoryCycle `json:"cycles"`
}

// CodexQuotaHistoryWindow 以角色稳定表达窗口，并携带该角色最新的周期标题。
type CodexQuotaHistoryWindow struct {
	// WindowRole 是 primary 或 secondary。
	WindowRole string `json:"window_role"`
	// WindowKind 是 five_hour/weekly/monthly 友好分类；未知窗口省略。
	WindowKind *string `json:"window_kind,omitempty"`
	// WindowSeconds 是该角色最近观察到的窗口长度，只用于前端周期标题。
	WindowSeconds int64 `json:"window_seconds"`
	// HasCurrentCycle 告诉前端优先选择哪个系列。
	HasCurrentCycle bool `json:"has_current_cycle"`
	// LastObservedAt 是无当前周期时选择最近系列与展示数据新鲜度的依据。
	LastObservedAt time.Time `json:"last_observed_at"`
}

// CodexQuotaHistoryCycle 汇总一个真实周期及相邻百分比状态形成的效率样本。
type CodexQuotaHistoryCycle struct {
	// ID 是父周期稳定 ID，前端可用于 React 分组 key。
	ID int64 `json:"id"`
	// Status 由后端按同一个 GeneratedAt 判定，前端只负责展示。
	Status string `json:"status"`
	// WindowSeconds 是这条历史周期自身的真实长度，不跟随角色最新周期变化。
	WindowSeconds int64 `json:"window_seconds"`
	// WindowStartedAt 是真实周期开始，不等于 Keeper 首次观察时间。
	WindowStartedAt time.Time `json:"window_started_at"`
	// ResetAt 是真实周期结束半开边界。
	ResetAt time.Time `json:"reset_at"`
	// EffectiveStartedAt 是角色在本周期实际生效的统计起点。
	EffectiveStartedAt time.Time `json:"effective_started_at"`
	// EffectiveEndedAt 是角色在本周期实际结束的统计终点。
	EffectiveEndedAt time.Time `json:"effective_ended_at"`
	// FirstObservedAt 标记 Keeper 何时首次获取该周期数据。
	FirstObservedAt time.Time `json:"first_observed_at"`
	// LastObservedAt 标记 Keeper 最近一次获取该周期数据。
	LastObservedAt time.Time `json:"last_observed_at"`
	// FirstRemainingPercent 与 LastRemainingPercent 让单份基线也能出现在周期记录中。
	FirstRemainingPercent *int `json:"first_remaining_percent"`
	LastRemainingPercent  *int `json:"last_remaining_percent"`
	// ObservationCount 是周期内所有百分比段的累计采样次数。
	ObservationCount int64 `json:"observation_count"`
	// Usage 包含稳定段在内的整个周期 UsageEvent 动态回溯总量。
	Usage CodexQuotaHistoryUsage `json:"usage"`
	// Transitions 只列真实相邻剩余百分比变化，不补造跨档中间点。
	Transitions []CodexQuotaHistoryTransition `json:"transitions"`
}

// CodexQuotaHistoryTransition 是柱状图和历史明细共用的单个百分比变化区间。
type CodexQuotaHistoryTransition struct {
	// FromRemainingPercent 是变化前页面口径的整数剩余百分比。
	FromRemainingPercent int `json:"from_remaining_percent"`
	// ToRemainingPercent 是变化后页面口径的整数剩余百分比。
	ToRemainingPercent int `json:"to_remaining_percent"`
	// PercentagePoints 是真实下降百分点，作为效率平均值分母。
	PercentagePoints int `json:"percentage_points"`
	// IsDirect 表示这是不是可精确归属于一个百分点的直接样本。
	IsDirect bool `json:"is_direct"`
	// IntervalStartedAt 是前一百分比首次出现时间，不属于统计区间。
	IntervalStartedAt time.Time `json:"interval_started_at"`
	// IntervalEndedAt 是后一百分比首次出现时间，属于统计区间且只归前一次下降。
	IntervalEndedAt time.Time `json:"interval_ended_at"`
	// Usage 是该观察间隔内的请求、Token 与动态 Cost。
	Usage CodexQuotaHistoryUsage `json:"usage"`
	// TokensPerPoint 是区间总 Token 除以真实下降百分点。
	TokensPerPoint float64 `json:"tokens_per_point"`
	// CostPerPoint 是区间动态 Cost 除以真实下降百分点。
	CostPerPoint float64 `json:"cost_per_point"`
	// CostPerPointAvailable 为 false 时前端不得把 CostPerPoint 显示成零成本。
	CostPerPointAvailable bool `json:"cost_per_point_available"`
}

// CodexQuotaHistoryUsage 是前端周期摘要和变化区间列表共享的动态聚合事实。
type CodexQuotaHistoryUsage struct {
	// Requests 是匹配请求总数。
	Requests int64 `json:"requests"`
	// SuccessfulRequests 是未失败请求数。
	SuccessfulRequests int64 `json:"successful_requests"`
	// FailedRequests 是失败请求数。
	FailedRequests int64 `json:"failed_requests"`
	// InputTokens 是输入 Token 总量。
	InputTokens int64 `json:"input_tokens"`
	// OutputTokens 是输出 Token 总量。
	OutputTokens int64 `json:"output_tokens"`
	// ReasoningTokens 是推理 Token 总量。
	ReasoningTokens int64 `json:"reasoning_tokens"`
	// CacheReadTokens 是缓存读取 Token 总量。
	CacheReadTokens int64 `json:"cache_read_tokens"`
	// CacheCreationTokens 是缓存写入 Token 总量。
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	// TotalTokens 是页面效率对比采用的总 Token。
	TotalTokens int64 `json:"total_tokens"`
	// TotalCostUSD 是按响应生成时当前定价动态回算的美元成本。
	TotalCostUSD float64 `json:"total_cost_usd"`
	// CostAvailable 只有全部有 Token 的定价分组均可计价时才为 true。
	CostAvailable bool `json:"cost_available"`
}

// GetCodexQuotaHistory 校验 Auth File 身份后，用单个 pricing snapshot 动态生成额度效率响应。
func (s *Service) GetCodexQuotaHistory(ctx context.Context, request CodexQuotaHistoryRequest) (CodexQuotaHistoryResponse, error) {
	response := CodexQuotaHistoryResponse{}
	if s == nil || s.db == nil {
		return response, fmt.Errorf("get codex quota history: service database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request.AuthIndex = strings.TrimSpace(request.AuthIndex)
	if request.AuthIndex == "" {
		return response, fmt.Errorf("%w: auth_index is required", ErrValidation)
	}
	if request.WindowRole != nil {
		role := strings.ToLower(strings.TrimSpace(*request.WindowRole))
		if role != string(entities.CodexQuotaWindowRolePrimary) && role != string(entities.CodexQuotaWindowRoleSecondary) {
			return response, fmt.Errorf("%w: window_role must be primary or secondary", ErrValidation)
		}
		request.WindowRole = &role
	}
	// 先确认当前仍是活跃 Auth File，删除身份和 AI Provider 都不能借 auth_index 查询历史。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, request.AuthIndex)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response, fmt.Errorf("%w: %s", ErrNotFound, request.AuthIndex)
		}
		return response, fmt.Errorf("get codex quota history identity: %w", err)
	}
	if !usageHeaderIdentityIsCodex(identity) {
		return response, fmt.Errorf("%w: %s", ErrUnsupportedType, normalizeIdentityType(identity.Type))
	}

	// now 在方法入口固定一次；单个 resolver 同样固定一个 snapshot，响应不会混用新旧价格。
	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}
	history, err := repository.BuildCodexQuotaEfficiencyHistory(ctx, s.db, repositorydto.CodexQuotaEfficiencyQuery{
		AuthIndex:  request.AuthIndex,
		Now:        now,
		RangeStart: now.Add(-codexQuotaHistoryRange),
		WindowRole: request.WindowRole,
	}, s.pricing.NewResolver())
	if err != nil {
		return response, fmt.Errorf("get codex quota history: %w", err)
	}
	return codexQuotaHistoryResponseFromRepository(history), nil
}

func codexQuotaHistoryResponseFromRepository(history repositorydto.CodexQuotaEfficiencyHistory) CodexQuotaHistoryResponse {
	response := CodexQuotaHistoryResponse{
		GeneratedAt: history.GeneratedAt,
		RangeStart:  history.RangeStart,
		Windows:     make([]CodexQuotaHistoryWindow, 0, len(history.Windows)),
		Cycles:      make([]CodexQuotaHistoryCycle, 0, len(history.Cycles)),
	}
	for _, window := range history.Windows {
		response.Windows = append(response.Windows, codexQuotaHistoryWindowFromRepository(window))
	}
	if history.SelectedWindow != nil {
		window := codexQuotaHistoryWindowFromRepository(*history.SelectedWindow)
		response.SelectedWindow = &window
	}
	for _, cycle := range history.Cycles {
		response.Cycles = append(response.Cycles, codexQuotaHistoryCycleFromRepository(cycle))
	}
	return response
}

func codexQuotaHistoryWindowFromRepository(window repositorydto.CodexQuotaEfficiencyWindow) CodexQuotaHistoryWindow {
	return CodexQuotaHistoryWindow{
		WindowRole:      window.WindowRole,
		WindowKind:      cloneCodexQuotaHistoryString(window.WindowKind),
		WindowSeconds:   window.WindowSeconds,
		HasCurrentCycle: window.HasCurrentCycle,
		LastObservedAt:  window.LastObservedAt,
	}
}

func cloneCodexQuotaHistoryString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func codexQuotaHistoryCycleFromRepository(cycle repositorydto.CodexQuotaEfficiencyCycle) CodexQuotaHistoryCycle {
	response := CodexQuotaHistoryCycle{
		ID:                    cycle.ID,
		Status:                cycle.Status,
		WindowSeconds:         cycle.WindowSeconds,
		WindowStartedAt:       cycle.WindowStartedAt,
		ResetAt:               cycle.ResetAt,
		EffectiveStartedAt:    cycle.EffectiveStartedAt,
		EffectiveEndedAt:      cycle.EffectiveEndedAt,
		FirstObservedAt:       cycle.FirstObservedAt,
		LastObservedAt:        cycle.LastObservedAt,
		FirstRemainingPercent: cloneCodexQuotaHistoryInt(cycle.FirstRemainingPercent),
		LastRemainingPercent:  cloneCodexQuotaHistoryInt(cycle.LastRemainingPercent),
		ObservationCount:      cycle.ObservationCount,
		Usage:                 codexQuotaHistoryUsageFromRepository(cycle.Usage),
		Transitions:           make([]CodexQuotaHistoryTransition, 0, len(cycle.Transitions)),
	}
	for _, transition := range cycle.Transitions {
		response.Transitions = append(response.Transitions, CodexQuotaHistoryTransition{
			FromRemainingPercent:  transition.FromRemainingPercent,
			ToRemainingPercent:    transition.ToRemainingPercent,
			PercentagePoints:      transition.PercentagePoints,
			IsDirect:              transition.IsDirect,
			IntervalStartedAt:     transition.IntervalStartedAt,
			IntervalEndedAt:       transition.IntervalEndedAt,
			Usage:                 codexQuotaHistoryUsageFromRepository(transition.Usage),
			TokensPerPoint:        transition.TokensPerPoint,
			CostPerPoint:          transition.CostPerPoint,
			CostPerPointAvailable: transition.CostPerPointAvailable,
		})
	}
	return response
}

func cloneCodexQuotaHistoryInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func codexQuotaHistoryUsageFromRepository(usage repositorydto.CodexQuotaEfficiencyUsage) CodexQuotaHistoryUsage {
	return CodexQuotaHistoryUsage{
		Requests:            usage.Requests,
		SuccessfulRequests:  usage.SuccessfulRequests,
		FailedRequests:      usage.FailedRequests,
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		ReasoningTokens:     usage.ReasoningTokens,
		CacheReadTokens:     usage.CacheReadTokens,
		CacheCreationTokens: usage.CacheCreationTokens,
		TotalTokens:         usage.TotalTokens,
		TotalCostUSD:        usage.TotalCostUSD,
		CostAvailable:       usage.CostAvailable,
	}
}
