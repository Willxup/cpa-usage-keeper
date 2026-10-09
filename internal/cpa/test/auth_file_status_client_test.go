package cpa_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

const authFilesStatusEndpoint = "/v0/management/auth-files/status"

// TestUpdateAuthFileStatusPatchesManagementEndpoint 锁定单条凭证开关的 PATCH 合同，含 auth_index 消歧字段。
func TestUpdateAuthFileStatusPatchesManagementEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("expected PATCH method, got %s", r.Method)
		}
		if r.URL.Path != authFilesStatusEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
			t.Fatalf("expected management Authorization header, got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected JSON content type, got %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["name"] != "codex-user.json" || body["disabled"] != true {
			t.Fatalf("unexpected status request body: %#v", body)
		}
		if body["auth_index"] != "idx-1" {
			t.Fatalf("expected auth_index in status request body, got %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
	statusCode, err := client.UpdateAuthFileStatus(context.Background(), "codex-user.json", "idx-1", true)
	if err != nil {
		t.Fatalf("UpdateAuthFileStatus returned error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("unexpected status code %d", statusCode)
	}
}

// TestUpdateAuthFileStatusOmitsAuthIndexWhenUnknown 批量接口没有 auth_index，请求体必须和旧行为完全一致。
func TestUpdateAuthFileStatusOmitsAuthIndexWhenUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != authFilesStatusEndpoint {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, ok := body["auth_index"]; ok {
			t.Fatalf("expected auth_index to be omitted, got %#v", body)
		}
		if body["name"] != "codex-user.json" || body["disabled"] != true {
			t.Fatalf("unexpected status request body: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
	if _, err := client.UpdateAuthFileStatus(context.Background(), "codex-user.json", "", true); err != nil {
		t.Fatalf("UpdateAuthFileStatus returned error: %v", err)
	}
}

// TestUpdateAuthFileStatusReturnsUpstreamStatusCode 锁定非 2xx 时状态码与错误一起返回。
// 服务层依赖这个状态码区分 404（列表过期）与 409（不可单独操作），丢了它就会退化成 500。
func TestUpdateAuthFileStatusReturnsUpstreamStatusCode(t *testing.T) {
	for _, wantStatus := range []int{http.StatusNotFound, http.StatusConflict} {
		wantStatus := wantStatus
		t.Run(http.StatusText(wantStatus), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(wantStatus)
				_, _ = w.Write([]byte(`{"error":"rejected"}`))
			}))
			defer server.Close()

			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			statusCode, err := client.UpdateAuthFileStatus(context.Background(), "plugin-user.json", "idx-plugin", true)
			if err == nil {
				t.Fatalf("expected error for status %d", wantStatus)
			}
			if statusCode != wantStatus {
				t.Fatalf("status code = %d, want %d", statusCode, wantStatus)
			}
		})
	}
}
