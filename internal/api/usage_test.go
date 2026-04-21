package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/redact"
)

type usageStub struct {
	usage *cpa.StatisticsSnapshot
	err   error
}

func (s usageStub) GetUsage(context.Context) (*cpa.StatisticsSnapshot, error) {
	return s.usage, s.err
}

func TestUsageReturnsEmptyStructureWithoutProvider(t *testing.T) {
	router := NewRouter("", nil, nil, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"usage":`) && contains(body, `"total_requests":0`) && contains(body, `"apis":{}`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestUsageReturnsAggregatedSnapshot(t *testing.T) {
	router := NewRouter("", nil, usageStub{usage: &cpa.StatisticsSnapshot{
		TotalRequests: 2,
		SuccessCount:  1,
		FailureCount:  1,
		TotalTokens:   40,
		APIs: map[string]cpa.APISnapshot{
			"provider-a": {
				TotalRequests: 2,
				TotalTokens:   40,
				Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {TotalRequests: 2, TotalTokens: 40},
				},
			},
		},
	}}, nil, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !(contains(body, `"total_requests":2`) && contains(body, `"success_count":1`) && contains(body, `"claude-sonnet"`)) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if contains(body, `"provider-a"`) {
		t.Fatalf("expected API key to be redacted in response body: %s", body)
	}
	if !contains(body, `"`+redact.APIAlias("provider-a")+`"`) {
		t.Fatalf("expected stable API alias in response body: %s", body)
	}
	if !contains(body, `"display_name":"prov**er-a"`) {
		t.Fatalf("expected star-masked API display name in response body: %s", body)
	}
}

func TestUsageResponsePreservesFilteringFields(t *testing.T) {
	router := NewRouter("", nil, usageStub{usage: &cpa.StatisticsSnapshot{
		TotalRequests: 1,
		SuccessCount:  1,
		TotalTokens:   20,
		APIs: map[string]cpa.APISnapshot{
			"sk-live-secret-value": {
				TotalRequests: 1,
				SuccessCount:  1,
				TotalTokens:   20,
				Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {
						TotalRequests: 1,
						SuccessCount:  1,
						TotalTokens:   20,
						Details: []cpa.RequestDetail{{
							Source:    "source-a",
							AuthIndex: "2",
							Failed:    false,
							Tokens:    cpa.TokenStats{TotalTokens: 20},
						}},
					},
				},
			},
		},
	}}, authFileStub{files: []models.AuthFile{{
		AuthIndex: "2",
		Email:     "user@example.com",
		Label:     "Work Account",
		Type:      "auth-file",
	}}}, nil, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"source":"user@example.com"`) {
		t.Fatalf("expected source to expose resolved display value: %s", body)
	}
	if !contains(body, `"source_type":"auth-file"`) {
		t.Fatalf("expected source_type to preserve auth-file type for frontend tags: %s", body)
	}
	if !contains(body, `"source_raw":"source-a"`) {
		t.Fatalf("expected source_raw to preserve raw value for filtering: %s", body)
	}
	if !contains(body, `"auth_index":"2"`) {
		t.Fatalf("expected auth_index to remain available for frontend filtering: %s", body)
	}
}

func TestUsagePrefersProviderMetadataOverAuthFileForTagResolution(t *testing.T) {
	router := NewRouter("", nil, usageStub{usage: &cpa.StatisticsSnapshot{
		TotalRequests: 1,
		SuccessCount:  1,
		TotalTokens:   20,
		APIs: map[string]cpa.APISnapshot{
			"sk-live-secret-value": {
				TotalRequests: 1,
				SuccessCount:  1,
				TotalTokens:   20,
				Models: map[string]cpa.ModelSnapshot{
					"gpt-5": {
						TotalRequests: 1,
						SuccessCount:  1,
						TotalTokens:   20,
						Details: []cpa.RequestDetail{{
							Source:    "sk-provider-key",
							AuthIndex: "2",
							Failed:    false,
							Tokens:    cpa.TokenStats{TotalTokens: 20},
						}},
					},
				},
			},
		},
	}}, authFileStub{files: []models.AuthFile{{
		AuthIndex: "2",
		Email:     "user@example.com",
		Type:      "codex",
	}}}, providerMetadataStub{items: []models.ProviderMetadata{{
		LookupKey:    "sk-provider-key",
		ProviderType: "openai",
		DisplayName:  "OpenAI Mirror",
		ProviderKey:  "openai:OpenAI Mirror",
	}}}, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"source":"OpenAI Mirror"`) {
		t.Fatalf("expected provider metadata display to win over auth-file display: %s", body)
	}
	if !contains(body, `"source_type":"openai"`) {
		t.Fatalf("expected provider metadata type to win over auth-file type: %s", body)
	}
	if !contains(body, `"source_key":"openai:OpenAI Mirror"`) {
		t.Fatalf("expected provider key to be used for stable grouping: %s", body)
	}
}
