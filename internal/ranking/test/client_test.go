package test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cpa-usage-keeper/internal/ranking"
)

func TestOfficialCenterURLUsesKeeperDomain(t *testing.T) {
	if ranking.OfficialCenterURL != "https://keepers.cc.cd/" {
		t.Fatalf("unexpected official ranking center URL: %s", ranking.OfficialCenterURL)
	}
}

func TestCenterClientRegistersWithCompatibleSignature(t *testing.T) {
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	now := time.Date(2026, 7, 24, 3, 4, 5, 6, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/participants" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.String())
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		assertSignedRequest(t, request, body, identity.PublicKey, ranking.SigningInput{
			Method:         http.MethodPost,
			Path:           "/api/v1/participants",
			Subject:        "public-key:" + identity.PublicKey,
			IdempotencyKey: "registration_once",
			Timestamp:      now,
		})
		if string(body) != `{"display_name":"Keeper_01","avatar_id":7}` {
			t.Fatalf("unexpected registration body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"participant_id":"p_example","display_name":"Keeper_01","avatar_id":7,"created_at":"2026-07-24T03:04:05Z","revoked":false,"deleted":false}`)
	}))
	defer server.Close()

	client, err := ranking.NewClientWithBaseURL(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClientWithBaseURL returned error: %v", err)
	}
	participant, err := client.Register(context.Background(), ranking.RegistrationCommand{
		Credentials:    ranking.Credentials{PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey},
		IdempotencyKey: "registration_once",
		DisplayName:    "Keeper_01",
		AvatarID:       7,
		RequestedAt:    now,
	})
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if participant.ParticipantID != "p_example" || participant.DisplayName != "Keeper_01" || participant.AvatarID != 7 {
		t.Fatalf("unexpected participant: %+v", participant)
	}
}

func TestCenterClientReportsAllMetricsAndMapsDeletedTombstone(t *testing.T) {
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	now := time.Date(2026, 7, 24, 4, 5, 6, 7, time.UTC)
	sequence := int64(9)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		assertSignedRequest(t, request, body, identity.PublicKey, ranking.SigningInput{
			Method:         http.MethodPost,
			Path:           "/api/v1/reports",
			Subject:        "participant:p_example",
			Sequence:       &sequence,
			IdempotencyKey: "report_once",
			Timestamp:      now,
		})
		var root map[string]json.RawMessage
		if err := json.Unmarshal(body, &root); err != nil {
			t.Fatalf("decode report body: %v", err)
		}
		if len(root) != 6 {
			t.Fatalf("expected six report fields, got %s", body)
		}
		var periodTimezone string
		if err := json.Unmarshal(root["period_timezone"], &periodTimezone); err != nil || periodTimezone != "Asia/Shanghai" {
			t.Fatalf("unexpected report period timezone: value=%q err=%v", periodTimezone, err)
		}
		var metrics map[string]json.RawMessage
		if err := json.Unmarshal(root["metrics"], &metrics); err != nil || len(metrics) != 15 {
			t.Fatalf("expected fifteen report metrics, got %s err=%v", root["metrics"], err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = io.WriteString(w, `{"error":"participant_deleted"}`)
	}))
	defer server.Close()

	client, err := ranking.NewClientWithBaseURL(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClientWithBaseURL returned error: %v", err)
	}
	_, err = client.SubmitReport(context.Background(), ranking.ReportCommand{
		Credentials: ranking.Credentials{
			PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey, ParticipantID: "p_example",
		},
		Sequence:       sequence,
		IdempotencyKey: "report_once",
		SnapshotAt:     now,
		PeriodTimezone: "Asia/Shanghai",
		DayKey:         "2026-07-24",
		Metrics: ranking.Metrics{
			RequestCount: 1, SuccessCount: 1, InputTokens: 2, OutputTokens: 3, TotalTokens: 5,
		},
	})
	if !errors.Is(err, ranking.ErrParticipantDeleted) {
		t.Fatalf("expected participant deleted error, got %v", err)
	}
}

