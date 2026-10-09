package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type keyViewerAnalysisUsageStub struct {
	service.UsageProvider
	analysisFilters []servicedto.UsageFilter
	latencyFilters  []servicedto.UsageFilter
}

func (s *keyViewerAnalysisUsageStub) GetAnalysis(_ context.Context, filter servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	s.analysisFilters = append(s.analysisFilters, filter)
	key := "sk-other654321"
	tokens := int64(99)
	if filter.APIKeyID == "42" {
		key = "sk-viewer123456"
		tokens = 42
	}
	return &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		APIKeyComposition: []servicedto.AnalysisCompositionItem{{
			Key: key, TotalTokens: tokens, Requests: 1,
		}},
		ModelComposition: []servicedto.AnalysisCompositionItem{{
			Key: "claude-sonnet", TotalTokens: tokens, Requests: 1,
		}},
		AuthFilesComposition: []servicedto.AnalysisCompositionItem{{
			Key: "auth-file-private", Label: "Private Auth File", TotalTokens: tokens, Requests: 1,
		}},
		AIProviderComposition: []servicedto.AnalysisCompositionItem{{
			Key: "provider-private", Label: "Private Provider", TotalTokens: tokens, Requests: 1,
		}},
		Heatmap: []servicedto.AnalysisHeatmapCell{{
			APIKey: key, Model: "claude-sonnet", TotalTokens: tokens, Requests: 1,
		}},
	}, nil
}

func (s *keyViewerAnalysisUsageStub) GetAnalysisLatency(_ context.Context, filter servicedto.UsageFilter) (*servicedto.AnalysisLatencyDiagnostics, error) {
	s.latencyFilters = append(s.latencyFilters, filter)
	return &servicedto.AnalysisLatencyDiagnostics{
		Points:      []servicedto.AnalysisLatencyPoint{{TTFTMS: 120, LatencyMS: 800}},
		TotalPoints: 1,
	}, nil
}

type keyViewerAnalysisKeyStub struct {
	service.CPAAPIKeyProvider
	row       entities.CPAAPIKey
	listCalls int
}

func (s *keyViewerAnalysisKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	s.listCalls++
	return []entities.CPAAPIKey{s.row, {ID: 99, APIKey: "sk-other654321", KeyAlias: "Other Key"}}, nil
}

func (s *keyViewerAnalysisKeyStub) FindActiveCPAAPIKeyByID(_ context.Context, id int64) (entities.CPAAPIKey, error) {
	if id != s.row.ID {
		return entities.CPAAPIKey{}, service.ErrInvalidID
	}
	return s.row, nil
}

func TestAPIKeyViewerAnalysisForcesSessionKeyAndOmitsSourceComposition(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	usageProvider := &keyViewerAnalysisUsageStub{}
	keyProvider := &keyViewerAnalysisKeyStub{row: entities.CPAAPIKey{
		ID: 42, APIKey: "sk-viewer123456", DisplayKey: "sk-*********123456", KeyAlias: "Viewer Key",
	}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, usageProvider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	analysisResponse := serveAPIGet(router, "/api/v1/key-analysis?range=24h&api_key_id=99", &http.Cookie{Name: standardSessionCookieName, Value: token})

	if analysisResponse.Code != http.StatusOK {
		t.Fatalf("analysis status=%d, want 200: %s", analysisResponse.Code, analysisResponse.Body.String())
	}
	if len(usageProvider.analysisFilters) != 1 || usageProvider.analysisFilters[0].APIKeyID != "42" {
		t.Fatalf("expected analysis filter to force session key 42, got %+v", usageProvider.analysisFilters)
	}
	var analysisPayload struct {
		APIKeyComposition     []struct{ Key, Label string } `json:"api_key_composition"`
		AuthFilesComposition  []json.RawMessage             `json:"auth_files_composition"`
		AIProviderComposition []json.RawMessage             `json:"ai_provider_composition"`
		Heatmap               struct {
			APIKeys      []string          `json:"api_keys"`
			APIKeyLabels map[string]string `json:"api_key_labels"`
		} `json:"heatmap"`
	}
	if err := json.Unmarshal(analysisResponse.Body.Bytes(), &analysisPayload); err != nil {
		t.Fatalf("decode analysis response: %v", err)
	}
	if len(analysisPayload.APIKeyComposition) != 1 || analysisPayload.APIKeyComposition[0].Key != "42" || analysisPayload.APIKeyComposition[0].Label != "Viewer Key" {
		t.Fatalf("expected only the session key identity, got %+v", analysisPayload.APIKeyComposition)
	}
	if len(analysisPayload.AuthFilesComposition) != 0 || len(analysisPayload.AIProviderComposition) != 0 {
		t.Fatalf("expected source composition to be omitted, got auth=%s provider=%s", analysisPayload.AuthFilesComposition, analysisPayload.AIProviderComposition)
	}
	if len(analysisPayload.Heatmap.APIKeys) != 1 || analysisPayload.Heatmap.APIKeys[0] != "42" || analysisPayload.Heatmap.APIKeyLabels["42"] != "Viewer Key" {
		t.Fatalf("expected heatmap to use only the session key identity, got %+v", analysisPayload.Heatmap)
	}
	if keyProvider.listCalls != 0 {
		t.Fatalf("expected viewer analysis not to list every API key, got %d calls", keyProvider.listCalls)
	}

	latencyResponse := serveAPIGet(router, "/api/v1/key-analysis/latency?range=24h&api_key_id=99", &http.Cookie{Name: standardSessionCookieName, Value: token})

	if latencyResponse.Code != http.StatusOK {
		t.Fatalf("latency status=%d, want 200: %s", latencyResponse.Code, latencyResponse.Body.String())
	}
	if len(usageProvider.latencyFilters) != 1 || usageProvider.latencyFilters[0].APIKeyID != "42" {
		t.Fatalf("expected latency filter to force session key 42, got %+v", usageProvider.latencyFilters)
	}
}

func TestAPIKeyViewerAnalysisRevokesInactiveKeySession(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	router := NewRouter(
		nil,
		nil,
		&keyViewerAnalysisUsageStub{},
		nil,
		config,
		NewAuthHandler(config, sessions),
		"/cpa",
		OptionalProviders{CPAAPIKeys: &keyViewerAnalysisKeyStub{}},
	)

	response := serveAPIGet(router, "/cpa/api/v1/key-analysis?range=24h", &http.Cookie{Name: standardSessionCookieName, Value: token})

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401: %s", response.Code, response.Body.String())
	}
	if sessions.Validate(token) {
		t.Fatal("expected inactive API key session to be revoked")
	}
	cookie := requireCookie(t, response.Result().Cookies(), standardSessionCookieName)
	if cookie.Path != "/cpa" || cookie.MaxAge >= 0 {
		t.Fatalf("expected the viewer cookie to be cleared on the configured base path, got %+v", cookie)
	}
}
