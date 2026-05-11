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

func TestGeminiCLIProviderUsesProjectIDForQuotaAndCodeAssistRequests(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{StatusCode: 200, BodyText: `{"buckets":[{"model_id":"gemini-2.5-pro_vertex","token_type":"PROMPT","remaining_fraction":0.7,"remaining_amount":42,"reset_time":"2026-05-09T12:00:00Z"}]}`, Body: json.RawMessage(`{"buckets":[{"model_id":"gemini-2.5-pro_vertex","token_type":"PROMPT","remaining_fraction":0.7,"remaining_amount":42,"reset_time":"2026-05-09T12:00:00Z"}]}`)},
		{StatusCode: 200, BodyText: `{"current_tier":{"id":"free-tier","available_credits":[{"credit_type":"GOOGLE_ONE_AI","credit_amount":10}]}}`, Body: json.RawMessage(`{"current_tier":{"id":"free-tier","available_credits":[{"credit_type":"GOOGLE_ONE_AI","credit_amount":10}]}}`)},
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewGeminiCLIProvider(caller, configs.GeminiCLI, configs.GeminiCLICodeAssist)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "gemini-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if output.Provider != "gemini-cli" {
		t.Fatalf("expected gemini-cli output provider, got %q", output.Provider)
	}
	if len(caller.requests) != 2 {
		t.Fatalf("expected two api-call requests, got %d", len(caller.requests))
	}

	quotaRequest := caller.requests[0]
	if quotaRequest.AuthIndex != "gemini-auth" || quotaRequest.Method != "POST" || quotaRequest.URL != "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota" {
		t.Fatalf("unexpected quota request: %+v", quotaRequest)
	}
	quotaData, ok := quotaRequest.Data.(map[string]string)
	if !ok || quotaData["project"] != "project-123" {
		t.Fatalf("unexpected quota request data: %#v", quotaRequest.Data)
	}
	if quotaRequest.Header["Authorization"] != "Bearer $TOKEN$" || quotaRequest.Header["Content-Type"] != "application/json" {
		t.Fatalf("unexpected quota request headers: %+v", quotaRequest.Header)
	}

	codeAssistRequest := caller.requests[1]
	if codeAssistRequest.AuthIndex != "gemini-auth" || codeAssistRequest.Method != "POST" || codeAssistRequest.URL != "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" {
		t.Fatalf("unexpected code assist request: %+v", codeAssistRequest)
	}
	codeAssistData, ok := codeAssistRequest.Data.(map[string]any)
	if !ok || codeAssistData["cloudaicompanionProject"] != "project-123" {
		t.Fatalf("unexpected code assist data: %#v", codeAssistRequest.Data)
	}
	if codeAssistRequest.Header["Authorization"] != "Bearer $TOKEN$" || codeAssistRequest.Header["Content-Type"] != "application/json" {
		t.Fatalf("unexpected code assist request headers: %+v", codeAssistRequest.Header)
	}
	metadata, ok := codeAssistData["metadata"].(map[string]string)
	if !ok || metadata["duetProject"] != "project-123" || metadata["pluginType"] != "GEMINI" {
		t.Fatalf("unexpected code assist metadata: %#v", codeAssistData["metadata"])
	}

	result, ok := output.Result.(quota.GeminiCLIResult)
	if !ok {
		t.Fatalf("expected gemini cli result type, got %T", output.Result)
	}
	if result.Quota == nil || len(result.Quota.Buckets) != 1 || result.Quota.Buckets[0].ModelID != "gemini-2.5-pro_vertex" || result.Quota.Buckets[0].RemainingFraction != 0.7 {
		t.Fatalf("expected parsed gemini quota payload, got %#v", result.Quota)
	}
	if result.CodeAssist == nil || result.CodeAssist.CurrentTier == nil || result.CodeAssist.CurrentTier.ID != "free-tier" || result.CodeAssist.CurrentTier.AvailableCredits[0].CreditAmount != 10 {
		t.Fatalf("expected parsed gemini code assist payload, got %#v", result.CodeAssist)
	}
	encoded, err := json.Marshal(output.Result)
	if err != nil {
		t.Fatalf("marshal gemini cli result: %v", err)
	}
	body := string(encoded)
	if !contains(body, `"quota":{"buckets"`) || !contains(body, `"codeAssist":{"currentTier"`) || contains(body, "bodyText") || contains(body, "statusCode") {
		t.Fatalf("unexpected gemini cli result JSON: %s", body)
	}
}

func TestGeminiCLIProviderRejectsMissingProjectID(t *testing.T) {
	caller := &recordingManagementCaller{}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewGeminiCLIProvider(caller, configs.GeminiCLI, configs.GeminiCLICodeAssist)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{Identity: "gemini-auth"}})
	if !errors.Is(err, quota.ErrProviderInput) || !strings.Contains(err.Error(), "missing project_id parameter") {
		t.Fatalf("expected missing project_id provider input error, got %v", err)
	}
	if len(caller.requests) != 0 {
		t.Fatalf("provider should not call api-call without project_id, got %d requests", len(caller.requests))
	}
}

func TestGeminiCLIProviderReturnsTargetErrorMessage(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{{
		StatusCode: 403,
		BodyText:   `{"message":"permission denied"}`,
		Body:       json.RawMessage(`{"message":"permission denied"}`),
	}}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewGeminiCLIProvider(caller, configs.GeminiCLI, configs.GeminiCLICodeAssist)

	_, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "gemini-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err == nil || err.Error() != "HTTP 403: permission denied" {
		t.Fatalf("expected target HTTP message, got %v", err)
	}
}

func TestGeminiCLIProviderKeepsQuotaWhenSupplementaryCallFails(t *testing.T) {
	caller := &recordingManagementCaller{responses: []*apicall.Response{
		{StatusCode: 200, BodyText: `{"buckets":[{"model_id":"gemini-2.5-pro_vertex","token_type":"PROMPT","remaining_fraction":0.7,"remaining_amount":42,"reset_time":"2026-05-09T12:00:00Z"}]}`, Body: json.RawMessage(`{"buckets":[{"model_id":"gemini-2.5-pro_vertex","token_type":"PROMPT","remaining_fraction":0.7,"remaining_amount":42,"reset_time":"2026-05-09T12:00:00Z"}]}`)},
		{StatusCode: 500, BodyText: `upstream error`, Body: json.RawMessage(`null`)},
	}}
	configs := quota.DefaultProviderConfigs()
	provider := quota.NewGeminiCLIProvider(caller, configs.GeminiCLI, configs.GeminiCLICodeAssist)

	output, err := provider.Check(context.Background(), quota.ProviderInput{Identity: entities.UsageIdentity{
		Identity:  "gemini-auth",
		ProjectID: stringPtr("project-123"),
	}})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	result, ok := output.Result.(quota.GeminiCLIResult)
	if !ok {
		t.Fatalf("expected gemini cli result type, got %T", output.Result)
	}
	if result.Quota == nil || len(result.Quota.Buckets) != 1 {
		t.Fatalf("expected quota to be preserved, got %#v", result.Quota)
	}
	if result.CodeAssist != nil {
		t.Fatalf("expected supplementary code assist to be omitted on failure, got %#v", result.CodeAssist)
	}
	if len(caller.requests) != 2 {
		t.Fatalf("expected two api-call requests, got %d", len(caller.requests))
	}
}
