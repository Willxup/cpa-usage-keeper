package test

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestCodexProviderUsesAccountIDForUsageRequest(t *testing.T) {
	codexUsageJSON := `{"user_id":"user-k7itHYqWm38P92JR13zywJOr","account_id":"user-k7itHYqWm38P92JR13zywJOr","email":"gykrcvk0839e@hotmail.com","plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":64,"limit_window_seconds":18000,"reset_after_seconds":11676,"reset_at":1778509871},"secondary_window":{"used_percent":10,"limit_window_seconds":604800,"reset_after_seconds":598476,"reset_at":1779096671}},"code_review_rate_limit":null,"additional_rate_limits":null,"rate_limit_reset_credits":{"available_count":1},"credits":{"has_credits":false,"unlimited":false,"overage_limit_reached":false,"balance":"0","approx_local_messages":[0,0],"approx_cloud_messages":[0,0]},"spend_control":{"reached":false,"individual_limit":null},"rate_limit_reached_type":null,"promo":null,"referral_beacon":null}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   codexUsageJSON,
		Body:       json.RawMessage(codexUsageJSON),
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
	if result.Usage.RateLimit == nil || result.Usage.RateLimit.PrimaryWindow == nil || result.Usage.RateLimit.PrimaryWindow.UsedPercent != 64 {
		t.Fatalf("expected parsed rate limit payload, got %#v", result.Usage.RateLimit)
	}
	if result.Usage.RateLimit.SecondaryWindow == nil || result.Usage.RateLimit.SecondaryWindow.UsedPercent != 10 {
		t.Fatalf("expected parsed secondary rate limit payload, got %#v", result.Usage.RateLimit)
	}
	if result.Usage.CodeReviewRateLimit != nil {
		t.Fatalf("expected nil code review rate limit payload, got %#v", result.Usage.CodeReviewRateLimit)
	}
	if result.Usage.AdditionalRateLimits != nil {
		t.Fatalf("expected nil additional rate limit payload, got %#v", result.Usage.AdditionalRateLimits)
	}
	if result.Usage.RateLimitResetCredits == nil || result.Usage.RateLimitResetCredits.AvailableCount == nil || *result.Usage.RateLimitResetCredits.AvailableCount != 1 {
		t.Fatalf("expected parsed reset credits payload, got %#v", result.Usage.RateLimitResetCredits)
	}
	rows := quota.NormalizeQuotaRows(output)
	if len(rows) != 2 || rows[0].PlanType != "plus" || rows[1].PlanType != "plus" {
		t.Fatalf("expected normalized Codex rows to carry planType plus, got %#v", rows)
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

func TestCodexProviderParsesZeroRateLimitResetCredits(t *testing.T) {
	codexUsageJSON := `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false},"rate_limit_reset_credits":{"available_count":0}}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   codexUsageJSON,
		Body:       json.RawMessage(codexUsageJSON),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "codex-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result, ok := output.Result.(quota.CodexResult)
	if !ok {
		t.Fatalf("expected codex result type, got %T", output.Result)
	}
	if result.Usage == nil || result.Usage.RateLimitResetCredits == nil || result.Usage.RateLimitResetCredits.AvailableCount == nil {
		t.Fatalf("expected parsed zero reset credits payload, got %#v", result.Usage)
	}
	if *result.Usage.RateLimitResetCredits.AvailableCount != 0 {
		t.Fatalf("expected zero reset credits available count, got %#v", result.Usage.RateLimitResetCredits.AvailableCount)
	}
}

func TestCodexProviderOmitsRateLimitResetCreditsWhenAvailableCountMissing(t *testing.T) {
	codexUsageJSON := `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false},"rate_limit_reset_credits":{}}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   codexUsageJSON,
		Body:       json.RawMessage(codexUsageJSON),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "codex-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result, ok := output.Result.(quota.CodexResult)
	if !ok {
		t.Fatalf("expected codex result type, got %T", output.Result)
	}
	if result.Usage == nil || result.Usage.RateLimitResetCredits != nil {
		t.Fatalf("expected missing available_count to omit reset credits payload, got %#v", result.Usage)
	}
}

func TestCodexProviderPreservesProWindowUsageFields(t *testing.T) {
	codexUsageJSON := `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":3,"limit_window_seconds":18000,"reset_after_seconds":13422,"reset_at":1780331042,"window_usage_tokens":11368055,"window_usage_cost":14.83442025},"secondary_window":{"used_percent":15,"limit_window_seconds":604800,"reset_after_seconds":528051,"reset_at":1780845672,"window_usage_tokens":623087989,"window_usage_cost":614.6869810999999}},"additional_rate_limits":[{"limit_name":"GPT-5.3-Codex-Spark","metered_feature":"codex_bengalfox","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"limit_window_seconds":18000,"reset_after_seconds":17698,"reset_at":1780335318,"window_usage_tokens":393311,"window_usage_cost":0.458464},"secondary_window":{"used_percent":0,"limit_window_seconds":604800,"reset_after_seconds":568595,"reset_at":1780886215,"window_usage_tokens":418184136,"window_usage_cost":405.1611734}}}]}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   codexUsageJSON,
		Body:       json.RawMessage(codexUsageJSON),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "codex-pro-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)

	primary := findCodexQuotaRow(t, rows, "rate_limit.primary_window")
	assertWindowUsage(t, primary, 11368055, 14.83442025)
	secondary := findCodexQuotaRow(t, rows, "rate_limit.secondary_window")
	assertWindowUsage(t, secondary, 623087989, 614.6869810999999)
	additional := findCodexQuotaRow(t, rows, "additional_rate_limits.GPT-5.3-Codex-Spark.primary_window")
	assertWindowUsage(t, additional, 393311, 0.458464)
	if additional.Scope != "additional" || additional.Metric != "codex_bengalfox" || additional.PlanType != "pro" {
		t.Fatalf("expected additional row metadata to survive normalization, got %#v", additional)
	}
	additionalSecondary := findCodexQuotaRow(t, rows, "additional_rate_limits.GPT-5.3-Codex-Spark.secondary_window")
	assertWindowUsage(t, additionalSecondary, 418184136, 405.1611734)
	if additionalSecondary.Scope != "additional" || additionalSecondary.Metric != "codex_bengalfox" || additionalSecondary.PlanType != "pro" {
		t.Fatalf("expected additional secondary row metadata to survive normalization, got %#v", additionalSecondary)
	}
}

