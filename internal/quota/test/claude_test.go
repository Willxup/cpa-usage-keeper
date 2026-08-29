package test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestClaudeProviderCallsUsageAndProfile(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{StatusCode: 200, BodyText: `{"five_hour":{"utilization":36,"resets_at":"2026-05-09T12:00:00Z"},"seven_day":{"utilization":72,"resets_at":"2026-05-10T12:00:00Z"},"seven_day_sonnet":{"utilization":18,"resets_at":"2026-05-10T08:00:00Z"},"extra_usage":{"is_enabled":true,"monthly_limit":1000,"used_credits":250,"utilization":25}}`, Body: json.RawMessage(`{"five_hour":{"utilization":36,"resets_at":"2026-05-09T12:00:00Z"},"seven_day":{"utilization":72,"resets_at":"2026-05-10T12:00:00Z"},"seven_day_sonnet":{"utilization":18,"resets_at":"2026-05-10T08:00:00Z"},"extra_usage":{"is_enabled":true,"monthly_limit":1000,"used_credits":250,"utilization":25}}`)},
		{StatusCode: 200, BodyText: `{"account":{"email":"user@example.com","has_claude_pro":true},"organization":{"organization_type":"claude_team","subscription_status":"active"}}`, Body: json.RawMessage(`{"account":{"email":"user@example.com","has_claude_pro":true},"organization":{"organization_type":"claude_team","subscription_status":"active"}}`)},
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "claude" {
		t.Fatalf("expected claude output provider, got %q", output.Provider)
	}
	if len(caller.requests) != 2 {
		t.Fatalf("expected two api-call requests, got %d", len(caller.requests))
	}
	usageRequest := caller.requests[0]
	if usageRequest.AuthIndex != "claude-auth" || usageRequest.Method != "GET" || usageRequest.URL != "https://api.anthropic.com/api/oauth/usage" {
		t.Fatalf("unexpected usage request: %+v", usageRequest)
	}
	if usageRequest.Header["Authorization"] != "Bearer $TOKEN$" || usageRequest.Header["Content-Type"] != "application/json" || usageRequest.Header["anthropic-beta"] != "oauth-2025-04-20" {
		t.Fatalf("unexpected usage request headers: %+v", usageRequest.Header)
	}
	profileRequest := caller.requests[1]
	if profileRequest.AuthIndex != "claude-auth" || profileRequest.Method != "GET" || profileRequest.URL != "https://api.anthropic.com/api/oauth/profile" {
		t.Fatalf("unexpected profile request: %+v", profileRequest)
	}
	if profileRequest.Header["Authorization"] != "Bearer $TOKEN$" || profileRequest.Header["Content-Type"] != "application/json" || profileRequest.Header["anthropic-beta"] != "oauth-2025-04-20" {
		t.Fatalf("unexpected profile request headers: %+v", profileRequest.Header)
	}

	result, ok := output.Result.(quota.ClaudeResult)
	if !ok {
		t.Fatalf("expected claude result type, got %T", output.Result)
	}
	if result.Usage == nil || result.Usage.FiveHour == nil || result.Usage.FiveHour.Utilization != 36 || result.Usage.SevenDay == nil || result.Usage.SevenDay.Utilization != 72 || result.Usage.SevenDaySonnet == nil || result.Usage.SevenDaySonnet.Utilization != 18 || result.Usage.ExtraUsage == nil || result.Usage.ExtraUsage.UsedCredits != 250 || result.Usage.ExtraUsage.Utilization == nil || *result.Usage.ExtraUsage.Utilization != 25 {
		t.Fatalf("expected parsed claude usage payload, got %#v", result.Usage)
	}
	if result.Profile == nil || result.Profile.Account == nil || result.Profile.Account.Email != "user@example.com" || result.Profile.Account.HasClaudePro == nil || !*result.Profile.Account.HasClaudePro {
		t.Fatalf("expected parsed claude profile payload, got %#v", result.Profile)
	}
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal claude result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"usage":{"fiveHour"`) || !contains(body, `"profile":{"account"`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected claude result JSON: %s", body)
	}
}

func TestClaudeProviderKeepsUsageWhenProfileIsUnavailable(t *testing.T) {
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
			caller := &recordingManagementCaller{responses: []*apicall.Response{
				{StatusCode: 200, BodyText: `{"five_hour":{"utilization":36}}`, Body: json.RawMessage(`{"five_hour":{"utilization":36}}`)},
				test.response,
			}}
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile)

			output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude-auth"}})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			result, ok := output.Result.(quota.ClaudeResult)
			if !ok || result.Usage == nil || result.Usage.FiveHour == nil || result.Usage.FiveHour.Utilization != 36 {
				t.Fatalf("expected usage to be preserved, got %#v", output.Result)
			}
			if result.Profile != nil {
				t.Fatalf("expected unavailable profile to be omitted, got %#v", result.Profile)
			}
			if len(caller.requests) != 2 {
				t.Fatalf("expected serial usage and profile requests, got %d", len(caller.requests))
			}
		})
	}
}

func TestClaudeProviderSkipsProfileWhenUsageFails(t *testing.T) {
	tests := []struct {
		name     string
		response *apicall.Response
	}{
		{name: "non 2xx", response: &apicall.Response{StatusCode: 500, BodyText: `{"message":"usage failed"}`}},
		{name: "parse failure", response: &apicall.Response{StatusCode: 200, BodyText: `not-json`, Body: json.RawMessage(`null`)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := &recordingManagementCaller{responses: []*apicall.Response{
				test.response,
				{StatusCode: 200, BodyText: `{}`, Body: json.RawMessage(`{}`)},
			}}
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile)

			_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude-auth"}})
			if err == nil {
				t.Fatal("expected usage failure")
			}
			if len(caller.requests) != 1 || caller.requests[0].URL != configs.ClaudeUsage.URL {
				t.Fatalf("expected only the usage request, got %+v", caller.requests)
			}
		})
	}
}

func TestClaudeProviderKeepsNullSubscriptionFlagsUnknown(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{StatusCode: 200, BodyText: `{}`, Body: json.RawMessage(`{}`)},
		{StatusCode: 200, BodyText: `{"account":{"has_claude_max":null,"has_claude_pro":null}}`, Body: json.RawMessage(`{"account":{"has_claude_max":null,"has_claude_pro":null}}`)},
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result := output.Result.(quota.ClaudeResult)
	if result.Profile == nil || result.Profile.Account == nil {
		t.Fatalf("expected parsed profile account, got %#v", result.Profile)
	}
	if result.Profile.Account.HasClaudeMax != nil || result.Profile.Account.HasClaudePro != nil {
		t.Fatalf("expected null flags to stay unknown, got %#v", result.Profile.Account)
	}
	if subscription := quota.NormalizeSubscription(output); subscription != nil {
		t.Fatalf("expected unknown subscription, got %#v", subscription)
	}
}

type claudeProfileContextCaller struct {
	calls           int
	profileDeadline time.Time
	profileErr      error
}

func (c *claudeProfileContextCaller) CallManagementAPI(ctx context.Context, _ apicall.Request) (*apicall.Response, error) {
	c.calls++
	if c.calls == 1 {
		return &apicall.Response{StatusCode: 200, BodyText: `{"five_hour":{"utilization":36}}`, Body: json.RawMessage(`{"five_hour":{"utilization":36}}`)}, nil
	}
	c.profileDeadline, _ = ctx.Deadline()
	return nil, c.profileErr
}

func TestClaudeProviderBoundsOptionalProfileContext(t *testing.T) {
	tests := []struct {
		name          string
		parentTimeout time.Duration
		maxRemaining  time.Duration
		minRemaining  time.Duration
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
			caller := &claudeProfileContextCaller{profileErr: context.DeadlineExceeded}
			configs := quota.DefaultProviderConfigs()
			provider := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile)

			output, err := provider.Check(ctx, quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude-auth"}})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			result := output.Result.(quota.ClaudeResult)
			if result.Usage == nil || result.Profile != nil {
				t.Fatalf("expected usage without optional profile, got %#v", result)
			}
			if caller.calls != 2 || caller.profileDeadline.IsZero() {
				t.Fatalf("expected profile call with deadline, calls=%d deadline=%v", caller.calls, caller.profileDeadline)
			}
			remaining := time.Until(caller.profileDeadline)
			if remaining < test.minRemaining || remaining > test.maxRemaining {
				t.Fatalf("unexpected profile deadline remaining %s", remaining)
			}
		})
	}
}

func TestClaudeProviderKeepsUsageWhenProfileCallReturnsNetworkError(t *testing.T) {
	caller := &claudeProfileContextCaller{profileErr: errors.New("network unavailable")}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewClaudeProvider(caller, configs.ClaudeUsage, configs.ClaudeProfile)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "claude-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result := output.Result.(quota.ClaudeResult)
	if result.Usage == nil || result.Profile != nil {
		t.Fatalf("expected usage without optional profile, got %#v", result)
	}
}
