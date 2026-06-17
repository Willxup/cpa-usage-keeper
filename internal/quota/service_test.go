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

type resetHandlerStub struct {
	refreshHandlerStub
	resetOutput ProviderResetOutput
	resetErr    error
	resetInputs []entities.UsageIdentity
	entered     chan string
}

func (s *resetHandlerStub) Reset(ctx context.Context, input ProviderInput) (ProviderResetOutput, error) {
	if s.entered != nil {
		s.entered <- input.Identity.Identity
	}
	if s.block != nil {
		select {
		case <-ctx.Done():
			return ProviderResetOutput{}, ctx.Err()
		case <-s.block:
		}
	}
	s.mu.Lock()
	s.resetInputs = append(s.resetInputs, input.Identity)
	s.mu.Unlock()
	if s.resetErr != nil {
		return ProviderResetOutput{}, s.resetErr
	}
	return s.resetOutput, nil
}

func TestResetConsumesCodexCredit(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &resetHandlerStub{resetOutput: ProviderResetOutput{Code: "reset", WindowsReset: 2}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}))

	response, err := service.Reset(context.Background(), ResetRequest{AuthIndex: " codex-auth "})
	if err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	if response.AuthIndex != "codex-auth" || response.Code != "reset" || response.WindowsReset != 2 {
		t.Fatalf("unexpected reset response: %+v", response)
	}
	if len(handler.resetInputs) != 1 || handler.resetInputs[0].Identity != "codex-auth" {
		t.Fatalf("expected reset input identity codex-auth, got %+v", handler.resetInputs)
	}
}

func TestResetRejectsUnsupportedProvider(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &resetHandlerStub{resetOutput: ProviderResetOutput{Code: "reset", WindowsReset: 1}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	_, err := service.Reset(context.Background(), ResetRequest{AuthIndex: "claude-auth"})
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestResetRejectsEmptyAuthIndex(t *testing.T) {
	service := NewServiceWithRegistry(openQuotaTestDatabase(t), NewProviderRegistry(nil))

	_, err := service.Reset(context.Background(), ResetRequest{AuthIndex: "   "})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestResetRejectsNilServiceWithoutInProgressError(t *testing.T) {
	var service *Service

	_, err := service.Reset(context.Background(), ResetRequest{AuthIndex: "codex-auth"})
	if err == nil {
		t.Fatal("expected nil service error")
	}
	if errors.Is(err, ErrResetInProgress) {
		t.Fatalf("expected nil service error to avoid in-progress semantics, got %v", err)
	}
	if err.Error() != "quota service is nil" {
		t.Fatalf("expected quota service nil error, got %v", err)
	}
}

func TestResetReturnsNotFoundForMissingAuthIndex(t *testing.T) {
	service := NewServiceWithRegistry(openQuotaTestDatabase(t), NewProviderRegistry(nil))

	_, err := service.Reset(context.Background(), ResetRequest{AuthIndex: "missing-auth"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestResetRejectsConcurrentRequestsForSameAuthIndex(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	block := make(chan struct{})
	handler := &resetHandlerStub{refreshHandlerStub: refreshHandlerStub{block: block}, resetOutput: ProviderResetOutput{Code: "reset", WindowsReset: 2}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}))

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Reset(context.Background(), ResetRequest{AuthIndex: "codex-auth"})
			results <- err
		}()
	}

	select {
	case err := <-results:
		if !errors.Is(err, ErrResetInProgress) {
			t.Fatalf("expected duplicate concurrent reset to be rejected as in progress, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for duplicate concurrent reset")
	}

	close(block)

	select {
	case err := <-results:
		if err != nil {
			t.Fatalf("expected blocked concurrent reset to succeed after release, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked concurrent reset")
	}

	wg.Wait()
	if len(handler.resetInputs) != 1 || handler.resetInputs[0].Identity != "codex-auth" {
		t.Fatalf("expected only one provider reset call, got %+v", handler.resetInputs)
	}
}

func TestResetAllowsConcurrentRequestsForDifferentAuthIndexes(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth-1", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth-2", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	block := make(chan struct{})
	entered := make(chan string, 2)
	handler := &resetHandlerStub{
		refreshHandlerStub: refreshHandlerStub{block: block},
		resetOutput:        ProviderResetOutput{Code: "reset", WindowsReset: 1},
		entered:            entered,
	}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}))

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, authIndex := range []string{"codex-auth-1", "codex-auth-2"} {
		wg.Add(1)
		go func(authIndex string) {
			defer wg.Done()
			_, err := service.Reset(context.Background(), ResetRequest{AuthIndex: authIndex})
			results <- err
		}(authIndex)
	}

	enteredAuthIndexes := map[string]bool{}
	for range 2 {
		select {
		case authIndex := <-entered:
			enteredAuthIndexes[authIndex] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for both reset calls to enter provider")
		}
	}
	if !enteredAuthIndexes["codex-auth-1"] || !enteredAuthIndexes["codex-auth-2"] {
		t.Fatalf("expected both auth indexes to enter provider concurrently, got %+v", enteredAuthIndexes)
	}

	close(block)
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("expected concurrent reset for different auth indexes to succeed, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for concurrent reset across auth indexes")
		}
	}

	wg.Wait()
	if len(handler.resetInputs) != 2 {
		t.Fatalf("expected two provider reset calls, got %+v", handler.resetInputs)
	}
}

