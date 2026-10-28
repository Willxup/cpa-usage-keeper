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
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

func TestProviderConfigClientPUTsEachSupplierList(t *testing.T) {
	for _, providerType := range []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "meta", "openai"} {
		t.Run(providerType, func(t *testing.T) {
			var document providerconfig.Document
			if err := json.Unmarshal([]byte(`[{"priority":5,"keys":[{"api-key":"key","auth_index":"target"}]}]`), &document); err != nil {
				t.Fatal(err)
			}
			location, _ := document.FindFirst("target")
			if providerType == "openai" {
				document.SetGroupPriority(location, -7)
			} else {
				document.SetKeyPriority(location, -7)
			}
			path, _ := cpa.ProviderConfigPath(providerType)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != path || r.URL.RawQuery != "" {
					t.Errorf("request = %s %s", r.Method, r.URL.String())
				}
				if r.Header.Get("Authorization") != "Bearer management-secret" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("management headers missing")
				}
				var received providerconfig.Document
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
					return
				}
				got := received.Groups[0].Keys[0].Fields["priority"]
				if providerType == "openai" {
					got = received.Groups[0].Fields["priority"]
				}
				if string(got) != "-7" {
					t.Errorf("priority = %s", got)
				}
				if _, exists := received.Groups[0].Keys[0].Fields["auth_index"]; exists {
					t.Error("read-only auth index entered PUT")
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			status, err := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false).UpdateProviderConfig(context.Background(), providerType, &document, "")
			if err != nil || status != http.StatusOK {
				t.Fatalf("PUT status = %d, err = %v", status, err)
			}
		})
	}
	if cpa.ProviderKeyStatusSupported("meta") || cpa.ProviderKeyStatusSupported("openai") {
		t.Fatal("priority support broadened status support")
	}
}

func TestCredentialPriorityClientSendsNameAndPriorityOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v8/management/credentials/fields" {
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
	if status, err := client.UpdateCredentialPriority(context.Background(), "auth.json", 0); err != nil || status != http.StatusOK {
		t.Fatalf("priority patch status=%d err=%v", status, err)
	}
}
