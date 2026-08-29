package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/ranking/httpapi"
	"github.com/gin-gonic/gin"
)

type rankingProviderStub struct {
	status       ranking.LocalStatus
	joinName     string
	joinAvatarID uint8
	joinErr      error
	syncErr      error
	runCalls     int
	pauseCalls   int
	resumeCalls  int
	exitCalls    int
	boardPeriod  ranking.LeaderboardPeriod
	boardMetric  ranking.LeaderboardMetric
	metadataErr  error
}

func (s *rankingProviderStub) Status(context.Context) (ranking.LocalStatus, error) {
	return s.status, nil
}

func (s *rankingProviderStub) Join(_ context.Context, name string, avatarID uint8) (ranking.LocalStatus, error) {
	s.joinName = name
	s.joinAvatarID = avatarID
	if s.joinErr != nil {
		return ranking.LocalStatus{}, s.joinErr
	}
	return s.status, nil
}

func (s *rankingProviderStub) SyncNow(context.Context) error {
	s.runCalls++
	return s.syncErr
}

func (s *rankingProviderStub) Pause(context.Context) (ranking.LocalStatus, error) {
	s.pauseCalls++
	return s.status, nil
}

func (s *rankingProviderStub) Resume(context.Context) (ranking.LocalStatus, error) {
	s.resumeCalls++
	return s.status, nil
}

func (s *rankingProviderStub) Exit(context.Context) (ranking.LocalStatus, error) {
	s.exitCalls++
	return s.status, nil
}

func (s *rankingProviderStub) Leaderboard(_ context.Context, period ranking.LeaderboardPeriod, metric ranking.LeaderboardMetric) (ranking.Leaderboard, error) {
	s.boardPeriod = period
	s.boardMetric = metric
	return ranking.Leaderboard{
		Period: period, PeriodKey: "2026-07-24", Metric: metric,
		GeneratedAt: time.Date(2026, 7, 24, 4, 0, 0, 0, time.UTC), Entries: []ranking.LeaderboardEntry{},
		ScoreExplanation: &ranking.ScoreExplanation{
			Version: 2,
			Texts:   map[string]string{"en": "Overall score V2", "zh": "综合分 V2", "zh-TW": "綜合分 V2"},
		},
	}, nil
}

func (s *rankingProviderStub) LeaderboardMetadata(context.Context) (ranking.LeaderboardMetadata, error) {
	if s.metadataErr != nil {
		return ranking.LeaderboardMetadata{}, s.metadataErr
	}
	return ranking.LeaderboardMetadata{ProtocolVersion: 1, MetricsVersion: 1, PeriodTimezone: "Asia/Shanghai", Periods: []ranking.LeaderboardPeriodMetadata{}, Metrics: []ranking.LeaderboardMetric{}}, nil
}

func TestRankingStatusNeverExposesSigningIdentity(t *testing.T) {
	provider := &rankingProviderStub{status: ranking.LocalStatus{
		Status: ranking.StatusActive, DisplayName: "Keeper_01", AvatarID: 7, ParticipantID: "p_example",
	}}
	router := rankingRouter(provider)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ranking/status", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"status":"active"`) || !strings.Contains(body, `"participant_id":"p_example"`) {
		t.Fatalf("unexpected status body: %s", body)
	}
	for _, secretField := range []string{"public_key", "private_key", "registration_idempotency_key"} {
		if strings.Contains(body, secretField) {
			t.Fatalf("status exposed %s: %s", secretField, body)
		}
	}
	if strings.Contains(body, "last_allocated_sequence") {
		t.Fatalf("status exposed internal signing sequence: %s", body)
	}
}

func TestRankingJoinAndManualSyncUseLocalService(t *testing.T) {
	provider := &rankingProviderStub{status: ranking.LocalStatus{Status: ranking.StatusActive, DisplayName: "Keeper_01", AvatarID: 7, ParticipantID: "p_example"}}
	router := rankingRouter(provider)

	join := httptest.NewRecorder()
	joinRequest := httptest.NewRequest(http.MethodPost, "/api/v1/ranking/join", strings.NewReader(`{"display_name":"Keeper_01","avatar_id":7}`))
	joinRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(join, joinRequest)
	if join.Code != http.StatusOK || provider.joinName != "Keeper_01" || provider.joinAvatarID != 7 {
		t.Fatalf("unexpected join result: status=%d body=%s provider=%+v", join.Code, join.Body.String(), provider)
	}

	syncResponse := httptest.NewRecorder()
	router.ServeHTTP(syncResponse, httptest.NewRequest(http.MethodPost, "/api/v1/ranking/sync", nil))
	if syncResponse.Code != http.StatusOK || provider.runCalls != 1 {
		t.Fatalf("unexpected sync result: status=%d body=%s calls=%d", syncResponse.Code, syncResponse.Body.String(), provider.runCalls)
	}
}

