package repository

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// codexQuotaEfficiencyCycleWork 把公开周期 DTO 与本次 UsageEvent 流的查询边界放在一起。
type codexQuotaEfficiencyCycleWork struct {
	// record 是后续同时累加周期总量和区间量的唯一对象。
	record *repositorydto.CodexQuotaEfficiencyCycle
	// queryStart 遵循周期理论起点及相邻周期切分边界，不用首次观察时间冒充周期开始。
	queryStart time.Time
	// queryEnd 对已结束周期等于角色有效终点，对当前周期固定截到 GeneratedAt。
	queryEnd time.Time
}

// codexQuotaEfficiencyCyclePeriod 保存一个角色周期在查询层实际生效的半开时间边界。
type codexQuotaEfficiencyCyclePeriod struct {
	start   time.Time
	end     time.Time
	current bool
}

// codexQuotaEfficiencySegmentRow 仅保留百分比摘要和下降区间所需的持久字段。
type codexQuotaEfficiencySegmentRow struct {
	ID               int64
	CycleID          int64
	RemainingPercent int
	FirstObservedAt  codexQuotaEfficiencyObservedTime
	LastObservedAt   codexQuotaEfficiencyObservedTime
	ObservationCount int64
}

// codexQuotaEfficiencyUsageEventRow 只保存周期和区间累计所需的已存请求事实。
type codexQuotaEfficiencyUsageEventRow struct {
	Timestamp           time.Time
	Failed              bool
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	CostUSD             float64
	CostAvailable       bool
}

