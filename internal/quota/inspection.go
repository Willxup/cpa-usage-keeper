package quota

import (
	"context"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/timeutil"
)

type InspectionResultStatus string

const (
	InspectionResultStatusNormal             InspectionResultStatus = "normal"
	InspectionResultStatusLimitReached       InspectionResultStatus = "limit_reached"
	InspectionResultStatusUnauthorized401    InspectionResultStatus = "unauthorized_401"
	InspectionResultStatusPaymentRequired402 InspectionResultStatus = "payment_required_402"
	InspectionResultStatusOtherFailed        InspectionResultStatus = "other_failed"
)

type InspectionStatus struct {
	// Total 来自当前仍有效且未禁用的 Auth Files 身份数量，不来自刷新缓存。
	Total int `json:"total"`
	// Cached 表示已经能从共享刷新缓存里解析出巡检结果的数量。
	Cached int `json:"cached"`
	// Running 只表示用户显式启动的巡检轮次仍有 queued/running 任务。
	Running bool `json:"running"`
	// Completed 只表示显式巡检轮次完成；普通手动刷新和自动刷新不能置 true。
	Completed bool `json:"completed"`
	// CompletedAt 是显式巡检轮次的完成时间，用于前端绿色小点和“上次巡检时间”。
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	Normal             int        `json:"normal"`
	LimitReached       int        `json:"limit_reached"`
	Unauthorized401    int        `json:"unauthorized_401"`
	PaymentRequired402 int        `json:"payment_required_402"`
	Unauthorized401402 int        `json:"unauthorized_401_402"`
	OtherFailed        int        `json:"other_failed"`
	// Unknown 是 active Auth Files 中“没有可展示结果、也没有参与当前巡检刷新”的剩余数量。
	Unknown int                `json:"unknown"`
	Results []InspectionResult `json:"results"`
}

type InspectionResult struct {
	AuthIndex      string                 `json:"auth_index"`
	Name           string                 `json:"name"`
	Type           string                 `json:"type"`
	FileName       *string                `json:"file_name,omitempty"`
	Status         InspectionResultStatus `json:"status"`
	Error          string                 `json:"error,omitempty"`
	HTTPStatusCode *int                   `json:"http_status_code,omitempty"`
	RefreshedAt    *time.Time             `json:"refreshed_at,omitempty"`
}

func (s *Service) StartInspection(ctx context.Context) (InspectionStatus, error) {
	// service 或数据库未初始化时直接返回空状态，保持 API 层的空安全语义。
	if s == nil || s.db == nil {
		return InspectionStatus{}, nil
	}
	// now 作为本轮启动时间，后续清理过期任务和扫描 Auth Files 都复用同一时间点。
	now := time.Now()
	// 巡检开始前先清掉已完成/已失败的旧任务，避免上轮缓存立刻把本轮算成完成。
	s.clearSettledRefreshTasks()
	// 重置 completed_at 和本轮 auth_index 集合；只有这次按钮触发的任务能写回巡检完成时间。
	s.resetInspectionRound()
	// 过期的短期失败任务可以在本轮重新入队，未过期的长期成功缓存已在上一步清掉。
	s.cleanupExpiredRefreshTasks(now)
	// 巡检复用 Auth Files 自动刷新扫描规则，但 source 必须标成 inspection 以便后续区分运行态。
	summary, err := s.queueAuthFileRefreshRound(ctx, now, authFileRefreshRoundOptions{
		// source 是巡检状态机的边界：只有 inspection 轮次中的任务能驱动 running/completed_at。
		source: RefreshSourceInspection,
		// 巡检是用户显式检查，401/402 这类缓存错误也要重新尝试，不能被自动刷新缓存拦住。
		skipCachedHTTPError: false,
	})
	if err != nil {
		return InspectionStatus{}, err
	}
	// 记录本轮参与巡检的 auth_index：包含新入队任务和同来源 inspection active 任务，不包含 manual/auto/unsupported。
	s.setInspectionRoundAuthIndexes(summary.roundAuthIndexes)
	// 没有新任务时也要返回状态；这可能代表全部 unsupported、或已有任务正在被本轮复用。
	if len(summary.queuedAuthIndexes) > 0 {
		// dispatcher 会按全局 worker 限制派发，避免一次巡检把所有 provider 同时打满。
		if !s.startRefreshGoroutine(func() {
			s.dispatchRefreshTasks(summary.queuedAuthIndexes)
		}) {
			// 应用关闭期间无法启动后台 goroutine 时，queued 任务必须失败，否则前端会一直轮询。
			s.markQueuedRefreshTasksFailed(summary.queuedAuthIndexes, context.Canceled)
		}
	}
	// 立即读一次状态，给前端返回 total/running/unknown 等首屏数据。
	return s.GetInspectionStatus(ctx)
}

