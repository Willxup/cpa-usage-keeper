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

type rankingRouteProviderStub struct {
	statusCalls      int
	leaderboardCalls int
}

func (s *rankingRouteProviderStub) Status(context.Context) (ranking.LocalStatus, error) {
	s.statusCalls++
	return ranking.LocalStatus{Status: ranking.StatusDisabled}, nil
}

func (*rankingRouteProviderStub) Join(context.Context, string, uint8) (ranking.LocalStatus, error) {
	return ranking.LocalStatus{}, nil
}

func (*rankingRouteProviderStub) SyncNow(context.Context) error { return nil }

func (*rankingRouteProviderStub) Pause(context.Context) (ranking.LocalStatus, error) {
	return ranking.LocalStatus{}, nil
}

func (*rankingRouteProviderStub) Resume(context.Context) (ranking.LocalStatus, error) {
	return ranking.LocalStatus{}, nil
}

func (*rankingRouteProviderStub) Exit(context.Context) (ranking.LocalStatus, error) {
	return ranking.LocalStatus{}, nil
}

func (s *rankingRouteProviderStub) Leaderboard(context.Context, ranking.LeaderboardPeriod, ranking.LeaderboardMetric) (ranking.Leaderboard, error) {
	s.leaderboardCalls++
	return ranking.Leaderboard{}, nil
}

func (*rankingRouteProviderStub) LeaderboardMetadata(context.Context) (ranking.LeaderboardMetadata, error) {
	return ranking.LeaderboardMetadata{PeriodTimezone: "Asia/Shanghai"}, nil
}

func TestRankingRoutesAreMountedOnlyInsideAdminGroup(t *testing.T) {
	sessions := auth.NewSessionManager(time.Hour)
	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	viewerToken, _, err := sessions.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("create viewer session: %v", err)
	}
	provider := &rankingRouteProviderStub{}
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour}
	router := NewRouter(nil, nil, nil, nil, config, NewAuthHandler(config, sessions), "", OptionalProviders{Ranking: provider})

	viewerResponse := httptest.NewRecorder()
	viewerRequest := httptest.NewRequest(http.MethodGet, "/api/v1/ranking/status", nil)
	viewerRequest.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: viewerToken})
	router.ServeHTTP(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden || provider.statusCalls != 0 {
		t.Fatalf("viewer reached ranking route: status=%d body=%s calls=%d", viewerResponse.Code, viewerResponse.Body.String(), provider.statusCalls)
	}

	adminResponse := httptest.NewRecorder()
	adminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/ranking/status", nil)
	adminRequest.AddCookie(&http.Cookie{Name: "cpa_usage_keeper_session", Value: adminToken})
	router.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK || provider.statusCalls != 1 {
		t.Fatalf("admin could not reach ranking route: status=%d body=%s calls=%d", adminResponse.Code, adminResponse.Body.String(), provider.statusCalls)
	}
}
