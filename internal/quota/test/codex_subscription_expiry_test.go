package test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestCodexRefreshWritesOfficialSubscriptionExpiry(t *testing.T) {
	for _, source := range []quota.RefreshSource{quota.RefreshSourceManual, quota.RefreshSourceScheduled} {
		t.Run(string(source), func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			accountID := "acct /123"
			oldExpiry := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			if source == quota.RefreshSourceScheduled {
				oldExpiry = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
			}
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID, ActiveUntil: &oldExpiry})
			caller := newCodexSubscriptionRouteCaller(
				quotaAPIResponse(200, `{"plan_type":"plus","rate_limit":{"allowed":true},"rate_limit_reset_credits":{"available_count":0}}`),
				quotaAPIResponse(200, `{"active_until":"2026-11-01T00:00:00Z"}`), nil)
			service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
			t.Cleanup(service.StopRefreshTasks)
			setRefreshCooldown(service, func(time.Duration) {})
			refresh, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: source})
			if err != nil || len(refresh.Tasks) != 1 {
				t.Fatalf("Refresh: %+v, %v", refresh, err)
			}
			waitForRefreshTask(t, service, "codex-auth", quota.RefreshTaskStatusCompleted)
			got := readCodexExpiry(t, db)
			if want := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
				t.Fatalf("official expiry = %s, want %s", got, want)
			}
			requests := caller.snapshot()
			if len(requests) != 2 {
				t.Fatalf("subscription api-call requests: %+v", requests)
			}
			var subscriptionRequest apicall.Request
			for _, request := range requests {
				if strings.HasPrefix(request.URL, quota.CodexSubscriptionsURL) {
					subscriptionRequest = request
				}
			}
			if subscriptionRequest.URL != quota.CodexSubscriptionsURL+"?account_id="+url.QueryEscape(accountID) || subscriptionRequest.AuthIndex != "codex-auth" || subscriptionRequest.Header["Authorization"] != "Bearer $TOKEN$" || subscriptionRequest.Header["Chatgpt-Account-Id"] != accountID {
				t.Fatalf("subscription api-call request: %+v", requests)
			}
		})
	}
}

func TestCodexSubscriptionFailureKeepsQuotaAndStoredExpiry(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *apicall.Response
	}{
		{"missing", quotaAPIResponse(200, `{"active_until":null}`)},
		{"zero", quotaAPIResponse(200, `{"activeUntil":"0"}`)},
		{"invalid", quotaAPIResponse(200, `{"active_until":"invalid"}`)},
		{"upstream error", quotaAPIResponse(503, `{"error":"unavailable"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			accountID := "acct_123"
			expiry := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID, ActiveUntil: &expiry})
			caller := newCodexSubscriptionRouteCaller(
				quotaAPIResponse(200, `{"plan_type":"plus","rate_limit":{"allowed":true},"rate_limit_reset_credits":{"available_count":0}}`),
				tc.response, nil)
			service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
			t.Cleanup(service.StopRefreshTasks)
			setRefreshCooldown(service, func(time.Duration) {})
			if _, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual}); err != nil {
				t.Fatal(err)
			}
			waitForRefreshTask(t, service, "codex-auth", quota.RefreshTaskStatusCompleted)
			if got := readCodexExpiry(t, db); !got.Equal(expiry) {
				t.Fatalf("expiry changed after optional failure: %s", got)
			}
		})
	}
}

func TestCodexSubscriptionTransportFailureDoesNotFailQuota(t *testing.T) {
	db := openQuotaTestDatabase(t)
	accountID := "acct_123"
	expiry := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &accountID, ActiveUntil: &expiry})
	caller := newCodexSubscriptionRouteCaller(quotaAPIResponse(200, `{"rate_limit":{"allowed":true},"rate_limit_reset_credits":{"available_count":0}}`), nil, errors.New("subscription transport unavailable"))
	service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
	t.Cleanup(service.StopRefreshTasks)
	setRefreshCooldown(service, func(time.Duration) {})
	if _, err := service.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"codex-auth"}, Source: quota.RefreshSourceManual}); err != nil {
		t.Fatal(err)
	}
	waitForRefreshTask(t, service, "codex-auth", quota.RefreshTaskStatusCompleted)
	if got := readCodexExpiry(t, db); !got.Equal(expiry) {
		t.Fatalf("expiry changed after transport failure: %s", got)
	}
}

func TestCodexSubscriptionOnlyFollowsManualOrScheduledRefreshWithAccount(t *testing.T) {
	for _, tc := range []struct {
		source    quota.RefreshSource
		accountID *string
	}{
		{quota.RefreshSourceInspection, new("acct_123")},
		{quota.RefreshSourceCacheBackfill, new("acct_123")},
		{quota.RefreshSourceManual, nil},
	} {
		t.Run(string(tc.source)+"/account="+stringAccountID(tc.accountID), func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: tc.accountID})
			caller := newCodexSubscriptionRouteCaller(quotaAPIResponse(200, `{"rate_limit":{"allowed":true},"rate_limit_reset_credits":{"available_count":0}}`), nil, nil)
			service := quota.NewServiceWithOptions(db, caller, quota.ServiceOptions{PricingCatalog: emptyPricingCatalogForTest()})
			t.Cleanup(service.StopRefreshTasks)
			if _, err := service.Check(context.Background(), quota.CheckRequest{AuthIndex: "codex-auth", Source: tc.source}); err != nil {
				t.Fatal(err)
			}
			if requests := caller.snapshot(); len(requests) != 1 {
				t.Fatalf("unexpected optional subscription request: %+v", requests)
			}
		})
	}
}

func newCodexSubscriptionRouteCaller(usage, subscription *apicall.Response, subscriptionErr error) *codexRouteCaller {
	return &codexRouteCaller{call: func(_ context.Context, request apicall.Request) (*apicall.Response, error) {
		switch {
		case request.URL == quota.DefaultProviderConfigs().Codex.URL:
			return usage, nil
		case strings.HasPrefix(request.URL, quota.CodexSubscriptionsURL):
			return subscription, subscriptionErr
		default:
			return nil, fmt.Errorf("unexpected Codex URL %s", request.URL)
		}
	}}
}

func stringAccountID(value *string) string {
	if value == nil {
		return "nil"
	}
	return *value
}

func TestCodexOfficialExpiryWriteRejectsChangedAccount(t *testing.T) {
	db := openQuotaTestDatabase(t)
	oldAccount := "old"
	newAccount := "new"
	seedUsageIdentity(t, db, entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile, AccountID: &oldAccount})
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, "codex-auth")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entities.UsageIdentity{}).Where("id = ?", identity.ID).Update("account_id", newAccount).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.UpdateCodexUsageIdentityActiveUntil(context.Background(), db, identity, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var updated entities.UsageIdentity
	if err := db.First(&updated, identity.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.ActiveUntil != nil {
		t.Fatalf("stale account response changed expiry: %s", updated.ActiveUntil)
	}
}

func readCodexExpiry(t *testing.T, db *gorm.DB) time.Time {
	t.Helper()
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(context.Background(), db, "codex-auth")
	if err != nil || identity.ActiveUntil == nil {
		t.Fatalf("read codex expiry: %+v, %v", identity, err)
	}
	return *identity.ActiveUntil
}
