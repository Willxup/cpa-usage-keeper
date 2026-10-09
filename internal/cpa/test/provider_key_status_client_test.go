package cpa_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

// TestFetchProviderKeyConfigUsesDedicatedEndpointPerProviderType 锁定开关流程的读取路径与列表解析。
func TestFetchProviderKeyConfigUsesDedicatedEndpointPerProviderType(t *testing.T) {
	cases := []struct {
		providerType string
		path         string
		payloadKey   string
	}{
		{providerType: "codex", path: "/v0/management/codex-api-key", payloadKey: "codex-api-key"},
		{providerType: "xai", path: "/v0/management/xai-api-key", payloadKey: "xai-api-key"},
		{providerType: "gemini", path: "/v0/management/gemini-api-key", payloadKey: "gemini-api-key"},
		{providerType: "gemini-interactions", path: "/v0/management/interactions-api-key", payloadKey: "interactions-api-key"},
		{providerType: "claude", path: "/v0/management/claude-api-key", payloadKey: "claude-api-key"},
		{providerType: "vertex", path: "/v0/management/vertex-api-key", payloadKey: "vertex-api-key"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.providerType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("method = %q, want GET", r.Method)
				}
				if r.URL.Path != tc.path {
					t.Fatalf("path = %q, want %q", r.URL.Path, tc.path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
					t.Fatalf("Authorization = %q", got)
				}
				// excluded-models 必须被解析出来，停用流程才能在原列表上做增删。
				_, _ = w.Write([]byte(`{"` + tc.payloadKey + `":[{"api-key":"secret-key","auth-index":"idx-1","excluded-models":["gpt-5"]}]}`))
			}))
			defer server.Close()

			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			result, err := client.FetchProviderKeyConfig(context.Background(), tc.providerType)
			if err != nil {
				t.Fatalf("FetchProviderKeyConfig returned error: %v", err)
			}
			if result == nil || len(result.Payload) != 1 {
				t.Fatalf("unexpected payload: %#v", result)
			}
			entry := result.Payload[0]
			if entry.AuthIndex != "idx-1" || len(entry.ExcludedModels) != 1 || entry.ExcludedModels[0] != "gpt-5" {
				t.Fatalf("unexpected entry: %+v", entry)
			}
		})
	}
}

// TestFetchProviderKeyConfigRejectsUnsupportedProviderType 保证 OpenAI 兼容类型不会误打到别的 endpoint。
func TestFetchProviderKeyConfigRejectsUnsupportedProviderType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request path %q", r.URL.Path)
	}))
	defer server.Close()

	client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
	if _, err := client.FetchProviderKeyConfig(context.Background(), "openai"); err == nil {
		t.Fatal("expected unsupported provider type error")
	}
	if cpa.ProviderKeyStatusSupported("openai") {
		t.Fatal("expected openai compatibility to be unsupported for status toggles")
	}
	for _, providerType := range []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex"} {
		if !cpa.ProviderKeyStatusSupported(providerType) {
			t.Fatalf("expected %s to support status toggles", providerType)
		}
	}
}

