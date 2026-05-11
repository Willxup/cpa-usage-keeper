package test

import (
	"context"
	"encoding/json"
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
