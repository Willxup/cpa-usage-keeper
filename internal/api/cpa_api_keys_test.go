package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"

	"gorm.io/gorm"
)

func TestCPAAPIKeyRoutesReturnDisplayDataWithoutRawKeys(t *testing.T) {
	db := openCPAAPIKeyAPITestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta654321"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, 1, "Primary Key"); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/api-keys", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if strings.Contains(body, "sk-alpha123456") || strings.Contains(body, "sk-beta654321") || strings.Contains(body, "apiKey") || strings.Contains(body, "api_key") {
		t.Fatalf("response leaked raw key data: %s", body)
	}
	var parsed struct {
		Items []struct {
			ID           string  `json:"id"`
			KeyAlias     string  `json:"keyAlias"`
			DisplayKey   string  `json:"displayKey"`
			Label        string  `json:"label"`
			LastSyncedAt *string `json:"lastSyncedAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(parsed.Items) != 2 {
		t.Fatalf("expected two API key rows, got %+v", parsed.Items)
	}
	if parsed.Items[0].ID != "1" || parsed.Items[0].KeyAlias != "Primary Key" || parsed.Items[0].DisplayKey != "sk-*********123456" || parsed.Items[0].Label != "Primary Key" || parsed.Items[0].LastSyncedAt == nil {
		t.Fatalf("unexpected aliased row: %+v", parsed.Items[0])
	}
	if parsed.Items[1].ID != "2" || parsed.Items[1].KeyAlias != "" || parsed.Items[1].DisplayKey != "sk-*********654321" || parsed.Items[1].Label != "sk-*********654321" {
		t.Fatalf("unexpected fallback row: %+v", parsed.Items[1])
	}
}

func TestCPAAPIKeySettingsRouteReturnsRawKeys(t *testing.T) {
	db := openCPAAPIKeyAPITestDatabase(t)
	syncedAt := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta654321"}, syncedAt); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, 1, "Primary Key"); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/api-keys/settings", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var parsed struct {
		Items []struct {
			ID           string  `json:"id"`
			APIKey       string  `json:"apiKey"`
			KeyAlias     string  `json:"keyAlias"`
			DisplayKey   string  `json:"displayKey"`
			Label        string  `json:"label"`
			LastSyncedAt *string `json:"lastSyncedAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(parsed.Items) != 2 {
		t.Fatalf("expected two API key rows, got %+v", parsed.Items)
	}
	if parsed.Items[0].ID != "1" || parsed.Items[0].APIKey != "sk-alpha123456" || parsed.Items[0].KeyAlias != "Primary Key" || parsed.Items[0].DisplayKey != "sk-*********123456" || parsed.Items[0].Label != "Primary Key" || parsed.Items[0].LastSyncedAt == nil {
		t.Fatalf("unexpected aliased settings row: %+v", parsed.Items[0])
	}
	if parsed.Items[1].ID != "2" || parsed.Items[1].APIKey != "sk-beta654321" || parsed.Items[1].KeyAlias != "" || parsed.Items[1].DisplayKey != "sk-*********654321" || parsed.Items[1].Label != "sk-*********654321" {
		t.Fatalf("unexpected fallback settings row: %+v", parsed.Items[1])
	}
}

func TestCPAAPIKeyRoutesNormalizeStaleDisplayKeys(t *testing.T) {
	db := openCPAAPIKeyAPITestDatabase(t)
	if err := db.Create(&entities.CPAAPIKey{
		APIKey:     "sk-BabcdefghijklmnopqrstuvwxyzmaWyTA",
		DisplayKey: "sk-B********************************maWy",
	}).Error; err != nil {
		t.Fatalf("seed stale API key: %v", err)
	}
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/api-keys", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var parsed struct {
		Items []struct {
			DisplayKey string `json:"displayKey"`
			Label      string `json:"label"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(parsed.Items) != 1 || parsed.Items[0].DisplayKey != "sk-*********maWyTA" || parsed.Items[0].Label != "sk-*********maWyTA" {
		t.Fatalf("expected canonical display data, got %+v", parsed.Items)
	}
}

func TestCPAAPIKeyOptionsReturnActiveLabels(t *testing.T) {
	db := openCPAAPIKeyAPITestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456", "sk-beta654321"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	if err := repository.UpdateCPAAPIKeyAlias(db, 1, "Primary Key"); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("delete missing key: %v", err)
	}
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/api-keys/options", nil)
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var parsed struct {
		Options []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"options"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(parsed.Options) != 1 || parsed.Options[0].ID != "1" || parsed.Options[0].Label != "Primary Key" {
		t.Fatalf("unexpected options: %+v", parsed.Options)
	}
	var raw struct {
		Options []map[string]any `json:"options"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	for _, option := range raw.Options {
		for _, key := range []string{"keyAlias", "displayKey", "lastSyncedAt"} {
			if _, ok := option[key]; ok {
				t.Fatalf("options response included settings-only field %q: %s", key, resp.Body.String())
			}
		}
	}
}

func TestUpdateCPAAPIKeyAliasUpdatesAndClearsAlias(t *testing.T) {
	db := openCPAAPIKeyAPITestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})

	for _, body := range []string{`{"keyAlias":"  Primary Key  "}`, `{"keyAlias":""}`} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/usage/api-keys/1", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
		}
	}

	rows, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil {
		t.Fatalf("ListActiveCPAAPIKeys returned error: %v", err)
	}
	if len(rows) != 1 || rows[0].KeyAlias != "" {
		t.Fatalf("expected alias to be cleared, got %+v", rows)
	}
}

func TestUpdateCPAAPIKeyAliasRejectsInvalidInputAndDeletedRows(t *testing.T) {
	db := openCPAAPIKeyAPITestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	if err := repository.SyncCPAAPIKeys(db, nil, time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}
	router := NewRouter(nil, statusStub{}, nil, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: service.NewCPAAPIKeyService(db)})

	for _, tc := range []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "invalid id", path: "/api/v1/usage/api-keys/not-an-int", body: `{"keyAlias":"ok"}`, want: http.StatusBadRequest},
		{name: "deleted id", path: "/api/v1/usage/api-keys/1", body: `{"keyAlias":"ok"}`, want: http.StatusNotFound},
		{name: "too long", path: "/api/v1/usage/api-keys/1", body: `{"keyAlias":"` + strings.Repeat("a", 129) + `"}`, want: http.StatusBadRequest},
		{name: "control char", path: "/api/v1/usage/api-keys/1", body: "{\"keyAlias\":\"bad\\u0001alias\"}", want: http.StatusBadRequest},
	} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(resp, req)
		if resp.Code != tc.want {
			t.Fatalf("%s: expected status %d, got %d body=%s", tc.name, tc.want, resp.Code, resp.Body.String())
		}
	}
}

func openCPAAPIKeyAPITestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "api-keys.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
