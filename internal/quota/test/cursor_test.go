package test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

const (
	cursorPlanInfoURL     = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetPlanInfo"
	cursorPeriodUsageURL  = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage"
	cursorAgentUsageURL   = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetSandUsageStatus"
	cursorEmptyJSONObject = `{}`
)

func TestCursorProviderCallsRequiredPeriodUsage(t *testing.T) {
	periodJSON := `{"billingCycleStart":"1787248499000","billingCycleEnd":"1789830899000","planUsage":{"totalSpend":183,"remaining":39817,"limit":40000}}`
	planJSON := `{"planInfo":{"planName":"Ultra","includedAmountCents":20000,"price":"$200/mo","billingCycleEnd":"1789830899000"}}`
	agentJSON := `{"usagePercent":17,"hasNonZeroIncludedLimit":true,"hasAvailableUsage":true,"currentPeriodStart":"2026-08-13T00:00:00Z","nextResetTimestampUtc":"2026-08-20T00:00:00Z"}`
	caller := newCursorManagementCaller(map[string]*apicall.Response{
		cursorPlanInfoURL:    cursorJSONResponse(planJSON),
		cursorPeriodUsageURL: cursorJSONResponse(periodJSON),
		cursorAgentUsageURL:  cursorJSONResponse(agentJSON),
	})
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewCursorProvider(caller, configs.CursorPlan, configs.CursorPeriod, configs.CursorAgent)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "4607c833fca18295"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "cursor" {
		t.Fatalf("expected cursor output provider, got %q", output.Provider)
	}
	result, ok := output.Result.(quota.CursorResult)
	if !ok {
		t.Fatalf("expected cursor result type, got %T", output.Result)
	}
	if result.Period == nil || result.Period.PlanUsage == nil || result.Period.PlanUsage.Limit != 40000 || result.Period.PlanUsage.TotalSpend != 183 {
		t.Fatalf("expected parsed period usage, got %#v", result.Period)
	}
	if result.Plan == nil || result.Plan.PlanInfo == nil || result.Plan.PlanInfo.PlanName != "Ultra" || result.Plan.PlanInfo.Price != "$200/mo" {
		t.Fatalf("expected parsed plan info, got %#v", result.Plan)
	}
	if result.Agent == nil || result.Agent.UsagePercent == nil || *result.Agent.UsagePercent != 17 {
		t.Fatalf("expected parsed agent usage, got %#v", result.Agent)
	}

	requests := caller.requestsSnapshot()
	if len(requests) != 3 {
		t.Fatalf("expected three api-call requests, got %d", len(requests))
	}
	periodRequest, ok := caller.requestForURL(cursorPeriodUsageURL)
	if !ok || periodRequest.AuthIndex != "4607c833fca18295" || periodRequest.Method != "POST" {
		t.Fatalf("unexpected period request: %+v", periodRequest)
	}
	if periodRequest.Header["Authorization"] != "Bearer $TOKEN$" || periodRequest.Header["Content-Type"] != "application/json" {
		t.Fatalf("unexpected period request headers: %+v", periodRequest.Header)
	}
	if periodRequest.Data != cursorEmptyJSONObject {
		t.Fatalf("expected Connect unary empty object body, got %#v", periodRequest.Data)
	}
	for _, url := range []string{cursorPlanInfoURL, cursorAgentUsageURL} {
		request, ok := caller.requestForURL(url)
		if !ok || request.Method != "POST" || request.Data != cursorEmptyJSONObject {
			t.Fatalf("unexpected optional request for %s: %+v ok=%v", url, request, ok)
		}
	}
}

