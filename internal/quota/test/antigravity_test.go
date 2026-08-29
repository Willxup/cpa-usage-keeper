package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestAntigravityProviderUsesProjectIDForQuotaRequest(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{
			StatusCode: 200,
			BodyText:   `{"body":{"groups":[{"displayName":"Gemini Models","description":"Models within this group: Gemini Flash, Gemini Pro","buckets":[{"bucketId":"gemini-5h","displayName":"Five Hour Limit","window":"5h","remainingFraction":0.4,"resetTime":"2026-05-09T12:00:00Z"},{"bucketId":"gemini-weekly","displayName":"Weekly Limit","window":"weekly","remainingFraction":0.9,"resetTime":"2026-05-10T12:00:00Z"}]}]}}`,
			Body:       json.RawMessage(`{"body":{"groups":[{"displayName":"Gemini Models","description":"Models within this group: Gemini Flash, Gemini Pro","buckets":[{"bucketId":"gemini-5h","displayName":"Five Hour Limit","window":"5h","remainingFraction":0.4,"resetTime":"2026-05-09T12:00:00Z"},{"bucketId":"gemini-weekly","displayName":"Weekly Limit","window":"weekly","remainingFraction":0.9,"resetTime":"2026-05-10T12:00:00Z"}]}]}}`),
		},
		{
			StatusCode: 200,
			BodyText:   `{"body":{"currentTier":{"id":"free-tier","name":"Free"},"paidTier":{"id":"g1-ultra-tier","name":"Ultra"}}}`,
			Body:       json.RawMessage(`{"body":{"currentTier":{"id":"free-tier","name":"Free"},"paidTier":{"id":"g1-ultra-tier","name":"Ultra"}}}`),
		},
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "ag-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "antigravity" {
		t.Fatalf("expected antigravity output provider, got %q", output.Provider)
	}
	result, ok := output.Result.(quota.AntigravityResult)
	if !ok {
		t.Fatalf("expected antigravity result type, got %T", output.Result)
	}
	if result.Quota == nil || len(result.Quota.Groups) != 1 || result.Quota.Groups[0].DisplayName != "Gemini Models" || len(result.Quota.Groups[0].Buckets) != 2 || result.Quota.Groups[0].Buckets[0].BucketID != "gemini-5h" || result.Quota.Groups[0].Buckets[0].RemainingFraction == nil || *result.Quota.Groups[0].Buckets[0].RemainingFraction != 0.4 {
		t.Fatalf("expected parsed antigravity quota payload, got %#v", result.Quota)
	}
	if result.Subscription == nil || result.Subscription.PaidTier == nil || result.Subscription.PaidTier.ID != "g1-ultra-tier" {
		t.Fatalf("expected parsed antigravity subscription payload, got %#v", result.Subscription)
	}
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal antigravity result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"groups":[`) || !contains(body, `"displayName":"Gemini Models"`) || !contains(body, `"description":"Models within this group: Gemini Flash, Gemini Pro"`) || !contains(body, `"bucketId":"gemini-5h"`) || !contains(body, `"subscription":{"currentTier"`) || contains(body, `"models"`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected antigravity result JSON: %s", body)
	}
	if len(caller.requests) != 2 {
		t.Fatalf("expected quota and subscription api-call requests, got %d", len(caller.requests))
	}
	request := caller.requests[0]
	if request.AuthIndex != "ag-auth" || request.Method != "POST" || request.URL != "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary" {
		t.Fatalf("unexpected api-call request: %+v", request)
	}
	if request.Header["Authorization"] != "Bearer $TOKEN$" || request.Header["Content-Type"] != "application/json" || request.Header["User-Agent"] != "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)" {
		t.Fatalf("unexpected api-call headers: %+v", request.Header)
	}
	data, ok := request.Data.(map[string]string)
	if !ok || data["project"] != "project-123" {
		t.Fatalf("unexpected api-call data: %#v", request.Data)
	}
	subscriptionRequest := caller.requests[1]
	if subscriptionRequest.AuthIndex != "ag-auth" || subscriptionRequest.Method != "POST" || subscriptionRequest.URL != "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" {
		t.Fatalf("unexpected subscription request: %+v", subscriptionRequest)
	}
	if subscriptionRequest.Header["Authorization"] != "Bearer $TOKEN$" || subscriptionRequest.Header["Content-Type"] != "application/json" || subscriptionRequest.Header["User-Agent"] != "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)" {
		t.Fatalf("unexpected subscription request headers: %+v", subscriptionRequest.Header)
	}
	subscriptionData, ok := subscriptionRequest.Data.(map[string]any)
	metadata, metadataOK := subscriptionData["metadata"].(map[string]string)
	if !ok || !metadataOK || metadata["ideType"] != "ANTIGRAVITY" {
		t.Fatalf("unexpected subscription request data: %#v", subscriptionRequest.Data)
	}
}

func TestAntigravityProviderFallsBackToProdSubscriptionEndpoint(t *testing.T) {
	tests := []struct {
		name          string
		dailyResponse *apicall.Response
	}{
		{name: "daily unavailable", dailyResponse: &apicall.Response{StatusCode: 503, BodyText: `{"message":"daily unavailable"}`}},
		{name: "daily missing tiers", dailyResponse: &apicall.Response{StatusCode: 200, BodyText: `{}`, Body: json.RawMessage(`{}`)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quotaBody := `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.6}]}]}`
			subscriptionBody := `{"currentTier":{"id":"free-tier","name":"Free"},"paidTier":{"id":"g1-pro-tier","name":"Pro"}}`
			caller := &recordingManagementCaller{responses: []*apicall.Response{
				{StatusCode: 200, BodyText: quotaBody, Body: json.RawMessage(quotaBody)},
				test.dailyResponse,
				{StatusCode: 200, BodyText: subscriptionBody, Body: json.RawMessage(subscriptionBody)},
			}}
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

			output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
				Identity:  "ag-auth",
				ProjectID: stringPtr("project-123"),
			}})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			result := output.Result.(quota.AntigravityResult)
			if result.Subscription == nil || result.Subscription.PaidTier == nil || result.Subscription.PaidTier.ID != "g1-pro-tier" {
				t.Fatalf("expected prod subscription fallback result, got %#v", result.Subscription)
			}
			if len(caller.requests) != 3 {
				t.Fatalf("expected quota plus two subscription requests, got %d", len(caller.requests))
			}
			if caller.requests[1].URL != "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" || caller.requests[2].URL != "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" {
				t.Fatalf("unexpected subscription fallback order: %+v", caller.requests[1:])
			}
		})
	}
}

func TestAntigravityProviderRejectsMissingProjectID(t *testing.T) {
	caller := &recordingManagementCaller{}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth"}})
	if !errors.Is(err, quota.ErrProviderInput) || !strings.Contains(err.Error(), "missing project_id parameter") {
		t.Fatalf("expected missing project_id provider input error, got %v", err)
	}
	if len(caller.requests) != 0 {
		t.Fatalf("provider should not call api-call without project_id, got %d requests", len(caller.requests))
	}
}

func TestAntigravityProviderContinuesAfterSuccessfulEmptyQuota(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{StatusCode: 200, BodyText: `{"groups":[]}`, Body: json.RawMessage(`{"groups":[]}`)},
		{StatusCode: 200, BodyText: `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.72}]}]}`, Body: json.RawMessage(`{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.72}]}]}`)},
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity, configs.AntigravitySubscriptions)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "ag-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result := output.Result.(quota.AntigravityResult)
	if result.Quota == nil || len(result.Quota.Groups) != 1 {
		t.Fatalf("expected later non-empty quota response, got %#v", result.Quota)
	}
	if len(caller.requests) != 4 {
		t.Fatalf("expected two quota requests and both subscription fallback requests, got %d requests", len(caller.requests))
	}
}

func TestAntigravityProviderReturnsSuccessfulEmptyQuotaAfterAllEndpoints(t *testing.T) {
	emptyResponse := func() *apicall.Response {
		return &apicall.Response{StatusCode: 200, BodyText: `{"groups":[]}`, Body: json.RawMessage(`{"groups":[]}`)}
	}
	caller := &recordingManagementCaller{responses: []*apicall.Response{emptyResponse(), emptyResponse(), emptyResponse()}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity, configs.AntigravitySubscriptions)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "ag-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err != nil {
		t.Fatalf("expected successful empty quota, got error: %v", err)
	}
	result := output.Result.(quota.AntigravityResult)
	if result.Quota == nil || len(result.Quota.Groups) != 0 {
		t.Fatalf("expected empty quota groups, got %#v", result.Quota)
	}
	if len(caller.requests) != 5 {
		t.Fatalf("expected all quota fallback endpoints plus both subscription fallback requests, got %d requests", len(caller.requests))
	}
}

func TestAntigravityProviderNormalizesFiniteQuotaFractions(t *testing.T) {
	body := `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"percent","window":"5h","remainingFraction":"72%"},{"bucketId":"nan","window":"5h","remainingFraction":"NaN"},{"bucketId":"infinity","window":"5h","remainingFraction":"+Inf"},{"bucketId":"invalid","window":"5h","remainingFraction":"not-a-number"},{"bucketId":"decimal","window":"weekly","remainingFraction":0.5}]}]}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   body,
		Body:       json.RawMessage(body),
	}}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "ag-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result := output.Result.(quota.AntigravityResult)
	if result.Quota == nil || len(result.Quota.Groups) != 1 || len(result.Quota.Groups[0].Buckets) != 2 {
		t.Fatalf("expected only finite quota fractions, got %#v", result.Quota)
	}
	buckets := result.Quota.Groups[0].Buckets
	if buckets[0].BucketID != "percent" || buckets[0].RemainingFraction == nil || *buckets[0].RemainingFraction != 0.72 {
		t.Fatalf("expected percentage string to normalize to 0.72, got %#v", buckets[0])
	}
	if buckets[1].BucketID != "decimal" || buckets[1].RemainingFraction == nil || *buckets[1].RemainingFraction != 0.5 {
		t.Fatalf("expected numeric fraction to remain 0.5, got %#v", buckets[1])
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("finite antigravity quota should marshal: %v", err)
	}
}

