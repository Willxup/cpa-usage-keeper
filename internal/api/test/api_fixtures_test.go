package test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	keeperapi "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

func openAPITestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "api-test.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func serveAPIGet(router http.Handler, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func newUsageViewerRouter(t *testing.T, provider service.UsageProvider) (http.Handler, *http.Cookie) {
	t.Helper()
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-cpa-viewer", DisplayKey: "sk-...viewer"}}
	config := keeperapi.AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := keeperapi.NewRouter(nil, nil, provider, nil, config, keeperapi.NewAuthHandler(config, sessions), "", keeperapi.OptionalProviders{CPAAPIKeys: keyProvider})
	return router, &http.Cookie{Name: standardSessionCookieName, Value: token}
}