func (s *Service) GetInspectionStatus(ctx context.Context) (InspectionStatus, error) {
	// service 或数据库未初始化时保持空状态，不让调用方额外判断 nil。
	if s == nil || s.db == nil {
		return InspectionStatus{}, nil
	}
	// total 必须来自 UsageIdentity 中仍有效的 Auth Files，不能从刷新缓存反推。
	identities, err := s.listAutoRefreshAuthFiles(ctx)
	if err != nil {
		return InspectionStatus{}, err
	}
	// Total 先落定，后续 cached/unknown 都围绕这批身份计算。
	status := InspectionStatus{Total: len(identities)}

	// 读状态前清理过期短期任务，避免过期失败缓存继续影响 unknown/result 分类。
	s.cleanupExpiredRefreshTasks(time.Now())
	// refreshTasks、inspectionRoundAuthIndexSet、inspectionCompletedAt 共用这把锁保护。
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	// activeInspectionTasks 只统计当前显式巡检轮次还没完成的任务，手动/自动任务不进入这里。
	activeInspectionTasks := 0
	// 逐个 Auth File 身份对照共享刷新缓存；没有缓存的身份最后会落到 unknown。
	for _, identity := range identities {
		// 刷新任务在入队时会 trim auth_index；状态读取也必须使用同一口径，否则旧数据或绕过写入层的数据会查不到缓存。
		authIndex := strings.TrimSpace(identity.Identity)
		if authIndex == "" {
			// 空 identity 属于异常身份数据，仍保留在 Total 中，最终由 unknown 暴露出来。
			continue
		}
		// identity 是 range 副本，改成归一化值只影响本次结果组装，不会反写数据库。
		identity.Identity = authIndex
		task, ok := s.refreshTasks[authIndex]
		if !ok {
			// 没有任何缓存或任务时暂不计数，统一由 unknown 兜底。
			continue
		}
		if task.isActive() {
			// running 只表示用户显式启动的巡检轮次仍有任务在跑；手动/自动刷新不污染巡检状态。
			if s.inspectionRoundActiveTaskLocked(authIndex, task) {
				// 参与当前巡检的 active task 已经是“处理中”，不能再算 unknown。
				activeInspectionTasks++
			}
			// active task 尚无稳定 quota/error 结果，不能进入 Results 列表。
			continue
		}
		// completed/failed 的缓存才可能被转换成巡检最近结果。
		result, ok := inspectionResultForTask(identity, task)
		if !ok {
			// completed 但没有 quota、或其它无展示意义状态，仍交给 unknown 统计。
			continue
		}
		// 只有能产出巡检结果的缓存记录才进入 cached；未刷新或 unsupported 统一留给 unknown。
		status.Cached++
		// cached 结果根据巡检状态拆分统计卡片；limit_reached 会从 normal 中单独扣出来。
		switch result.Status {
		case InspectionResultStatusNormal:
			status.Normal++
		case InspectionResultStatusLimitReached:
			status.LimitReached++
		case InspectionResultStatusUnauthorized401:
			status.Unauthorized401++
			// 摘要口径把 401/402 合并，行级结果仍保留原始状态。
			status.Unauthorized401402++
		case InspectionResultStatusPaymentRequired402:
			status.PaymentRequired402++
			status.Unauthorized401402++
		case InspectionResultStatusOtherFailed:
			status.OtherFailed++
		}
		// Results 保留最近可展示的身份级结果，排序在循环后统一处理。
		status.Results = append(status.Results, result)
	}
	// 最近结果按刷新时间倒序排列；同一时间按 auth_index 稳定排序，避免列表跳动。
	sortInspectionResults(status.Results)
	// 只有显式巡检任务能驱动 Running；共享刷新池里其它 active task 不影响巡检按钮。
	status.Running = activeInspectionTasks > 0
	// unknown 不参与进度条分母；activeInspectionTasks 是本轮可巡检任务，不能被算未知。
	status.Unknown = status.Total - status.Cached - activeInspectionTasks
	if status.Unknown < 0 {
		// 防御重复缓存或异常状态导致的负数，前端永远只接收非负统计。
		status.Unknown = 0
	}
	if !status.Running && s.inspectionRoundCompletedLocked() {
		// completed 只在没有巡检 active task 且本轮确实已经开始过时才为 true。
		status.Completed = true
		// completed_at 是显式巡检轮次的一部分：第一次观察到本轮刷新完成时记录，后续轮询保持稳定。
		if s.inspectionCompletedAt.IsZero() {
			// NormalizeStorageTime 让内存时间和数据库/前端展示保持同一时间口径。
			s.inspectionCompletedAt = timeutil.NormalizeStorageTime(time.Now())
		}
		// 复制一份再取地址，避免把内部状态指针直接暴露给调用方。
		completedAt := s.inspectionCompletedAt
		status.CompletedAt = &completedAt
	}
	return status, nil
}

