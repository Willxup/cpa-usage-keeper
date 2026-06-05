package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type usageAnalysisStub struct {
	analysis      *servicedto.AnalysisSnapshot
	err           error
	lastFilter    servicedto.UsageFilter
	analysisCalls int
}

type usageAnalysisAPIKeyStub struct {
	rows []entities.CPAAPIKey
	err  error
}

func (s usageAnalysisAPIKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return s.rows, s.err
}

func (s usageAnalysisAPIKeyStub) FindActiveCPAAPIKeyByValue(context.Context, string) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, service.ErrInvalidID
}

func (s usageAnalysisAPIKeyStub) FindActiveCPAAPIKeyByID(context.Context, int64) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, service.ErrInvalidID
}

func (s usageAnalysisAPIKeyStub) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, service.ErrInvalidID
}

func (s *usageAnalysisStub) GetUsageOverview(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	return nil, nil
}

func (s *usageAnalysisStub) ListUsageEvents(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	return nil, nil
}

func (s *usageAnalysisStub) ListUsageEventFilterOptions(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	return nil, nil
}

func (s *usageAnalysisStub) GetAnalysis(_ context.Context, filter servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	s.lastFilter = filter
	s.analysisCalls++
	return s.analysis, s.err
}

func TestUsageAnalysisReturnsAggregatedRows(t *testing.T) {
	bucket := time.Date(2026, 4, 22, 10, 0, 0, 0, time.Local)
	provider := &usageAnalysisStub{analysis: &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		TokenUsage: []servicedto.AnalysisTokenUsageBucket{{
			Bucket:          bucket,
			InputTokens:     30,
			OutputTokens:    9,
			CachedTokens:    1,
			ReasoningTokens: 2,
			TotalTokens:     42,
			Requests:        2,
			CostUSD:         1.23,
			CostAvailable:   true,
		}},
		APIKeyComposition: []servicedto.AnalysisCompositionItem{{
			Key:           "sk-provider123456",
			TotalTokens:   42,
			Requests:      2,
			CostUSD:       1.23,
			CostAvailable: true,
		}},
		ModelComposition: []servicedto.AnalysisCompositionItem{{
			Key:           "claude-sonnet",
			TotalTokens:   42,
			Requests:      2,
			CostUSD:       1.23,
			CostAvailable: true,
		}},
		AuthFilesComposition: []servicedto.AnalysisCompositionItem{{
			Key:           "auth-file-1",
			Label:         "Auth File One",
			TotalTokens:   30,
			Requests:      1,
			CostUSD:       0.8,
			CostAvailable: true,
		}},
		AIProviderComposition: []servicedto.AnalysisCompositionItem{{
			Key:           "provider-1",
			Label:         "Provider One",
			TotalTokens:   12,
			Requests:      1,
			CostUSD:       0.43,
			CostAvailable: true,
		}},
		Heatmap: []servicedto.AnalysisHeatmapCell{{
			APIKey:          "sk-provider123456",
			Model:           "claude-sonnet",
			InputTokens:     30,
			OutputTokens:    9,
			CachedTokens:    1,
			ReasoningTokens: 2,
			TotalTokens:     42,
			Requests:        2,
			CostUSD:         1.23,
			CostAvailable:   true,
		}},
		CostBreakdown: servicedto.AnalysisCostBreakdown{
			InputCostUSD:  0.3,
			OutputCostUSD: 0.8,
			CachedCostUSD: 0.13,
			TotalCostUSD:  1.23,
			CostAvailable: true,
		},
		ModelEfficiency: []servicedto.AnalysisModelEfficiencyItem{{
			Model:                  "claude-sonnet",
			Requests:               2,
			InputTokens:            30,
			OutputTokens:           9,
			CachedTokens:           1,
			ReasoningTokens:        2,
			TotalTokens:            42,
			CostUSD:                1.23,
			CostAvailable:          true,
			CostPerRequestUSD:      0.615,
			OutputTokensPerRequest: 5.5,
			CacheRate:              1.0 / 30.0,
		}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"granularity":"hourly"`) || !contains(body, `"token_usage":[`) || !contains(body, `"heatmap":`) {
		t.Fatalf("unexpected response body: %s", body)
	}
	if !contains(body, `"cost_usd":1.23`) || !contains(body, `"cost_available":true`) {
		t.Fatalf("expected token/composition cost fields in response body: %s", body)
	}
	if !contains(body, `"api_key_composition":[`) || !contains(body, `"model_composition":[`) || !contains(body, `"auth_files_composition":[`) || !contains(body, `"ai_provider_composition":[`) {
		t.Fatalf("expected composition payloads in response body: %s", body)
	}
	if !contains(body, `"key":"sk-*********123456"`) || !contains(body, `"label":"sk-*********123456"`) {
		t.Fatalf("expected redacted api key composition in response body: %s", body)
	}
	if !contains(body, `"key":"aut*********file-1"`) || !contains(body, `"label":"Auth File One"`) || !contains(body, `"percent":100`) {
		t.Fatalf("expected auth file composition in response body: %s", body)
	}
	if !contains(body, `"key":"pro*********ider-1"`) || !contains(body, `"label":"Provider One"`) {
		t.Fatalf("expected ai provider composition in response body: %s", body)
	}
	if !contains(body, `"model":"claude-sonnet"`) || !contains(body, `"intensity":1`) || !contains(body, `"input_tokens":30`) || !contains(body, `"reasoning_tokens":2`) {
		t.Fatalf("expected heatmap cell in response body: %s", body)
	}
	if !contains(body, `"cost_breakdown":`) || !contains(body, `"input_cost_usd":0.3`) || !contains(body, `"total_cost_usd":1.23`) {
		t.Fatalf("expected cost breakdown in response body: %s", body)
	}
	if !contains(body, `"model_efficiency":`) || !contains(body, `"cost_per_request_usd":0.615`) || !contains(body, `"output_tokens_per_request":5.5`) || !contains(body, `"cache_rate":0.03333333333333333`) {
		t.Fatalf("expected model efficiency in response body: %s", body)
	}
	if provider.analysisCalls != 1 {
		t.Fatalf("expected GetAnalysis to be called once, got %d", provider.analysisCalls)
	}
	if provider.lastFilter.Range != "24h" {
		t.Fatalf("expected range to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
		t.Fatalf("expected resolved time bounds in filter, got %+v", provider.lastFilter)
	}
}

func TestUsageAnalysisUsesCPAAPIKeyOptionLabels(t *testing.T) {
	bucket := time.Date(2026, 4, 22, 10, 0, 0, 0, time.Local)
	lastSyncedAt := time.Date(2026, 5, 13, 10, 0, 0, 0, time.Local)
	provider := &usageAnalysisStub{analysis: &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		TokenUsage:  []servicedto.AnalysisTokenUsageBucket{{Bucket: bucket, TotalTokens: 42, Requests: 2}},
		APIKeyComposition: []servicedto.AnalysisCompositionItem{{
			Key:         "sk-alpha123456",
			TotalTokens: 42,
			Requests:    2,
		}},
		ModelComposition: []servicedto.AnalysisCompositionItem{{Key: "claude-sonnet", TotalTokens: 42, Requests: 2}},
		Heatmap: []servicedto.AnalysisHeatmapCell{{
			APIKey:      "sk-alpha123456",
			Model:       "claude-sonnet",
			TotalTokens: 42,
			Requests:    2,
		}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: usageAnalysisAPIKeyStub{rows: []entities.CPAAPIKey{{
		ID:           1,
		APIKey:       "sk-alpha123456",
		DisplayKey:   "sk-*********123456",
		KeyAlias:     "Primary Key",
		LastSyncedAt: &lastSyncedAt,
	}}}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis?range=24h&api_key_id=1", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	maskedKey := helper.RedactSensitiveValue("sk-alpha123456")
	if !contains(body, `"key":"`+maskedKey+`"`) || !contains(body, `"label":"Primary Key"`) || !contains(body, `"api_key":"`+maskedKey+`"`) || !contains(body, `"api_key_labels":{"`+maskedKey+`":"Primary Key"}`) {
		t.Fatalf("expected analysis payload to use redacted CPA API key and display label, got %s", body)
	}
	if contains(body, "sk-alpha123456") || contains(body, `"key":"1"`) || contains(body, `"api_key":"1"`) {
		t.Fatalf("expected raw key and database id to stay hidden when a CPA key label exists, got %s", body)
	}
	if provider.lastFilter.APIKeyID != "1" {
		t.Fatalf("expected API key id to pass into usage filter, got %+v", provider.lastFilter)
	}
}

func TestBuildAnalysisHeatmapPayloadSortsKeysByRequests(t *testing.T) {
	payload := buildAnalysisHeatmapPayload([]servicedto.AnalysisHeatmapCell{
		{APIKey: "sk-low123456", Model: "model-low", Requests: 1, TotalTokens: 100},
		{APIKey: "sk-high654321", Model: "model-high", Requests: 5, TotalTokens: 50},
		{APIKey: "sk-high654321", Model: "model-low", Requests: 2, TotalTokens: 20},
	}, nil)

	if got := payload.APIKeys; len(got) != 2 || got[0] != helper.RedactSensitiveValue("sk-high654321") || got[1] != helper.RedactSensitiveValue("sk-low123456") {
		t.Fatalf("expected api keys sorted by total requests desc, got %+v", got)
	}
	if got := payload.Models; len(got) != 2 || got[0] != "model-high" || got[1] != "model-low" {
		t.Fatalf("expected models sorted by total requests desc, got %+v", got)
	}
}

func TestBuildAnalysisHeatmapPayloadKeepsDuplicateAPIKeyLabelsSeparate(t *testing.T) {
	payload := buildAnalysisHeatmapPayload([]servicedto.AnalysisHeatmapCell{
		{APIKey: "sk-alpha123456", Model: "model", Requests: 1, TotalTokens: 100},
		{APIKey: "sk-beta654321", Model: "model", Requests: 2, TotalTokens: 200},
	}, map[string]analysisAPIKeyInfo{
		"sk-alpha123456": {Label: "Shared"},
		"sk-beta654321":  {Label: "Shared"},
	})

	alphaKey := helper.RedactSensitiveValue("sk-alpha123456")
	betaKey := helper.RedactSensitiveValue("sk-beta654321")
	if got := payload.APIKeys; len(got) != 2 || got[0] != betaKey || got[1] != alphaKey {
		t.Fatalf("expected heatmap API keys to use redacted response keys sorted by requests, got %+v", got)
	}
	if payload.APIKeyLabels[alphaKey] != "Shared" || payload.APIKeyLabels[betaKey] != "Shared" {
		t.Fatalf("expected duplicate labels to be stored separately by response key, got %+v", payload.APIKeyLabels)
	}
	if len(payload.Cells) != 2 || payload.Cells[0].APIKey != alphaKey || payload.Cells[1].APIKey != betaKey {
		t.Fatalf("expected heatmap cells to use redacted response keys, got %+v", payload.Cells)
	}
}

func TestUsageAnalysisUsesCachedTokensWithoutCacheReadCreationDetails(t *testing.T) {
	bucket := time.Date(2026, 4, 22, 10, 0, 0, 0, time.Local)
	provider := &usageAnalysisStub{analysis: &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		TokenUsage: []servicedto.AnalysisTokenUsageBucket{{
			Bucket:       bucket,
			InputTokens:  130,
			OutputTokens: 30,
			CachedTokens: 20,
			TotalTokens:  160,
			Requests:     1,
		}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis?range=24h", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"cached_tokens":20`) {
		t.Fatalf("expected cached_tokens in analysis payload, got %s", body)
	}
	if contains(body, "cache_read_tokens") || contains(body, "cache_creation_tokens") {
		t.Fatalf("expected analysis payload to omit cache read/creation fields, got %s", body)
	}
}

func TestUsageAnalysisRequiresAuthWhenEnabled(t *testing.T) {
	router := NewRouter(nil, nil, &usageAnalysisStub{}, nil, AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}
