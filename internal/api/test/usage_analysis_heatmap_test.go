package test

import (
	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"encoding/json"
	"net/http"
	"testing"

	"cpa-usage-keeper/internal/helper"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestBuildAnalysisHeatmapPayloadSortsKeysByRequests(t *testing.T) {
	payload := requestAnalysisHeatmap(t, []servicedto.AnalysisHeatmapCell{
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
	payload := requestAnalysisHeatmap(t, []servicedto.AnalysisHeatmapCell{
		{APIKey: "sk-alpha123456", Model: "model", Requests: 1, TotalTokens: 100},
		{APIKey: "sk-beta654321", Model: "model", Requests: 2, TotalTokens: 200},
	}, []entities.CPAAPIKey{
		{ID: 1, APIKey: "sk-alpha123456", KeyAlias: "Shared"},
		{ID: 2, APIKey: "sk-beta654321", KeyAlias: "Shared"},
	})

	alphaKey := "1"
	betaKey := "2"
	if got := payload.APIKeys; len(got) != 2 || got[0] != betaKey || got[1] != alphaKey {
		t.Fatalf("expected heatmap API keys to use response IDs sorted by requests, got %+v", got)
	}
	if payload.APIKeyLabels[alphaKey] != "Shared" || payload.APIKeyLabels[betaKey] != "Shared" {
		t.Fatalf("expected duplicate labels to be stored separately by response key, got %+v", payload.APIKeyLabels)
	}
	if len(payload.Cells) != 2 || payload.Cells[0].APIKey != alphaKey || payload.Cells[1].APIKey != betaKey {
		t.Fatalf("expected heatmap cells to use response IDs, got %+v", payload.Cells)
	}
}

// 通过真实路由构造响应，覆盖排序与同名标签，而无需复制生产私有结构体。
type heatmapTestResponse struct {
	APIKeys      []string          `json:"api_keys"`
	APIKeyLabels map[string]string `json:"api_key_labels"`
	Models       []string          `json:"models"`
	Cells        []struct {
		APIKey string `json:"api_key"`
	} `json:"cells"`
}

func requestAnalysisHeatmap(t *testing.T, cells []servicedto.AnalysisHeatmapCell, keys []entities.CPAAPIKey) heatmapTestResponse {
	t.Helper()
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{Heatmap: cells}}
	optional := OptionalProviders{}
	if keys != nil {
		optional.CPAAPIKeys = usageAnalysisAPIKeyStub{rows: keys}
	}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", optional)
	response := serveAPIGet(router, "/api/v1/usage/analysis?range=24h")
	if response.Code != http.StatusOK {
		t.Fatalf("analysis status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Heatmap heatmapTestResponse `json:"heatmap"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode analysis heatmap: %v", err)
	}
	return body.Heatmap
}
