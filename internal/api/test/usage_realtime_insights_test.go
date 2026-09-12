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

func TestRealtimeInsightsAPIMapsCachedTotalsAndScopesViewer(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "realtime.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	key := entities.CPAAPIKey{ID: 42, APIKey: "sk-viewer123456", KeyAlias: "Viewer"}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Create(&[]entities.UsageEvent{
		{EventKey: "own", Timestamp: now.Add(-time.Minute), APIGroupKey: key.APIKey, Model: "own-model", InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CacheReadTokens: 40},
		{EventKey: "other", Timestamp: now.Add(-time.Minute), APIGroupKey: "sk-other654321", Model: "other-model", Failed: true},
	}).Error; err != nil {
		t.Fatal(err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cache.Close)
	provider := service.NewUsageServiceWithRecentCache(db, cache, emptyPricingCatalogForTest())
	keys := &keyViewerAnalysisKeyStub{row: key}
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatal(err)
	}
	authConfig := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	for _, viewer := range []bool{false, true} {
		router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keys})
		path := "/api/v1/usage/overview/realtime?window=15m"
		if viewer {
			router = NewRouter(nil, nil, provider, nil, authConfig, NewAuthHandler(authConfig, sessions), "", OptionalProviders{CPAAPIKeys: keys})
			path = "/api/v1/key-overview/realtime?window=15m&api_key_id=99"
		}
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if viewer {
			request.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status %d: %s", response.Code, response.Body.String())
		}
		var payload struct {
			Insights struct {
				Summary struct {
					Requests, Failures int64
					TotalTokens        int64 `json:"total_tokens"`
					Cost               *float64
				}
				Outcomes []json.RawMessage
			}
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		wantRequests, wantFailures := int64(2), int64(1)
		if viewer {
			wantRequests, wantFailures = 1, 0
		}
		if payload.Insights.Summary.Requests != wantRequests || payload.Insights.Summary.Failures != wantFailures || payload.Insights.Summary.TotalTokens != 120 || payload.Insights.Summary.Cost != nil || len(payload.Insights.Outcomes) != 30 {
			t.Fatalf("incorrect insights: %+v", payload.Insights)
		}
		if strings.Contains(response.Body.String(), key.APIKey) || (viewer && strings.Contains(response.Body.String(), "other-model")) {
			t.Fatal("realtime scope leaked")
		}
	}
}