func TestCheckExposesCodexRateLimitResetCredits(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{RateLimitResetCredits: &CodexRateLimitResetCredits{AvailableCount: quotaIntPtr(2)}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}))

	response, err := service.Check(context.Background(), CheckRequest{AuthIndex: "codex-auth"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if response.RateLimitResetCreditsAvailableCount == nil || *response.RateLimitResetCreditsAvailableCount != 2 {
		t.Fatalf("expected reset credits available count 2, got %#v", response.RateLimitResetCreditsAvailableCount)
	}
}

func TestRefreshCachesCodexRateLimitResetCredits(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, FileName: quotaStringPtr("codex-user.json")})
	handler := &refreshHandlerStub{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{RateLimitResetCredits: &CodexRateLimitResetCredits{AvailableCount: quotaIntPtr(0)}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 1 || response.Skipped != 0 || len(response.Tasks) != 1 {
		t.Fatalf("unexpected refresh response: %+v", response)
	}

	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)
	if task.Quota == nil || task.Quota.RateLimitResetCreditsAvailableCount == nil || *task.Quota.RateLimitResetCreditsAvailableCount != 0 {
		t.Fatalf("expected completed task quota to expose zero reset credits, got %+v", task.Quota)
	}

	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"codex-auth"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 || cache.Items[0].Quota == nil || cache.Items[0].Quota.RateLimitResetCreditsAvailableCount == nil || *cache.Items[0].Quota.RateLimitResetCreditsAvailableCount != 0 {
		t.Fatalf("expected cached quota to expose zero reset credits, got %+v", cache)
	}
}

func TestRefreshCreatesTaskPerAuthIndexAndCachesCompletedQuota(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile, FileName: quotaStringPtr("claude-user.json")})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 1 || response.Skipped != 0 || len(response.Tasks) != 1 {
		t.Fatalf("unexpected refresh response: %+v", response)
	}

	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)
	if task.AuthIndex != "auth-1" || task.Quota == nil || task.Quota.ID != "auth-1" || len(task.Quota.Quota) != 1 {
		t.Fatalf("expected completed task to expose cached quota, got %+v", task)
	}
	if task.RefreshedAt == nil || task.RefreshedAt.IsZero() {
		t.Fatalf("expected completed task to expose refreshed_at, got %+v", task)
	}
	if task.ExpiresAt != nil {
		t.Fatalf("expected completed quota cache to have no expiry, got %v", task.ExpiresAt)
	}
	service.cleanupExpiredRefreshTasks(time.Now().Add(RefreshTransientTaskTTL * 2))
	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 || cache.Items[0].AuthIndex != "auth-1" || cache.Items[0].Quota == nil || cache.Items[0].Quota.ID != "auth-1" {
		t.Fatalf("expected completed quota cache to survive cleanup, got %+v", cache)
	}
	if cache.Items[0].FileName == nil || *cache.Items[0].FileName != "claude-user.json" {
		t.Fatalf("expected completed quota cache to expose file_name, got %+v", cache.Items[0])
	}
	if cache.Items[0].RefreshedAt == nil || cache.Items[0].RefreshedAt.IsZero() {
		t.Fatalf("expected completed quota cache to expose refreshed_at, got %+v", cache.Items[0])
	}
	if handler.callCount() != 1 {
		t.Fatalf("expected one provider call, got %d", handler.callCount())
	}
}

func TestRefreshOverwritesPreviousCompletedTaskForSameAuthIndex(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	first, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("first Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, first.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)

	handler.output = ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 60}}}}
	second, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("second Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, second.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)
	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 || cache.Items[0].Quota == nil || cache.Items[0].Quota.Quota[0].UsedPercent == nil || *cache.Items[0].Quota.Quota[0].UsedPercent != 60 {
		t.Fatalf("expected cache to expose latest quota, got %+v", cache)
	}
}

func TestManualRefreshIgnoresRecentAutoRefreshRound(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}
	service.autoRefreshMu.Lock()
	service.lastAutoRefreshRoundAt = time.Now()
	service.autoRefreshMu.Unlock()

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 1 || len(response.Tasks) != 1 {
		t.Fatalf("expected manual refresh to ignore recent auto round, got %+v", response)
	}
	waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)
	if handler.callCount() != 1 {
		t.Fatalf("expected manual refresh provider call, got %d", handler.callCount())
	}
}

