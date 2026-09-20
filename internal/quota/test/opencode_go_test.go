package test

import (
	"context"
	"encoding/json"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

const openCodeGoUsageBody = `{"usage":{"rolling":{"status":"ok","percent":12,"resetsAt":"2026-09-17T14:00:00Z"},"weekly":{"status":"ok","percent":45.5,"resetsAt":"2026-09-22T00:00:00Z"},"monthly":{"status":"exceeded","percent":100,"resetsAt":"2026-10-01T00:00:00Z"}}}`

func TestOpenCodeGoProviderCallsUsageRequest(t *testing.T) {
	body := json.RawMessage(openCodeGoUsageBody)
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   openCodeGoUsageBody,
		Body:       body,
	}}}
	provider := quota.NewOpenCodeGoProvider(caller, quota.DefaultProviderConfigs().OpenCodeGo)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "opencode-go-key-abc"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "opencode-go" {
		t.Fatalf("expected opencode-go output provider, got %q", output.Provider)
	}
	result, ok := output.Result.(quota.OpenCodeGoResult)
	if !ok {
		t.Fatalf("expected opencode-go result type, got %T", output.Result)
	}
	if result.Usage == nil || result.Usage.Rolling == nil || result.Usage.Weekly == nil || result.Usage.Monthly == nil {
		t.Fatalf("expected three parsed quota windows, got %#v", result.Usage)
	}
	assertApproxFloatField(t, result.Usage.Rolling.Percent, 12, "rolling percent")
	assertApproxFloatField(t, result.Usage.Weekly.Percent, 45.5, "weekly percent")

	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal opencode-go result: %v", err)
	}
	serialized := string(encoded)
	if !contains(serialized, `"usage":{"rolling"`) || contains(serialized, "bodyText") || contains(serialized, "statusCode") {
		t.Fatalf("unexpected opencode-go result JSON: %s", serialized)
	}

	if len(caller.requests) != 1 {
		t.Fatalf("expected one api-call request, got %d", len(caller.requests))
	}
	request := caller.requests[0]
	if request.AuthIndex != "opencode-go-key-abc" || request.Method != "GET" || request.URL != "https://opencode.ai/zen/go/v1/usage" {
		t.Fatalf("unexpected api-call request: %+v", request)
	}
	// 凭证是 api_key auth record，必须由 api-call 通过 $TOKEN$ 注入。
	if request.Header["Authorization"] != "Bearer $TOKEN$" || request.Header["Accept"] != "application/json" {
		t.Fatalf("unexpected api-call headers: %+v", request.Header)
	}
	if request.Data != nil {
		t.Fatalf("expected no data body, got %#v", request.Data)
	}
}

func TestOpenCodeGoProviderNormalizesWindows(t *testing.T) {
	body := json.RawMessage(openCodeGoUsageBody)
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   openCodeGoUsageBody,
		Body:       body,
	}}}
	provider := quota.NewOpenCodeGoProvider(caller, quota.DefaultProviderConfigs().OpenCodeGo)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "opencode-go-key-abc"}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	rows := quota.NormalizeQuotaRows(output)
	if len(rows) != 3 {
		t.Fatalf("expected three quota rows, got %#v", rows)
	}

	rolling := rows[0]
	if rolling.Key != "opencode_go.rolling" || rolling.Label != "Rolling" || rolling.Scope != "window" {
		t.Fatalf("unexpected rolling row: %#v", rolling)
	}
	assertApproxFloatField(t, rolling.UsedPercent, 12, "rolling usedPercent")
	if rolling.ResetAt != "2026-09-17T14:00:00Z" {
		t.Fatalf("unexpected rolling resetAt: %#v", rolling)
	}
	if rolling.Window != nil {
		t.Fatalf("rolling duration is undocumented upstream, expected no window: %#v", rolling.Window)
	}
	if rolling.LimitReached == nil || *rolling.LimitReached {
		t.Fatalf("expected rolling not marked as limit reached: %#v", rolling.LimitReached)
	}

	weekly := rows[1]
	if weekly.Key != "opencode_go.weekly" || weekly.Label != "Weekly" {
		t.Fatalf("unexpected weekly row: %#v", weekly)
	}
	assertApproxFloatField(t, weekly.UsedPercent, 45.5, "weekly usedPercent")
	if weekly.Window == nil {
		t.Fatalf("expected weekly window seconds, got %#v", weekly)
	}
	assertIntField(t, weekly.Window.Seconds, 7*24*60*60, "weekly window seconds")

	monthly := rows[2]
	if monthly.Key != "opencode_go.monthly" || monthly.Label != "Monthly" {
		t.Fatalf("unexpected monthly row: %#v", monthly)
	}
	assertApproxFloatField(t, monthly.UsedPercent, 100, "monthly usedPercent")
	if monthly.Window == nil {
		t.Fatalf("expected monthly window seconds, got %#v", monthly)
	}
	assertIntField(t, monthly.Window.Seconds, 30*24*60*60, "monthly window seconds")
	if monthly.LimitReached == nil || !*monthly.LimitReached {
		t.Fatalf("expected monthly marked as limit reached: %#v", monthly.LimitReached)
	}
}

func TestOpenCodeGoProviderRejectsPayloadWithoutWindow(t *testing.T) {
	// 非预期响应必须报错，避免缓存一次空成功。
	body := json.RawMessage(`{"usage":{}}`)
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   string(body),
		Body:       body,
	}}}
	provider := quota.NewOpenCodeGoProvider(caller, quota.DefaultProviderConfigs().OpenCodeGo)

	if _, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "opencode-go-key-abc"}}); err == nil {
		t.Fatal("expected an error for a payload without any quota window")
	}
}
