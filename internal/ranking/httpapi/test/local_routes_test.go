package test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/ranking/httpapi"
	"github.com/gin-gonic/gin"
)

type localRankingProviderStub struct {
	period          ranking.LeaderboardPeriod
	metric          ranking.LeaderboardMetric
	err             error
	profileID       int64
	profileAlias    string
	profileAvatarID uint8
}

func (s *localRankingProviderStub) UpdateProfile(_ context.Context, id int64, keyAlias string, avatarID uint8) (ranking.LocalProfile, error) {
	s.profileID = id
	s.profileAlias = keyAlias
	s.profileAvatarID = avatarID
	if s.err != nil {
		return ranking.LocalProfile{}, s.err
	}
	return ranking.LocalProfile{
		ParticipantID: strconv.FormatInt(id, 10), KeyAlias: strings.TrimSpace(keyAlias), DisplayName: strings.TrimSpace(keyAlias), AvatarID: avatarID,
	}, nil
}

func (s *localRankingProviderStub) Leaderboard(_ context.Context, period ranking.LeaderboardPeriod, metric ranking.LeaderboardMetric) (ranking.Leaderboard, error) {
	s.period = period
	s.metric = metric
	if s.err != nil {
		return ranking.Leaderboard{}, s.err
	}
	return ranking.Leaderboard{
		Period: period, PeriodKey: "2026-07-31", Metric: metric, GeneratedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), Entries: []ranking.LeaderboardEntry{},
	}, nil
}

func TestLocalRankingProfileRouteUpdatesAliasAndAvatar(t *testing.T) {
	provider := &localRankingProviderStub{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/ranking/local/profiles/42", strings.NewReader(`{"key_alias":"  Primary  ","avatar_id":17}`))
	request.Header.Set("Content-Type", "application/json")
	localRankingRouter(provider).ServeHTTP(response, request)

	if response.Code != http.StatusOK || provider.profileID != 42 || provider.profileAlias != "  Primary  " || provider.profileAvatarID != 17 {
		t.Fatalf("unexpected local profile update: status=%d body=%s provider=%+v", response.Code, response.Body.String(), provider)
	}
	if !strings.Contains(response.Body.String(), `"key_alias":"Primary"`) || !strings.Contains(response.Body.String(), `"avatar_id":17`) {
		t.Fatalf("unexpected local profile response: %s", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("local profile response allowed caching: %+v", response.Header())
	}
}

func TestLocalRankingRouteValidatesSelectionAndDisablesCaching(t *testing.T) {
	provider := &localRankingProviderStub{}
	router := localRankingRouter(provider)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ranking/local/leaderboards?period=today&metric=overall", nil))
	if response.Code != http.StatusOK || provider.period != ranking.LeaderboardToday || provider.metric != ranking.MetricOverall {
		t.Fatalf("unexpected local leaderboard result: status=%d body=%s provider=%+v", response.Code, response.Body.String(), provider)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" || response.Header().Get("Expires") != "0" {
		t.Fatalf("local leaderboard response allowed caching: %+v", response.Header())
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/ranking/local/leaderboards?period=today&metric=unknown", nil))
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid_leaderboard_selection") {
		t.Fatalf("unexpected invalid local selection response: status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestLocalRankingRouteUsesLocalFailureCode(t *testing.T) {
	response := httptest.NewRecorder()
	localRankingRouter(&localRankingProviderStub{err: errors.New("boom")}).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/ranking/local/leaderboards?period=today&metric=overall", nil),
	)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "local_ranking_failed") {
		t.Fatalf("unexpected local failure response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func localRankingRouter(provider httpapi.LocalProvider) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	httpapi.RegisterLocalRoutes(router.Group("/api/v1"), provider)
	return router
}
