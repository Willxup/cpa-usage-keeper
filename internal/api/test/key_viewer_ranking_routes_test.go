package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
)

type keyViewerRankingKeyStub struct {
	row entities.CPAAPIKey
}

func (s *keyViewerRankingKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return []entities.CPAAPIKey{s.row}, nil
}

func (s *keyViewerRankingKeyStub) FindActiveCPAAPIKeyByValue(context.Context, string) (entities.CPAAPIKey, error) {
	return s.row, nil
}

func (s *keyViewerRankingKeyStub) FindActiveCPAAPIKeyByID(_ context.Context, id int64) (entities.CPAAPIKey, error) {
	if id != s.row.ID {
		return entities.CPAAPIKey{}, service.ErrInvalidID
	}
	return s.row, nil
}

func (s *keyViewerRankingKeyStub) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return s.row, nil
}

func newKeyViewerRankingRouter(t *testing.T, localEnabled bool) (*auth.SessionManager, string, *rankingRouteProviderStub, *adminLocalRankingProviderStub, http.Handler) {
	t.Helper()
	sessions := auth.NewSessionManager(time.Hour)
	viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("create viewer session: %v", err)
	}
	community := &rankingRouteProviderStub{}
	local := &adminLocalRankingProviderStub{}
	keyProvider := &keyViewerRankingKeyStub{row: entities.CPAAPIKey{ID: 42, APIKey: "sk-viewer123456", KeyAlias: "Viewer"}}
	config := AuthConfig{
		Enabled:                         true,
		LoginPassword:                   "secret",
		SessionTTL:                      time.Hour,
		APIKeyViewerLocalRankingEnabled: localEnabled,
	}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{
		CPAAPIKeys:   keyProvider,
		Ranking:      community,
		LocalRanking: local,
	})
	return sessions, viewerToken, community, local, router
}

func viewerRankingRequest(method, target, token string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: token})
	return request
}

func TestKeyViewerCommunityRankingRouteIsReadOnly(t *testing.T) {
	_, viewerToken, community, _, router := newKeyViewerRankingRouter(t, false)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, viewerRankingRequest(http.MethodGet, "/api/v1/key-ranking/leaderboards?period=today&metric=overall", viewerToken))
	if response.Code != http.StatusOK || community.leaderboardCalls != 1 {
		t.Fatalf("viewer could not read community ranking: status=%d body=%s calls=%d", response.Code, response.Body.String(), community.leaderboardCalls)
	}

	mutation := httptest.NewRecorder()
	router.ServeHTTP(mutation, viewerRankingRequest(http.MethodPost, "/api/v1/key-ranking/join", viewerToken))
	if mutation.Code != http.StatusNotFound {
		t.Fatalf("expected viewer ranking mutation route to remain absent, got %d %s", mutation.Code, mutation.Body.String())
	}
}

func TestKeyViewerLocalRankingRouteFollowsExplicitAccessFlag(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		_, viewerToken, _, local, router := newKeyViewerRankingRouter(t, false)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, viewerRankingRequest(http.MethodGet, "/api/v1/key-ranking/local/leaderboards?period=today&metric=overall", viewerToken))
		if response.Code != http.StatusNotFound || local.calls != 0 {
			t.Fatalf("disabled local ranking was exposed: status=%d body=%s calls=%d", response.Code, response.Body.String(), local.calls)
		}
	})

	t.Run("enabled read only", func(t *testing.T) {
		_, viewerToken, _, local, router := newKeyViewerRankingRouter(t, true)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, viewerRankingRequest(http.MethodGet, "/api/v1/key-ranking/local/leaderboards?period=today&metric=overall", viewerToken))
		if response.Code != http.StatusOK || local.calls != 1 {
			t.Fatalf("enabled local ranking was unavailable: status=%d body=%s calls=%d", response.Code, response.Body.String(), local.calls)
		}

		mutation := httptest.NewRecorder()
		router.ServeHTTP(mutation, viewerRankingRequest(http.MethodPatch, "/api/v1/key-ranking/local/profiles/42", viewerToken))
		if mutation.Code != http.StatusNotFound || local.calls != 1 {
			t.Fatalf("viewer local profile mutation was exposed: status=%d body=%s calls=%d", mutation.Code, mutation.Body.String(), local.calls)
		}

		session := httptest.NewRecorder()
		router.ServeHTTP(session, viewerRankingRequest(http.MethodGet, "/api/v1/auth/session", viewerToken))
		if session.Code != http.StatusOK || !strings.Contains(session.Body.String(), `"local_ranking_enabled":true`) {
			t.Fatalf("viewer session omitted local ranking capability: status=%d body=%s", session.Code, session.Body.String())
		}
	})
}
