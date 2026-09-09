package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestAPIKeyViewerAllowsRepeatedReadQueries(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	token, _, err := sessions.CreateAPIKeyViewerWithSource(42, auth.SessionSourceStandard)
	if err != nil {
		t.Fatalf("create API key viewer session: %v", err)
	}
	provider := &usageActivityRouteStub{
		UsageProvider: &usageEventsStub{},
		activity: &servicedto.UsageActivitySnapshot{
			Window:  servicedto.UsageActivityWindowWeek,
			Grain:   "medium",
			Rows:    7,
			Columns: 52,
			Blocks:  []servicedto.UsageActivityBlock{},
		},
	}
	keyProvider := &authCPAAPIKeyStub{row: entities.CPAAPIKey{
		ID: 42, APIKey: "provider-a", DisplayKey: "provider-a",
	}}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{CPAAPIKeys: keyProvider})

	for _, path := range []string{
		"/api/v1/key-overview?range=24h",
		"/api/v1/key-overview/realtime?window=60m",
		"/api/v1/key-activity?window=year",
	} {
		t.Run(path, func(t *testing.T) {
			for requestNumber := 1; requestNumber <= 2; requestNumber++ {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.AddCookie(&http.Cookie{Name: standardSessionCookieName, Value: token})
				router.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					t.Fatalf("request %d status=%d, want 200: %s", requestNumber, response.Code, response.Body.String())
				}
			}
		})
	}
}