// BuildCodexQuotaEfficiencyHistory 将额度历史与已存 UsageEvent 费用按真实周期和下降区间关联，不改写历史表。
func BuildCodexQuotaEfficiencyHistory(ctx context.Context, db *gorm.DB, query repositorydto.CodexQuotaEfficiencyQuery) (repositorydto.CodexQuotaEfficiencyHistory, error) {
	// 先构造空响应，使“账号暂时没有历史”仍返回稳定的时间口径。
	result := repositorydto.CodexQuotaEfficiencyHistory{GeneratedAt: query.Now, RangeStart: query.RangeStart}
	if db == nil {
		return result, fmt.Errorf("build codex quota efficiency history: database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// auth_index 必须精确限定 UsageEvent；空值会退化成跨账号全表扫描，因此直接拒绝。
	query.AuthIndex = strings.TrimSpace(query.AuthIndex)
	query.Provider = strings.ToLower(strings.TrimSpace(query.Provider))
	if query.Provider == "" {
		query.Provider = "codex"
	}
	if _, ok := repositorydto.QuotaWindowKey(query.Provider, "primary"); !ok {
		return result, fmt.Errorf("unsupported quota history provider %q", query.Provider)
	}
	if query.AuthIndex == "" {
		return result, fmt.Errorf("build codex quota efficiency history: auth_index is required")
	}
	if query.Now.IsZero() || query.RangeStart.IsZero() || !query.RangeStart.Before(query.Now) {
		return result, fmt.Errorf("build codex quota efficiency history: invalid time range")
	}
	// 整个响应固定同一 instant；后续当前周期判断、查询截点和 JSON 时间都复用它。
	query.Now = timeutil.NormalizeStorageTime(query.Now)
	query.RangeStart = timeutil.NormalizeStorageTime(query.RangeStart)
	result.GeneratedAt = query.Now
	result.RangeStart = query.RangeStart

	// 父表时间使用 sortableTime，SQL 参数也必须用固定宽度 UTC 文本才能保持 instant 顺序。
	var cycles []entities.QuotaCycle
	err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Where("provider = ? AND auth_index = ? AND reset_at >= ? AND window_started_at < ?", query.Provider, query.AuthIndex, timeutil.FormatSortableStorageTime(query.RangeStart), timeutil.FormatSortableStorageTime(query.Now)).
		Order("reset_at DESC, id DESC").
		Find(&cycles).Error
	if err != nil {
		return result, fmt.Errorf("list codex quota efficiency cycles: %w", err)
	}
	if len(cycles) == 0 {
		return result, nil
	}

	// 一次父表结果同时生成窗口选项，避免切换器为同一批数据再执行一条 distinct 查询。
	result.Windows = buildQuotaEfficiencyWindows(cycles, query.Provider, query.Now)
	selected := selectCodexQuotaEfficiencyWindow(result.Windows, query.WindowRole)
	if selected == nil {
		return result, nil
	}
	selectedCopy := *selected
	result.SelectedWindow = &selectedCopy

	// 角色是稳定选择身份；同一个 Primary/Secondary 的 5h、Weekly、Monthly 周期共同进入完整历史。
	selectedCycles := make([]entities.QuotaCycle, 0, len(cycles))
	for _, cycle := range cycles {
		windowRole, ok := repositorydto.QuotaWindowRole(query.Provider, cycle.QuotaKey)
		if !ok || windowRole != selected.WindowRole {
			continue
		}
		selectedCycles = append(selectedCycles, cycle)
	}
	if len(selectedCycles) == 0 {
		return result, nil
	}
	currentCycleID := int64(0)
	if selected.HasCurrentCycle {
		currentCycleID = latestCodexQuotaEfficiencyCycleID(selectedCycles)
	}
	periods := buildCodexQuotaEfficiencyCyclePeriods(selectedCycles, currentCycleID, query.Now)

	// 完整父周期先决定窗口及相邻切分，随后只为最终可返回的周期读取子段。
	returnedCycles := selectedCycles[:0]
	cycleIDs := make([]int64, 0, len(selectedCycles))
	for _, cycle := range selectedCycles {
		period := periods[cycle.ID]
		if !period.start.Before(period.end) || period.end.Before(query.RangeStart) {
			continue
		}
		if !period.current && period.end.After(query.Now) {
			continue
		}
		returnedCycles = append(returnedCycles, cycle)
		cycleIDs = append(cycleIDs, cycle.ID)
	}
	if len(returnedCycles) == 0 {
		// 有父历史但无有效周期时，原响应保留非 nil 的空周期列表。
		result.Cycles = make([]repositorydto.CodexQuotaEfficiencyCycle, 0)
		return result, nil
	}

	// 所有子段用一次轻量 IN 查询读出，不允许对父周期逐条 Preload。
	// 一元 + 只绕过 DATETIME 声明解析，保留原值类型；排序限定原列，避免 alias 触发额外排序。
	var segments []codexQuotaEfficiencySegmentRow
	if err := db.WithContext(ctx).Clauses(dbresolver.Read).
		Table("quota_percent_segments").
		Select("id, cycle_id, remaining_percent, +first_observed_at AS first_observed_at, +last_observed_at AS last_observed_at, observation_count").
		Where("cycle_id IN ?", cycleIDs).
		Order("quota_percent_segments.cycle_id ASC, quota_percent_segments.first_observed_at ASC, quota_percent_segments.id ASC").
		Find(&segments).Error; err != nil {
		return result, fmt.Errorf("list codex quota efficiency segments: %w", err)
	}
	segmentsByCycle := make(map[int64][]codexQuotaEfficiencySegmentRow, len(returnedCycles))
	for start := 0; start < len(segments); {
		end := start + 1
		for end < len(segments) && segments[end].CycleID == segments[start].CycleID {
			end++
		}
		// SQL 已按周期及观察顺序排列；只读视图共享底层数组并限制容量，避免复制或跨组追加。
		segmentsByCycle[segments[start].CycleID] = segments[start:end:end]
		start = end
	}

	// 先建立全部指针对象再聚合；统一列表状态完全由本次固定的 now 判定。
	works := make([]codexQuotaEfficiencyCycleWork, 0, len(returnedCycles))
	records := make([]*repositorydto.CodexQuotaEfficiencyCycle, 0, len(returnedCycles))
	for _, cycle := range returnedCycles {
		period := periods[cycle.ID]
		active := period.current
		status := "completed"
		if active {
			status = "current"
		}
		firstPercent, lastPercent, observationCount := quotaCyclePercentSummary(segmentsByCycle[cycle.ID])
		record := &repositorydto.CodexQuotaEfficiencyCycle{
			ID:                    cycle.ID,
			Status:                status,
			WindowSeconds:         cycle.WindowSeconds,
			WindowStartedAt:       cycle.WindowStartedAt,
			ResetAt:               cycle.ResetAt,
			EffectiveStartedAt:    period.start,
			EffectiveEndedAt:      period.end,
			FirstObservedAt:       cycle.FirstObservedAt,
			LastObservedAt:        cycle.LastObservedAt,
			FirstRemainingPercent: firstPercent,
			LastRemainingPercent:  lastPercent,
			ObservationCount:      observationCount,
			Usage:                 repositorydto.CodexQuotaEfficiencyUsage{CostAvailable: true},
			Transitions:           buildCodexQuotaEfficiencyTransitions(segmentsByCycle[cycle.ID]),
		}
		queryEnd := period.end
		if active {
			queryEnd = query.Now
		}
		records = append(records, record)
		works = append(works, codexQuotaEfficiencyCycleWork{record: record, queryStart: period.start, queryEnd: queryEnd})
	}

	// 按时区段流式读取已存费用，首个额度观察前的请求仍计入周期。
	if err := streamCodexQuotaEfficiencyUsage(ctx, db, query.Provider, query.AuthIndex, works); err != nil {
		return result, err
	}
	// 流式聚合结束后再计算每百分点值，保证可用性已吸收区间内所有事件。
	for _, work := range works {
		finalizeCodexQuotaEfficiencyTransitions(work.record)
	}
	result.Cycles = make([]repositorydto.CodexQuotaEfficiencyCycle, 0, len(records))
	for _, record := range records {
		result.Cycles = append(result.Cycles, *record)
	}
	// 当前周期固定优先；历史按角色实际结束时间倒序，窗口切换不会继续沿用更远的原始 reset。
	sort.SliceStable(result.Cycles, func(left, right int) bool {
		if result.Cycles[left].Status != result.Cycles[right].Status {
			return result.Cycles[left].Status == "current"
		}
		return result.Cycles[left].EffectiveEndedAt.After(result.Cycles[right].EffectiveEndedAt)
	})
	return result, nil
}

func codexWindowRoleFromQuotaKey(quotaKey string) (string, bool) {
	switch quotaKey {
	case codexPrimaryQuotaKey:
		return string(entities.CodexQuotaWindowRolePrimary), true
	case codexSecondaryQuotaKey:
		return string(entities.CodexQuotaWindowRoleSecondary), true
	default:
		return "", false
	}
}

func codexQuotaEfficiencyWindowKind(windowSeconds int64) *string {
	var kind string
	switch windowSeconds {
	case 5 * 60 * 60:
		kind = string(entities.CodexQuotaWindowKindFiveHour)
	case 7 * 24 * 60 * 60:
		kind = string(entities.CodexQuotaWindowKindWeekly)
	case 30 * 24 * 60 * 60, 365 * 24 * 60 * 60 / 12:
		kind = string(entities.CodexQuotaWindowKindMonthly)
	default:
		return nil
	}
	return &kind
}

func quotaCyclePercentSummary(segments []codexQuotaEfficiencySegmentRow) (*int, *int, int64) {
	if len(segments) == 0 {
		return nil, nil, 0
	}
	first := segments[0].RemainingPercent
	last := segments[len(segments)-1].RemainingPercent
	var observationCount int64
	for _, segment := range segments {
		observationCount += segment.ObservationCount
	}
	return &first, &last, observationCount
}

func buildCodexQuotaEfficiencyWindows(cycles []entities.QuotaCycle, now time.Time) []repositorydto.CodexQuotaEfficiencyWindow {
	return buildQuotaEfficiencyWindows(cycles, "codex", now)
}

func buildQuotaEfficiencyWindows(cycles []entities.QuotaCycle, provider string, now time.Time) []repositorydto.CodexQuotaEfficiencyWindow {
	// 上游角色是稳定窗口身份；每个角色只保留最近一次观察到的周期长度作为选择器标题。
	latestCycleByRole := make(map[string]entities.QuotaCycle, 2)
	for _, cycle := range cycles {
		role, ok := repositorydto.QuotaWindowRole(provider, cycle.QuotaKey)
		if !ok {
			continue
		}
		latest, found := latestCycleByRole[role]
		if !found || cycle.LastObservedAt.After(latest.LastObservedAt) || (cycle.LastObservedAt.Equal(latest.LastObservedAt) && cycle.ID > latest.ID) {
			latestCycleByRole[role] = cycle
		}
	}
	windows := make([]repositorydto.CodexQuotaEfficiencyWindow, 0, len(latestCycleByRole))
	for role, cycle := range latestCycleByRole {
		// 历史入口只由该角色是否还有记录决定，删除或上游角色变化不能隐藏另一份历史。
		windows = append(windows, repositorydto.CodexQuotaEfficiencyWindow{
			WindowRole:      role,
			WindowKind:      codexQuotaEfficiencyWindowKind(cycle.WindowSeconds),
			WindowSeconds:   cycle.WindowSeconds,
			HasCurrentCycle: !now.Before(cycle.WindowStartedAt) && now.Before(cycle.ResetAt),
			LastObservedAt:  cycle.LastObservedAt,
		})
	}
	// 确定性顺序让前端键盘切换稳定：Primary 固定优先，Secondary 固定随后。
	sort.Slice(windows, func(left, right int) bool {
		return windows[left].WindowRole == string(entities.CodexQuotaWindowRolePrimary) && windows[right].WindowRole != string(entities.CodexQuotaWindowRolePrimary)
	})
	return windows
}

func selectCodexQuotaEfficiencyWindow(windows []repositorydto.CodexQuotaEfficiencyWindow, role *string) *repositorydto.CodexQuotaEfficiencyWindow {
	// 显式筛选只按上游角色匹配；周期长度变化不会创建第二个同角色选择项。
	if role != nil {
		for index := range windows {
			if windows[index].WindowRole != strings.ToLower(strings.TrimSpace(*role)) {
				continue
			}
			return &windows[index]
		}
		return nil
	}
	// 默认仍从最近一次观察的角色中选择；同次响应优先当前周期，再沿用 Primary 顺序。
	// 历史入口放宽后，即使最新角色已经到期，旧角色较远的 reset 也不能抢走默认选择。
	var selected *repositorydto.CodexQuotaEfficiencyWindow
	for index := range windows {
		if selected == nil || windows[index].LastObservedAt.After(selected.LastObservedAt) ||
			(windows[index].LastObservedAt.Equal(selected.LastObservedAt) && windows[index].HasCurrentCycle && !selected.HasCurrentCycle) {
			selected = &windows[index]
		}
	}
	return selected
}

// latestCodexQuotaEfficiencyCycleID 与窗口标题共享 LastObservedAt 口径，返回该角色最近被确认的父周期。
func latestCodexQuotaEfficiencyCycleID(cycles []entities.QuotaCycle) int64 {
	var latest entities.QuotaCycle
	for _, cycle := range cycles {
		if latest.ID == 0 || cycle.LastObservedAt.After(latest.LastObservedAt) || (cycle.LastObservedAt.Equal(latest.LastObservedAt) && cycle.ID > latest.ID) {
			latest = cycle
		}
	}
	return latest.ID
}

func buildCodexQuotaEfficiencyCyclePeriods(cycles []entities.QuotaCycle, currentCycleID int64, now time.Time) map[int64]codexQuotaEfficiencyCyclePeriod {
	// 最近观察顺序表达角色当前演进；复用旧父行后它必须排到中间错误周期之后。
	ordered := append([]entities.QuotaCycle(nil), cycles...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if !ordered[left].LastObservedAt.Equal(ordered[right].LastObservedAt) {
			return ordered[left].LastObservedAt.Before(ordered[right].LastObservedAt)
		}
		return ordered[left].ID < ordered[right].ID
	})
	periods := make(map[int64]codexQuotaEfficiencyCyclePeriod, len(ordered))
	// 反向维护所有后续周期中最早的理论起点；这样一次线性扫描就能覆盖多个连续 detour。
	var earliestLaterStart time.Time
	for index := len(ordered) - 1; index >= 0; index-- {
		cycle := ordered[index]
		periodEnd := cycle.ResetAt
		// 后续观察到的周期从其理论起点接管；此前周期只能保留到该起点之前。
		if !earliestLaterStart.IsZero() && earliestLaterStart.Before(periodEnd) {
			periodEnd = earliestLaterStart
		}
		periods[cycle.ID] = codexQuotaEfficiencyCyclePeriod{start: cycle.WindowStartedAt, end: periodEnd}
		// 更早周期需要同时避开当前周期和已经处理过的所有后续周期，因此只保留最早起点。
		if earliestLaterStart.IsZero() || cycle.WindowStartedAt.Before(earliestLaterStart) {
			earliestLaterStart = cycle.WindowStartedAt
		}
	}
	if len(ordered) == 0 {
		return periods
	}
	if currentCycleID == 0 {
		return periods
	}
	currentPeriod, found := periods[currentCycleID]
	if !found {
		return periods
	}
	currentPeriod.current = !now.Before(currentPeriod.start) && now.Before(currentPeriod.end)
	periods[currentCycleID] = currentPeriod
	return periods
}

func buildCodexQuotaEfficiencyTransitions(segments []codexQuotaEfficiencySegmentRow) []repositorydto.CodexQuotaEfficiencyTransition {
	transitions := make([]repositorydto.CodexQuotaEfficiencyTransition, 0, max(0, len(segments)-1))
	for index := 1; index < len(segments); index++ {
		previous := segments[index-1]
		current := segments[index]
		points := previous.RemainingPercent - current.RemainingPercent
		// 历史写入保证单调不升；查询层仍跳过异常非下降行，避免制造负效率样本。
		if points <= 0 {
			continue
		}
		transition := repositorydto.CodexQuotaEfficiencyTransition{
			FromRemainingPercent: previous.RemainingPercent,
			ToRemainingPercent:   current.RemainingPercent,
			PercentagePoints:     points,
			IsDirect:             points == 1,
			// 前一百分比首次出现后的请求共同消耗这一档额度；重复观察不能把区间起点向后推。
			IntervalStartedAt:     previous.FirstObservedAt.Time,
			IntervalEndedAt:       current.FirstObservedAt.Time,
			Usage:                 repositorydto.CodexQuotaEfficiencyUsage{CostAvailable: true},
			CostPerPointAvailable: true,
		}
		transitions = append(transitions, transition)
	}
	return transitions
}

// streamCodexQuotaEfficiencyUsage 按时间索引流式归属周期与区间，费用仅取事件持久列。
func streamCodexQuotaEfficiencyUsage(ctx context.Context, db *gorm.DB, provider string, authIndex string, works []codexQuotaEfficiencyCycleWork) error {
	if len(works) == 0 {
		return nil
	}
	// 周期边界保持真实时间有序，正常事件沿时间线前进，文本倒序事件可二分重新定位。
	sort.Slice(works, func(left, right int) bool {
		if !works[left].queryStart.Equal(works[right].queryStart) {
			return works[left].queryStart.Before(works[right].queryStart)
		}
		if !works[left].queryEnd.Equal(works[right].queryEnd) {
			return works[left].queryEnd.Before(works[right].queryEnd)
		}
		return works[left].record.ID < works[right].record.ID
	})
	globalStart := works[0].queryStart
	globalEnd := works[0].queryEnd
	for _, work := range works[1:] {
		if work.queryStart.Before(globalStart) {
			globalStart = work.queryStart
		}
		if work.queryEnd.After(globalEnd) {
			globalEnd = work.queryEnd
		}
	}

	cursor := codexQuotaEfficiencyUsageCursor{works: works}
	for _, read := range usageEventTimeReads(globalStart, globalEnd, false) {
		if err := streamCodexQuotaEfficiencyUsageRead(ctx, db, provider, authIndex, read, &cursor); err != nil {
			return err
		}
	}
	return nil
}

// addCodexQuotaEfficiencyUsage 只累计已存费用与原 Token，并使不可用状态逐事件传播。
func addCodexQuotaEfficiencyUsage(target *repositorydto.CodexQuotaEfficiencyUsage, event codexQuotaEfficiencyUsageEventRow) error {
	target.Requests++
	if event.Failed {
		target.FailedRequests++
	} else {
		target.SuccessfulRequests++
	}
	target.InputTokens += event.InputTokens
	target.OutputTokens += event.OutputTokens
	target.ReasoningTokens += event.ReasoningTokens
	target.CacheReadTokens += event.CacheReadTokens
	target.CacheCreationTokens += event.CacheCreationTokens
	target.TotalTokens += event.TotalTokens
	target.TotalCostUSD += event.CostUSD
	if math.IsInf(target.TotalCostUSD, 0) {
		return fmt.Errorf("stored cost overflow")
	}
	if !event.CostAvailable {
		target.CostAvailable = false
	}
	return nil
}

func finalizeCodexQuotaEfficiencyTransitions(cycle *repositorydto.CodexQuotaEfficiencyCycle) {
	for index := range cycle.Transitions {
		transition := &cycle.Transitions[index]
		points := float64(transition.PercentagePoints)
		transition.TokensPerPoint = float64(transition.Usage.TotalTokens) / points
		transition.CostPerPoint = transition.Usage.TotalCostUSD / points
		transition.CostPerPointAvailable = transition.Usage.CostAvailable
	}
}
