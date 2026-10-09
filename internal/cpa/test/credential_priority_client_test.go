package cpa_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestPriorityClientSendsOnlyPriorityFields(t *testing.T) {
	cases := []struct {
		name, providerType, path string
		openAI                   bool
	}{
		{name: "codex", providerType: "codex", path: "/v0/management/codex-api-key"},
		{name: "xai", providerType: "xai", path: "/v0/management/xai-api-key"},
		{name: "gemini", providerType: "gemini", path: "/v0/management/gemini-api-key"},
		{name: "gemini-interactions", providerType: "gemini-interactions", path: "/v0/management/interactions-api-key"},
		{name: "claude", providerType: "claude", path: "/v0/management/claude-api-key"},
		{name: "vertex", providerType: "vertex", path: "/v0/management/vertex-api-key"},
		{name: "meta", providerType: "meta", path: "/v0/management/meta-api-key"},
		{name: "openai", path: "/v0/management/openai-compatibility", openAI: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != tc.path || r.URL.RawQuery != "" {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"index": float64(3), "value": map[string]any{"priority": float64(-7)}}
				if !reflect.DeepEqual(body, want) {
					t.Fatalf("PATCH body = %#v, want %#v", body, want)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			var status int
			var err error
			if tc.openAI {
				status, err = client.UpdateOpenAICompatibilityPriority(context.Background(), 3, -7)
			} else {
				status, err = client.UpdateProviderPriority(context.Background(), tc.providerType, 3, -7)
			}
			if err != nil || status != http.StatusOK {
				t.Fatalf("priority patch status=%d err=%v", status, err)
			}
		})
	}
	if cpa.ProviderKeyStatusSupported("meta") || cpa.ProviderKeyStatusSupported("openai") {
		t.Fatal("priority support must not broaden status support")
	}
}

func TestAuthFilePriorityClientSendsNameAndPriorityOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v0/management/auth-files/fields" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"name": "auth.json", "priority": float64(0)}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("PATCH body = %#v, want %#v", body, want)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
	if status, err := client.UpdateAuthFilePriority(context.Background(), "auth.json", 0); err != nil || status != http.StatusOK {
		t.Fatalf("priority patch status=%d err=%v", status, err)
	}
}
