package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type analysisModelUsagePayload struct {
	ModelUsage struct {
		Buckets []time.Time `json:"buckets"`
		Series  []struct {
			Model       string  `json:"model"`
			TotalTokens []int64 `json:"total_tokens"`
			Requests    []int64 `json:"requests"`
		} `json:"series"`
	} `json:"model_usage"`
}

func TestUsageAnalysisReturnsCompactAlignedModelUsageSeries(t *testing.T) {
	start := time.Date(2026, 8, 1, 10, 0, 0, 0, time.Local)
	buckets := []time.Time{start, start.Add(time.Hour), start.Add(2 * time.Hour)}
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{
		Granularity: servicedto.AnalysisGranularityHourly,
		TokenUsage: []servicedto.AnalysisTokenUsageBucket{
			{Bucket: buckets[0], TotalTokens: 300},
			{Bucket: buckets[1], TotalTokens: 700},
			{Bucket: buckets[2], TotalTokens: 100},
		},
		ModelUsage: []servicedto.AnalysisModelUsage{
			{Bucket: buckets[0], Model: "model-alpha", TotalTokens: 100, Requests: 1},
			{Bucket: buckets[1], Model: "model-alpha", TotalTokens: 200, Requests: 2},
			{Bucket: buckets[0], Model: "model-beta", TotalTokens: 200, Requests: 3},
			{Bucket: buckets[2], Model: "model-beta", TotalTokens: 100, Requests: 1},
			{Bucket: buckets[1], Model: "model-gamma", TotalTokens: 500, Requests: 4},
		},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis?range=24h", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload analysisModelUsagePayload
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.ModelUsage.Buckets) != len(buckets) {
		t.Fatalf("buckets = %v, want %v", payload.ModelUsage.Buckets, buckets)
	}
	for index := range buckets {
		if !payload.ModelUsage.Buckets[index].Equal(buckets[index]) {
			t.Fatalf("bucket[%d] = %v, want %v", index, payload.ModelUsage.Buckets[index], buckets[index])
		}
	}
	if len(payload.ModelUsage.Series) != 3 {
		t.Fatalf("series = %+v, want 3 models", payload.ModelUsage.Series)
	}
	assertAnalysisModelUsageSeries(t, payload.ModelUsage.Series[0].Model, payload.ModelUsage.Series[0].TotalTokens, payload.ModelUsage.Series[0].Requests, "model-gamma", []int64{0, 500, 0}, []int64{0, 4, 0})
	assertAnalysisModelUsageSeries(t, payload.ModelUsage.Series[1].Model, payload.ModelUsage.Series[1].TotalTokens, payload.ModelUsage.Series[1].Requests, "model-alpha", []int64{100, 200, 0}, []int64{1, 2, 0})
	assertAnalysisModelUsageSeries(t, payload.ModelUsage.Series[2].Model, payload.ModelUsage.Series[2].TotalTokens, payload.ModelUsage.Series[2].Requests, "model-beta", []int64{200, 0, 100}, []int64{3, 0, 1})
}

func TestUsageAnalysisEmptyResponseIncludesEmptyModelUsage(t *testing.T) {
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/analysis?range=24h", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload analysisModelUsagePayload
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.ModelUsage.Buckets == nil || payload.ModelUsage.Series == nil {
		t.Fatalf("expected empty arrays instead of null, got %s", response.Body.String())
	}
}

func assertAnalysisModelUsageSeries(t *testing.T, model string, tokens, requests []int64, wantModel string, wantTokens, wantRequests []int64) {
	t.Helper()
	if model != wantModel || !reflect.DeepEqual(tokens, wantTokens) || !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("series = model:%q tokens:%v requests:%v, want model:%q tokens:%v requests:%v", model, tokens, requests, wantModel, wantTokens, wantRequests)
	}
}