func TestCursorProviderFailsWhenRequiredPeriodUsageIsUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		response *apicall.Response
	}{
		{name: "non 2xx", response: &apicall.Response{StatusCode: 401, BodyText: `{"message":"unauthorized"}`}},
		{name: "parse failure", response: &apicall.Response{StatusCode: 200, BodyText: `not-json`, Body: json.RawMessage(`null`)}},
		{name: "empty body", response: &apicall.Response{StatusCode: 200}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := newCursorManagementCaller(map[string]*apicall.Response{
				cursorPlanInfoURL:    cursorJSONResponse(`{"planInfo":{"planName":"Ultra"}}`),
				cursorPeriodUsageURL: test.response,
				cursorAgentUsageURL:  cursorJSONResponse(`{"usagePercent":17}`),
			})
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewCursorProvider(caller, configs.CursorPlan, configs.CursorPeriod, configs.CursorAgent)

			_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "cursor-auth"}})
			if err == nil {
				t.Fatal("expected required period usage failure")
			}
			if _, ok := caller.requestForURL(cursorPeriodUsageURL); !ok {
				t.Fatalf("expected the period usage request, got %+v", caller.requestsSnapshot())
			}
		})
	}
}

func TestCursorProviderKeepsPeriodWhenOptionalReadsFail(t *testing.T) {
	periodJSON := `{"planUsage":{"totalSpend":183,"limit":40000}}`
	caller := newCursorManagementCaller(map[string]*apicall.Response{
		cursorPlanInfoURL:    {StatusCode: 503, BodyText: `{"message":"unavailable"}`},
		cursorPeriodUsageURL: cursorJSONResponse(periodJSON),
		cursorAgentUsageURL:  {StatusCode: 200, BodyText: `not-json`, Body: json.RawMessage(`null`)},
	})
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewCursorProvider(caller, configs.CursorPlan, configs.CursorPeriod, configs.CursorAgent)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "cursor-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result, ok := output.Result.(quota.CursorResult)
	if !ok || result.Period == nil || result.Period.PlanUsage == nil || result.Period.PlanUsage.Limit != 40000 {
		t.Fatalf("expected period usage to be preserved, got %#v", output.Result)
	}
	if result.Plan != nil || result.Agent != nil {
		t.Fatalf("expected unavailable optional payloads to be omitted, got %#v", result)
	}
}

func TestCursorProviderPrefersPeriodLimitOverPlanIncludedAmount(t *testing.T) {
	periodJSON := `{"billingCycleEnd":"1789830899000","planUsage":{"totalSpend":132,"limit":40000}}`
	planJSON := `{"planInfo":{"planName":"Ultra","includedAmountCents":2000,"price":"$200/mo","billingCycleEnd":"1780000000000"}}`
	caller := newCursorManagementCaller(map[string]*apicall.Response{
		cursorPlanInfoURL:    cursorJSONResponse(planJSON),
		cursorPeriodUsageURL: cursorJSONResponse(periodJSON),
	})
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewCursorProvider(caller, configs.CursorPlan, configs.CursorPeriod, configs.CursorAgent)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "cursor-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)
	monthly := findQuotaRow(t, rows, "plan.monthly")
	assertFloatField(t, monthly.Used, 132, "monthly used")
	assertFloatField(t, monthly.Limit, 40000, "monthly limit")
	assertFloatField(t, monthly.Remaining, 39868, "monthly remaining")
	assertApproxFloatField(t, monthly.UsedPercent, 0.33, "monthly usedPercent")
}

type cursorManagementCaller struct {
	mu        sync.Mutex
	requests  []apicall.Request
	responses map[string]*apicall.Response
}

func newCursorManagementCaller(responses map[string]*apicall.Response) *cursorManagementCaller {
	return &cursorManagementCaller{responses: responses}
}

func cursorJSONResponse(body string) *apicall.Response {
	return &apicall.Response{StatusCode: 200, BodyText: body, Body: json.RawMessage(body)}
}

func (c *cursorManagementCaller) CallManagementAPI(_ context.Context, request apicall.Request) (*apicall.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	response := c.responses[request.URL]
	if response == nil {
		return &apicall.Response{StatusCode: 500, BodyText: "missing test response"}, nil
	}
	return response, nil
}

func (c *cursorManagementCaller) requestsSnapshot() []apicall.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]apicall.Request(nil), c.requests...)
}

func (c *cursorManagementCaller) requestForURL(targetURL string) (apicall.Request, bool) {
	for _, request := range c.requestsSnapshot() {
		if request.URL == targetURL {
			return request, true
		}
	}
	return apicall.Request{}, false
}