func TestCodexProviderTreatsNullWindowUsageAsMissingAndPreservesCamelCaseZero(t *testing.T) {
	codexUsageJSON := `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"limit_window_seconds":18000,"window_usage_tokens":null,"window_usage_cost":null},"secondary_window":{"used_percent":0,"limit_window_seconds":604800,"windowUsageTokens":0,"windowUsageCost":0}}}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   codexUsageJSON,
		Body:       json.RawMessage(codexUsageJSON),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "codex-pro-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)

	primary := findCodexQuotaRow(t, rows, "rate_limit.primary_window")
	if primary.WindowUsageTokens != nil || primary.WindowUsageCost != nil {
		t.Fatalf("expected null provider window usage to stay missing, got tokens=%#v cost=%#v", primary.WindowUsageTokens, primary.WindowUsageCost)
	}
	secondary := findCodexQuotaRow(t, rows, "rate_limit.secondary_window")
	assertWindowUsage(t, secondary, 0, 0)
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

func TestCodexProviderConsumesRateLimitResetCredit(t *testing.T) {
	resetJSON := `{"code":"reset","windows_reset":2}`
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   resetJSON,
		Body:       json.RawMessage(resetJSON),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)
	resetter, ok := provider.(quota.ProviderResetter)
	if !ok {
		t.Fatalf("expected codex provider to implement ProviderResetter")
	}

	output, err := resetter.Reset(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "codex-auth",
		AccountID: stringPtr("acct_123"),
	}})
	if err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	if output.Code != "reset" || output.WindowsReset != 2 {
		t.Fatalf("unexpected reset output: %+v", output)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("expected one api-call request, got %d", len(caller.requests))
	}
	request := caller.requests[0]
	if request.AuthIndex != "codex-auth" || request.Method != "POST" || request.URL != quota.CodexRateLimitResetCreditsConsumeURL {
		t.Fatalf("unexpected api-call request: %+v", request)
	}
	if request.Header["Chatgpt-Account-Id"] != "acct_123" {
		t.Fatalf("unexpected api-call headers: %+v", request.Header)
	}
	dataMap, ok := request.Data.(map[string]string)
	if !ok || dataMap["redeem_request_id"] == "" {
		t.Fatalf("expected redeem_request_id in data map, got %#v", request.Data)
	}
	uuidV4Pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidV4Pattern.MatchString(dataMap["redeem_request_id"]) {
		t.Fatalf("expected redeem_request_id to be UUID v4, got %q", dataMap["redeem_request_id"])
	}
}

func TestCodexProviderRejectsNonSuccessResetResponse(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 429,
		BodyText:   `{"error":{"message":"rate limited"}}`,
		Body:       json.RawMessage(`{"error":{"message":"rate limited"}}`),
	}}}
	provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)
	resetter, ok := provider.(quota.ProviderResetter)
	if !ok {
		t.Fatalf("expected codex provider to implement ProviderResetter")
	}

	_, err := resetter.Reset(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "codex-auth"}})
	if err == nil || err.Error() != "HTTP 429: rate limited" {
		t.Fatalf("expected target HTTP message, got %v", err)
	}
}

func TestCodexProviderRejectsMalformedResetResponse(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "non reset code", body: `{"code":"noop","windows_reset":2}`},
		{name: "missing windows reset", body: `{"code":"reset"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caller := &recordingManagementCaller{responses: []*apicall.Response{{
				StatusCode: 200,
				BodyText:   tc.body,
				Body:       json.RawMessage(tc.body),
			}}}
			provider := quota.NewCodexProvider(caller, quota.DefaultProviderConfigs().Codex)
			resetter, ok := provider.(quota.ProviderResetter)
			if !ok {
				t.Fatalf("expected codex provider to implement ProviderResetter")
			}

			_, err := resetter.Reset(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "codex-auth"}})
			if err == nil {
				t.Fatalf("expected malformed reset response to return error")
			}
		})
	}
}

func findCodexQuotaRow(t *testing.T, rows []quota.QuotaRow, key string) quota.QuotaRow {
	t.Helper()
	for _, row := range rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("missing quota row %q in %#v", key, rows)
	return quota.QuotaRow{}
}

func assertWindowUsage(t *testing.T, row quota.QuotaRow, tokens int64, cost float64) {
	t.Helper()
	if row.WindowUsageTokens == nil || *row.WindowUsageTokens != tokens {
		t.Fatalf("expected %s window usage tokens %d, got %#v", row.Key, tokens, row.WindowUsageTokens)
	}
	if row.WindowUsageCost == nil || math.Abs(*row.WindowUsageCost-cost) > 0.000000001 {
		t.Fatalf("expected %s window usage cost %.8f, got %#v", row.Key, cost, row.WindowUsageCost)
	}
}
