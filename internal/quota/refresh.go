package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

type RefreshSource string

const (
	RefreshSourceManual        RefreshSource = "manual"
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
	Items []CheckResponse `json:"items"`
}

type RefreshRequest struct {
	AuthIndexes []string      `json:"auth_indexes"`
	Source      RefreshSource `json:"source"`
}

type RefreshResponse struct {
	Tasks    []RefreshTaskID            `json:"tasks"`
	Rejected []RefreshRejectedAuthIndex `json:"rejected"`
	Accepted int                        `json:"accepted"`
	Skipped  int                        `json:"skipped"`
	Limit    int                        `json:"limit"`
}

type RefreshTaskID struct {
	AuthIndex string `json:"authIndex"`
	TaskID    string `json:"taskId"`
}

type RefreshRejectedAuthIndex struct {
	AuthIndex string `json:"authIndex"`
	Error     string `json:"error"`
}

type RefreshTaskResponse struct {
	TaskID    string            `json:"taskId"`
	AuthIndex string            `json:"authIndex"`
	Status    RefreshTaskStatus `json:"status"`
	Quota     *CheckResponse    `json:"quota,omitempty"`
	Error     string            `json:"error,omitempty"`
	CachedAt  *time.Time        `json:"cachedAt,omitempty"`
	ExpiresAt *time.Time        `json:"expiresAt,omitempty"`
}

type RefreshTaskRecord struct {
	TaskID     string
	AuthIndex  string
	Status     RefreshTaskStatus
	Quota      *CheckResponse
	Error      string
	Source     RefreshSource
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	CachedAt   time.Time
	ExpiresAt  time.Time
}

func (s *Service) GetCachedQuota(ctx context.Context, request CacheRequest) (CacheResponse, error) {
	_ = ctx
	// 缓存读取只返回已完成任务的结果，不触发新的 provider 请求。
	if len(request.AuthIndexes) == 0 {
		return CacheResponse{}, fmt.Errorf("%w: auth_indexes are required", ErrValidation)
	}
	response := CacheResponse{Items: make([]CheckResponse, 0, len(request.AuthIndexes))}
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
		taskID, ok := s.refreshTaskIDsByAuth[authIndex]
		if !ok {
			continue
		}
		task, ok := s.refreshTasks[taskID]
		if !ok || task.Status != RefreshTaskStatusCompleted || task.Quota == nil {
			continue
		}
		quota := *task.Quota
		response.Items = append(response.Items, quota)
	}
	return response, nil
}

func (s *Service) Refresh(ctx context.Context, request RefreshRequest) (RefreshResponse, error) {
	// 刷新入口只负责校验、去重、建任务；实际 provider 调用交给后台 worker。
	limit := len(request.AuthIndexes)
	if limit <= 0 {
		return RefreshResponse{}, fmt.Errorf("%w: auth_indexes are required", ErrValidation)
	}
	response := RefreshResponse{Limit: limit}
	seen := make(map[string]struct{}, len(request.AuthIndexes))
	s.cleanupExpiredRefreshTasks(time.Now())

	for _, rawAuthIndex := range request.AuthIndexes {
		// 每个 auth_index 独立生成任务，便于前端逐行轮询和展示错误。
		authIndex := strings.TrimSpace(rawAuthIndex)
		if authIndex == "" {
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "invalid"})
			continue
		}
		if _, ok := seen[authIndex]; ok {
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "duplicate"})
			continue
		}
		seen[authIndex] = struct{}{}
		if response.Accepted >= limit {
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "invalid"})
			continue
		}
		if rejection, err := s.validateRefreshAuthIndex(ctx, authIndex); err != nil {
			return RefreshResponse{}, err
		} else if rejection != "" {
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: rejection})
			continue
		}

		task, created := s.ensureRefreshTask(authIndex, request.Source)
		if !created {
			response.Rejected = append(response.Rejected, RefreshRejectedAuthIndex{AuthIndex: authIndex, Error: "duplicate"})
			continue
		}
		response.Tasks = append(response.Tasks, RefreshTaskID{AuthIndex: authIndex, TaskID: task.TaskID})
		response.Accepted++
		go s.runRefreshTask(task.TaskID)
	}
	response.Skipped = len(response.Rejected)
	return response, nil
}

func (s *Service) GetRefreshTask(ctx context.Context, taskID string) (RefreshTaskResponse, error) {
	_ = ctx
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return RefreshTaskResponse{}, fmt.Errorf("%w: task_id is required", ErrValidation)
	}
	s.cleanupExpiredRefreshTasks(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	task, ok := s.refreshTasks[taskID]
	if !ok {
		return RefreshTaskResponse{}, ErrTaskNotFound
	}
	return task.response(), nil
}

func (s *Service) validateRefreshAuthIndex(ctx context.Context, authIndex string) (string, error) {
	// 先按 auth-file 身份查找；查不到时再区分“非 auth file”和“不存在”。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err == nil {
		if _, _, ok := s.resolveQuotaHandler(identity.Provider, identity.Type); !ok {
			return "unsupported", nil
		}
		return "", nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	var active entities.UsageIdentity
	if err := s.db.WithContext(ctx).Where("identity = ? AND is_deleted = ?", authIndex, false).First(&active).Error; err == nil {
		return "not_auth_file", nil
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return "not_found", nil
	} else {
		return "", err
	}
}

