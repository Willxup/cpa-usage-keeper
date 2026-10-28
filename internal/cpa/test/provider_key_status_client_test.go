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
	}{
		{providerType: "codex", path: "/v8/management/config/api-keys/codex"},
		{providerType: "xai", path: "/v8/management/config/api-keys/xai"},
		{providerType: "gemini", path: "/v8/management/config/api-keys/gemini"},
		{providerType: "gemini-interactions", path: "/v8/management/config/api-keys/interactions"},
		{providerType: "claude", path: "/v8/management/config/api-keys/claude"},
		{providerType: "vertex", path: "/v8/management/config/api-keys/vertex"},
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
				_, _ = w.Write([]byte(`[{"excluded-models":["gpt-5"],"keys":[{"api-key":"secret-key","auth_index":"idx-1"}]}]`))
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

func TestProviderConfigClientKeepsRawFieldsAndWritesEmptyMemberOverride(t *testing.T) {
	raw := `[{"base-url":"https://example.invalid","excluded-models":["*"],"headers":{"auth_index":"group-header","auth-index":"other"},"auth_index":"group-id","keys":[{"api-key":"key","auth_index":"target","auth-index":"discard","models":[],"headers":{},"opaque":{"integer":9007199254740993,"decimal":1.234567890123456789,"auth_index":"keep","null":null}},{"api-key":"other","auth_index":"other","weight":2,"proxy-url":null}]}]`
	var document providerconfig.Document
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	location, found := document.FindFirst("target")
	if !found {
		t.Fatal("target missing")
	}
	if err := document.SetKeyExcludedModels(location, nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v8/management/config/api-keys/codex" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var received providerconfig.Document
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
			return
		}
		fields := received.Groups[0].Keys[0].Fields
		if string(fields["excluded-models"]) != "[]" || string(fields["models"]) != "[]" || string(fields["headers"]) != "{}" {
			t.Errorf("explicit empty fields = %#v", fields)
		}
		if string(fields["opaque"]) != string(document.Groups[0].Keys[0].Fields["opaque"]) {
			t.Errorf("opaque value changed: %s", fields["opaque"])
		}
		if string(received.Groups[0].Fields["headers"]) != string(document.Groups[0].Fields["headers"]) {
			t.Error("header auth_index stripped")
		}
		if string(received.Groups[0].Keys[1].Fields["proxy-url"]) != "null" || string(received.Groups[0].Keys[1].Fields["weight"]) != "2" {
			t.Error("other key changed")
		}
		for _, group := range received.Groups {
			if _, exists := group.Fields["auth_index"]; exists {
				t.Error("group auth index survived")
			}
			for _, key := range group.Keys {
				if _, exists := key.Fields["auth_index"]; exists {
					t.Error("key auth index survived")
				}
				if _, exists := key.Fields["auth-index"]; exists {
					t.Error("key auth-index survived")
				}
			}
		}
	}))
	defer server.Close()
	if _, err := cpa.NewClient(server.URL, "management-secret", time.Second, false).UpdateProviderConfig(context.Background(), "codex", &document, ""); err != nil {
		t.Fatal(err)
	}
	if string(document.Groups[0].Keys[0].Fields["auth_index"]) != `"target"` {
		t.Fatal("write mutated source document")
	}
}

func TestProviderConfigClientReturnsFailedPUTStatus(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed, http.StatusUnauthorized, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			got, err := cpa.NewClient(server.URL, "management-secret", time.Second, false).UpdateProviderConfig(context.Background(), "gemini", &providerconfig.Document{}, "")
			if got != status || err == nil {
				t.Fatalf("status = %d, err = %v", got, err)
			}
		})
	}
}
