package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

type antigravityCallerFunc func(context.Context, apicall.Request) (*apicall.Response, error)

func (f antigravityCallerFunc) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	return f(ctx, request)
}

func TestAntigravityProviderQueriesQuotaAndSubscriptionConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	quotaStarted := make(chan struct{})
	subscriptionStarted := make(chan struct{})
	caller := antigravityCallerFunc(func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
		started, otherStarted := quotaStarted, subscriptionStarted
		body := `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.6}]}]}`
		if strings.HasSuffix(request.URL, ":loadCodeAssist") {
			started, otherStarted = subscriptionStarted, quotaStarted
			body = `{"paidTier":{"id":"g1-pro-tier","name":"Pro"}}`
		}
		close(started)
		// 两个请求都开始后才允许返回，串行实现无法通过此屏障。
		select {
		case <-otherStarted:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return quotaAPIResponse(200, body), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions[:1])
	output, err := provider.Check(ctx, quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth", ProjectID: new("project-123")}})
	if err != nil {
		t.Fatalf("concurrent quota check failed: %v", err)
	}
	result := output.Result.(quota.AntigravityResult)
	if result.Quota == nil || len(result.Quota.Groups) != 1 || result.Subscription == nil || result.Subscription.PaidTier == nil || result.Subscription.PaidTier.ID != "g1-pro-tier" {
		t.Fatalf("expected quota and subscription results, got %#v", result)
	}
}

func TestAntigravityProviderCancelsAndJoinsSubscriptionWhenQuotaFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	subscriptionStarted := make(chan struct{})
	subscriptionStopped := make(chan struct{})
	quotaErr := errors.New("quota unavailable")
	caller := antigravityCallerFunc(func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
		if strings.HasSuffix(request.URL, ":loadCodeAssist") {
			if strings.Contains(request.URL, "//cloudcode-pa.") {
				t.Error("subscription fallback must not start after cancellation")
				return nil, ctx.Err()
			}
			close(subscriptionStarted)
			<-ctx.Done()
			close(subscriptionStopped)
			return nil, ctx.Err()
		}
		select {
		case <-subscriptionStarted:
			return nil, quotaErr
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)
	_, err := provider.Check(ctx, quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth", ProjectID: new("project-123")}})
	if !errors.Is(err, quotaErr) {
		t.Fatalf("expected original quota error, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("quota failure waited until the parent deadline: %v", ctx.Err())
	}
	select {
	case <-subscriptionStopped:
	default:
		t.Fatal("Check returned before the subscription request exited")
	}
}

func TestAntigravityProviderSubscriptionFailureDoesNotCancelQuota(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	subscriptionFailed := make(chan struct{})
	caller := antigravityCallerFunc(func(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
		if strings.HasSuffix(request.URL, ":loadCodeAssist") {
			if strings.Contains(request.URL, "//cloudcode-pa.") {
				close(subscriptionFailed)
			}
			return nil, context.DeadlineExceeded
		}
		// 套餐两次请求先失败，仍在途的额度请求必须能正常完成。
		select {
		case <-subscriptionFailed:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return quotaAPIResponse(200, `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.6}]}]}`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)
	output, err := provider.Check(ctx, quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth", ProjectID: new("project-123")}})
	if err != nil {
		t.Fatalf("optional subscription failure canceled quota: %v", err)
	}
	result := output.Result.(quota.AntigravityResult)
	if result.Quota == nil || len(result.Quota.Groups) != 1 || result.Subscription != nil {
		t.Fatalf("expected quota without subscription, got %#v", result)
	}
}

func TestAntigravityProviderParentCancellationStopsBothRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 5)
	stopped := make(chan struct{}, 5)
	caller := antigravityCallerFunc(func(ctx context.Context, _ apicall.Request) (*apicall.Response, error) {
		started <- struct{}{}
		<-ctx.Done()
		stopped <- struct{}{}
		return nil, ctx.Err()
	})
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity, configs.AntigravitySubscriptions)
	done := make(chan error, 1)
	go func() {
		_, err := provider.Check(ctx, quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth", ProjectID: new("project-123")}})
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("both requests did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected parent cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Check did not stop after cancellation")
	}
	if len(stopped) != 2 || len(started) != 0 {
		t.Fatalf("expected both requests stopped without fallbacks, stopped=%d extra requests=%d", len(stopped), len(started))
	}
}