func TestManualRefreshAllowsDisabledAuthFile(t *testing.T) {
	db := openQuotaTestDatabase(t)
	// disabled 只限制自动刷新扫描，手动刷新仍允许用户显式触发。
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile, Disabled: boolPtr(true)})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 1 || response.Skipped != 0 || len(response.Tasks) != 1 {
		t.Fatalf("expected disabled auth file to be accepted for manual refresh, got %+v", response)
	}
	waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)
	if handler.callCount() != 1 {
		t.Fatalf("expected manual refresh provider call, got %d", handler.callCount())
	}
}

func TestManualRefreshFallsBackToIdentityTypeWhenProviderUnsupported(t *testing.T) {
	db := openQuotaTestDatabase(t)
	// provider 不支持但 type 支持时，手动刷新应复用 Check/auto 的同一套 handler 解析规则。
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "unknown-provider", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 1 || response.Skipped != 0 || len(response.Tasks) != 1 {
		t.Fatalf("expected manual refresh to fall back to identity type, got %+v", response)
	}
	waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)
	if handler.callCount() != 1 {
		t.Fatalf("expected type fallback provider call, got %d", handler.callCount())
	}
}

func TestManualRefreshSkipsUnsupportedAuthFileWithoutCaching(t *testing.T) {
	db := openQuotaTestDatabase(t)
	// Auth File 存在但 provider/type 都没有 handler 时，手动刷新静默跳过，不创建任务缓存或错误缓存。
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "unknown-provider", Type: "unknown-type", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if response.Accepted != 0 || response.Skipped != 1 || len(response.Tasks) != 0 || len(response.Rejected) != 0 {
		t.Fatalf("expected unsupported auth file to be skipped without rejection, got %+v", response)
	}
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "auth-1"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected unsupported auth file to stay out of refresh cache, got %v", err)
	}
	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 0 {
		t.Fatalf("expected unsupported auth file to stay out of page cache, got %+v", cache.Items)
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
	if !hasRefreshRejection(response.Rejected, "auth-1", "duplicate_request") || !hasRefreshRejection(response.Rejected, "provider-1", "not_auth_file") || !hasRefreshRejection(response.Rejected, "deleted-1", "not_found") || !hasRefreshRejection(response.Rejected, "missing", "not_found") {
		t.Fatalf("unexpected rejected entries: %+v", response.Rejected)
	}

	firstTaskID := response.Tasks[0].AuthIndex
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

func TestManualRefreshReturnsDuplicateForRunningTaskEvenWhenIdentityDeleted(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}

	first, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("first Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, first.Tasks[0].AuthIndex, RefreshTaskStatusRunning)
	if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", "auth-1").Update("is_deleted", true).Error; err != nil {
		t.Fatalf("delete usage identity returned error: %v", err)
	}

	second, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("second Refresh returned error: %v", err)
	}

	if second.Accepted != 0 || second.Skipped != 1 || len(second.Tasks) != 0 || !hasRefreshRejection(second.Rejected, "auth-1", "duplicate") {
		t.Fatalf("expected active task to win over deleted identity validation, got %+v", second)
	}
	close(block)
	waitForRefreshTask(t, service, first.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)
}

func TestRefreshQueueUsesConfiguredWorkersTimeoutAndCooldown(t *testing.T) {
	if RefreshWorkerLimit != 10 {
		t.Fatalf("expected refresh worker limit 10, got %d", RefreshWorkerLimit)
	}
	if RefreshTaskTimeout != 20*time.Second {
		t.Fatalf("expected refresh task timeout 20s, got %s", RefreshTaskTimeout)
	}
	if RefreshTaskCooldown != time.Second {
		t.Fatalf("expected refresh task cooldown 1s, got %s", RefreshTaskCooldown)
	}
}

func TestNewServiceWithRegistryAndOptionsUsesConfiguredWorkerLimit(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{RefreshWorkerLimit: 7})
	if cap(service.refreshWorkerTokens) != 7 {
		t.Fatalf("expected configured worker limit 7, got %d", cap(service.refreshWorkerTokens))
	}
}

func TestNewServiceWithRegistryAndOptionsCapsConfiguredWorkerLimit(t *testing.T) {
	db := openQuotaTestDatabase(t)
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{RefreshWorkerLimit: 101})
	if cap(service.refreshWorkerTokens) != 100 {
		t.Fatalf("expected configured worker limit to be capped at 100, got %d", cap(service.refreshWorkerTokens))
	}
}

func TestRefreshTaskWaitsForCooldownBeforeReleasingWorker(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	cooldownCalls := make(chan time.Duration, 1)
	service.refreshCooldown = func(duration time.Duration) {
		cooldownCalls <- duration
	}

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)

	select {
	case duration := <-cooldownCalls:
		if duration != RefreshTaskCooldown {
			t.Fatalf("expected cooldown %s, got %s", RefreshTaskCooldown, duration)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected refresh task to call cooldown")
	}
}

