package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

type RefreshSource string

const (
	RefreshSourceManual        RefreshSource = "manual"
	RefreshSourceAuto          RefreshSource = "auto"
	RefreshSourceInspection    RefreshSource = "inspection"
	RefreshSourceScheduled     RefreshSource = "scheduled"
	RefreshSourceCacheBackfill RefreshSource = "cache_backfill"
)

type RefreshTaskStatus string

const (
	RefreshTaskStatusQueued    RefreshTaskStatus = "queued"
	RefreshTaskStatusRunning   RefreshTaskStatus = "running"
	RefreshTaskStatusCompleted RefreshTaskStatus = "completed"
	RefreshTaskStatusFailed    RefreshTaskStatus = "failed"
)

type CacheRequest struct {
	AuthIndexes []string `json:"auth_indexes"`
}

type CacheResponse struct {
	Items []CachedQuotaItem `json:"items"`
}

type CachedQuotaItem struct {
	AuthIndex      string            `json:"auth_index"`
	FileName       *string           `json:"file_name,omitempty"`
	Status         RefreshTaskStatus `json:"status"`
	Quota          *CheckResponse    `json:"quota,omitempty"`
	Error          string            `json:"error,omitempty"`
	HTTPStatusCode *int              `json:"http_status_code,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	RefreshedAt    *time.Time        `json:"refreshed_at,omitempty"`
}

type RefreshRequest struct {
	AuthIndexes []string      `json:"auth_indexes"`
	Source      RefreshSource `json:"source"`
}

type RefreshResponse struct {
	Tasks    []RefreshTaskRef           `json:"tasks"`
	Rejected []RefreshRejectedAuthIndex `json:"rejected"`
	Accepted int                        `json:"accepted"`
	Skipped  int                        `json:"skipped"`
	Limit    int                        `json:"limit"`
}

type RefreshTaskRef struct {
	AuthIndex string `json:"authIndex"`
}

type RefreshRejectedAuthIndex struct {
	AuthIndex string `json:"authIndex"`
	Error     string `json:"error"`
}

type RefreshTaskResponse struct {
	AuthIndex      string            `json:"authIndex"`
	FileName       *string           `json:"file_name,omitempty"`
	Status         RefreshTaskStatus `json:"status"`
	Quota          *CheckResponse    `json:"quota,omitempty"`
	Error          string            `json:"error,omitempty"`
	HTTPStatusCode *int              `json:"http_status_code,omitempty"`
	RefreshedAt    *time.Time        `json:"refreshed_at,omitempty"`
	ExpiresAt      *time.Time        `json:"expiresAt,omitempty"`
}

type RefreshTaskRecord struct {
	AuthIndex      string
	Name           string
	Type           string
	FileName       *string
	Status         RefreshTaskStatus
	Quota          *CheckResponse
	Error          string
	HTTPStatusCode *int
	Source         RefreshSource
	CreatedAt      time.Time
	StartedAt      time.Time
	RefreshedAt    time.Time
	ExpiresAt      time.Time
}

func (s *Service) GetCachedQuota(ctx context.Context, request CacheRequest) (CacheResponse, error) {
	_ = ctx
	// 缓存读取只返回已完成任务的结果，不触发新的 provider 请求。
	if len(request.AuthIndexes) == 0 {
		return CacheResponse{}, fmt.Errorf("%w: auth_indexes are required", ErrValidation)
	}
	response := CacheResponse{Items: make([]CachedQuotaItem, 0, len(request.AuthIndexes))}
	s.cleanupExpiredRefreshTasks(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	// 按请求顺序去重并读取每个 auth_index 最近一次完成的任务缓存。
	seen := make(map[string]struct{}, len(request.AuthIndexes))
	for _, rawAuthIndex := range request.AuthIndexes {
		authIndex := strings.TrimSpace(rawAuthIndex)
		if authIndex == "" {
			continue
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		task, ok := s.refreshTasks[authIndex]
		if !ok {
			continue
		}
		// 页面恢复缓存只暴露两类稳定状态：成功 quota 和配置允许持久展示的 HTTP 错误。
		// 普通网络错误/500/超时只给当前轮询读取，不从 cache 接口恢复，避免刷新页面后展示不可长期判断的瞬时失败。
		switch {
		case task.Status == RefreshTaskStatusCompleted && task.Quota != nil:
			quota := *task.Quota
			refreshedAt := task.RefreshedAt
			response.Items = append(response.Items, CachedQuotaItem{AuthIndex: authIndex, FileName: task.FileName, Status: RefreshTaskStatusCompleted, Quota: &quota, RefreshedAt: &refreshedAt})
		case task.Status == RefreshTaskStatusFailed && task.HTTPStatusCode != nil && isRefreshCacheableHTTPStatus(*task.HTTPStatusCode):
			expiresAt := task.ExpiresAt
			refreshedAt := task.RefreshedAt
			response.Items = append(response.Items, CachedQuotaItem{AuthIndex: authIndex, FileName: task.FileName, Status: RefreshTaskStatusFailed, Error: task.Error, HTTPStatusCode: task.HTTPStatusCode, ExpiresAt: &expiresAt, RefreshedAt: &refreshedAt})
		}
	}
	return response, nil
}

func (s *Service) Refresh(ctx context.Context, request RefreshRequest) (RefreshResponse, error) {
	// 刷新入口只负责校验、去重、建任务；实际 provider 调用交给后台 worker。
	// limit 保存本次请求传入的 auth_index 数量，用于响应和容量预估。
	limit := len(request.AuthIndexes)
	// 没有 auth_index 时无法创建任何刷新任务，直接返回校验错误。
	if limit <= 0 {
		// 返回 validation error，让 API 层按统一错误格式响应。
		return RefreshResponse{}, fmt.Errorf("%w: auth_indexes are required", ErrValidation)
	}
	// response 先记录 Limit，后续循环逐步填充 accepted/skipped/tasks/rejected。
	response := RefreshResponse{Limit: limit}
	// seen 记录本次请求内已经处理过的 auth_index，避免一个请求内重复入队。
	seen := make(map[string]struct{}, len(request.AuthIndexes))
	// queuedAuthIndexes 收集本次真正入队的任务，循环结束后交给单个 dispatcher 派发。
	queuedAuthIndexes := make([]string, 0, len(request.AuthIndexes))
	// 创建新任务前先清理过期缓存，避免旧失败/瞬时任务占住同一个 auth_index。
	s.cleanupExpiredRefreshTasks(time.Now())

	// 逐个处理前端或自动刷新传入的 auth_index。
	for _, rawAuthIndex := range request.AuthIndexes {
		// 每个 auth_index 独立生成任务，便于前端逐行轮询和展示错误。
		// auth_index 先 trim，避免输入空白导致查不到身份或重复入队。
		authIndex := strings.TrimSpace(rawAuthIndex)
		// 空 auth_index 没有业务含义，直接记为 invalid。
		if authIndex == "" {
			// rejected 使用规范化后的 auth_index，前端可以直接对应到失败项。
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "invalid"})
			// 当前项处理完毕，继续看下一项。
			continue
		}
		// 如果本次请求里已经出现过该 auth_index，就拒绝重复项。
		if _, ok := seen[authIndex]; ok {
			// duplicate_request 表示同一个请求体里重复提交，不代表后端已有可轮询任务。
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "duplicate_request"})
			// 当前项处理完毕，继续看下一项。
			continue
		}
		// 记录该 auth_index 已经在本次请求中出现过。
		seen[authIndex] = struct{}{}
		// Accepted 理论上不会超过 limit，这里保留防御避免响应计数越界。
		if response.Accepted >= limit {
			// 超出限制时按 invalid 处理，不创建后台任务。
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "invalid"})
			// 当前项处理完毕，继续看下一项。
			continue
		}
		// 已有 queued/running 任务时优先按 duplicate 返回，避免身份刚删除时把“正在刷新”误报成 not_found。
		if s.hasActiveRefreshTaskByAuthIndex(authIndex) {
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "duplicate"})
			continue
		}
		// 入队前先确认 auth_index 对应有效 auth file，并且 provider 支持 quota 查询。
		if identity, rejection, err := s.validateRefreshAuthIndex(ctx, authIndex); err != nil {
			// 数据库错误等非业务拒绝直接返回，避免继续创建不可靠任务。
			return RefreshResponse{}, err
		} else if rejection != "" {
			// 业务拒绝写入 rejected，常见原因是 not_found/not_auth_file/unsupported。
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: rejection})
			// 当前项处理完毕，继续看下一项。
			continue
		} else {
			// ensureRefreshTask 负责在锁内检查 queued/running 并创建新的 queued 任务。
			task, created := s.ensureRefreshTaskWithIdentity(authIndex, request.Source, identity)
			// created 为 false 表示同一 auth_index 已经 queued/running，不能重复入队。
			if !created {
				// duplicate 表示已有任务会产出结果，当前请求不再创建第二个任务。
				response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "duplicate"})
				// 当前项处理完毕，继续看下一项。
				continue
			}
			// 返回任务引用只暴露 auth_index，前端后续也按 auth_index 轮询。
			response.Tasks = append(response.Tasks, RefreshTaskRef{AuthIndex: task.AuthIndex})
			// Accepted 记录实际新建并准备派发的任务数。
			response.Accepted++
			// 把任务放入本次派发列表，避免为每个等待 worker slot 的任务都创建阻塞 goroutine。
			queuedAuthIndexes = append(queuedAuthIndexes, task.AuthIndex)
		}
	}
	// 如果本次有任务入队，就启动一个 dispatcher 顺序等待 worker slot 并派发实际 worker。
	if len(queuedAuthIndexes) > 0 {
		// dispatcher 自身只有一个 goroutine，大批量自动刷新不会产生“每个任务一个阻塞 goroutine”。
		if !s.startRefreshGoroutine(func() {
			s.dispatchRefreshTasks(queuedAuthIndexes)
		}) {
			// App 关闭期间不再启动 dispatcher，已创建的 queued 任务要快速失败，避免前端无限等待。
			s.markQueuedRefreshTasksFailed(queuedAuthIndexes, context.Canceled)
		}
	}
	// Skipped 直接等于 rejected 数量，表示本次未入队的项。
	response.Skipped = len(response.Rejected)
	// 返回入队结果；后台任务完成后由轮询/cache 接口读取。
	return response, nil
}

func (s *Service) hasActiveRefreshTaskByAuthIndex(authIndex string) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	task, ok := s.refreshTasks[authIndex]
	return ok && task.isActive()
}

func (s *Service) GetRefreshTaskByAuthIndex(ctx context.Context, authIndex string) (RefreshTaskResponse, error) {
	_ = ctx
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return RefreshTaskResponse{}, fmt.Errorf("%w: auth_index is required", ErrValidation)
	}
	s.cleanupExpiredRefreshTasks(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	task, ok := s.refreshTasks[authIndex]
	if !ok {
		return RefreshTaskResponse{}, ErrTaskNotFound
	}
	return task.response(), nil
}

func (s *Service) validateRefreshAuthIndex(ctx context.Context, authIndex string) (entities.UsageIdentity, string, error) {
	// 先按 auth-file 身份查找；查不到时再区分“非 auth file”和“不存在”。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err == nil {
		if _, _, ok := s.resolveQuotaHandlerForIdentity(identity); !ok {
			return identity, "unsupported", nil
		}
		return identity, "", nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return entities.UsageIdentity{}, "", err
	}

	var active entities.UsageIdentity
	if err := s.db.WithContext(ctx).Select("id, auth_type").Where("identity = ? AND is_deleted = ?", authIndex, false).First(&active).Error; err == nil {
		return entities.UsageIdentity{}, "not_auth_file", nil
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return entities.UsageIdentity{}, "not_found", nil
	} else {
		return entities.UsageIdentity{}, "", err
	}
}

func (s *Service) ensureRefreshTask(authIndex string, source RefreshSource) (*RefreshTaskRecord, bool) {
	return s.ensureRefreshTaskWithIdentity(authIndex, source, entities.UsageIdentity{Identity: authIndex})
}

func (s *Service) ensureRefreshTaskWithIdentity(authIndex string, source RefreshSource, identity entities.UsageIdentity) (*RefreshTaskRecord, bool) {
	// auth_index 本身就是任务唯一标识；queued/running 时直接拒绝重复入队，避免重复打到上游接口。
	// now 使用 storage time 归一化，保证任务时间字段和数据库/前端时间口径一致。
	now := timeutil.NormalizeStorageTime(time.Now())
	// refreshTasks 是共享 map，读写前必须加锁。
	s.refreshMu.Lock()
	// 函数退出时释放锁，确保创建任务和重复检查是原子操作。
	defer s.refreshMu.Unlock()
	// 如果同一个 auth_index 已经 queued/running，就复用现有记录并告知调用方未创建。
	if task, ok := s.refreshTasks[authIndex]; ok && task.isActive() {
		// 返回 false 表示当前请求不应该再启动 goroutine。
		return task, false
	}
	// 创建 queued 状态任务，后续 runRefreshTask 会把它切到 running。
	task := &RefreshTaskRecord{
		// AuthIndex 是任务唯一 key，也是前端轮询 key。
		AuthIndex: authIndex,
		// 展示字段来自入队时的身份快照，巡检弹框读取缓存时无需逐条回查 identity。
		Name: identity.Name,
		Type: identity.Type,
		// FileName 是 CPA auth-files 的原始 name，后续删除功能不能复用展示名。
		FileName: identity.FileName,
		// Status 初始为 queued，表示已经入队但尚未占用 worker。
		Status: RefreshTaskStatusQueued,
		// Source 记录任务来源，便于区分手动刷新和自动刷新。
		Source: source,
		// CreatedAt 记录入队时间，便于前端和清理逻辑判断任务生命周期。
		CreatedAt: now,
	}
	// 把任务写入 auth_index keyed map，后续轮询和 cache 都从这里读取。
	s.refreshTasks[authIndex] = task
	// 返回新任务和 created=true，调用方会启动后台 goroutine。
	return task, true
}

func (s *Service) dispatchRefreshTasks(authIndexes []string) {
	// dispatcher 顺序处理本次入队列表，避免为每个 queued 任务创建一个等待 token 的 goroutine。
	refreshDone := s.refreshContextSnapshot().Done()
	for index, authIndex := range authIndexes {
		// 等待 worker slot 时同时监听 refreshContext，确保应用关闭时 queued 任务可以快速失败。
		select {
		// worker token 控制全局并发，防止一次批量刷新同时压垮 CPA/上游接口。
		case s.refreshWorkerTokens <- struct{}{}:
			// 拿到 worker slot 后再启动真正执行 provider 调用的 worker goroutine。
			if !s.startRefreshGoroutine(func() {
				s.runRefreshTaskWithWorker(authIndex)
			}) {
				// 关闭期间如果拿到 token 后无法启动 worker，需要释放 token 并让当前及剩余 queued 任务失败。
				<-s.refreshWorkerTokens
				s.markQueuedRefreshTasksFailed(authIndexes[index:], context.Canceled)
				return
			}
		// refreshContext 取消说明应用正在关闭或刷新服务停止。
		case <-refreshDone:
			// 当前任务和剩余任务都还没有调用 provider，需要一起标记失败避免 queued 记录永久占位。
			s.markQueuedRefreshTasksFailed(authIndexes[index:], context.Canceled)
			// 当前 dispatcher 退出，已标记失败的任务会按普通失败 TTL 清理。
			return
		}
	}
}

func (s *Service) runRefreshTaskWithWorker(authIndex string) {
	// defer 保证无论成功、失败还是提前返回都会冷却并释放 worker slot。
	defer func() {
		// 冷却必须发生在释放 worker slot 之前，否则队列会立刻补进下一条任务，无法形成“每个 worker 完成后停 1 秒”的节流效果。
		s.refreshCooldown(RefreshTaskCooldown)
		// 释放 worker token，让队列中的下一个任务可以继续执行。
		<-s.refreshWorkerTokens
	}()

	// 把任务从 queued 切到 running，并拿到锁内确认后的 auth_index。
	authIndex, ok := s.markRefreshTaskRunning(authIndex)
	// 如果任务不存在或状态已经不是 queued，说明它被清理或状态异常，直接结束 goroutine。
	if !ok {
		// 不再调用 provider，避免无任务记录时产生不可见结果。
		return
	}
	// 每个任务独立设置超时；超时或 provider 错误都会沉淀到任务状态里给前端展示。
	ctx, cancel := context.WithTimeout(s.refreshContextSnapshot(), RefreshTaskTimeout)
	// 任务结束时释放 timeout timer，避免资源泄漏。
	defer cancel()
	// Check 会按 auth_index 读取身份、调用对应 provider，并标准化 quota rows。
	response, err := s.Check(ctx, CheckRequest{AuthIndex: authIndex})
	// provider 或身份校验失败时进入失败状态。
	if err != nil {
		// markRefreshTaskFailed 会把友好错误、HTTP 状态和缓存 TTL 写入任务记录。
		s.markRefreshTaskFailed(authIndex, err)
		// 失败任务不再计算 token/cost，也不会写 completed quota 缓存。
		return
	}
	// provider 成功后立即把窗口内 token/cost 补进同一次缓存，前端读取缓存时不再触发额外统计请求。
	response = s.attachWindowUsageStats(ctx, authIndex, response, time.Now())
	// quota rows 和 token/cost 都准备好后，把任务切到 completed 并写入长期成功缓存。
	s.markRefreshTaskCompleted(authIndex, response)
}

func refreshTaskErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "Quota refresh timed out. Please try again later."
	}
	if errors.Is(err, ErrProviderInput) {
		return ProviderInputErrorMessage(err, "Quota request is missing required parameters.")
	}
	var httpErr ProviderHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Error()
	}
	if strings.HasPrefix(err.Error(), "HTTP ") {
		return err.Error()
	}
	return "Quota refresh failed. Please try again later."
}

func refreshTaskHTTPStatusCode(err error) *int {
	var httpErr ProviderHTTPError
	if !errors.As(err, &httpErr) {
		return nil
	}
	statusCode := httpErr.StatusCode
	return &statusCode
}

func isRefreshCacheableHTTPStatus(statusCode int) bool {
	_, ok := RefreshCacheableHTTPStatusCodes[statusCode]
	return ok
}

func (s *Service) markRefreshTaskRunning(authIndex string) (string, bool) {
	// now 记录任务真正开始执行的时间。
	now := timeutil.NormalizeStorageTime(time.Now())
	// refreshTasks 是共享 map，状态切换前必须加锁。
	s.refreshMu.Lock()
	// 函数退出时释放锁，保证状态检查和写入原子完成。
	defer s.refreshMu.Unlock()
	// 按 auth_index 找到刚才入队的任务记录。
	task, ok := s.refreshTasks[authIndex]
	// 只有 queued 任务可以切到 running，避免重复 goroutine 改写已完成任务。
	if !ok || task.Status != RefreshTaskStatusQueued {
		// 返回 false 告诉 worker 当前任务不应继续执行。
		return "", false
	}
	// 把任务状态切到 running，前端轮询会看到正在刷新。
	task.Status = RefreshTaskStatusRunning
	// 记录开始时间，便于前端展示或后续排查耗时。
	task.StartedAt = now
	// 返回任务记录中的 auth_index，保证后续使用锁内确认过的值。
	return task.AuthIndex, true
}

func (s *Service) markRefreshTaskCompleted(authIndex string, response CheckResponse) {
	// now 同时作为完成时间和成功缓存写入时间。
	now := timeutil.NormalizeStorageTime(time.Now())
	// refreshTasks 是共享 map，写 completed 状态前必须加锁。
	s.refreshMu.Lock()
	// 函数退出时释放锁，保证 quota 缓存和状态一起写入。
	defer s.refreshMu.Unlock()
	// 按 auth_index 找到运行中的任务记录。
	task, ok := s.refreshTasks[authIndex]
	// 如果任务已经被清理，就没有地方写结果，直接返回。
	if !ok {
		// 不再创建新记录，避免后台结果复活已清理任务。
		return
	}
	// 标记任务完成，前端轮询会停止 pending 状态。
	task.Status = RefreshTaskStatusCompleted
	// RefreshedAt 是对外唯一的刷新时间口径，成功缓存不设置 ExpiresAt。
	task.RefreshedAt = now
	// 保存包含 token/cost 的 quota 响应，后续 cache 接口直接复用。
	task.Quota = &response
}

func (s *Service) markQueuedRefreshTasksFailed(authIndexes []string, err error) {
	// dispatcher 取消时批量处理剩余 queued 任务，避免未派发任务永久停留在 queued。
	for _, authIndex := range authIndexes {
		// 复用单任务失败逻辑，确保错误信息、HTTP 状态和 TTL 语义一致。
		s.markRefreshTaskFailed(authIndex, err)
	}
}

func (s *Service) markRefreshTaskFailed(authIndex string, err error) {
	// now 同时作为失败完成时间和失败缓存写入时间。
	now := timeutil.NormalizeStorageTime(time.Now())
	// 把底层错误转换成前端可展示的友好信息。
	message := refreshTaskErrorMessage(err)
	// 提取 HTTP 状态码，用于判断是否进入页面恢复缓存。
	httpStatusCode := refreshTaskHTTPStatusCode(err)
	// refreshTasks 是共享 map，写失败状态前必须加锁。
	s.refreshMu.Lock()
	// 函数退出时释放锁，保证错误信息和 TTL 一起写入。
	defer s.refreshMu.Unlock()
	// 按 auth_index 找到当前任务记录。
	task, ok := s.refreshTasks[authIndex]
	// 如果任务已经被清理，就没有地方写失败结果，直接返回。
	if !ok {
		// 不再创建新记录，避免后台失败复活已清理任务。
		return
	}
	// 失败任务分两类保存：401/402 这类可配置 HTTP 错误要进入页面恢复缓存；其它失败只短期保留给当前轮询。
	task.Status = RefreshTaskStatusFailed
	// RefreshedAt 是对外唯一的刷新时间口径，失败缓存也使用同一字段。
	task.RefreshedAt = now
	// 写入前端展示的错误信息。
	task.Error = message
	// 写入可选 HTTP 状态码，cache 接口会用它判断是否可恢复展示。
	task.HTTPStatusCode = httpStatusCode
	// 可缓存 HTTP 错误使用专门 TTL，让刷新页面后仍能看到稳定认证/余额错误。
	if httpStatusCode != nil && isRefreshCacheableHTTPStatus(*httpStatusCode) {
		// 401/402 等可配置错误使用较长的错误缓存 TTL。
		task.ExpiresAt = now.Add(RefreshErrorCacheTTL)
		// 已经写入错误 TTL，直接结束。
		return
	}
	// 普通失败只保留短 TTL，避免刷新页面后长期展示瞬时错误。
	task.ExpiresAt = now.Add(s.refreshTaskTTL)
}

func (s *Service) cleanupExpiredRefreshTasks(now time.Time) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.cleanupExpiredRefreshTasksLocked(now)
}

func (s *Service) cleanupExpiredRefreshTasksLocked(now time.Time) {
	// refreshTasks 直接以 auth_index 为 key；过期时删除这一条缓存即可，不再维护额外 taskId 索引。
	for authIndex, task := range s.refreshTasks {
		if task.ExpiresAt.IsZero() || now.Before(task.ExpiresAt) {
			continue
		}
		delete(s.refreshTasks, authIndex)
	}
}

func (t *RefreshTaskRecord) isActive() bool {
	return t.Status == RefreshTaskStatusQueued || t.Status == RefreshTaskStatusRunning
}

func (t *RefreshTaskRecord) response() RefreshTaskResponse {
	response := RefreshTaskResponse{
		AuthIndex:      t.AuthIndex,
		FileName:       t.FileName,
		Status:         t.Status,
		Error:          t.Error,
		HTTPStatusCode: t.HTTPStatusCode,
	}
	if t.Quota != nil {
		quota := *t.Quota
		response.Quota = &quota
	}
	if !t.RefreshedAt.IsZero() {
		refreshedAt := t.RefreshedAt
		response.RefreshedAt = &refreshedAt
	}
	if !t.ExpiresAt.IsZero() {
		expiresAt := t.ExpiresAt
		response.ExpiresAt = &expiresAt
	}
	return response
}
