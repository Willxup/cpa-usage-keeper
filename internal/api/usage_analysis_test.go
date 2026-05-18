package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/redact"
	"cpa-usage-keeper/internal/repository/dto"
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

func (s usageAnalysisAPIKeyStub) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return entities.CPAAPIKey{}, service.ErrInvalidID
}

func (s *usageAnalysisStub) GetUsageWithFilter(context.Context, servicedto.UsageFilter) (*dto.StatisticsSnapshot, error) {
	return nil, nil
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
		}},
		APIKeyComposition: []servicedto.AnalysisCompositionItem{{
			Key:         "provider-a",
			TotalTokens: 42,
			Requests:    2,
		}},
		ModelComposition: []servicedto.AnalysisCompositionItem{{
			Key:         "claude-sonnet",
			TotalTokens: 42,
			Requests:    2,
		}},
		AuthFilesComposition: []servicedto.AnalysisCompositionItem{{
			Key:         "auth-file-1",
			Label:       "Auth File One",
			TotalTokens: 30,
			Requests:    1,
		}},
		AIProviderComposition: []servicedto.AnalysisCompositionItem{{
			Key:         "provider-1",
			Label:       "Provider One",
			TotalTokens: 12,
			Requests:    1,
		}},
		Heatmap: []servicedto.AnalysisHeatmapCell{{
			APIKey:      "provider-a",
			Model:       "claude-sonnet",
			TotalTokens: 42,
			Requests:    2,
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
	if !contains(body, `"api_key_composition":[`) || !contains(body, `"model_composition":[`) || !contains(body, `"auth_files_composition":[`) || !contains(body, `"ai_provider_composition":[`) {
		t.Fatalf("expected composition payloads in response body: %s", body)
	}
	if !contains(body, `"key":"prov**er-a"`) || !contains(body, `"label":"prov**er-a"`) {
		t.Fatalf("expected redacted api key composition in response body: %s", body)
	}
	if !contains(body, `"key":"auth***le-1"`) || !contains(body, `"label":"Auth File One"`) || !contains(body, `"percent":100`) {
		t.Fatalf("expected auth file composition in response body: %s", body)
	}
	if !contains(body, `"key":"prov**er-1"`) || !contains(body, `"label":"Provider One"`) {
		t.Fatalf("expected ai provider composition in response body: %s", body)
	}
	if !contains(body, `"model":"claude-sonnet"`) || !contains(body, `"intensity":1`) {
		t.Fatalf("expected heatmap cell in response body: %s", body)
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
	if !contains(body, `"key":"1"`) || !contains(body, `"label":"Primary Key"`) || !contains(body, `"api_key":"Primary Key"`) {
		t.Fatalf("expected analysis payload to use CPA API key id and display label, got %s", body)
	}
	if contains(body, "sk-alpha123456") || contains(body, redact.APIAlias("sk-alpha123456")) {
		t.Fatalf("expected raw and alias key values to stay hidden when a CPA key label exists, got %s", body)
	}
	if provider.lastFilter.APIKeyID != "1" {
		t.Fatalf("expected API key id to pass into usage filter, got %+v", provider.lastFilter)
	}
}

func TestBuildAnalysisHeatmapPayloadSortsKeysByRequests(t *testing.T) {
	payload := buildAnalysisHeatmapPayload([]servicedto.AnalysisHeatmapCell{
		{APIKey: "sk-low", Model: "model-low", Requests: 1, TotalTokens: 100},
		{APIKey: "sk-high", Model: "model-high", Requests: 5, TotalTokens: 50},
		{APIKey: "sk-high", Model: "model-low", Requests: 2, TotalTokens: 20},
	}, nil)

	if got := payload.APIKeys; len(got) != 2 || got[0] != redact.APIKeyDisplayName("sk-high") || got[1] != redact.APIKeyDisplayName("sk-low") {
		t.Fatalf("expected api keys sorted by total requests desc, got %+v", got)
	}
	if got := payload.Models; len(got) != 2 || got[0] != "model-high" || got[1] != "model-low" {
		t.Fatalf("expected models sorted by total requests desc, got %+v", got)
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