func (s *Service) inspectionRoundActiveTaskLocked(authIndex string, task *RefreshTaskRecord) bool {
	// 调用方已持有 refreshMu；这里不再加锁，避免同一 goroutine 重入死锁。
	if task == nil {
		return false
	}
	if task.Source != RefreshSourceInspection || !task.isActive() {
		return false
	}
	if !s.inspectionRoundActive || !s.inspectionCompletedAt.IsZero() {
		// 已完成的巡检轮次不能被后续手动/自动 active task 重新点亮 running。
		return false
	}
	// 只有 StartInspection 记录到本轮集合中的 auth_index 才能代表巡检运行中。
	_, ok := s.inspectionRoundAuthIndexSet[authIndex]
	return ok
}

func (s *Service) clearSettledRefreshTasks() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	for authIndex, task := range s.refreshTasks {
		if task == nil || task.isActive() {
			continue
		}
		delete(s.refreshTasks, authIndex)
	}
}

func (s *Service) resetInspectionRound() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.inspectionCompletedAt = time.Time{}
	s.inspectionRoundActive = false
	s.inspectionRoundAuthIndexSet = nil
}

func (s *Service) resetInspectionCompletedAt() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.inspectionCompletedAt = time.Time{}
}

func (s *Service) setInspectionRoundAuthIndexes(authIndexes []string) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.inspectionRoundActive = true
	s.inspectionRoundAuthIndexSet = make(map[string]struct{}, len(authIndexes))
	for _, authIndex := range authIndexes {
		s.inspectionRoundAuthIndexSet[authIndex] = struct{}{}
	}
}

func (s *Service) inspectionRoundCompletedLocked() bool {
	if !s.inspectionRoundActive {
		return false
	}
	if len(s.inspectionRoundAuthIndexSet) == 0 {
		return false
	}
	if !s.inspectionCompletedAt.IsZero() {
		return true
	}
	for authIndex := range s.inspectionRoundAuthIndexSet {
		task, ok := s.refreshTasks[authIndex]
		if ok && task.Source == RefreshSourceInspection && task.isActive() {
			return false
		}
	}
	return true
}

func inspectionResultForTask(identity entities.UsageIdentity, task *RefreshTaskRecord) (InspectionResult, bool) {
	// nil task 没有任何可读缓存，调用方会把该身份留给 unknown。
	if task == nil {
		return InspectionResult{}, false
	}
	// 展示字段优先使用入队时的身份快照，避免刷新过程中用户改名导致最近结果跳动。
	result := InspectionResult{
		AuthIndex:      identity.Identity,
		Name:           firstNonEmpty(strings.TrimSpace(task.Name), helper.UsageIdentityDisplayName(identity)),
		Type:           firstNonEmpty(task.Type, identity.Type),
		FileName:       task.FileName,
		Error:          task.Error,
		HTTPStatusCode: task.HTTPStatusCode,
	}
	if !task.RefreshedAt.IsZero() {
		// RefreshedAt 只在任务进入 completed/failed 后出现，用于最近结果排序和展示。
		refreshedAt := task.RefreshedAt
		result.RefreshedAt = &refreshedAt
	}
	// 巡检最近结果只接受稳定终态：completed 或 failed；queued/running 由 Running/进度处理。
	switch task.Status {
	case RefreshTaskStatusCompleted:
		if task.Quota == nil {
			// completed 但缺 quota 是不可展示缓存，不能误报 normal。
			return InspectionResult{}, false
		}
		// 到达限额不是通用判断，各 provider 的字段语义不同，必须走专用解析。
		if inspectionQuotaLimitReached(identity, task, task.Quota.Quota) {
			result.Status = InspectionResultStatusLimitReached
			return result, true
		}
		// completed 且没有触发 provider-specific 限额判断时，才算 normal。
		result.Status = InspectionResultStatusNormal
		return result, true
	case RefreshTaskStatusFailed:
		// failed 只按 HTTP 状态细分 401/402，其余错误统一归到 other_failed。
		result.Status = inspectionFailedResultStatus(task.HTTPStatusCode)
		return result, true
	default:
		// 其它状态不产出最近结果，避免 active 任务提前出现在结果列表。
		return InspectionResult{}, false
	}
}

func inspectionFailedResultStatus(statusCode *int) InspectionResultStatus {
	if statusCode == nil {
		return InspectionResultStatusOtherFailed
	}
	switch *statusCode {
	case 401:
		return InspectionResultStatusUnauthorized401
	case 402:
		return InspectionResultStatusPaymentRequired402
	default:
		return InspectionResultStatusOtherFailed
	}
}