func TestQueuedRefreshTaskFailsWhenParentContextCancelsBeforeWorkerSlot(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}), ServiceOptions{RefreshWorkerLimit: 1})
	service.refreshWorkerTokens <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	service.SetRefreshContext(ctx)

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	cancel()
	defer func() { <-service.refreshWorkerTokens }()
	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusFailed)
	if task.Error != "Quota refresh timed out. Please try again later." {
		t.Fatalf("expected canceled queued task to fail with timeout message, got %+v", task)
	}
	if handler.callCount() != 0 {
		t.Fatalf("expected canceled queued task not to call provider, got %d", handler.callCount())
	}
}

func TestQueuedRefreshDispatcherFailsRemainingTasksOnParentContextCancel(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-2", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}), ServiceOptions{RefreshWorkerLimit: 1})
	service.refreshWorkerTokens <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	service.SetRefreshContext(ctx)

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1", "auth-2"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	cancel()
	defer func() { <-service.refreshWorkerTokens }()
	first := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusFailed)
	second := waitForRefreshTask(t, service, response.Tasks[1].AuthIndex, RefreshTaskStatusFailed)
	if first.ExpiresAt == nil || second.ExpiresAt == nil {
		t.Fatalf("expected canceled queued tasks to get expiry, got first=%+v second=%+v", first, second)
	}
	if handler.callCount() != 0 {
		t.Fatalf("expected canceled queued tasks not to call provider, got %d", handler.callCount())
	}
}

func TestRefreshTaskUsesParentContextCancellation(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}
	ctx, cancel := context.WithCancel(context.Background())
	service.SetRefreshContext(ctx)

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusRunning)
	cancel()
	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusFailed)
	if task.Error != "Quota refresh timed out. Please try again later." {
		t.Fatalf("expected canceled task to fail with timeout message, got %+v", task)
	}
	close(block)
}

func TestStopRefreshTasksPreventsNewRefreshWorkers(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	service.StopRefreshTasks()
	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}

	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusFailed)
	if task.Error != "Quota refresh timed out. Please try again later." {
		t.Fatalf("expected stopped service task to fail with timeout message, got %+v", task)
	}
	if handler.callCount() != 0 {
		t.Fatalf("expected stopped service not to start worker, got %d provider calls", handler.callCount())
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
	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusFailed)
	if task.Error != "Quota refresh failed. Please try again later." {
		t.Fatalf("expected friendly error message, got %q", task.Error)
	}
	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 0 {
		t.Fatalf("expected transient failure to stay out of page cache, got %+v", cache.Items)
	}
}

func TestInspectionStatusSummarizesActiveAuthFileCache(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "ok", Name: "Claude OK", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "unauthorized", Name: "Codex Expired", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "payment", Name: "Gemini Billing", Provider: "gemini", Type: "gemini-cli", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "other", Name: "Claude Other", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "pending", Name: "Pending", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "uncached", Name: "No Cache", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "unsupported", Name: "Unsupported", Provider: "vertex", Type: "vertex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "disabled", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile, Disabled: boolPtr(true)})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "deleted", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile, IsDeleted: true})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "provider", Provider: "openai", Type: "openai", AuthType: entities.UsageIdentityAuthTypeAIProvider})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	now := time.Date(2026, 6, 3, 10, 30, 0, 0, time.UTC)
	code401 := 401
	code402 := 402

	service.refreshTasks = map[string]*RefreshTaskRecord{
		"ok":           {AuthIndex: "ok", Status: RefreshTaskStatusCompleted, Quota: &CheckResponse{ID: "ok", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h"}}}, RefreshedAt: now.Add(-time.Minute)},
		"unauthorized": {AuthIndex: "unauthorized", Status: RefreshTaskStatusFailed, Error: "HTTP 401 expired", HTTPStatusCode: &code401, RefreshedAt: now.Add(-2 * time.Minute)},
		"payment":      {AuthIndex: "payment", Status: RefreshTaskStatusFailed, Error: "HTTP 402 payment required", HTTPStatusCode: &code402, RefreshedAt: now.Add(-3 * time.Minute)},
		"other":        {AuthIndex: "other", Status: RefreshTaskStatusFailed, Error: "network down", RefreshedAt: now.Add(-4 * time.Minute)},
		"pending":      {AuthIndex: "pending", Status: RefreshTaskStatusRunning},
		"disabled":     {AuthIndex: "disabled", Status: RefreshTaskStatusCompleted, Quota: &CheckResponse{ID: "disabled"}},
	}

	status, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("GetInspectionStatus returned error: %v", err)
	}
	if status.Total != 7 || status.Cached != 4 || status.Unknown != 3 || status.Running != false || status.Completed != false {
		t.Fatalf("unexpected inspection progress: %+v", status)
	}
	if status.Normal != 1 || status.Unauthorized401 != 1 || status.PaymentRequired402 != 1 || status.Unauthorized401402 != 2 || status.OtherFailed != 1 {
		t.Fatalf("unexpected inspection summary: %+v", status)
	}
	if len(status.Results) != 4 {
		t.Fatalf("expected four cached results, got %+v", status.Results)
	}
	if status.Results[0].AuthIndex != "ok" || status.Results[0].Name != "Claude OK" || status.Results[0].Type != "claude" || status.Results[0].Status != InspectionResultStatusNormal {
		t.Fatalf("expected normal result with identity metadata first, got %+v", status.Results)
	}
	if status.CompletedAt != nil {
		t.Fatalf("expected cached refresh results without an explicit inspection round to avoid completed_at, got %v", status.CompletedAt)
	}
}

func TestInspectionStatusNormalizesIdentityBeforeReadingRefreshTask(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: " auth-1 ", Name: "Claude Account", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
	now := time.Date(2026, 6, 3, 10, 30, 0, 0, time.UTC)
	service.refreshTasks = map[string]*RefreshTaskRecord{
		"auth-1": {
			AuthIndex:   "auth-1",
			Status:      RefreshTaskStatusCompleted,
			Quota:       &CheckResponse{ID: "auth-1", Quota: []QuotaRow{{Key: "rate_limit.primary_window", Label: "5h"}}},
			RefreshedAt: now,
		},
	}

	status, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("GetInspectionStatus returned error: %v", err)
	}
	if status.Total != 1 || status.Cached != 1 || status.Unknown != 0 || status.Normal != 1 {
		t.Fatalf("expected trimmed auth_index to match refresh task cache, got %+v", status)
	}
	if len(status.Results) != 1 || status.Results[0].AuthIndex != "auth-1" {
		t.Fatalf("expected inspection result to expose normalized auth_index, got %+v", status.Results)
	}
}