func TestAntigravityProviderReturnsTargetErrorMessage(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 500,
		BodyText:   `{"error":"backend unavailable"}`,
		Body:       json.RawMessage(`{"error":"backend unavailable"}`),
	}}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "ag-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err == nil || err.Error() != "HTTP 500: backend unavailable" {
		t.Fatalf("expected target HTTP message, got %v", err)
	}
}

func TestAntigravityProviderKeepsQuotaWhenSubscriptionIsUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		response *apicall.Response
	}{
		{name: "non 2xx", response: &apicall.Response{StatusCode: 503, BodyText: `{"message":"unavailable"}`}},
		{name: "parse failure", response: &apicall.Response{StatusCode: 200, BodyText: `not-json`, Body: json.RawMessage(`null`)}},
		{name: "empty body", response: &apicall.Response{StatusCode: 200}},
		{name: "missing response"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quotaBody := `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.6}]}]}`
			caller := &recordingManagementCaller{responses: []*apicall.Response{
				{StatusCode: 200, BodyText: quotaBody, Body: json.RawMessage(quotaBody)},
				test.response,
				test.response,
			}}
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

			output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth", ProjectID: stringPtr("project-123")}})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			result := output.Result.(quota.AntigravityResult)
			if result.Quota == nil || len(result.Quota.Groups) != 1 || result.Subscription != nil {
				t.Fatalf("expected quota without optional subscription, got %#v", result)
			}
			if len(caller.requests) != 3 {
				t.Fatalf("expected quota and both subscription fallback requests, got %d", len(caller.requests))
			}
		})
	}
}

