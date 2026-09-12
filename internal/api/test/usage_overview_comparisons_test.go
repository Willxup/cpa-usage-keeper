package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestOverviewComparisonAPIUsesAliasesAndViewerScope(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "comparisons.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	key := entities.CPAAPIKey{ID: 42, APIKey: "sk-viewer123456", KeyAlias: "Viewer Key"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	rows := []entities.UsageOverviewDailyStat{
		{BucketStart: today, APIGroupKey: key.APIKey, Model: "my-model", RequestCount: 2, SuccessCount: 2, InputTokens: 50, TotalTokens: 50},
		{BucketStart: today, APIGroupKey: "sk-other654321", Model: "other-model", RequestCount: 5, SuccessCount: 5, TotalTokens: 100},
		{BucketStart: today, APIGroupKey: "sk-legacy-one-123456", Model: "legacy-model", RequestCount: 1, SuccessCount: 1},
		{BucketStart: today, APIGroupKey: "sk-legacy-two-123456", Model: "legacy-model", RequestCount: 1, SuccessCount: 1},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	provider := service.NewUsageService(db, emptyPricingCatalogForTest())
	keys := &keyViewerAnalysisKeyStub{row: key}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keys})
	query := "?range=custom&unit=day&start=" + today.Format(time.DateOnly) + "&end=" + today.Format(time.DateOnly)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview/comparisons"+query, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("admin status %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Models []struct {
			Key  string
			Cost *float64
		}
		APIKeys []struct{ Key, Label string } `json:"api_keys"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.APIKeys) != 4 {
		t.Fatalf("key count: %d", len(payload.APIKeys))
	}
	seen := map[string]bool{}
	foundAlias := false
	for _, item := range payload.APIKeys {
		if seen[item.Key] {
			t.Fatal("history key identifiers collided")
		}
		seen[item.Key] = true
		if item.Key == "42" && item.Label == "Viewer Key" {
			foundAlias = true
		}
	}
	if !foundAlias {
		t.Fatal("missing current key id/alias")
	}
	for _, raw := range []string{key.APIKey, "sk-other654321", "sk-legacy-one-123456", "sk-legacy-two-123456"} {
		if strings.Contains(response.Body.String(), raw) {
			t.Fatal("raw API key leaked")
		}
	}
	keys.listCalls = 0
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatal(err)
	}
	authConfig := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	viewerRouter := NewRouter(nil, nil, provider, nil, authConfig, NewAuthHandler(authConfig, sessions), "", OptionalProviders{CPAAPIKeys: keys})
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/key-overview/comparisons"+query+"&api_key_id=99", nil)
	request.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
	viewerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("viewer status %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "api_keys") || strings.Contains(response.Body.String(), "other-model") || strings.Contains(response.Body.String(), key.APIKey) {
		t.Fatal("viewer received data outside API Key scope")
	}
	if !strings.Contains(response.Body.String(), `"key":"42"`) || keys.listCalls != 0 {
		t.Fatal("viewer should receive its own API Key data without listing keys")
	}
}
