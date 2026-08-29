package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/ranking"
)

type adminLocalRankingProviderStub struct {
	calls int
}

func (s *adminLocalRankingProviderStub) Leaderboard(context.Context, ranking.LeaderboardPeriod, ranking.LeaderboardMetric) (ranking.Leaderboard, error) {
	s.calls++
	return ranking.Leaderboard{Entries: []ranking.LeaderboardEntry{}}, nil
}

func (s *adminLocalRankingProviderStub) UpdateProfile(_ context.Context, id int64, keyAlias string, avatarID uint8) (ranking.LocalProfile, error) {
	s.calls++
	return ranking.LocalProfile{ParticipantID: "42", KeyAlias: keyAlias, DisplayName: keyAlias, AvatarID: avatarID}, nil
}

func TestLocalRankingRouteIsAdminOnly(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("create viewer session: %v", err)
	}
	provider := &adminLocalRankingProviderStub{}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{LocalRanking: provider})

	viewer := httptest.NewRecorder()
	viewerRequest := httptest.NewRequest(http.MethodGet, "/api/v1/ranking/local/leaderboards?period=today&metric=overall", nil)
	viewerRequest.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: viewerToken})
	router.ServeHTTP(viewer, viewerRequest)
	if viewer.Code != http.StatusForbidden || provider.calls != 0 {
		t.Fatalf("API Key viewer reached local ranking: status=%d body=%s calls=%d", viewer.Code, viewer.Body.String(), provider.calls)
	}

	admin := httptest.NewRecorder()
	adminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/ranking/local/leaderboards?period=today&metric=overall", nil)
	adminRequest.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: adminToken})
	router.ServeHTTP(admin, adminRequest)
	if admin.Code != http.StatusOK || provider.calls != 1 {
		t.Fatalf("admin could not reach local ranking: status=%d body=%s calls=%d", admin.Code, admin.Body.String(), provider.calls)
	}
}