func TestCenterClientDoesNotMapServerFailureToDeletedTombstone(t *testing.T) {
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	now := time.Date(2026, 7, 24, 4, 5, 6, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"participant_deleted"}`)
	}))
	defer server.Close()

	client, err := ranking.NewClientWithBaseURL(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClientWithBaseURL returned error: %v", err)
	}
	_, err = client.SubmitReport(context.Background(), ranking.ReportCommand{
		Credentials: ranking.Credentials{
			PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey, ParticipantID: "p_example",
		},
		Sequence:       1,
		IdempotencyKey: "report_once",
		SnapshotAt:     now,
		PeriodTimezone: "Asia/Shanghai",
		DayKey:         "2026-07-24",
		Metrics:        ranking.Metrics{RequestCount: 1, SuccessCount: 1},
	})
	if errors.Is(err, ranking.ErrParticipantDeleted) {
		t.Fatalf("server failure was mapped to a permanent deleted tombstone: %v", err)
	}
	var centerErr *ranking.CenterError
	if !errors.As(err, &centerErr) || centerErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected original center failure, got %v", err)
	}
}

func TestCenterClientReadsLeaderboardWithKeeperMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/leaderboards" || request.URL.RawQuery != "metric=overall&period=today" {
			t.Fatalf("unexpected leaderboard request: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("X-Keeper-Read-Marker") != "keeper-ranking-center-read/v1" {
			t.Fatalf("missing Keeper read marker: %v", request.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"period":"today","period_key":"2026-07-24","metric":"overall","generated_at":"2026-07-24T04:05:06Z","stale":false,"entries":[],"score_explanation":{"version":2,"texts":{"en":"Overall score V2","zh":"综合分 V2","zh-TW":"綜合分 V2"}}}`)
	}))
	defer server.Close()

	client, err := ranking.NewClientWithBaseURL(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClientWithBaseURL returned error: %v", err)
	}
	board, err := client.Leaderboard(context.Background(), ranking.LeaderboardToday, ranking.MetricOverall)
	if err != nil {
		t.Fatalf("Leaderboard returned error: %v", err)
	}
	if board.Period != ranking.LeaderboardToday || board.Metric != ranking.MetricOverall || board.Entries == nil {
		t.Fatalf("unexpected leaderboard: %+v", board)
	}
	if board.ScoreExplanation == nil || board.ScoreExplanation.Version != 2 || board.ScoreExplanation.Texts["zh"] != "综合分 V2" {
		t.Fatalf("score explanation was not preserved: %+v", board.ScoreExplanation)
	}
}

func TestCenterClientValidatesFixedMetadataContract(t *testing.T) {
	valid := ranking.LeaderboardMetadata{
		ProtocolVersion: 1, MetricsVersion: 1, PeriodTimezone: "Asia/Shanghai", AvatarCatalogVersion: 1, AvatarCount: 66, ReadMarkerVersion: 1,
		RefreshIntervalSeconds: 60, SuggestedSyncIntervalSeconds: 1800,
		Periods: []ranking.LeaderboardPeriodMetadata{
			{Period: ranking.LeaderboardToday, PeriodKey: "2026-07-24", Online: true},
			{Period: ranking.LeaderboardYesterday, PeriodKey: "2026-07-23", Online: true},
			{Period: ranking.LeaderboardCurrentMonth, PeriodKey: "2026-07", Online: true},
			{Period: ranking.LeaderboardPreviousMonth, PeriodKey: "2026-06", Online: true},
		},
		Metrics: []ranking.LeaderboardMetric{
			ranking.MetricTotalTokens, ranking.MetricRequestCount, ranking.MetricCacheReadRate, ranking.MetricTTFTAverage,
			ranking.MetricLatencyAverage, ranking.MetricPeakTPM, ranking.MetricPeakRPM, ranking.MetricOverall,
		},
	}
	tests := []struct {
		name    string
		mutate  func(*ranking.LeaderboardMetadata)
		wantErr bool
	}{
		{name: "valid"},
		{name: "unsupported protocol", mutate: func(metadata *ranking.LeaderboardMetadata) { metadata.ProtocolVersion = 2 }, wantErr: true},
		{name: "missing period timezone", mutate: func(metadata *ranking.LeaderboardMetadata) { metadata.PeriodTimezone = "" }, wantErr: true},
		{name: "invalid period timezone", mutate: func(metadata *ranking.LeaderboardMetadata) { metadata.PeriodTimezone = "Mars/Olympus" }, wantErr: true},
		{name: "center changes leaderboard refresh interval", mutate: func(metadata *ranking.LeaderboardMetadata) { metadata.RefreshIntervalSeconds = 30 }},
		{name: "center changes suggested sync interval", mutate: func(metadata *ranking.LeaderboardMetadata) { metadata.SuggestedSyncIntervalSeconds = 900 }},
		{name: "consumer clamps interval below its safety floor", mutate: func(metadata *ranking.LeaderboardMetadata) {
			metadata.RefreshIntervalSeconds = 1
			metadata.SuggestedSyncIntervalSeconds = 1
		}},
		{name: "missing metric", mutate: func(metadata *ranking.LeaderboardMetadata) { metadata.Metrics = metadata.Metrics[:7] }, wantErr: true},
		{name: "duplicate period", mutate: func(metadata *ranking.LeaderboardMetadata) { metadata.Periods[3] = metadata.Periods[0] }, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadata := valid
			metadata.Periods = append([]ranking.LeaderboardPeriodMetadata(nil), valid.Periods...)
			metadata.Metrics = append([]ranking.LeaderboardMetric(nil), valid.Metrics...)
			if test.mutate != nil {
				test.mutate(&metadata)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Header.Get("X-Keeper-Read-Marker") != "keeper-ranking-center-read/v1" {
					t.Fatalf("missing Keeper read marker: %v", request.Header)
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(metadata); err != nil {
					t.Fatalf("encode metadata: %v", err)
				}
			}))
			defer server.Close()
			client, err := ranking.NewClientWithBaseURL(server.URL, server.Client())
			if err != nil {
				t.Fatalf("NewClientWithBaseURL returned error: %v", err)
			}
			_, err = client.LeaderboardMetadata(context.Background())
			if test.wantErr && !errors.Is(err, ranking.ErrIncompatibleCenter) {
				t.Fatalf("expected incompatible center error, got %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("valid metadata returned error: %v", err)
			}
		})
	}
}

