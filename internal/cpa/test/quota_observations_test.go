package cpa_test

import (
	"context"
	"cpa-usage-keeper/internal/cpa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuotaObservationClientUsesAuthenticatedV8AndOldServerFallback(t *testing.T) {
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v8/management/quota/observations" || r.Header.Get("Authorization") != "Bearer synthetic-management" {
			t.Error("wrong route or auth")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, `{"items":[{"auth_index":"synthetic","provider":"codex","window":"primary","observed_at":"2026-10-01T12:00:00Z","source":"websocket_event","headers":{"X-Codex-Primary-Used-Percent":["12"]}}]}`)
	}))
	defer server.Close()
	client := cpa.NewClient(server.URL, "synthetic-management", time.Second, false)
	items, err := client.FetchQuotaObservations(context.Background())
	if err != nil || len(items) != 1 || items[0].Source != "websocket_event" {
		t.Fatal("observation missing", err)
	}
	status = 404
	items, err = client.FetchQuotaObservations(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatal("old server fallback failed", err)
	}
	status = 500
	if _, err = client.FetchQuotaObservations(context.Background()); err == nil {
		t.Fatal("server error hidden")
	}
}
