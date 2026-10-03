package cpa_test

import (
	"context"
	"cpa-usage-keeper/internal/cpa"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSharedKeyNameClientUsesV8AndKeepsCredentialsOutOfMetadata(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic-client")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v8/management/access/api-key-names" || r.Header.Get("Authorization") != "Bearer synthetic-management" {
			t.Error("incorrect management route or authentication")
			w.WriteHeader(401)
			return
		}
		if r.Method == http.MethodPatch {
			var request struct {
				Names map[string]string `json:"names"`
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.Names[digest] != "Team" {
				t.Error("incorrect name write")
			}
			if _, exists := request.Names["synthetic-client"]; exists {
				t.Error("metadata contains credential")
			}
		}
		fmt.Fprintf(w, `{"names":{"%s":"Team"}}`, digest)
	}))
	defer server.Close()
	client := cpa.NewClient(server.URL, "synthetic-management", time.Second, false)
	names, err := client.FetchAPIKeyNames(context.Background())
	if err != nil || names[digest] != "Team" {
		t.Fatal("name read failed")
	}
	if err := client.UpdateAPIKeyName(context.Background(), "synthetic-client", "Team"); err != nil {
		t.Fatal(err)
	}
}
func TestSharedKeyNameClientOldServerAndErrors(t *testing.T) {
	status := 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, "sensitive-response-body")
	}))
	defer server.Close()
	client := cpa.NewClient(server.URL, "synthetic-management", time.Second, false)
	names, err := client.FetchAPIKeyNames(context.Background())
	if err != nil || names != nil {
		t.Fatal("old server should preserve local aliases")
	}
	if err := client.UpdateAPIKeyName(context.Background(), "key", "Team"); err != nil {
		t.Fatal("old server should retain local alias editing", err)
	}
	status = 500
	_, err = client.FetchAPIKeyNames(context.Background())
	if err == nil || strings.Contains(err.Error(), "sensitive-response-body") {
		t.Fatal("error handling lost or disclosed response body")
	}
	if err := client.UpdateAPIKeyName(context.Background(), "key", "Team"); err == nil {
		t.Fatal("write failure not surfaced")
	}
}

func TestSharedKeyNameValidationBeforeRemoteWrite(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
	defer server.Close()
	client := cpa.NewClient(server.URL, "synthetic-management", time.Second, false)
	for _, name := range []string{strings.Repeat("x", 129), "hidden\u202ename", "synthetic-client"} {
		if err := client.UpdateAPIKeyName(context.Background(), "synthetic-client", name); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid metadata reached upstream")
	}
}