// TestUpdateProviderKeyExcludedModelsPatchesIndexAndValue 锁定 PATCH 结构：用配置数组下标定位，并带上 excluded-models。
func TestUpdateProviderKeyExcludedModelsPatchesIndexAndValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %q, want PATCH", r.Method)
		}
		if r.URL.Path != "/v0/management/gemini-api-key" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		var body struct {
			Index *int    `json:"index"`
			Match *string `json:"match"`
			Value *struct {
				ExcludedModels *[]string `json:"excluded-models"`
			} `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.Index == nil || *body.Index != 3 || body.Value == nil || body.Value.ExcludedModels == nil {
			t.Fatalf("unexpected patch body: %+v", body)
		}
		// 重复 API Key 只靠下标消歧，不能再发值匹配。
		if body.Match != nil {
			t.Fatalf("unexpected match field in patch body: %q", *body.Match)
		}
		// 停用时必须真的把精确 "*" 发给 CPA。
		if len(*body.Value.ExcludedModels) != 2 || (*body.Value.ExcludedModels)[0] != "gpt-5" || (*body.Value.ExcludedModels)[1] != "*" {
			t.Fatalf("unexpected excluded models: %#v", *body.Value.ExcludedModels)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
	statusCode, err := client.UpdateProviderKeyExcludedModels(context.Background(), "gemini", 3, []string{"gpt-5", cpa.ProviderKeyDisabledExcludedModel})
	if err != nil {
		t.Fatalf("UpdateProviderKeyExcludedModels returned error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("status code = %d", statusCode)
	}
}

// TestUpdateProviderKeyExcludedModelsSendsEmptyArrayToClearExclusions 防止空列表被编码成 null 而变成“未提供”。
func TestUpdateProviderKeyExcludedModelsSendsEmptyArrayToClearExclusions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(body["value"], &value); err != nil {
			t.Fatalf("decode value: %v", err)
		}
		if string(value["excluded-models"]) != "[]" {
			t.Fatalf("excluded-models = %s, want []", string(value["excluded-models"]))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
	if _, err := client.UpdateProviderKeyExcludedModels(context.Background(), "codex", 0, nil); err != nil {
		t.Fatalf("UpdateProviderKeyExcludedModels returned error: %v", err)
	}
}

// TestUpdateProviderKeyExcludedModelsSendsNoDisambiguationQuery 锁定重复 Key 不再依赖 Gemini 专属的 base-url 查询参数。
func TestUpdateProviderKeyExcludedModelsSendsNoDisambiguationQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raw := r.URL.RawQuery; raw != "" {
			t.Fatalf("unexpected query string %q", raw)
		}
		var body struct {
			Index int `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.Index != 1 {
			t.Fatalf("index = %d, want 1", body.Index)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
	if _, err := client.UpdateProviderKeyExcludedModels(context.Background(), "gemini", 1, []string{"*"}); err != nil {
		t.Fatalf("UpdateProviderKeyExcludedModels returned error: %v", err)
	}
}

// TestProviderKeyExcludedModelsDecodeKeepsKeeperDisabledDerivation 锁定 "*" 与显式 disabled 的关系。
func TestProviderKeyExcludedModelsDecodeKeepsKeeperDisabledDerivation(t *testing.T) {
	var withoutDisabled providerconfig.ProviderKeyConfig
	if err := json.Unmarshal([]byte(`{"api-key":"secret","auth-index":"idx","excluded-models":["*"]}`), &withoutDisabled); err != nil {
		t.Fatalf("decode provider key: %v", err)
	}
	if withoutDisabled.Disabled == nil || !*withoutDisabled.Disabled {
		t.Fatalf("expected wildcard exclusions to derive disabled, got %+v", withoutDisabled.Disabled)
	}

	explicit := false
	var withExplicit providerconfig.ProviderKeyConfig
	if err := json.Unmarshal([]byte(`{"api-key":"secret","auth-index":"idx","disabled":false,"excluded-models":["*"]}`), &withExplicit); err != nil {
		t.Fatalf("decode provider key: %v", err)
	}
	if withExplicit.Disabled == nil || *withExplicit.Disabled != explicit {
		t.Fatalf("expected explicit disabled to win, got %+v", withExplicit.Disabled)
	}
}

// TestUpdateProviderKeyExcludedModelsReturnsUpstreamStatusCode 锁定 provider PATCH 同样把状态码交给调用方。
func TestUpdateProviderKeyExcludedModelsReturnsUpstreamStatusCode(t *testing.T) {
	for _, wantStatus := range []int{http.StatusNotFound, http.StatusConflict} {
		wantStatus := wantStatus
		t.Run(http.StatusText(wantStatus), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v0/management/gemini-api-key" {
					t.Fatalf("path = %q", r.URL.Path)
				}
				w.WriteHeader(wantStatus)
				_, _ = w.Write([]byte(`{"error":"rejected"}`))
			}))
			defer server.Close()

			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			statusCode, err := client.UpdateProviderKeyExcludedModels(context.Background(), "gemini", 2, []string{"*"})
			if err == nil {
				t.Fatalf("expected error for status %d", wantStatus)
			}
			if statusCode != wantStatus {
				t.Fatalf("status code = %d, want %d", statusCode, wantStatus)
			}
		})
	}
}