func TestRankingManualSyncRejectsInactiveParticipation(t *testing.T) {
	provider := &rankingProviderStub{status: ranking.LocalStatus{
		Status: ranking.StatusPaused, DisplayName: "Keeper_01", AvatarID: 7, ParticipantID: "p_example",
	}, syncErr: ranking.ErrParticipation}
	response := httptest.NewRecorder()
	rankingRouter(provider).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/ranking/sync", nil))

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"ranking_participation_state_conflict"`) {
		t.Fatalf("inactive sync result: status=%d body=%s calls=%d", response.Code, response.Body.String(), provider.runCalls)
	}
}

func TestRankingPauseAndResumeUseOnlyLocalService(t *testing.T) {
	provider := &rankingProviderStub{status: ranking.LocalStatus{Status: ranking.StatusPaused, DisplayName: "Keeper_01", AvatarID: 7, ParticipantID: "p_example"}}
	router := rankingRouter(provider)

	pause := httptest.NewRecorder()
	router.ServeHTTP(pause, httptest.NewRequest(http.MethodPost, "/api/v1/ranking/pause", nil))
	if pause.Code != http.StatusOK || provider.pauseCalls != 1 {
		t.Fatalf("pause result: status=%d body=%s calls=%d", pause.Code, pause.Body.String(), provider.pauseCalls)
	}
	resume := httptest.NewRecorder()
	router.ServeHTTP(resume, httptest.NewRequest(http.MethodPost, "/api/v1/ranking/resume", nil))
	if resume.Code != http.StatusOK || provider.resumeCalls != 1 {
		t.Fatalf("resume result: status=%d body=%s calls=%d", resume.Code, resume.Body.String(), provider.resumeCalls)
	}
}

func TestRankingJoinForwardsCenterRetryAfter(t *testing.T) {
	provider := &rankingProviderStub{joinErr: &ranking.CenterError{
		StatusCode: http.StatusTooManyRequests,
		Code:       "registration_rate_limited",
		RetryAfter: "3599",
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ranking/join", strings.NewReader(`{"display_name":"Keeper_01","avatar_id":7}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	rankingRouter(provider).ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"error":"ranking_center_registration_rate_limited"`) {
		t.Fatalf("unexpected rate-limit response: status=%d body=%s", response.Code, response.Body.String())
	}
	if retryAfter := response.Header().Get("Retry-After"); retryAfter != "3599" {
		t.Fatalf("Retry-After = %q, want 3599", retryAfter)
	}
}

func TestRankingLeaderboardValidatesAndForwardsSelection(t *testing.T) {
	provider := &rankingProviderStub{}
	router := rankingRouter(provider)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ranking/leaderboards?period=today&metric=overall", nil))
	if response.Code != http.StatusOK || provider.boardPeriod != ranking.LeaderboardToday || provider.boardMetric != ranking.MetricOverall {
		t.Fatalf("unexpected leaderboard result: status=%d body=%s provider=%+v", response.Code, response.Body.String(), provider)
	}
	if !strings.Contains(response.Body.String(), `"score_explanation":{"version":2`) || !strings.Contains(response.Body.String(), `"zh":"综合分 V2"`) {
		t.Fatalf("leaderboard response dropped score explanation: %s", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" || response.Header().Get("Expires") != "0" {
		t.Fatalf("leaderboard response allowed browser caching: %+v", response.Header())
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v1/ranking/leaderboards?period=all&metric=overall", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid selection status 400, got %d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestRankingMetadataReportsIncompatibleCenter(t *testing.T) {
	provider := &rankingProviderStub{metadataErr: ranking.ErrIncompatibleCenter}
	response := httptest.NewRecorder()
	rankingRouter(provider).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ranking/leaderboards/metadata", nil))

	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"error":"ranking_center_incompatible"`) {
		t.Fatalf("unexpected incompatible metadata response: status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" || response.Header().Get("Expires") != "0" {
		t.Fatalf("metadata response allowed browser caching: %+v", response.Header())
	}
}

func rankingRouter(provider httpapi.Provider) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	httpapi.RegisterRoutes(router.Group("/api/v1"), provider)
	return router
}