func TestManualRefreshDoesNotMarkInspectionCompleted(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusCompleted)

	status, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("GetInspectionStatus returned error: %v", err)
	}
	if status.Total != 1 || status.Cached != 1 || status.Normal != 1 || status.Unknown != 0 {
		t.Fatalf("expected manual cache to appear only as inspection result data, got %+v", status)
	}
	if status.Completed || status.CompletedAt != nil {
		t.Fatalf("expected manual refresh cache to avoid marking inspection completed, got completed=%v completedAt=%v", status.Completed, status.CompletedAt)
	}
}

func TestManualRefreshAfterInspectionCompletionDoesNotSetInspectionRunning(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}

	if _, err := service.StartInspection(context.Background()); err != nil {
		t.Fatalf("StartInspection returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusRunning)
	close(block)
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	completed, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("completed GetInspectionStatus returned error: %v", err)
	}
	if !completed.Completed || completed.CompletedAt == nil {
		t.Fatalf("expected completed inspection before manual refresh, got %+v", completed)
	}

	manualBlock := make(chan struct{})
	handler.block = manualBlock
	if _, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual}); err != nil {
		t.Fatalf("manual Refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusRunning)

	status, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("running GetInspectionStatus returned error: %v", err)
	}
	if status.Running {
		t.Fatalf("expected manual refresh to avoid inspection running, got %+v", status)
	}
	if !status.Completed || status.CompletedAt == nil || !status.CompletedAt.Equal(*completed.CompletedAt) {
		t.Fatalf("expected prior inspection completion to stay stable, before=%+v after=%+v", completed, status)
	}
	close(manualBlock)
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
}

func TestStartInspectionIgnoresNonInspectionActiveRefreshTasks(t *testing.T) {
	tests := []struct {
		name   string
		source RefreshSource
	}{
		{name: "manual", source: RefreshSourceManual},
		{name: "auto", source: RefreshSourceAuto},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
			block := make(chan struct{})
			blockClosed := false
			t.Cleanup(func() {
				if !blockClosed {
					close(block)
				}
			})
			handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
			service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
			service.refreshCooldown = func(time.Duration) {}

			refresh, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: tt.source})
			if err != nil {
				t.Fatalf("%s Refresh returned error: %v", tt.source, err)
			}
			waitForRefreshTask(t, service, refresh.Tasks[0].AuthIndex, RefreshTaskStatusRunning)

			status, err := service.StartInspection(context.Background())
			if err != nil {
				t.Fatalf("StartInspection returned error: %v", err)
			}
			if status.Total != 1 || status.Cached != 0 || status.Unknown != 1 || status.Running || status.Completed || status.CompletedAt != nil {
				t.Fatalf("expected %s task to stay outside inspection state, got %+v", tt.source, status)
			}

			close(block)
			blockClosed = true
			waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
			finalStatus, err := service.GetInspectionStatus(context.Background())
			if err != nil {
				t.Fatalf("GetInspectionStatus returned error: %v", err)
			}
			if finalStatus.Total != 1 || finalStatus.Cached != 1 || finalStatus.Unknown != 0 || finalStatus.Running || finalStatus.Completed || finalStatus.CompletedAt != nil {
				t.Fatalf("expected completed %s cache to avoid inspection completion, got %+v", tt.source, finalStatus)
			}
		})
	}
}