func TestCenterClientDoesNotForwardSignedRequestAcrossRedirect(t *testing.T) {
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		forwarded = true
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	client, err := ranking.NewClientWithBaseURL(redirect.URL, redirect.Client())
	if err != nil {
		t.Fatalf("NewClientWithBaseURL returned error: %v", err)
	}
	_, err = client.Register(context.Background(), ranking.RegistrationCommand{
		Credentials:    ranking.Credentials{PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey},
		IdempotencyKey: "registration_once", DisplayName: "Keeper_01", AvatarID: 7,
		RequestedAt: time.Date(2026, 7, 24, 3, 4, 5, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected redirect response to be rejected")
	}
	if forwarded {
		t.Fatal("signed ranking request was forwarded to redirect target")
	}
}

func TestCenterClientRejectsMismatchedReportReceipt(t *testing.T) {
	identity, err := ranking.GenerateIdentity(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateIdentity returned error: %v", err)
	}
	now := time.Date(2026, 7, 24, 4, 5, 6, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"participant_id":"p_other","sequence":99,"snapshot_at":"2026-07-24T04:05:06Z","synced_at":"2026-07-24T04:05:06Z"}`)
	}))
	defer server.Close()

	client, err := ranking.NewClientWithBaseURL(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClientWithBaseURL returned error: %v", err)
	}
	_, err = client.SubmitReport(context.Background(), ranking.ReportCommand{
		Credentials: ranking.Credentials{PublicKey: identity.PublicKey, PrivateKey: identity.PrivateKey, ParticipantID: "p_example"},
		Sequence:    3, IdempotencyKey: "report_once", SnapshotAt: now, PeriodTimezone: "Asia/Shanghai", DayKey: "2026-07-24",
	})
	if err == nil {
		t.Fatal("expected mismatched report receipt to be rejected")
	}
}

func assertSignedRequest(t *testing.T, request *http.Request, body []byte, encodedPublicKey string, input ranking.SigningInput) {
	t.Helper()
	if request.Header.Get("X-Keeper-Protocol-Version") != "1" || request.Header.Get("X-Keeper-Timestamp") != input.Timestamp.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected protocol headers: %v", request.Header)
	}
	if input.Subject[:11] == "public-key:" {
		if request.Header.Get("X-Keeper-Public-Key") != encodedPublicKey {
			t.Fatalf("unexpected public key header: %v", request.Header)
		}
	} else if request.Header.Get("X-Keeper-Participant-ID") != "p_example" {
		t.Fatalf("unexpected participant header: %v", request.Header)
	}
	if input.Sequence != nil && request.Header.Get("X-Keeper-Sequence") != "9" {
		t.Fatalf("unexpected sequence header: %v", request.Header)
	}
	if input.IdempotencyKey != "" && request.Header.Get("X-Keeper-Idempotency-Key") != input.IdempotencyKey {
		t.Fatalf("unexpected idempotency header: %v", request.Header)
	}
	payload, err := ranking.CanonicalSigningPayload(input, body)
	if err != nil {
		t.Fatalf("CanonicalSigningPayload returned error: %v", err)
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(encodedPublicKey)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(request.Header.Get("X-Keeper-Signature"))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		t.Fatalf("invalid request signature: %v", err)
	}
}
