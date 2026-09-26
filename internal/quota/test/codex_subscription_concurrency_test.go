package test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"gorm.io/gorm"
)

type codexRouteCaller struct {
	mu       sync.Mutex
	requests []apicall.Request
	call     func(context.Context, apicall.Request) (*apicall.Response, error)
}

func (c *codexRouteCaller) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, request)
	c.mu.Unlock()
	return c.call(ctx, request)
}

func (c *codexRouteCaller) ResetQuota(context.Context, string) error { return nil }

func (c *codexRouteCaller) snapshot() []apicall.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]apicall.Request(nil), c.requests...)
}

func TestCodexRefreshQueriesSubscriptionInParallelAndCalculatesStatsBeforeItFinishes(t *testing.T) {
	db := openQuotaTestDatabase(t)
	accountID := "acct_123"
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID})
	usageStarted := make(chan time.Time, 1)
	subscriptionStarted := make(chan time.Time, 1)
	releaseUsage := make(chan struct{})
	releaseSubscription := make(chan struct{})
	var releaseUsageOnce, releaseSubscriptionOnce sync.Once
	finishUsage := func() { releaseUsageOnce.Do(func() { close(releaseUsage) }) }
	finishSubscription := func() { releaseSubscriptionOnce.Do(func() { close(releaseSubscription) }) }
	t.Cleanup(finishUsage)
	t.Cleanup(finishSubscription)
	usageBody := fmt.Sprintf(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":12,"limit_window_seconds":18000,"reset_at":%d}},"rate_limit_reset_credits":{"available_count":0}}`, time.Now().Add(time.Hour).Unix())
	caller := &codexRouteCaller{call: func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil, fmt.Errorf("missing refresh deadline")
		}
		switch {
		case request.URL == quota.DefaultProviderConfigs().Codex.URL:
			usageStarted <- deadline
			select {
			case <-releaseUsage:
				return quotaAPIResponse(200, usageBody), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case strings.HasPrefix(request.URL, quota.CodexSubscriptionsURL):
			subscriptionStarted <- deadline
			select {
			case <-releaseSubscription:
				return quotaAPIResponse(503, `{"error":"optional unavailable"}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			return nil, fmt.Errorf("unexpected URL %s", request.URL)
		}
	}}
	statsDone := make(chan struct{}, 1)
	statsCallback := func(tx *gorm.DB) {
		if tx.Statement.Table == "usage_events" {
			select {
			case statsDone <- struct{}{}:
			default:
			}
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:codex_subscription_query_stats", statsCallback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:codex_subscription_row_stats", statsCallback); err != nil {
		t.Fatal(err)
	}
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest(), QuotaUpstreamResponsesEnabled: true})
	t.Cleanup(service.StopRefreshTasks)
	setRefreshCooldown(service, func(time.Duration) {})
	if _, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual}); err != nil {
		t.Fatal(err)
	}
	usageDeadline := waitForDeadline(t, usageStarted, "usage start")
	subscriptionDeadline := waitForDeadline(t, subscriptionStarted, "subscription start before usage release")
	if !usageDeadline.Equal(subscriptionDeadline) {
		t.Fatalf("usage deadline %s differs from subscription deadline %s", usageDeadline, subscriptionDeadline)
	}
	finishUsage()
	select {
	case <-statsDone:
	case <-time.After(2 * time.Second):
		t.Fatal("window token/cost calculation waited for subscription")
	}
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "codex-auth")
	if err != nil || task.Status != quota.RefreshTaskStatusRunning {
		t.Fatalf("task completed before subscription settled: %+v, %v", task, err)
	}
	finishSubscription()
	completed := waitForRefreshTask(t, service, "codex-auth", quota.RefreshTaskStatusCompleted)
	if completed.Quota == nil || len(completed.Quota.Quota) == 0 || completed.Quota.Quota[0].WindowUsageTokens == nil || completed.Quota.Quota[0].WindowUsageCost == nil {
		t.Fatalf("completed cache lost window token/cost after optional failure: %+v", completed.Quota)
	}
	if len(completed.UpstreamResponses) != 2 {
		t.Fatalf("upstream collector did not retain both requests: %+v", completed.UpstreamResponses)
	}
}

func TestCodexSubscriptionDeadlineKeepsCompletedQuota(t *testing.T) {
	db := openQuotaTestDatabase(t)
	accountID := "acct_123"
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID})
	subscriptionStarted := make(chan struct{}, 1)
	subscriptionExited := make(chan struct{}, 1)
	usageBody := fmt.Sprintf(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":12,"limit_window_seconds":18000,"reset_at":%d}},"rate_limit_reset_credits":{"available_count":0}}`, time.Now().Add(time.Hour).Unix())
	caller := &codexRouteCaller{call: func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
		switch {
		case request.URL == quota.DefaultProviderConfigs().Codex.URL:
			return quotaAPIResponse(200, usageBody), nil
		case strings.HasPrefix(request.URL, quota.CodexSubscriptionsURL):
			subscriptionStarted <- struct{}{}
			<-ctx.Done()
			subscriptionExited <- struct{}{}
			return nil, ctx.Err()
		default:
			return nil, fmt.Errorf("unexpected URL %s", request.URL)
		}
	}}
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
	t.Cleanup(service.StopRefreshTasks)
	setRefreshCooldown(service, func(time.Duration) {})
	refreshContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	service.SetRefreshContext(refreshContext)
	if _, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-subscriptionStarted:
	case <-time.After(time.Second):
		t.Fatal("subscription request did not start")
	}
	select {
	case <-subscriptionExited:
	case <-time.After(3 * time.Second):
		t.Fatal("subscription request did not exit at task deadline")
	}
	completed := waitForRefreshTask(t, service, "codex-auth", quota.RefreshTaskStatusCompleted)
	if completed.Quota == nil || len(completed.Quota.Quota) == 0 || completed.Quota.Quota[0].WindowUsageTokens == nil || completed.Quota.Quota[0].WindowUsageCost == nil {
		t.Fatalf("optional deadline lost completed quota stats: %+v", completed.Quota)
	}
}

func TestCodexQuotaFailureCancelsSubscriptionGoroutine(t *testing.T) {
	db := openQuotaTestDatabase(t)
	accountID := "acct_123"
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID})
	subscriptionStarted := make(chan struct{}, 1)
	subscriptionExited := make(chan struct{}, 1)
	caller := &codexRouteCaller{call: func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
		switch {
		case request.URL == quota.DefaultProviderConfigs().Codex.URL:
			select {
			case <-subscriptionStarted:
				return quotaAPIResponse(503, `{"error":"quota unavailable"}`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case strings.HasPrefix(request.URL, quota.CodexSubscriptionsURL):
			subscriptionStarted <- struct{}{}
			<-ctx.Done()
			subscriptionExited <- struct{}{}
			return nil, ctx.Err()
		default:
			return nil, fmt.Errorf("unexpected URL %s", request.URL)
		}
	}}
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
	t.Cleanup(service.StopRefreshTasks)
	setRefreshCooldown(service, func(time.Duration) {})
	if _, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual}); err != nil {
		t.Fatal(err)
	}
	waitForRefreshTask(t, service, "codex-auth", quota.RefreshTaskStatusFailed)
	select {
	case <-subscriptionExited:
	default:
		t.Fatal("quota failure left subscription goroutine running")
	}
}