func TestInspectionStatusClassifiesLimitReachedByKnownAuthFileType(t *testing.T) {
	tests := []struct {
		name             string
		identity         entities.UsageIdentity
		quota            []QuotaRow
		wantStatus       string
		wantNormal       int
		wantLimitReached int
	}{
		{
			name:     "codex limitReached flag",
			identity: entities.UsageIdentity{Identity: "codex-auth", Name: "Codex", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:          "rate_limit.primary_window",
				Label:        "5h",
				LimitReached: boolPtr(true),
			}},
			wantStatus:       "limit_reached",
			wantLimitReached: 1,
		},
		{
			name:     "claude used percent",
			identity: entities.UsageIdentity{Identity: "claude-auth", Name: "Claude", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:         "five_hour",
				Label:       "5h",
				UsedPercent: floatPtr(100),
			}},
			wantStatus:       "limit_reached",
			wantLimitReached: 1,
		},
		{
			name:     "gemini remaining fraction",
			identity: entities.UsageIdentity{Identity: "gemini-auth", Name: "Gemini", Provider: "gemini", Type: "gemini-cli", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:               "bucket.gemini-pro.PROMPT",
				Label:             "gemini-pro",
				RemainingFraction: floatPtr(0),
			}},
			wantStatus:       "limit_reached",
			wantLimitReached: 1,
		},
		{
			name:     "antigravity remaining fraction",
			identity: entities.UsageIdentity{Identity: "ag-auth", Name: "Antigravity", Provider: "antigravity", Type: "antigravity", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:               "model.pro",
				Label:             "Pro",
				RemainingFraction: floatPtr(0),
			}},
			wantStatus:       "limit_reached",
			wantLimitReached: 1,
		},
		{
			name:     "kimi used reaches limit",
			identity: entities.UsageIdentity{Identity: "kimi-auth", Name: "Kimi", Provider: "kimi", Type: "kimi", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:   "usage",
				Label: "Usage",
				Used:  floatPtr(10),
				Limit: floatPtr(10),
			}},
			wantStatus:       "limit_reached",
			wantLimitReached: 1,
		},
		{
			name:     "xai limit reached flag",
			identity: entities.UsageIdentity{Identity: "xai-auth", Name: "xAI", Provider: "xai", Type: "xai", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:          "billing.monthly",
				Label:        "Monthly Spend",
				LimitReached: boolPtr(true),
			}},
			wantStatus:       "limit_reached",
			wantLimitReached: 1,
		},
		{
			name:     "generic type with known provider does not use provider fallback",
			identity: entities.UsageIdentity{Identity: "generic-codex-auth", Name: "Generic Codex", Provider: "codex", Type: "generic", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:          "quota",
				Label:        "Quota",
				LimitReached: boolPtr(true),
			}},
			wantStatus: "normal",
			wantNormal: 1,
		},
		{
			name:     "unknown type does not use generic limit detection",
			identity: entities.UsageIdentity{Identity: "unknown-auth", Name: "Unknown", Provider: "unknown", Type: "unknown", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			quota: []QuotaRow{{
				Key:          "quota",
				Label:        "Quota",
				LimitReached: boolPtr(true),
				Used:         floatPtr(10),
				Limit:        floatPtr(10),
			}},
			wantStatus: "normal",
			wantNormal: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, tt.identity)
			service := NewServiceWithRegistry(db, NewProviderRegistry(nil))
			now := time.Date(2026, 6, 3, 10, 30, 0, 0, time.UTC)
			service.refreshTasks = map[string]*RefreshTaskRecord{
				tt.identity.Identity: {
					AuthIndex:   tt.identity.Identity,
					Name:        tt.identity.Name,
					Type:        tt.identity.Type,
					Status:      RefreshTaskStatusCompleted,
					Quota:       &CheckResponse{ID: tt.identity.Identity, Quota: tt.quota},
					RefreshedAt: now,
				},
			}

			status, err := service.GetInspectionStatus(context.Background())
			if err != nil {
				t.Fatalf("GetInspectionStatus returned error: %v", err)
			}
			if status.Total != 1 || status.Cached != 1 || status.Unknown != 0 {
				t.Fatalf("unexpected inspection progress: %+v", status)
			}
			if status.Normal != tt.wantNormal || status.LimitReached != tt.wantLimitReached {
				t.Fatalf("unexpected inspection summary: %+v", status)
			}
			if len(status.Results) != 1 || string(status.Results[0].Status) != tt.wantStatus {
				t.Fatalf("expected status %s, got %+v", tt.wantStatus, status.Results)
			}
		})
	}
}