func TestAntigravityProviderSkipsSubscriptionWhenQuotaFails(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{StatusCode: 200, BodyText: `not-json`, Body: json.RawMessage(`null`)},
		{StatusCode: 200, BodyText: `{}`, Body: json.RawMessage(`{}`)},
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth", ProjectID: stringPtr("project-123")}})
	if err == nil {
		t.Fatal("expected quota failure")
	}
	if len(caller.requests) != 1 || caller.requests[0].URL != configs.Antigravity[0].URL {
		t.Fatalf("expected only the quota request, got %+v", caller.requests)
	}
}

type antigravitySubscriptionContextCaller struct {
	calls                 int
	subscriptionDeadlines []time.Time
	subscriptionErr       error
}

func (c *antigravitySubscriptionContextCaller) CallManagementAPI(ctx context.Context, _ apicall.Request) (*apicall.Response, error) {
	c.calls++
	if c.calls == 1 {
		body := `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.6}]}]}`
		return &apicall.Response{StatusCode: 200, BodyText: body, Body: json.RawMessage(body)}, nil
	}
	deadline, _ := ctx.Deadline()
	c.subscriptionDeadlines = append(c.subscriptionDeadlines, deadline)
	return nil, c.subscriptionErr
}

func TestAntigravityProviderBoundsOptionalSubscriptionContext(t *testing.T) {
	tests := []struct {
		name          string
		parentTimeout time.Duration
		minRemaining  time.Duration
		maxRemaining  time.Duration
	}{
		{name: "ten second helper timeout", minRemaining: 9 * time.Second, maxRemaining: 11 * time.Second},
		{name: "inherits earlier parent deadline", parentTimeout: 250 * time.Millisecond, maxRemaining: 350 * time.Millisecond},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.parentTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, test.parentTimeout)
				defer cancel()
			}
			caller := &antigravitySubscriptionContextCaller{subscriptionErr: context.DeadlineExceeded}
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewAntigravityProvider(caller, configs.Antigravity[:1], configs.AntigravitySubscriptions)

			output, err := provider.Check(ctx, quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth", ProjectID: stringPtr("project-123")}})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			result := output.Result.(quota.AntigravityResult)
			if result.Quota == nil || result.Subscription != nil {
				t.Fatalf("expected quota without optional subscription, got %#v", result)
			}
			if caller.calls != 3 || len(caller.subscriptionDeadlines) != 2 || caller.subscriptionDeadlines[0].IsZero() {
				t.Fatalf("expected both subscription calls with deadlines, calls=%d deadlines=%v", caller.calls, caller.subscriptionDeadlines)
			}
			if !caller.subscriptionDeadlines[0].Equal(caller.subscriptionDeadlines[1]) {
				t.Fatalf("expected subscription fallbacks to share one deadline, got %v", caller.subscriptionDeadlines)
			}
			remaining := time.Until(caller.subscriptionDeadlines[0])
			if remaining < test.minRemaining || remaining > test.maxRemaining {
				t.Fatalf("unexpected subscription deadline remaining %s", remaining)
			}
		})
	}
}
