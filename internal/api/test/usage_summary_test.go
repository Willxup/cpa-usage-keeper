package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type usageSummaryUsageStub struct {
	service.UsageProvider
	windows []time.Duration
	err     error
}

func (s *usageSummaryUsageStub) GetUsageSummaryWindows(_ context.Context, _ servicedto.UsageFilter, windows []time.Duration) ([]servicedto.UsageSummaryWindow, error) {
	s.windows = windows
	if s.err != nil {
		return nil, s.err
	}
	// 每个窗口返回固定的合成行，便于断言窗口数量、顺序和派生指标。
	anchor := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	rows := make([]servicedto.UsageSummaryWindow, 0, len(windows))
	for _, window := range windows {
		rows = append(rows, servicedto.UsageSummaryWindow{
			WindowSeconds:    int64(window / time.Second),
			WindowStart:      anchor.Add(-window),
			WindowEnd:        anchor,
			Requests:         2,
			TokenRequests:    2,
			CacheHitRequests: 1,
			TotalTokens:      120,
			InputTokens:      120,
			OutputTokens:     10,
			CacheReadTokens:  60,
			CostUSD:          0.5,
			CostAvailable:    true,
		})
	}
	return rows, nil
}

type usageSummaryWindowPayload struct {
	Window        string   `json:"window"`
	WindowSeconds int64    `json:"window_seconds"`
	Requests      int64    `json:"requests"`
	TotalTokens   int64    `json:"total_tokens"`
	CacheRate     *float64 `json:"cache_rate"`
	CacheHitRate  *float64 `json:"cache_hit_rate"`
	CostUSD       *float64 `json:"cost_usd"`
	CostAvailable bool     `json:"cost_available"`
	TPM           float64  `json:"tpm"`
	RPM           float64  `json:"rpm"`
}

type usageSummaryCredentialPayload struct {
	ID        string  `json:"id"`
	AuthIndex string  `json:"auth_index"`
	Alias     *string `json:"alias"`
	Quota     *struct {
		Status string `json:"status"`
	} `json:"quota"`
}

type usageSummaryBody struct {
	Windows     []usageSummaryWindowPayload     `json:"windows"`
	Credentials []usageSummaryCredentialPayload `json:"credentials"`
}

func newUsageSummaryRouter(usage service.UsageProvider, identities service.UsageIdentityProvider, quotaProvider QuotaProvider) http.Handler {
	return NewRouter(nil, nil, usage, nil, AuthConfig{}, nil, "", OptionalProviders{
		UsageIdentity: identities,
		Quota:         quotaProvider,
	})
}

func decodeUsageSummary(t *testing.T, body []byte) (usageSummaryBody, map[string]json.RawMessage) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	var decoded usageSummaryBody
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return decoded, raw
}

func TestUsageSummaryWithoutParamsReturnsNoSections(t *testing.T) {
	stub := &usageSummaryUsageStub{}
	router := newUsageSummaryRouter(stub, usageIdentitiesStub{}, nil)

	resp := serveAPIGet(router, "/api/v1/usage/summary")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", resp.Code, resp.Body.String())
	}
	_, raw := decodeUsageSummary(t, resp.Body.Bytes())
	if _, ok := raw["windows"]; ok {
		t.Fatalf("expected no windows section, got %s", resp.Body.String())
	}
	if _, ok := raw["credentials"]; ok {
		t.Fatalf("expected no credentials section, got %s", resp.Body.String())
	}
	if stub.windows != nil {
		t.Fatalf("expected provider not called, got windows %v", stub.windows)
	}
}

