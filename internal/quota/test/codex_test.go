package test

import (
	"context"
	"encoding/json"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestCodexProviderUsesAccountIDForUsageRequest(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":25,"limit_window_seconds":18000,"reset_after_seconds":1200}},"code_review_rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":40,"limit_window_seconds":18000,"reset_after_seconds":600}},"additional_rate_limits":[{"limit_name":"codex-spark","metered_feature":"codex-spark","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":12,"limit_window_seconds":18000,"reset_after_seconds":900}}}]}`,
		Body:       json.RawMessage(`{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":25,"limit_window_seconds":18000,"reset_after_seconds":1200}},"code_review_rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":40,"limit_window_seconds":18000,"reset_after_seconds":600}},"additional_rate_limits":[{"limit_name":"codex-spark","metered_feature":"codex-spark","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":12,"limit_window_seconds":18000,"reset_after_seconds":900}}}]}`),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "codex-auth",
		AccountID: stringPtr("acct_123"),
	}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "codex" {
		t.Fatalf("expected codex output provider, got %q", output.Provider)
	}
	result, ok := output.Result.(quota.CodexResult)
	if !ok {
		t.Fatalf("expected codex result type, got %T", output.Result)
	}
	if result.Usage == nil || result.Usage.PlanType != "plus" {
		t.Fatalf("expected parsed usage payload, got %#v", result.Usage)
	}
	if result.Usage.RateLimit == nil || result.Usage.RateLimit.PrimaryWindow == nil || result.Usage.RateLimit.PrimaryWindow.UsedPercent != 25 {
		t.Fatalf("expected parsed rate limit payload, got %#v", result.Usage.RateLimit)
	}
	if result.Usage.CodeReviewRateLimit == nil || result.Usage.CodeReviewRateLimit.PrimaryWindow == nil || result.Usage.CodeReviewRateLimit.PrimaryWindow.UsedPercent != 40 {
		t.Fatalf("expected parsed code review rate limit payload, got %#v", result.Usage.CodeReviewRateLimit)
	}
	if len(result.Usage.AdditionalRateLimits) != 1 || result.Usage.AdditionalRateLimits[0].LimitName != "codex-spark" || result.Usage.AdditionalRateLimits[0].RateLimit == nil {
		t.Fatalf("expected parsed additional rate limit payload, got %#v", result.Usage.AdditionalRateLimits)
	}
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal codex result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"usage":{"planType":"plus"`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected codex result JSON: %s", body)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("expected one api-call request, got %d", len(caller.requests))
	}
	request := caller.requests[0]
	if request.AuthIndex != "codex-auth" || request.Method != "GET" || request.URL != "https://chatgpt.com/backend-api/wham/usage" {
		t.Fatalf("unexpected api-call request: %+v", request)
	}
	if request.Header["Authorization"] != "Bearer $TOKEN$" || request.Header["Content-Type"] != "application/json" || request.Header["User-Agent"] != "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal" || request.Header["Chatgpt-Account-Id"] != "acct_123" {
		t.Fatalf("unexpected api-call headers: %+v", request.Header)
	}
	if request.Data != nil {
		t.Fatalf("expected no data body, got %#v", request.Data)
	}
}

func TestCodexProviderOmitsAccountIDHeaderWhenMissing(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false}}`,
		Body:       json.RawMessage(`{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false}}`),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "codex-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("expected one api-call request without account_id, got %d", len(caller.requests))
	}
	if _, ok := caller.requests[0].Header["Chatgpt-Account-Id"]; ok {
		t.Fatalf("expected account id header to be omitted, got headers: %+v", caller.requests[0].Header)
	}
}

func TestCodexProviderRejectsNonSuccessUsageResponse(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 429,
		BodyText:   `{"error":{"message":"rate limited"}}`,
		Body:       json.RawMessage(`{"error":{"message":"rate limited"}}`),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "codex-auth",
		AccountID: stringPtr("acct_123"),
	}})
	if err == nil || err.Error() != "HTTP 429: rate limited" {
		t.Fatalf("expected target HTTP message, got %v", err)
	}
}
