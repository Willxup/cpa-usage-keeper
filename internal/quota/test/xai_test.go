package test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

const (
	xaiWeeklyBillingURL  = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	xaiMonthlyBillingURL = "https://cli-chat-proxy.grok.com/v1/billing"
)

func TestXAIProviderCallsBillingRequest(t *testing.T) {
	weeklyJSON := `{"config":{"currentPeriod":{"type":"weekly","end":"2026-07-13T00:00:00Z"},"creditUsagePercent":10}}`
	xaiBillingJSON := `{"config":{"monthlyLimit":{"val":20000},"used":{"val":167},"onDemandCap":{"val":0},"billingPeriodStart":"2026-06-01T00:00:00+00:00","billingPeriodEnd":"2026-07-01T00:00:00+00:00","history":[{"billingCycle":{"year":2026,"month":5},"includedUsed":{"val":0},"onDemandUsed":{"val":0},"totalUsed":{"val":0}}]}}`
	caller := newXAIManagementCaller(
		&apicall.Response{StatusCode: 200, BodyText: weeklyJSON, Body: json.RawMessage(weeklyJSON)},
		&apicall.Response{StatusCode: 200, BodyText: xaiBillingJSON, Body: json.RawMessage(xaiBillingJSON)},
	)
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "xai" {
		t.Fatalf("expected xai output provider, got %q", output.Provider)
	}
	result, ok := output.Result.(quota.XAIResult)
	if !ok {
		t.Fatalf("expected xai result type, got %T", output.Result)
	}
	if result.Monthly == nil || result.Monthly.Config == nil || result.Monthly.Config.MonthlyLimit.Val == nil || *result.Monthly.Config.MonthlyLimit.Val != 20000 || result.Monthly.Config.Used.Val == nil || *result.Monthly.Config.Used.Val != 167 || result.Monthly.Config.OnDemandCap.Val == nil || *result.Monthly.Config.OnDemandCap.Val != 0 || len(result.Monthly.Config.History) != 1 {
		t.Fatalf("expected parsed xai monthly payload, got %#v", result.Monthly)
	}
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal xai result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"weekly":{"config"`) || !contains(body, `"monthly":{"config"`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected xai result JSON: %s", body)
	}
	requests := caller.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected two api-call requests, got %d", len(requests))
	}
	request, ok := caller.requestForURL(xaiMonthlyBillingURL)
	if !ok || request.AuthIndex != "xai-auth" || request.Method != "GET" {
		t.Fatalf("unexpected api-call request: %+v", request)
	}
	if request.Header["Authorization"] != "Bearer $TOKEN$" {
		t.Fatalf("unexpected api-call headers: %+v", request.Header)
	}
	if request.Data != nil {
		t.Fatalf("expected no data body, got %#v", request.Data)
	}
}

func TestXAIProviderCallsWeeklyThenMonthlyWithIndependentHeaders(t *testing.T) {
	weeklyJSON := `{"config":{"currentPeriod":{"type":"weekly","start":"2026-07-06T00:00:00Z","end":"2026-07-13T00:00:00Z"},"creditUsagePercent":25}}`
	monthlyJSON := `{"config":{"monthlyLimit":{"val":20000},"used":{"val":167},"billingPeriodEnd":"2026-08-01T00:00:00Z"}}`
	caller := newXAIManagementCaller(
		&apicall.Response{StatusCode: 200, BodyText: weeklyJSON, Body: json.RawMessage(weeklyJSON)},
		&apicall.Response{StatusCode: 200, BodyText: monthlyJSON, Body: json.RawMessage(monthlyJSON)},
	)
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result, ok := output.Result.(quota.XAIResult)
	if !ok {
		t.Fatalf("expected xai result type, got %T", output.Result)
	}
	if result.Weekly == nil || result.Weekly.Config == nil || result.Monthly == nil || result.Monthly.Config == nil {
		t.Fatalf("expected both weekly and monthly payloads, got %#v", result)
	}
	requests := caller.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected weekly and monthly requests, got %d", len(requests))
	}
	seenURLs := make(map[string]bool, 2)
	for index, request := range requests {
		if request.AuthIndex != "xai-auth" || request.Method != "GET" || (request.URL != xaiWeeklyBillingURL && request.URL != xaiMonthlyBillingURL) {
			t.Fatalf("unexpected request %d: %+v", index+1, request)
		}
		seenURLs[request.URL] = true
		if request.Header["Authorization"] != "Bearer $TOKEN$" ||
			request.Header["x-xai-token-auth"] != "xai-grok-cli" ||
			request.Header["x-grok-client-version"] != "0.2.93" {
			t.Fatalf("unexpected request %d headers: %+v", index+1, request.Header)
		}
		if _, ok := request.Header["x-userid"]; ok {
			t.Fatalf("request %d must not include an unverified x-userid: %+v", index+1, request.Header)
		}
		if request.Data != nil {
			t.Fatalf("expected request %d to have no data body, got %#v", index+1, request.Data)
		}
	}
	if !seenURLs[xaiWeeklyBillingURL] || !seenURLs[xaiMonthlyBillingURL] {
		t.Fatalf("expected both billing endpoint URLs, got %+v", seenURLs)
	}
}

func TestXAIProviderCompletesWithEitherValidBillingResult(t *testing.T) {
	weeklyJSON := `{"config":{"currentPeriod":{"type":"weekly","end":"2026-07-13T00:00:00Z"},"creditUsagePercent":25}}`
	monthlyJSON := `{"config":{"monthlyLimit":{"val":20000},"used":{"val":167},"billingPeriodEnd":"2026-08-01T00:00:00Z"}}`
	tests := []struct {
		name        string
		weekly      *apicall.Response
		monthly     *apicall.Response
		wantWeekly  bool
		wantMonthly bool
	}{
		{
			name:        "weekly fails and monthly succeeds",
			weekly:      &apicall.Response{StatusCode: 500, BodyText: "weekly unavailable"},
			monthly:     &apicall.Response{StatusCode: 200, BodyText: monthlyJSON, Body: json.RawMessage(monthlyJSON)},
			wantMonthly: true,
		},
		{
			name:       "weekly succeeds and monthly fails",
			weekly:     &apicall.Response{StatusCode: 200, BodyText: weeklyJSON, Body: json.RawMessage(weeklyJSON)},
			monthly:    &apicall.Response{StatusCode: 500, BodyText: "monthly unavailable"},
			wantWeekly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := newXAIManagementCaller(tt.weekly, tt.monthly)
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

			output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
			if err != nil {
				t.Fatalf("Check returned error for a partial success: %v", err)
			}
			result, ok := output.Result.(quota.XAIResult)
			if !ok {
				t.Fatalf("expected xai result type, got %T", output.Result)
			}
			if (result.Weekly != nil) != tt.wantWeekly || (result.Monthly != nil) != tt.wantMonthly {
				t.Fatalf("unexpected partial result: %#v", result)
			}
			if requests := caller.requestsSnapshot(); len(requests) != 2 {
				t.Fatalf("expected both endpoints to be attempted, got %d requests", len(requests))
			}
		})
	}
}

func TestXAIProviderPreservesCacheableHTTPStatusWhenBothBillingRequestsFail(t *testing.T) {
	caller := newXAIManagementCaller(
		&apicall.Response{StatusCode: 500, BodyText: "weekly unavailable"},
		&apicall.Response{StatusCode: 401, BodyText: "token expired"},
	)
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
	if err == nil {
		t.Fatal("expected both failed billing requests to fail the provider check")
	}
	var httpErr quota.ProviderHTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 401 {
		t.Fatalf("expected cacheable HTTP 401 to be preserved, got %v", err)
	}
	if requests := caller.requestsSnapshot(); len(requests) != 2 {
		t.Fatalf("expected both endpoints to be attempted, got %d requests", len(requests))
	}
}

func TestXAIProviderParsesWeeklyAndMonthlyBillingShapes(t *testing.T) {
	weeklyJSON := `{"config":{"current_period":{"type":"weekly","start":"2026-07-06T00:00:00Z","end":"2026-07-13T00:00:00Z"},"credit_usage_percent":"37.5","product_usage":[{"product":"Grok 4","usage_percent":80},{"product":"Grok Code","usagePercent":"25"},{"product":"No Data","usage_percent":null}]}}`
	monthlyJSON := `{"config":{"monthly_limit":{"val":"1000"},"used":1250,"on_demand_cap":{"val":500},"on_demand_used":{"val":"250"},"billing_period_start":"2026-07-01T00:00:00Z","billing_period_end":"2026-08-01T00:00:00Z"}}`
	caller := newXAIManagementCaller(
		&apicall.Response{StatusCode: 200, BodyText: weeklyJSON, Body: json.RawMessage(weeklyJSON)},
		&apicall.Response{StatusCode: 200, BodyText: monthlyJSON, Body: json.RawMessage(monthlyJSON)},
	)
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result := output.Result.(quota.XAIResult)
	weekly := result.Weekly.Config
	if weekly.CurrentPeriod == nil || weekly.CurrentPeriod.Type != "weekly" || weekly.CurrentPeriod.End != "2026-07-13T00:00:00Z" {
		t.Fatalf("unexpected weekly period: %#v", weekly.CurrentPeriod)
	}
	if weekly.CreditUsagePercent == nil || *weekly.CreditUsagePercent != 37.5 {
		t.Fatalf("unexpected weekly usage percent: %#v", weekly.CreditUsagePercent)
	}
	if len(weekly.ProductUsage) != 3 || weekly.ProductUsage[0].UsagePercent == nil || *weekly.ProductUsage[0].UsagePercent != 80 || weekly.ProductUsage[1].UsagePercent == nil || *weekly.ProductUsage[1].UsagePercent != 25 || weekly.ProductUsage[2].UsagePercent != nil {
		t.Fatalf("unexpected weekly product usage: %#v", weekly.ProductUsage)
	}
	monthly := result.Monthly.Config
	if monthly.MonthlyLimit.Val == nil || *monthly.MonthlyLimit.Val != 1000 || monthly.Used.Val == nil || *monthly.Used.Val != 1250 || monthly.OnDemandCap.Val == nil || *monthly.OnDemandCap.Val != 500 || monthly.OnDemandUsed.Val == nil || *monthly.OnDemandUsed.Val != 250 {
		t.Fatalf("unexpected monthly money values: %#v", monthly)
	}
	if monthly.BillingPeriodStart != "2026-07-01T00:00:00Z" || monthly.BillingPeriodEnd != "2026-08-01T00:00:00Z" {
		t.Fatalf("unexpected monthly billing period: %#v", monthly)
	}
}

func TestXAIProviderDistinguishesEmptyBillingFromExplicitZeroQuota(t *testing.T) {
	t.Run("both empty responses fail", func(t *testing.T) {
		caller := newXAIManagementCaller(
			&apicall.Response{StatusCode: 200, BodyText: `{"config":{}}`, Body: json.RawMessage(`{"config":{}}`)},
			&apicall.Response{StatusCode: 200, BodyText: `{"config":null}`, Body: json.RawMessage(`{"config":null}`)},
		)
		configs := quota.DefaultProviderConfigs()
		provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

		if _, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}}); err == nil {
			t.Fatal("expected empty weekly and monthly billing responses to fail")
		}
	})

	t.Run("explicit zero weekly usage is valid", func(t *testing.T) {
		weeklyJSON := `{"config":{"current_period":{"type":"weekly","end":"2026-07-13T00:00:00Z"},"credit_usage_percent":0}}`
		caller := newXAIManagementCaller(
			&apicall.Response{StatusCode: 200, BodyText: weeklyJSON, Body: json.RawMessage(weeklyJSON)},
			&apicall.Response{StatusCode: 500, BodyText: "monthly unavailable"},
		)
		configs := quota.DefaultProviderConfigs()
		provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

		output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
		if err != nil {
			t.Fatalf("explicit zero quota must remain a valid partial result: %v", err)
		}
		result := output.Result.(quota.XAIResult)
		if result.Weekly == nil || result.Weekly.Config == nil || result.Weekly.Config.CreditUsagePercent == nil || *result.Weekly.Config.CreditUsagePercent != 0 {
			t.Fatalf("unexpected explicit zero weekly result: %#v", result.Weekly)
		}
	})
}

func TestXAIProviderStartsBothEqualBillingSourcesWithoutBlocking(t *testing.T) {
	monthlyJSON := `{"config":{"monthlyLimit":{"val":1000},"used":{"val":250}}}`
	caller := &blockingWeeklyManagementCaller{
		weeklyStarted:  make(chan struct{}),
		monthlyStarted: make(chan struct{}),
		releaseWeekly:  make(chan struct{}),
		monthlyResponse: &apicall.Response{
			StatusCode: 200,
			BodyText:   monthlyJSON,
			Body:       json.RawMessage(monthlyJSON),
		},
	}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)
	type checkResult struct {
		output quota.ProviderOutput
		err    error
	}
	resultCh := make(chan checkResult, 1)
	go func() {
		output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
		resultCh <- checkResult{output: output, err: err}
	}()

	select {
	case <-caller.weeklyStarted:
	case <-time.After(time.Second):
		t.Fatal("weekly billing request did not start")
	}
	select {
	case <-caller.monthlyStarted:
		close(caller.releaseWeekly)
	case <-time.After(200 * time.Millisecond):
		close(caller.releaseWeekly)
		<-resultCh
		t.Fatal("monthly billing request was blocked behind weekly")
	}
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("expected monthly partial success, got %v", result.err)
	}
	xaiResult := result.output.Result.(quota.XAIResult)
	if xaiResult.Monthly == nil || xaiResult.Weekly != nil {
		t.Fatalf("unexpected concurrent partial result: %#v", xaiResult)
	}
}

func TestXAIProviderParsesNestedBodyTextBillingResponse(t *testing.T) {
	inner := `{"config":{"monthlyLimit":{"val":1000},"used":{"val":250},"billingPeriodStart":"2026-06-01T00:00:00+00:00","billingPeriodEnd":"2026-07-01T00:00:00+00:00"}}`
	wrapped := `{"status_code":200,"body":` + strconv.Quote(inner) + `}`
	caller := newXAIManagementCaller(
		&apicall.Response{StatusCode: 500, BodyText: "weekly unavailable"},
		&apicall.Response{StatusCode: 200, BodyText: wrapped, Body: json.RawMessage(wrapped)},
	)
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result, ok := output.Result.(quota.XAIResult)
	if !ok {
		t.Fatalf("expected xai result type, got %T", output.Result)
	}
	if result.Monthly == nil || result.Monthly.Config == nil || result.Monthly.Config.Used.Val == nil || *result.Monthly.Config.Used.Val != 250 {
		t.Fatalf("expected nested body xai monthly payload, got %#v", result.Monthly)
	}
}

func TestXAIProviderAddsXAIUserIDToBothBillingRequests(t *testing.T) {
	weeklyJSON := `{"config":{"currentPeriod":{"type":"weekly","end":"2026-07-13T00:00:00Z"},"creditUsagePercent":10}}`
	monthlyJSON := `{"config":{"monthlyLimit":{"val":20000},"used":{"val":167},"billingPeriodEnd":"2026-08-01T00:00:00Z"}}`
	caller := newXAIManagementCaller(
		&apicall.Response{StatusCode: 200, BodyText: weeklyJSON, Body: json.RawMessage(weeklyJSON)},
		&apicall.Response{StatusCode: 200, BodyText: monthlyJSON, Body: json.RawMessage(monthlyJSON)},
	)
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)
	xaiUserID := "  xai-user-123  "

	if _, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "xai-auth",
		XAIUserID: &xaiUserID,
	}}); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	requests := caller.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected weekly and monthly requests, got %d", len(requests))
	}
	for _, request := range requests {
		if request.Header["x-userid"] != "xai-user-123" {
			t.Fatalf("expected %s request to include trimmed x-userid, got %+v", request.URL, request.Header)
		}
	}
	if _, ok := configs.XAIWeekly.Headers["x-userid"]; ok {
		t.Fatalf("weekly config header template must remain unchanged: %+v", configs.XAIWeekly.Headers)
	}
	if _, ok := configs.XAIMonthly.Headers["x-userid"]; ok {
		t.Fatalf("monthly config header template must remain unchanged: %+v", configs.XAIMonthly.Headers)
	}
}

func TestXAIProviderDoesNotLeakXAIUserIDBetweenIdentities(t *testing.T) {
	weeklyJSON := `{"config":{"currentPeriod":{"type":"weekly","end":"2026-07-13T00:00:00Z"},"creditUsagePercent":10}}`
	monthlyJSON := `{"config":{"monthlyLimit":{"val":20000},"used":{"val":167},"billingPeriodEnd":"2026-08-01T00:00:00Z"}}`
	caller := newXAIManagementCaller(
		&apicall.Response{StatusCode: 200, BodyText: weeklyJSON, Body: json.RawMessage(weeklyJSON)},
		&apicall.Response{StatusCode: 200, BodyText: monthlyJSON, Body: json.RawMessage(monthlyJSON)},
	)
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)
	firstUserID := "first-user"

	if _, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "first-auth",
		XAIUserID: &firstUserID,
	}}); err != nil {
		t.Fatalf("first Check returned error: %v", err)
	}
	if _, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity: "second-auth",
	}}); err != nil {
		t.Fatalf("second Check returned error: %v", err)
	}

	requests := caller.requestsSnapshot()
	if len(requests) != 4 {
		t.Fatalf("expected four billing requests, got %d", len(requests))
	}
	for _, request := range requests {
		switch request.AuthIndex {
		case "first-auth":
			if request.Header["x-userid"] != firstUserID {
				t.Fatalf("expected first identity header on %s, got %+v", request.URL, request.Header)
			}
		case "second-auth":
			if _, ok := request.Header["x-userid"]; ok {
				t.Fatalf("second identity must not inherit x-userid on %s: %+v", request.URL, request.Header)
			}
		default:
			t.Fatalf("unexpected auth index in request: %+v", request)
		}
	}
}

func TestXAIProviderCopiesHeadersForEachBillingRequest(t *testing.T) {
	xaiBillingJSON := `{"config":{"monthlyLimit":{"val":20000},"used":{"val":167},"billingPeriodEnd":"2026-07-01T00:00:00+00:00"}}`
	caller := &mutatingHeaderManagementCaller{response: &apicall.Response{
		StatusCode: 200,
		BodyText:   xaiBillingJSON,
		Body:       json.RawMessage(xaiBillingJSON),
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewXAIProvider(caller, configs.XAIWeekly, configs.XAIMonthly)

	for index := 0; index < 2; index++ {
		if _, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "xai-auth"}}); err != nil {
			t.Fatalf("Check %d returned error: %v", index+1, err)
		}
	}
	if len(caller.authorizations) != 4 {
		t.Fatalf("expected four xai billing requests, got %d", len(caller.authorizations))
	}
	for index, authorization := range caller.authorizations {
		if authorization != "Bearer $TOKEN$" {
			t.Fatalf("expected request %d to start from template authorization header, got %q", index+1, authorization)
		}
	}
}

type xaiManagementCaller struct {
	mu        sync.Mutex
	requests  []apicall.Request
	responses map[string]*apicall.Response
}

func newXAIManagementCaller(weekly *apicall.Response, monthly *apicall.Response) *xaiManagementCaller {
	return &xaiManagementCaller{responses: map[string]*apicall.Response{
		xaiWeeklyBillingURL:  weekly,
		xaiMonthlyBillingURL: monthly,
	}}
}

func (c *xaiManagementCaller) CallManagementAPI(_ context.Context, request apicall.Request) (*apicall.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	response := c.responses[request.URL]
	if response == nil {
		return &apicall.Response{StatusCode: 500, BodyText: "missing test response"}, nil
	}
	return response, nil
}

func (c *xaiManagementCaller) requestsSnapshot() []apicall.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]apicall.Request(nil), c.requests...)
}

func (c *xaiManagementCaller) requestForURL(targetURL string) (apicall.Request, bool) {
	for _, request := range c.requestsSnapshot() {
		if request.URL == targetURL {
			return request, true
		}
	}
	return apicall.Request{}, false
}

type mutatingHeaderManagementCaller struct {
	mu             sync.Mutex
	authorizations []string
	response       *apicall.Response
}

func (c *mutatingHeaderManagementCaller) CallManagementAPI(ctx context.Context, request apicall.Request) (*apicall.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authorizations = append(c.authorizations, request.Header["Authorization"])
	request.Header["Authorization"] = "Bearer leaked-token"
	return c.response, nil
}

type blockingWeeklyManagementCaller struct {
	weeklyStarted   chan struct{}
	monthlyStarted  chan struct{}
	releaseWeekly   chan struct{}
	monthlyResponse *apicall.Response
}

func (c *blockingWeeklyManagementCaller) CallManagementAPI(_ context.Context, request apicall.Request) (*apicall.Response, error) {
	if request.URL == "https://cli-chat-proxy.grok.com/v1/billing?format=credits" {
		close(c.weeklyStarted)
		<-c.releaseWeekly
		return &apicall.Response{StatusCode: 500, BodyText: "weekly unavailable"}, nil
	}
	close(c.monthlyStarted)
	return c.monthlyResponse, nil
}