func (s *Service) ensureRefreshTask(authIndex string, source RefreshSource) (*RefreshTaskRecord, bool) {
	// 同一个 auth_index 已经 queued/running 时复用现有任务，避免重复打到上游接口。
	now := timeutil.NormalizeStorageTime(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if taskID, ok := s.refreshTaskIDsByAuth[authIndex]; ok {
		if task, ok := s.refreshTasks[taskID]; ok && task.isActive() {
			return task, false
		}
	}
	task := &RefreshTaskRecord{
		TaskID:    fmt.Sprintf("quota-refresh-%d", atomic.AddUint64(&s.refreshTaskSeq, 1)),
		AuthIndex: authIndex,
		Status:    RefreshTaskStatusQueued,
		Source:    source,
		CreatedAt: now,
	}
	s.refreshTasks[task.TaskID] = task
	s.refreshTaskIDsByAuth[authIndex] = task.TaskID
	return task, true
}

func (s *Service) runRefreshTask(taskID string) {
	// worker token 控制全局并发，防止一次批量刷新同时压垮 CPA/上游接口。
	s.refreshWorkerTokens <- struct{}{}
	defer func() { <-s.refreshWorkerTokens }()

	authIndex, ok := s.markRefreshTaskRunning(taskID)
	if !ok {
		return
	}
	// 每个任务独立设置超时；超时或 provider 错误都会沉淀到任务状态里给前端展示。
	ctx, cancel := context.WithTimeout(context.Background(), defaultRefreshTaskTimeout)
	defer cancel()
	response, err := s.Check(ctx, CheckRequest{AuthIndex: authIndex})
	if err != nil {
		s.markRefreshTaskFailed(taskID, refreshTaskErrorMessage(err))
		return
	}
	s.markRefreshTaskCompleted(taskID, response)
}

func refreshTaskErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "Quota refresh timed out. Please try again later."
	}
	if errors.Is(err, ErrProviderInput) {
		return ProviderInputErrorMessage(err, "Quota request is missing required parameters.")
	}
	if strings.HasPrefix(err.Error(), "HTTP ") {
		return err.Error()
	}
	return "Quota refresh failed. Please try again later."
}

func (s *Service) markRefreshTaskRunning(taskID string) (string, bool) {
	now := timeutil.NormalizeStorageTime(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	task, ok := s.refreshTasks[taskID]
	if !ok || task.Status != RefreshTaskStatusQueued {
		return "", false
	}
	task.Status = RefreshTaskStatusRunning
	task.StartedAt = now
	return task.AuthIndex, true
}

func (s *Service) markRefreshTaskCompleted(taskID string, response CheckResponse) {
	now := timeutil.NormalizeStorageTime(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	task, ok := s.refreshTasks[taskID]
	if !ok {
		return
	}
	task.Status = RefreshTaskStatusCompleted
	task.FinishedAt = now
	task.CachedAt = now
	task.ExpiresAt = now.Add(s.refreshTaskTTL)
	task.Quota = &response
}

func (s *Service) markRefreshTaskFailed(taskID string, message string) {
	now := timeutil.NormalizeStorageTime(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	task, ok := s.refreshTasks[taskID]
	if !ok {
		return
	}
	task.Status = RefreshTaskStatusFailed
	task.FinishedAt = now
	task.ExpiresAt = now.Add(s.refreshTaskTTL)
	task.Error = message
}

func (s *Service) cleanupExpiredRefreshTasks(now time.Time) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.cleanupExpiredRefreshTasksLocked(now)
}

func (s *Service) cleanupExpiredRefreshTasksLocked(now time.Time) {
	// 任务过期时同步删除 task_id 和 auth_index 索引，避免缓存映射残留。
	for taskID, task := range s.refreshTasks {
		if task.ExpiresAt.IsZero() || now.Before(task.ExpiresAt) {
			continue
		}
		delete(s.refreshTasks, taskID)
		if s.refreshTaskIDsByAuth[task.AuthIndex] == taskID {
			delete(s.refreshTaskIDsByAuth, task.AuthIndex)
		}
	}
}

func (t *RefreshTaskRecord) isActive() bool {
	return t.Status == RefreshTaskStatusQueued || t.Status == RefreshTaskStatusRunning
}

func (t *RefreshTaskRecord) response() RefreshTaskResponse {
	response := RefreshTaskResponse{
		TaskID:    t.TaskID,
		AuthIndex: t.AuthIndex,
		Status:    t.Status,
		Error:     t.Error,
	}
	if t.Quota != nil {
		quota := *t.Quota
		response.Quota = &quota
	}
	if !t.CachedAt.IsZero() {
		cachedAt := t.CachedAt
		response.CachedAt = &cachedAt
	}
	if !t.ExpiresAt.IsZero() {
		expiresAt := t.ExpiresAt
		response.ExpiresAt = &expiresAt
	}
	return response
}
