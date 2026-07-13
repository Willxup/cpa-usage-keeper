package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestAntigravityProviderUsesProjectIDForQuotaRequest(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 200,
		BodyText:   `{"body":{"groups":[{"displayName":"Gemini Models","description":"Models within this group: Gemini Flash, Gemini Pro","buckets":[{"bucketId":"gemini-5h","displayName":"Five Hour Limit","window":"5h","remainingFraction":0.4,"resetTime":"2026-05-09T12:00:00Z"},{"bucketId":"gemini-weekly","displayName":"Weekly Limit","window":"weekly","remainingFraction":0.9,"resetTime":"2026-05-10T12:00:00Z"}]}]}}`,
		Body:       json.RawMessage(`{"body":{"groups":[{"displayName":"Gemini Models","description":"Models within this group: Gemini Flash, Gemini Pro","buckets":[{"bucketId":"gemini-5h","displayName":"Five Hour Limit","window":"5h","remainingFraction":0.4,"resetTime":"2026-05-09T12:00:00Z"},{"bucketId":"gemini-weekly","displayName":"Weekly Limit","window":"weekly","remainingFraction":0.9,"resetTime":"2026-05-10T12:00:00Z"}]}]}}`),
	}}}
	provider := quota.NewAntigravityProvider(caller, quota.DefaultProviderConfigs().Antigravity[0])

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
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal antigravity result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"groups":[`) || !contains(body, `"displayName":"Gemini Models"`) || !contains(body, `"description":"Models within this group: Gemini Flash, Gemini Pro"`) || !contains(body, `"bucketId":"gemini-5h"`) || contains(body, `"models"`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected antigravity result JSON: %s", body)
	}
	if len(caller.requests) != 1 {
		t.Fatalf("expected one api-call request, got %d", len(caller.requests))
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
}

func TestAntigravityProviderRejectsMissingProjectID(t *testing.T) {
	caller := &recordingManagementCaller{}
	provider := quota.NewAntigravityProvider(caller, quota.DefaultProviderConfigs().Antigravity[0])

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "ag-auth"}})
	if !errors.Is(err, quota.ErrProviderInput) || !strings.Contains(err.Error(), "missing project_id parameter") {
		t.Fatalf("expected missing project_id provider input error, got %v", err)
	}
	if len(caller.requests) != 0 {
		t.Fatalf("provider should not call api-call without project_id, got %d requests", len(caller.requests))
	}
}

func TestAntigravityProviderReturnsTargetErrorMessage(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 500,
		BodyText:   `{"error":"backend unavailable"}`,
		Body:       json.RawMessage(`{"error":"backend unavailable"}`),
	}}}
	provider := quota.NewAntigravityProvider(caller, quota.DefaultProviderConfigs().Antigravity[0])

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "ag-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err == nil || err.Error() != "HTTP 500: backend unavailable" {
		t.Fatalf("expected target HTTP message, got %v", err)
	}
}
