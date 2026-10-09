package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
	sessions, viewerToken, community, local, router := newKeyViewerRankingRouter(t, false)
	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	for _, tc := range []struct {
		path  string
		calls func() int
	}{
		{"/api/v1/ranking/status", func() int { return community.statusCalls }},
		{"/api/v1/ranking/local/leaderboards?period=today&metric=overall", func() int { return local.calls }},
	} {
		t.Run(tc.path, func(t *testing.T) {
			viewer := httptest.NewRecorder()
			router.ServeHTTP(viewer, viewerRankingRequest(http.MethodGet, tc.path, viewerToken))
			if viewer.Code != http.StatusForbidden || tc.calls() != 0 {
				t.Fatalf("viewer reached admin ranking: status=%d body=%s calls=%d", viewer.Code, viewer.Body.String(), tc.calls())
			}
			admin := httptest.NewRecorder()
			router.ServeHTTP(admin, viewerRankingRequest(http.MethodGet, tc.path, adminToken))
			if admin.Code != http.StatusOK || tc.calls() != 1 {
				t.Fatalf("admin could not reach ranking: status=%d body=%s calls=%d", admin.Code, admin.Body.String(), tc.calls())
			}
		})
	}
}

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