func TestUsageSummaryDedupesWindowsAndPreservesOrder(t *testing.T) {
	stub := &usageSummaryUsageStub{}
	router := newUsageSummaryRouter(stub, nil, nil)

	// 大小写、等价写法、重复项都应合并：1m / 1M / 60s -> 60s，5m，1m30s。
	resp := serveAPIGet(router, "/api/v1/usage/summary?windows=1M,1m,60s,5m,1m30s")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", resp.Code, resp.Body.String())
	}
	want := []time.Duration{time.Minute, 5 * time.Minute, 90 * time.Second}
	if len(stub.windows) != len(want) {
		t.Fatalf("expected %d windows, got %v", len(want), stub.windows)
	}
	for index, window := range want {
		if stub.windows[index] != window {
			t.Fatalf("window[%d] = %s, want %s", index, stub.windows[index], window)
		}
	}

	body, _ := decodeUsageSummary(t, resp.Body.Bytes())
	if len(body.Windows) != 3 {
		t.Fatalf("expected 3 window rows, got %d", len(body.Windows))
	}
	if body.Windows[0].Window != "1m" || body.Windows[1].Window != "5m" || body.Windows[2].Window != "1m30s" {
		t.Fatalf("unexpected window labels: %+v", body.Windows)
	}
	// 60s 窗口：120 tokens / 1 分钟 = 120 TPM，2 请求 / 1 分钟 = 2 RPM。
	if body.Windows[0].WindowSeconds != 60 || body.Windows[0].TPM != 120 || body.Windows[0].RPM != 2 {
		t.Fatalf("unexpected 1m derived metrics: %+v", body.Windows[0])
	}
	// cache_read 60 / input 120 = 50%，请求命中 1/2 = 50%。
	if body.Windows[0].CacheRate == nil || *body.Windows[0].CacheRate != 50 {
		t.Fatalf("unexpected cache_rate: %+v", body.Windows[0])
	}
	if body.Windows[0].CacheHitRate == nil || *body.Windows[0].CacheHitRate != 50 {
		t.Fatalf("unexpected cache_hit_rate: %+v", body.Windows[0])
	}
	if body.Windows[0].CostUSD == nil || *body.Windows[0].CostUSD != 0.5 {
		t.Fatalf("unexpected cost: %+v", body.Windows[0])
	}
}

func TestUsageSummaryRejectsInvalidWindows(t *testing.T) {
	router := newUsageSummaryRouter(&usageSummaryUsageStub{}, nil, nil)
	for _, target := range []string{
		"/api/v1/usage/summary?windows=61m",
		"/api/v1/usage/summary?windows=500ms",
		"/api/v1/usage/summary?windows=1.5s",
		"/api/v1/usage/summary?windows=abc",
		"/api/v1/usage/summary?windows=,,",
	} {
		resp := serveAPIGet(router, target)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d %s", target, resp.Code, resp.Body.String())
		}
	}
}

func TestUsageSummaryReturns503WhenCacheUnavailable(t *testing.T) {
	stub := &usageSummaryUsageStub{err: repository.ErrRecentUsageCacheUnavailable}
	router := newUsageSummaryRouter(stub, nil, nil)

	resp := serveAPIGet(router, "/api/v1/usage/summary?windows=1m")
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestUsageSummaryCredentialsAllAndSelective(t *testing.T) {
	identities := usageIdentitiesStub{items: []entities.UsageIdentity{
		{ID: 1, Name: "Alpha", Identity: "27d78dfe59a99760", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Provider: "claude"},
		{ID: 2, Name: "Beta", Identity: "1a7304eec55b9895", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Provider: "codex"},
	}}
	quotaStub := &quotaProviderStub{cacheResponse: quota.CacheResponse{Items: []quota.CachedQuotaItem{
		{AuthIndex: "27d78dfe59a99760", Status: quota.RefreshTaskStatusCompleted, Quota: &quota.CheckResponse{ID: "claude"}},
	}}}
	router := newUsageSummaryRouter(nil, identities, quotaStub)

	resp := serveAPIGet(router, "/api/v1/usage/summary?credentials=all")
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", resp.Code, resp.Body.String())
	}
	body, _ := decodeUsageSummary(t, resp.Body.Bytes())
	if len(body.Credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %+v", body.Credentials)
	}
	if body.Credentials[0].AuthIndex != "27d78dfe59a99760" || body.Credentials[0].Quota == nil || body.Credentials[0].Quota.Status != "completed" {
		t.Fatalf("unexpected first credential: %+v", body.Credentials[0])
	}
	// 没有缓存过额度的凭证仍然列出，只是不带 quota。
	if body.Credentials[1].AuthIndex != "1a7304eec55b9895" || body.Credentials[1].Quota != nil {
		t.Fatalf("unexpected second credential: %+v", body.Credentials[1])
	}
	if len(quotaStub.cacheRequest.AuthIndexes) != 2 {
		t.Fatalf("expected quota lookup for both auth indexes, got %v", quotaStub.cacheRequest.AuthIndexes)
	}

	// 按 auth_index 选择。
	resp = serveAPIGet(router, "/api/v1/usage/summary?credentials=1a7304eec55b9895")
	body, _ = decodeUsageSummary(t, resp.Body.Bytes())
	if len(body.Credentials) != 1 || body.Credentials[0].ID != "2" {
		t.Fatalf("expected only auth_index match, got %+v", body.Credentials)
	}

	// 按本地 id 选择，并验证去重。
	resp = serveAPIGet(router, "/api/v1/usage/summary?credentials=1,1,2")
	body, _ = decodeUsageSummary(t, resp.Body.Bytes())
	if len(body.Credentials) != 2 {
		t.Fatalf("expected id selection to return 2 credentials, got %+v", body.Credentials)
	}
}