func TestInspectionStatusClassifiesLimitReachedThroughRefreshPipeline(t *testing.T) {
	tests := []struct {
		name     string
		identity entities.UsageIdentity
		output   ProviderOutput
	}{
		{
			name:     "codex limitReached from normalized rate limit",
			identity: entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{RateLimit: &CodexRateLimitInfo{
				Allowed:      boolPtr(false),
				LimitReached: boolPtr(true),
				PrimaryWindow: &CodexUsageWindow{
					UsedPercent:        100,
					LimitWindowSeconds: 18000,
				},
			}}}},
		},
		{
			name:     "claude used percent from normalized utilization",
			identity: entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			output: ProviderOutput{Provider: "claude", Result: ClaudeResult{Usage: &ClaudeUsagePayload{
				FiveHour: &ClaudeUsageWindow{Utilization: 100},
			}}},
		},
		{
			name:     "xai used at billing limit from normalized billing row",
			identity: entities.UsageIdentity{Identity: "xai-auth", Provider: "xai", Type: "xai", AuthType: entities.UsageIdentityAuthTypeAuthFile},
			output: ProviderOutput{Provider: "xai", Result: XAIResult{Billing: &XAIBillingPayload{Config: &XAIBillingConfig{
				MonthlyLimit:       XAIMoneyValue{Val: 100},
				Used:               XAIMoneyValue{Val: 100},
				BillingPeriodEnd:   "2026-07-01T00:00:00+00:00",
				BillingPeriodStart: "2026-06-01T00:00:00+00:00",
			}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, tt.identity)
			handler := &refreshHandlerStub{output: tt.output}
			service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{tt.identity.Type: handler}))
			service.refreshCooldown = func(time.Duration) {}

			if _, err := service.StartInspection(context.Background()); err != nil {
				t.Fatalf("StartInspection returned error: %v", err)
			}
			waitForRefreshTask(t, service, tt.identity.Identity, RefreshTaskStatusCompleted)
			status, err := service.GetInspectionStatus(context.Background())
			if err != nil {
				t.Fatalf("GetInspectionStatus returned error: %v", err)
			}

			if status.Total != 1 || status.Cached != 1 || status.Unknown != 0 || status.Normal != 0 || status.LimitReached != 1 {
				t.Fatalf("expected normalized refresh pipeline to classify limit reached, got %+v", status)
			}
			if len(status.Results) != 1 || status.Results[0].Status != InspectionResultStatusLimitReached {
				t.Fatalf("expected limit_reached result, got %+v", status.Results)
			}
		})
	}
}

func TestStartInspectionClearsSettledCacheAndStartsOneAuthFileRound(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-2", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "unsupported", Provider: "vertex", Type: "vertex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "disabled", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile, Disabled: boolPtr(true)})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}
	service.autoRefreshMu.Lock()
	service.lastAutoRefreshRoundAt = time.Now()
	service.autoRefreshMu.Unlock()
	service.refreshTasks = map[string]*RefreshTaskRecord{
		"auth-1":   {AuthIndex: "auth-1", Status: RefreshTaskStatusCompleted, Quota: &CheckResponse{ID: "auth-1"}, RefreshedAt: time.Now().Add(-time.Hour)},
		"disabled": {AuthIndex: "disabled", Status: RefreshTaskStatusCompleted, Quota: &CheckResponse{ID: "disabled"}, RefreshedAt: time.Now().Add(-time.Hour)},
	}

	status, err := service.StartInspection(context.Background())
	if err != nil {
		t.Fatalf("StartInspection returned error: %v", err)
	}
	if status.Total != 3 || status.Cached != 0 || status.Unknown != 1 || !status.Running || status.Completed || status.CompletedAt != nil {
		t.Fatalf("expected fresh running inspection without stale cache, got %+v", status)
	}
	if _, err := service.GetRefreshTaskByAuthIndex(context.Background(), "unsupported"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected unsupported auth file to stay out of inspection cache, got %v", err)
	}

	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusRunning)
	waitForRefreshTask(t, service, "auth-2", RefreshTaskStatusRunning)
	close(block)
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	waitForRefreshTask(t, service, "auth-2", RefreshTaskStatusCompleted)
	finalStatus, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("GetInspectionStatus returned error: %v", err)
	}
	if finalStatus.Total != 3 || finalStatus.Cached != 2 || finalStatus.Unknown != 1 || finalStatus.Normal != 2 || !finalStatus.Completed || finalStatus.CompletedAt == nil {
		t.Fatalf("expected completed inspection status, got %+v", finalStatus)
	}
	if handler.callCount() != 2 {
		t.Fatalf("expected inspection to refresh two enabled auth files, got %d calls", handler.callCount())
	}
}

func TestInspectionStatusUsesRefreshTaskIdentitySnapshot(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Name: "Original Account", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile, FileName: quotaStringPtr("original.json")})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}

	if _, err := service.StartInspection(context.Background()); err != nil {
		t.Fatalf("StartInspection returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusRunning)
	if err := db.Model(&entities.UsageIdentity{}).Where("identity = ?", "auth-1").Updates(map[string]any{"name": "Renamed Account", "type": "gemini-cli", "file_name": "renamed.json"}).Error; err != nil {
		t.Fatalf("rename usage identity returned error: %v", err)
	}

	close(block)
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	status, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("GetInspectionStatus returned error: %v", err)
	}
	if len(status.Results) != 1 {
		t.Fatalf("expected one inspection result, got %+v", status.Results)
	}
	if status.Results[0].Name != "Original Account" || status.Results[0].Type != "claude" || status.Results[0].FileName == nil || *status.Results[0].FileName != "original.json" {
		t.Fatalf("expected task identity snapshot to be reused, got %+v", status.Results[0])
	}
}