func inspectionQuotaLimitReached(identity entities.UsageIdentity, task *RefreshTaskRecord, rows []QuotaRow) bool {
	// provider 类型只认已知 Auth File 类型；generic/unknown 不做猜测，避免误判。
	switch inspectionQuotaProvider(identity, task) {
	case "codex":
		return codexInspectionLimitReached(rows)
	case "claude":
		return claudeInspectionLimitReached(rows)
	case "gemini-cli":
		return geminiCLIInspectionLimitReached(rows)
	case "antigravity":
		return antigravityInspectionLimitReached(rows)
	case "kimi":
		return kimiInspectionLimitReached(rows)
	case "xai":
		return xaiInspectionLimitReached(rows)
	default:
		return false
	}
}

func inspectionQuotaProvider(identity entities.UsageIdentity, task *RefreshTaskRecord) string {
	// task.Type 是入队快照，优先于当前 identity.Type，保证一次巡检内展示和判断一致。
	var taskType string
	if task != nil {
		taskType = task.Type
	}
	// 只从 type 字段识别已知 provider，不从 provider 字段 fallback 到可能不可信的通用值。
	for _, value := range []string{taskType, identity.Type} {
		normalized := strings.ToLower(strings.TrimSpace(value))
		switch normalized {
		case "antigravity", "codex", "gemini-cli", "claude", "kimi", "xai":
			return normalized
		}
	}
	return ""
}

func codexInspectionLimitReached(rows []QuotaRow) bool {
	// Codex 既可能显式给 limit_reached，也可能只给 used_percent。
	for _, row := range rows {
		if quotaRowLimitReached(row) || quotaRowUsedPercentAtLeast(row, 100) {
			return true
		}
	}
	return false
}

func claudeInspectionLimitReached(rows []QuotaRow) bool {
	// Claude 当前以窗口利用率为主，100% 及以上视为达到限额。
	for _, row := range rows {
		if quotaRowUsedPercentAtLeast(row, 100) {
			return true
		}
	}
	return false
}

func geminiCLIInspectionLimitReached(rows []QuotaRow) bool {
	// Gemini CLI 当前常见字段是剩余额度或剩余比例，任一归零都视为达到限额。
	for _, row := range rows {
		if quotaRowRemainingFractionAtMost(row, 0) || quotaRowRemainingAtMost(row, 0) {
			return true
		}
	}
	return false
}

func antigravityInspectionLimitReached(rows []QuotaRow) bool {
	// Antigravity 和 Gemini 类似，按剩余比例/剩余额度归零判断。
	for _, row := range rows {
		if quotaRowRemainingFractionAtMost(row, 0) || quotaRowRemainingAtMost(row, 0) {
			return true
		}
	}
	return false
}

func kimiInspectionLimitReached(rows []QuotaRow) bool {
	// Kimi 当前可能给 used/limit，也可能给 remaining；两种格式分别判断。
	for _, row := range rows {
		if quotaRowUsedAtLimit(row) || quotaRowRemainingAtMost(row, 0) {
			return true
		}
	}
	return false
}

func xaiInspectionLimitReached(rows []QuotaRow) bool {
	// 新 xAI 行的显式 false 代表仍有其它额度（例如 PAYG）；旧缓存没有标志时才回退 used/limit。
	for _, row := range rows {
		if row.LimitReached != nil {
			if *row.LimitReached {
				return true
			}
			continue
		}
		if quotaRowUsedAtLimit(row) {
			return true
		}
	}
	return false
}

func quotaRowLimitReached(row QuotaRow) bool {
	return row.LimitReached != nil && *row.LimitReached
}

func quotaRowUsedPercentAtLeast(row QuotaRow, threshold float64) bool {
	return row.UsedPercent != nil && *row.UsedPercent >= threshold
}

func quotaRowRemainingFractionAtMost(row QuotaRow, threshold float64) bool {
	return row.RemainingFraction != nil && *row.RemainingFraction <= threshold
}

func quotaRowRemainingAtMost(row QuotaRow, threshold float64) bool {
	return row.Remaining != nil && *row.Remaining <= threshold
}

func quotaRowUsedAtLimit(row QuotaRow) bool {
	return row.Used != nil && row.Limit != nil && *row.Limit > 0 && *row.Used >= *row.Limit
}

func sortInspectionResults(results []InspectionResult) {
	sort.SliceStable(results, func(i, j int) bool {
		left := results[i].RefreshedAt
		right := results[j].RefreshedAt
		switch {
		case left == nil && right == nil:
			return results[i].AuthIndex < results[j].AuthIndex
		case left == nil:
			return false
		case right == nil:
			return true
		default:
			if left.Equal(*right) {
				return results[i].AuthIndex < results[j].AuthIndex
			}
			return left.After(*right)
		}
	})
}