func TestCodexTaskCancellationStopsBothRequests(t *testing.T) {
	db := openQuotaTestDatabase(t)
	accountID := "acct_123"
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID})
	started := make(chan struct{}, 2)
	exited := make(chan struct{}, 2)
	caller := &codexRouteCaller{call: func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
		started <- struct{}{}
		<-ctx.Done()
		exited <- struct{}{}
		return nil, ctx.Err()
	}}
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
	t.Cleanup(service.StopRefreshTasks)
	setRefreshCooldown(service, func(time.Duration) {})
	refreshContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	service.SetRefreshContext(refreshContext)
	if _, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("both requests did not start")
		}
	}
	cancel()
	waitForRefreshTask(t, service, "codex-auth", quota.RefreshTaskStatusFailed)
	for range 2 {
		select {
		case <-exited:
		default:
			t.Fatal("canceled task left an upstream request running")
		}
	}
}

type nonCodexSubscriptionProbe struct {
	refreshHandlerStub
	subscriptionCalled chan struct{}
}

func (p *nonCodexSubscriptionProbe) FetchSubscriptionActiveUntil(context.Context, quota.ProviderInput) *time.Time {
	p.subscriptionCalled <- struct{}{}
	return nil
}

func TestNonCodexManualRefreshDoesNotStartSubscription(t *testing.T) {
	db := openQuotaTestDatabase(t)
	accountID := "acct_123"
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "claude-auth", Type: "claude", Provider: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID})
	probe := &nonCodexSubscriptionProbe{refreshHandlerStub: refreshHandlerStub{output: quota.ProviderOutput{Result: quota.ClaudeResult{Usage: &quota.ClaudeUsagePayload{FiveHour: &quota.ClaudeUsageWindow{Utilization: 25}}}}}, subscriptionCalled: make(chan struct{}, 1)}
	service := newQuotaRefreshService(t, db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": probe}))
	if _, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"claude-auth"}, Source: quota.RefreshSourceManual}); err != nil {
		t.Fatal(err)
	}
	completed := waitForRefreshTask(t, service, "claude-auth", quota.RefreshTaskStatusCompleted)
	if probe.callCount() != 1 || completed.Quota == nil || len(completed.Quota.Quota) == 0 {
		t.Fatalf("non-Codex refresh changed: calls=%d quota=%+v", probe.callCount(), completed.Quota)
	}
	select {
	case <-probe.subscriptionCalled:
		t.Fatal("non-Codex refresh started subscription lookup")
	default:
	}
}

func waitForDeadline(t *testing.T, input <-chan time.Time, event string) time.Time {
	t.Helper()
	select {
	case deadline := <-input:
		return deadline
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", event)
		return time.Time{}
	}
}