func TestSortInspectionResultsUsesAuthIndexForMatchingRefreshTime(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 30, 0, 0, time.UTC)
	results := []InspectionResult{
		{AuthIndex: "beta", RefreshedAt: &now},
		{AuthIndex: "alpha", RefreshedAt: &now},
		{AuthIndex: "pending"},
	}

	sortInspectionResults(results)

	if results[0].AuthIndex != "alpha" || results[1].AuthIndex != "beta" || results[2].AuthIndex != "pending" {
		t.Fatalf("expected matching timestamps to sort by auth_index with nil refreshed_at last, got %+v", results)
	}
}

func TestInspectionStatusCachesCompletedAtWhenExplicitInspectionRoundSettles(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-2", Provider: "claude", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	block := make(chan struct{})
	handler := &refreshHandlerStub{block: block, output: ProviderOutput{Result: ClaudeResult{Usage: &ClaudeUsagePayload{FiveHour: &ClaudeUsageWindow{Utilization: 25}}}}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))
	service.refreshCooldown = func(time.Duration) {}

	if _, err := service.StartInspection(context.Background()); err != nil {
		t.Fatalf("StartInspection returned error: %v", err)
	}
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusRunning)
	waitForRefreshTask(t, service, "auth-2", RefreshTaskStatusRunning)
	close(block)
	waitForRefreshTask(t, service, "auth-1", RefreshTaskStatusCompleted)
	waitForRefreshTask(t, service, "auth-2", RefreshTaskStatusCompleted)

	first, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("first GetInspectionStatus returned error: %v", err)
	}
	if !first.Completed || first.CompletedAt == nil || first.CompletedAt.IsZero() {
		t.Fatalf("expected completed inspection to expose cached completed_at, got %+v", first)
	}
	second, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("second GetInspectionStatus returned error: %v", err)
	}
	if second.CompletedAt == nil || !second.CompletedAt.Equal(*first.CompletedAt) {
		t.Fatalf("expected completed_at to stay cached, first=%v second=%v", first.CompletedAt, second.CompletedAt)
	}
	service.resetInspectionCompletedAt()
	time.Sleep(time.Millisecond)
	reset, err := service.GetInspectionStatus(context.Background())
	if err != nil {
		t.Fatalf("reset GetInspectionStatus returned error: %v", err)
	}
	if reset.CompletedAt == nil || reset.CompletedAt.Equal(*first.CompletedAt) {
		t.Fatalf("expected completed_at to be recorded again after reset, first=%v reset=%v", first.CompletedAt, reset.CompletedAt)
	}
}

func TestRefreshTaskCachesConfiguredHTTPError(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	handler := &refreshHandlerStub{err: ProviderHTTPError{StatusCode: 401, Message: "expired token"}}
	service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"claude": handler}))

	response, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	task := waitForRefreshTask(t, service, response.Tasks[0].AuthIndex, RefreshTaskStatusFailed)
	if task.HTTPStatusCode == nil || *task.HTTPStatusCode != 401 {
		t.Fatalf("expected task to expose HTTP status 401, got %+v", task)
	}
	if task.RefreshedAt == nil || task.RefreshedAt.IsZero() {
		t.Fatalf("expected failed task to expose refreshed_at, got %+v", task)
	}
	if task.ExpiresAt == nil || task.ExpiresAt.Sub(*task.RefreshedAt) != RefreshErrorCacheTTL {
		t.Fatalf("expected 401 cache TTL %s, got refreshedAt=%v expiresAt=%v", RefreshErrorCacheTTL, task.RefreshedAt, task.ExpiresAt)
	}

	cache, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("GetCachedQuota returned error: %v", err)
	}
	if len(cache.Items) != 1 || cache.Items[0].Status != RefreshTaskStatusFailed || cache.Items[0].HTTPStatusCode == nil || *cache.Items[0].HTTPStatusCode != 401 {
		t.Fatalf("expected cached failed item with HTTP 401, got %+v", cache.Items)
	}
	if cache.Items[0].RefreshedAt == nil || cache.Items[0].RefreshedAt.IsZero() {
		t.Fatalf("expected cached failed item to expose refreshed_at, got %+v", cache.Items[0])
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
	if identity.Name == "" {
		identity.Name = identity.Identity
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatalf("seed usage identity %q: %v", identity.Identity, err)
	}
}

func waitForRefreshTask(t *testing.T, service *Service, authIndex string, status RefreshTaskStatus) RefreshTaskResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var task RefreshTaskResponse
	var err error
	for time.Now().Before(deadline) {
		task, err = service.GetRefreshTaskByAuthIndex(context.Background(), authIndex)
		if err == nil && task.Status == status {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("auth_index %s did not reach status %s, last task=%+v err=%v", authIndex, status, task, err)
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

func quotaStringPtr(value string) *string {
	return &value
}

func quotaIntPtr(value int) *int {
	return &value
}
