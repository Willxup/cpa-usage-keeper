package quota

import (
	"context"
	"sort"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
)

type InspectionResultStatus string

const (
	InspectionResultStatusNormal             InspectionResultStatus = "normal"
	InspectionResultStatusUnauthorized401    InspectionResultStatus = "unauthorized_401"
	InspectionResultStatusPaymentRequired402 InspectionResultStatus = "payment_required_402"
	InspectionResultStatusOtherFailed        InspectionResultStatus = "other_failed"
)

type InspectionStatus struct {
	Total              int                `json:"total"`
	Cached             int                `json:"cached"`
	Running            bool               `json:"running"`
	Completed          bool               `json:"completed"`
	CompletedAt        *time.Time         `json:"completed_at,omitempty"`
	Normal             int                `json:"normal"`
	Unauthorized401    int                `json:"unauthorized_401"`
	PaymentRequired402 int                `json:"payment_required_402"`
	OtherFailed        int                `json:"other_failed"`
	Results            []InspectionResult `json:"results"`
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
	if s == nil || s.db == nil {
		return InspectionStatus{}, nil
	}
	now := time.Now()
	s.clearSettledRefreshTasks()
	s.resetInspectionCompletedAt()
	s.cleanupExpiredRefreshTasks(now)
	summary, err := s.queueAuthFileRefreshRound(ctx, now, authFileRefreshRoundOptions{
		source:              RefreshSourceInspection,
		skipCachedHTTPError: false,
	})
	if err != nil {
		return InspectionStatus{}, err
	}
	if len(summary.queuedAuthIndexes) > 0 {
		if !s.startRefreshGoroutine(func() {
			s.dispatchRefreshTasks(summary.queuedAuthIndexes)
		}) {
			s.markQueuedRefreshTasksFailed(summary.queuedAuthIndexes, context.Canceled)
		}
	}
	return s.GetInspectionStatus(ctx)
}

func (s *Service) GetInspectionStatus(ctx context.Context) (InspectionStatus, error) {
	if s == nil || s.db == nil {
		return InspectionStatus{}, nil
	}
	identities, err := s.listAutoRefreshAuthFiles(ctx)
	if err != nil {
		return InspectionStatus{}, err
	}
	status := InspectionStatus{Total: len(identities)}

	s.cleanupExpiredRefreshTasks(time.Now())
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	for _, identity := range identities {
		task, ok := s.refreshTasks[identity.Identity]
		if !ok {
			continue
		}
		if task.isActive() {
			status.Running = true
			continue
		}
		result, ok := inspectionResultForTask(identity, task)
		if !ok {
			continue
		}
		status.Cached++
		switch result.Status {
		case InspectionResultStatusNormal:
			status.Normal++
		case InspectionResultStatusUnauthorized401:
			status.Unauthorized401++
		case InspectionResultStatusPaymentRequired402:
			status.PaymentRequired402++
		case InspectionResultStatusOtherFailed:
			status.OtherFailed++
		}
		status.Results = append(status.Results, result)
	}
	sortInspectionResults(status.Results)
	status.Completed = status.Total > 0 && status.Cached >= status.Total && !status.Running
	if status.Completed {
		// completed_at 是巡检轮次缓存的一部分：第一次观察到全量完成时记录，后续轮询保持稳定。
		if s.inspectionCompletedAt.IsZero() {
			s.inspectionCompletedAt = timeutil.NormalizeStorageTime(time.Now())
		}
		completedAt := s.inspectionCompletedAt
		status.CompletedAt = &completedAt
	}
	return status, nil
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

func (s *Service) resetInspectionCompletedAt() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.inspectionCompletedAt = time.Time{}
}

func inspectionResultForTask(identity entities.UsageIdentity, task *RefreshTaskRecord) (InspectionResult, bool) {
	if task == nil {
		return InspectionResult{}, false
	}
	result := InspectionResult{
		AuthIndex:      identity.Identity,
		Name:           firstNonEmpty(task.Name, identity.Name),
		Type:           firstNonEmpty(task.Type, identity.Type),
		FileName:       task.FileName,
		Error:          task.Error,
		HTTPStatusCode: task.HTTPStatusCode,
	}
	if !task.RefreshedAt.IsZero() {
		refreshedAt := task.RefreshedAt
		result.RefreshedAt = &refreshedAt
	}
	switch task.Status {
	case RefreshTaskStatusCompleted:
		if task.Quota == nil {
			return InspectionResult{}, false
		}
		result.Status = InspectionResultStatusNormal
		return result, true
	case RefreshTaskStatusFailed:
		result.Status = inspectionFailedResultStatus(task.HTTPStatusCode)
		return result, true
	default:
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
