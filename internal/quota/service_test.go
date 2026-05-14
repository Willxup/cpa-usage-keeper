package quota

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type refreshHandlerStub struct {
	mu     sync.Mutex
	calls  []string
	block  <-chan struct{}
	output ProviderOutput
	err    error
}

func (s *refreshHandlerStub) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	if s.block != nil {
		select {
		case <-ctx.Done():
			return ProviderOutput{}, ctx.Err()
		case <-s.block:
		}
	}
	s.mu.Lock()
	s.calls = append(s.calls, input.Identity.Identity)
	s.mu.Unlock()
	if s.err != nil {
		return ProviderOutput{}, s.err
	}
	return s.output, nil
}

func (s *refreshHandlerStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func TestRefreshCreatesTaskPerAuthIndexAndCachesCompletedQuota(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 1 || response.Skipped != 0 || len(response.Tasks) != 1 {
		t.Fatalf("unexpected refresh response: %+v", response)
	}

	task := waitForRefreshTask(t, service, response.Tasks[0].TaskID, RefreshTaskStatusCompleted)
	if task.AuthIndex != "auth-1" || task.Quota == nil || task.Quota.ID != "auth-1" || len(task.Quota.Quota) != 1 {
		t.Fatalf("expected completed task to expose cached quota, got %+v", task)
	}
	if task.ExpiresAt != nil {
		t.Fatalf("expected completed quota cache to have no expiry, got %v", task.ExpiresAt)
	}
	service.cleanupExpiredRefreshTasks(time.Now().Add(defaultRefreshTaskTTL * 2))
	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 || cache.Items[0].ID != "auth-1" {
		t.Fatalf("expected completed quota cache to survive cleanup, got %+v", cache)
	}
	if handler.callCount() != 1 {
		t.Fatalf("expected one provider call, got %d", handler.callCount())
	}
}

func TestRefreshPrunesPreviousCompletedTaskForSameAuthIndex(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	first, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("first Refresh returned error: %v", err)
	}
	firstTask := waitForRefreshTask(t, service, first.Tasks[0].TaskID, RefreshTaskStatusCompleted)

	handler.output = ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 60}}}}
	second, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("second Refresh returned error: %v", err)
	}
	secondTask := waitForRefreshTask(t, service, second.Tasks[0].TaskID, RefreshTaskStatusCompleted)
	if firstTask.TaskID == secondTask.TaskID {
		t.Fatalf("expected a new task id for the second refresh, got %s", secondTask.TaskID)
	}
	if _, err := service.GetRefreshTask(context.Background(), firstTask.TaskID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected first task to be pruned, got error %v", err)
	}
	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 || cache.Items[0].Quota[0].UsedPercent == nil || *cache.Items[0].Quota[0].UsedPercent != 60 {
		t.Fatalf("expected cache to expose latest quota, got %+v", cache)
	}
}

func TestRefreshRejectsInvalidEntriesAndIgnoresRunningTask(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "provider-1", Provider: "openai", Type: "openai", AuthType: entities.UsageIdentityAuthTypeAIProvider})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "deleted-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile, IsDeleted: true})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1", "auth-1", "provider-1", "deleted-1", "missing"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 1 || response.Skipped != 4 || len(response.Tasks) != 1 || len(response.Rejected) != 4 {
		t.Fatalf("unexpected refresh response: %+v", response)
	}
	if !hasRefreshRejection(response.Rejected, "auth-1", "duplicate") || !hasRefreshRejection(response.Rejected, "provider-1", "not_auth_file") || !hasRefreshRejection(response.Rejected, "deleted-1", "not_found") || !hasRefreshRejection(response.Rejected, "missing", "not_found") {
		t.Fatalf("unexpected rejected entries: %+v", response.Rejected)
	}

	firstTaskID := response.Tasks[0].TaskID
	waitForRefreshTask(t, service, firstTaskID, RefreshTaskStatusRunning)
	second, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("second Refresh returned error: %v", err)
	}
	if second.Accepted != 0 || second.Skipped != 1 || len(second.Tasks) != 0 || !hasRefreshRejection(second.Rejected, "auth-1", "duplicate") {
		t.Fatalf("expected running task to be ignored as duplicate, got %+v", second)
	}
	close(block)
	waitForRefreshTask(t, service, firstTaskID, RefreshTaskStatusCompleted)
	if handler.callCount() != 1 {
		t.Fatalf("expected duplicate refresh to reuse provider call, got %d", handler.callCount())
	}
}

func TestRefreshQueueUsesFiveWorkersAndTwentySecondTimeout(t *testing.T) {
	if defaultRefreshWorkerLimit != 5 {
		t.Fatalf("expected refresh worker limit 5, got %d", defaultRefreshWorkerLimit)
	}
	if defaultRefreshTaskTimeout != 20*time.Second {
		t.Fatalf("expected refresh task timeout 20s, got %s", defaultRefreshTaskTimeout)
	}
}

func TestRefreshTaskFailureReturnsFriendlyMessage(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{err: errors.New("upstream exploded")}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	task := waitForRefreshTask(t, service, response.Tasks[0].TaskID, RefreshTaskStatusFailed)
	if task.Error != "Quota refresh failed. Please try again later." {
		t.Fatalf("expected friendly error message, got %q", task.Error)
	}
}

func openQuotaTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "quota.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(entities.All()...); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}
	return db
}

func seedUsageIdentity(t *testing.T, db *gorm.DB, identity entities.UsageIdentity) {
	t.Helper()
	identity.Name = identity.Identity
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity %q: %v", identity.Identity, err)
	}
}

func waitForRefreshTask(t *testing.T, service *Service, taskID string, status RefreshTaskStatus) RefreshTaskResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var task RefreshTaskResponse
	var err error
	for time.Now().Before(deadline) {
		task, err = service.GetRefreshTask(context.Background(), taskID)
		if err == nil && task.Status == status {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %s, last task=%+v err=%v", taskID, status, task, err)
	return RefreshTaskResponse{}
}

func hasRefreshRejection(rejections []RefreshRejectedAuthIndex, authIndex string, code string) bool {
	for _, rejection := range rejections {
		if rejection.AuthIndex == authIndex && rejection.Error == code {
			return true
		}
	}
	return false
}
