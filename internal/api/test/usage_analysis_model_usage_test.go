package test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type analysisModelUsagePayload struct {
	ModelUsage struct {
		Buckets []time.Time                `json:"buckets"`
		Series  []analysisModelUsageSeries `json:"series"`
	} `json:"model_usage"`
}

type analysisModelUsageSeries struct {
	Model       string  `json:"model"`
	TotalTokens []int64 `json:"total_tokens"`
	Requests    []int64 `json:"requests"`
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
	response := serveAPIGet(router, "/api/v1/usage/analysis?range=24h")

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	var payload analysisModelUsagePayload
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !slices.EqualFunc(payload.ModelUsage.Buckets, buckets, time.Time.Equal) {
		t.Fatalf("buckets = %v, want %v", payload.ModelUsage.Buckets, buckets)
	}
	want := []analysisModelUsageSeries{
		{"model-gamma", []int64{0, 500, 0}, []int64{0, 4, 0}},
		{"model-alpha", []int64{100, 200, 0}, []int64{1, 2, 0}},
		{"model-beta", []int64{200, 0, 100}, []int64{3, 0, 1}},
	}
	if !reflect.DeepEqual(payload.ModelUsage.Series, want) {
		t.Fatalf("model series = %+v, want %+v", payload.ModelUsage.Series, want)
	}
}

func TestUsageAnalysisEmptyResponseIncludesEmptyModelUsage(t *testing.T) {
	provider := &analysisSplitStub{analysis: &servicedto.AnalysisSnapshot{}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	response := serveAPIGet(router, "/api/v1/usage/analysis?range=24h")

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
