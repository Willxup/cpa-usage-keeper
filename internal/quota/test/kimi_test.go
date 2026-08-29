package test

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestKimiProviderCallsUsageRequest(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   `{"usage":{"used":3,"limit":10,"remaining":7,"reset_at":"2026-05-09T12:00:00Z"},"limits":[{"name":"daily","title":"Daily","scope":"request","used":3,"limit":10,"remaining":7,"window":{"duration":1,"timeUnit":"day"},"detail":{"used":3,"limit":10,"remaining":7,"resetIn":3600,"ttl":7200}}]}`,
		Body:       json.RawMessage(`{"usage":{"used":3,"limit":10,"remaining":7,"reset_at":"2026-05-09T12:00:00Z"},"limits":[{"name":"daily","title":"Daily","scope":"request","used":3,"limit":10,"remaining":7,"window":{"duration":1,"timeUnit":"day"},"detail":{"used":3,"limit":10,"remaining":7,"resetIn":3600,"ttl":7200}}]}`),
	}}}
	provider := quota.NewKimiProvider(caller, quota.DefaultProviderConfigs().Kimi)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "kimi-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "kimi" {
		t.Fatalf("expected kimi output provider, got %q", output.Provider)
	}
	result, ok := output.Result.(quota.KimiResult)
	if !ok {
		t.Fatalf("expected kimi result type, got %T", output.Result)
	}
	if result.Usage == nil || result.Usage.Usage == nil || result.Usage.Usage.Limit != 10 || len(result.Usage.Limits) != 1 || result.Usage.Limits[0].Window.Duration != 1 || result.Usage.Limits[0].Title != "Daily" || result.Usage.Limits[0].Detail == nil || result.Usage.Limits[0].Detail.ResetIn != 3600 || result.Usage.Limits[0].TTL != 7200 {
		t.Fatalf("expected parsed kimi usage payload, got %#v", result.Usage)
	}
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal kimi result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"usage":{"usage"`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected kimi result JSON: %s", body)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("expected one api-call request, got %d", len(caller.requests))
	}
	request := caller.requests[0]
	if request.AuthIndex != "kimi-auth" || request.Method != "GET" || request.URL != "https://api.kimi.com/coding/v1/usages" {
		t.Fatalf("unexpected api-call request: %+v", request)
	}
	if request.Header["Authorization"] != "Bearer $TOKEN$" {
		t.Fatalf("unexpected api-call headers: %+v", request.Header)
	}
	if request.Data != nil {
		t.Fatalf("expected no data body, got %#v", request.Data)
	}
}

func TestKimiProviderNormalizesNestedFiveHourAndWeeklyUsage(t *testing.T) {
	// #354 的真实响应把短窗口用量放在 detail，数值使用字符串，窗口单位使用枚举值。
	body := json.RawMessage(`{"usage":{"limit":"100","used":"28","remaining":"72","resetTime":"2026-07-31T02:59:25.127311Z"},"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"100","used":"92","remaining":"8","resetTime":"2026-07-24T12:59:25.127311Z"}}]}`)
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   string(body),
		Body:       body,
	}}}
	provider := quota.NewKimiProvider(caller, quota.DefaultProviderConfigs().Kimi)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "kimi-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)
	if len(rows) != 2 {
		t.Fatalf("expected five-hour and weekly quota rows, got %#v", rows)
	}

	fiveHour := rows[0]
	if fiveHour.Key != "limits.0" || fiveHour.Label != "5h" {
		t.Fatalf("expected five-hour row first, got %#v", fiveHour)
	}
	assertFloatField(t, fiveHour.Used, 92, "five-hour used")
	assertFloatField(t, fiveHour.Limit, 100, "five-hour limit")
	assertFloatField(t, fiveHour.Remaining, 8, "five-hour remaining")
	assertApproxFloatField(t, fiveHour.UsedPercent, 92, "five-hour usedPercent")
	if fiveHour.Window == nil {
		t.Fatalf("expected five-hour window, got %#v", fiveHour)
	}
	assertIntField(t, fiveHour.Window.Seconds, 5*60*60, "five-hour window seconds")
	if fiveHour.ResetAt != "2026-07-24T12:59:25.127311Z" {
		t.Fatalf("unexpected five-hour resetAt: %#v", fiveHour)
	}

	weekly := rows[1]
	if weekly.Key != "usage" || weekly.Label != "Weekly" {
		t.Fatalf("expected weekly row second, got %#v", weekly)
	}
	assertFloatField(t, weekly.Used, 28, "weekly used")
	assertFloatField(t, weekly.Limit, 100, "weekly limit")
	assertFloatField(t, weekly.Remaining, 72, "weekly remaining")
	assertApproxFloatField(t, weekly.UsedPercent, 28, "weekly usedPercent")
	if weekly.ResetAt != "2026-07-31T02:59:25.127311Z" {
		t.Fatalf("unexpected weekly resetAt: %#v", weekly)
	}
}

func TestKimiProviderDerivesMissingUsedFromLimitAndRemaining(t *testing.T) {
	// Kimi 可能省略 used；字段仍在原始 JSON 时应由 limit 与 remaining 推导已用额度。
	body := json.RawMessage(`{"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"100","remaining":"85","resetTime":"2026-07-24T12:59:25.127311Z"}}]}`)
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   string(body),
		Body:       body,
	}}}
	provider := quota.NewKimiProvider(caller, quota.DefaultProviderConfigs().Kimi)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "kimi-auth"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)
	if len(rows) != 1 {
		t.Fatalf("expected one five-hour quota row, got %#v", rows)
	}

	fiveHour := rows[0]
	assertFloatField(t, fiveHour.Used, 15, "five-hour derived used")
	assertFloatField(t, fiveHour.Limit, 100, "five-hour limit")
	assertFloatField(t, fiveHour.Remaining, 85, "five-hour remaining")
	assertApproxFloatField(t, fiveHour.UsedPercent, 15, "five-hour usedPercent")
}

func assertApproxFloatField(t *testing.T, value *float64, expected float64, label string) {
	t.Helper()
	if value == nil || math.Abs(*value-expected) > 1e-9 {
		t.Fatalf("unexpected %s: got %#v want %v", label, value, expected)
	}
}
