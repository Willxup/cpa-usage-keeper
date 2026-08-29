package ranking

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	OfficialCenterURL = "https://keepers.cc.cd/"
	clientTimeout     = 10 * time.Second
	maxResponseBytes  = 128 * 1024
	readMarkerValue   = "keeper-ranking-center-read/v1"
)

var (
	ErrParticipantDeleted = errors.New("ranking participant deleted")
	ErrPeriodClosed       = errors.New("ranking period closed")
	ErrProfileConflict    = errors.New("ranking profile conflict")
	ErrReplayedSequence   = errors.New("ranking sequence replayed")
	ErrIncompatibleCenter = errors.New("ranking center is incompatible")
)

type Credentials struct {
	PublicKey     string
	PrivateKey    string
	ParticipantID string
}

type RegistrationCommand struct {
	Credentials
	IdempotencyKey string
	DisplayName    string
	AvatarID       uint8
	RequestedAt    time.Time
}

type ParticipantInfo struct {
	ParticipantID string    `json:"participant_id"`
	DisplayName   string    `json:"display_name"`
	AvatarID      uint8     `json:"avatar_id"`
	CreatedAt     time.Time `json:"created_at"`
	Revoked       bool      `json:"revoked"`
	Deleted       bool      `json:"deleted"`
}

type SelfStatus struct {
	ParticipantID  string     `json:"participant_id"`
	DisplayName    string     `json:"display_name"`
	AvatarID       uint8      `json:"avatar_id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	RestoredAt     *time.Time `json:"restored_at,omitempty"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	LastSnapshotAt *time.Time `json:"last_snapshot_at,omitempty"`
	LastSyncedAt   *time.Time `json:"last_synced_at,omitempty"`
	LastSequence   int64      `json:"last_sequence"`
}

type ReportCommand struct {
	Credentials
	Sequence       int64
	IdempotencyKey string
	SnapshotAt     time.Time
	PeriodTimezone string
	DayKey         string
	Complete       bool
	Metrics        Metrics
}

type ReportReceipt struct {
	ParticipantID string    `json:"participant_id"`
	Sequence      int64     `json:"sequence"`
	SnapshotAt    time.Time `json:"snapshot_at"`
	SyncedAt      time.Time `json:"synced_at"`
}

type DeleteCommand struct {
	Credentials
	Sequence       int64
	IdempotencyKey string
	RequestedAt    time.Time
}

type DeleteReceipt struct {
	ParticipantID string    `json:"participant_id"`
	Sequence      int64     `json:"sequence"`
	DeletedAt     time.Time `json:"deleted_at"`
}

type LeaderboardPeriod string

const (
	LeaderboardToday         LeaderboardPeriod = "today"
	LeaderboardYesterday     LeaderboardPeriod = "yesterday"
	LeaderboardCurrentMonth  LeaderboardPeriod = "current_month"
	LeaderboardPreviousMonth LeaderboardPeriod = "previous_month"
)

type LeaderboardMetric string

const (
	MetricTotalTokens    LeaderboardMetric = "total_tokens"
	MetricRequestCount   LeaderboardMetric = "request_count"
	MetricCacheReadRate  LeaderboardMetric = "cache_read_rate"
	MetricTTFTAverage    LeaderboardMetric = "ttft_average"
	MetricLatencyAverage LeaderboardMetric = "latency_average"
	MetricPeakTPM        LeaderboardMetric = "peak_tpm"
	MetricPeakRPM        LeaderboardMetric = "peak_rpm"
	MetricOverall        LeaderboardMetric = "overall"
)

type LeaderboardEntry struct {
	Rank            uint16                      `json:"rank"`
	ParticipantID   string                      `json:"participant_id"`
	DisplayName     string                      `json:"display_name"`
	KeyAlias        string                      `json:"key_alias,omitempty"`
	AvatarID        uint8                       `json:"avatar_id"`
	Value           int64                       `json:"value"`
	RateNumerator   int64                       `json:"rate_numerator,omitempty"`
	RateDenominator int64                       `json:"rate_denominator,omitempty"`
	Metrics         map[LeaderboardMetric]int64 `json:"metrics,omitempty"`
}

type ScoreExplanation struct {
	Version int               `json:"version"`
	Texts   map[string]string `json:"texts"`
}

type Leaderboard struct {
	Period           LeaderboardPeriod  `json:"period"`
	PeriodKey        string             `json:"period_key"`
	Metric           LeaderboardMetric  `json:"metric"`
	GeneratedAt      time.Time          `json:"generated_at"`
	Stale            bool               `json:"stale"`
	Entries          []LeaderboardEntry `json:"entries"`
	ScoreExplanation *ScoreExplanation  `json:"score_explanation,omitempty"`
}

type LeaderboardPeriodMetadata struct {
	Period    LeaderboardPeriod `json:"period"`
	PeriodKey string            `json:"period_key"`
	Online    bool              `json:"online"`
}

type LeaderboardMetadata struct {
	ServerTime                   time.Time                   `json:"server_time"`
	GeneratedAt                  time.Time                   `json:"generated_at"`
	Stale                        bool                        `json:"stale"`
	ProtocolVersion              int                         `json:"protocol_version"`
	MetricsVersion               int                         `json:"metrics_version"`
	PeriodTimezone               string                      `json:"period_timezone"`
	AvatarCatalogVersion         int                         `json:"avatar_catalog_version"`
	AvatarCount                  int                         `json:"avatar_count"`
	ReadMarkerVersion            int                         `json:"read_marker_version"`
	RefreshIntervalSeconds       int64                       `json:"refresh_interval_seconds"`
	SuggestedSyncIntervalSeconds int64                       `json:"suggested_sync_interval_seconds"`
	Periods                      []LeaderboardPeriodMetadata `json:"periods"`
	Metrics                      []LeaderboardMetric         `json:"metrics"`
	OverallWeights               map[string]int64            `json:"overall_weights"`
}

type CenterError struct {
	StatusCode int
	Code       string
	RetryAfter string
}

func (e *CenterError) Error() string {
	if e == nil {
		return "ranking center request failed"
	}
	return fmt.Sprintf("ranking center request failed: status=%d code=%s", e.StatusCode, e.Code)
}

func (e *CenterError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrParticipantDeleted:
		return e.StatusCode == http.StatusGone && e.Code == "participant_deleted"
	case ErrPeriodClosed:
		return e.StatusCode == http.StatusConflict && e.Code == "period_closed"
	case ErrProfileConflict:
		return e.StatusCode == http.StatusConflict && e.Code == "profile_conflict"
	case ErrReplayedSequence:
		return e.StatusCode == http.StatusConflict && e.Code == "replayed_sequence"
	default:
		return false
	}
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient() *Client {
	client, _ := NewClientWithBaseURL(OfficialCenterURL, &http.Client{Timeout: clientTimeout})
	return client
}

// NewClientWithBaseURL 仅用于测试注入本地 ranking-center；生产装配必须调用 NewClient。
func NewClientWithBaseURL(baseURL string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("ranking center base URL is invalid")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("ranking center base URL cannot contain a path")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: clientTimeout}
	}
	// 签名只绑定路径而不绑定目标主机，禁止自动重定向，避免自定义 X-Keeper 签名头被转发。
	isolatedHTTPClient := *httpClient
	isolatedHTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{baseURL: parsed.Scheme + "://" + parsed.Host, httpClient: &isolatedHTTPClient}, nil
}

func (c *Client) Register(ctx context.Context, command RegistrationCommand) (ParticipantInfo, error) {
	body, err := json.Marshal(struct {
		DisplayName string `json:"display_name"`
		AvatarID    uint8  `json:"avatar_id"`
	}{DisplayName: command.DisplayName, AvatarID: command.AvatarID})
	if err != nil {
		return ParticipantInfo{}, fmt.Errorf("encode ranking registration: %w", err)
	}
	request, err := c.newSignedRequest(ctx, http.MethodPost, "/api/v1/participants", body, command.Credentials, nil, command.IdempotencyKey, command.RequestedAt, true)
	if err != nil {
		return ParticipantInfo{}, err
	}
	var response ParticipantInfo
	if err := c.doJSON(request, &response); err != nil {
		return ParticipantInfo{}, err
	}
	return response, nil
}

func (c *Client) Self(ctx context.Context, credentials Credentials, requestedAt time.Time) (SelfStatus, error) {
	request, err := c.newSignedRequest(ctx, http.MethodGet, "/api/v1/participants/self", nil, credentials, nil, "", requestedAt, false)
	if err != nil {
		return SelfStatus{}, err
	}
	var response SelfStatus
	if err := c.doJSON(request, &response); err != nil {
		return SelfStatus{}, err
	}
	return response, nil
}

func (c *Client) SubmitReport(ctx context.Context, command ReportCommand) (ReportReceipt, error) {
	body, err := json.Marshal(struct {
		MetricsVersion int       `json:"metrics_version"`
		PeriodTimezone string    `json:"period_timezone"`
		SnapshotAt     time.Time `json:"snapshot_at"`
		DayKey         string    `json:"day_key"`
		Complete       bool      `json:"complete"`
		Metrics        Metrics   `json:"metrics"`
	}{MetricsVersion: 1, PeriodTimezone: command.PeriodTimezone, SnapshotAt: command.SnapshotAt, DayKey: command.DayKey, Complete: command.Complete, Metrics: command.Metrics})
	if err != nil {
		return ReportReceipt{}, fmt.Errorf("encode ranking report: %w", err)
	}
	request, err := c.newSignedRequest(ctx, http.MethodPost, "/api/v1/reports", body, command.Credentials, &command.Sequence, command.IdempotencyKey, command.SnapshotAt, false)
	if err != nil {
		return ReportReceipt{}, err
	}
	var response ReportReceipt
	if err := c.doJSON(request, &response); err != nil {
		return ReportReceipt{}, err
	}
	if response.ParticipantID != command.ParticipantID || response.Sequence != command.Sequence || !response.SnapshotAt.Equal(command.SnapshotAt) || response.SyncedAt.IsZero() {
		return ReportReceipt{}, fmt.Errorf("ranking center returned an inconsistent report receipt")
	}
	return response, nil
}

func (c *Client) Delete(ctx context.Context, command DeleteCommand) (DeleteReceipt, error) {
	request, err := c.newSignedRequest(ctx, http.MethodDelete, "/api/v1/participants/self", nil, command.Credentials, &command.Sequence, command.IdempotencyKey, command.RequestedAt, false)
	if err != nil {
		return DeleteReceipt{}, err
	}
	var response DeleteReceipt
	if err := c.doJSON(request, &response); err != nil {
		return DeleteReceipt{}, err
	}
	if response.ParticipantID != command.ParticipantID || response.Sequence != command.Sequence || response.DeletedAt.IsZero() {
		return DeleteReceipt{}, fmt.Errorf("ranking center returned an inconsistent delete receipt")
	}
	return response, nil
}

func (c *Client) Leaderboard(ctx context.Context, period LeaderboardPeriod, metric LeaderboardMetric) (Leaderboard, error) {
	if !validLeaderboardPeriod(period) || !validLeaderboardMetric(metric) {
		return Leaderboard{}, fmt.Errorf("invalid leaderboard selection")
	}
	query := url.Values{}
	query.Set("period", string(period))
	query.Set("metric", string(metric))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/leaderboards?"+query.Encode(), nil)
	if err != nil {
		return Leaderboard{}, fmt.Errorf("create leaderboard request: %w", err)
	}
	request.Header.Set("X-Keeper-Read-Marker", readMarkerValue)
	var response Leaderboard
	if err := c.doJSON(request, &response); err != nil {
		return Leaderboard{}, err
	}
	return response, nil
}

func (c *Client) LeaderboardMetadata(ctx context.Context) (LeaderboardMetadata, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/leaderboards/metadata", nil)
	if err != nil {
		return LeaderboardMetadata{}, fmt.Errorf("create leaderboard metadata request: %w", err)
	}
	request.Header.Set("X-Keeper-Read-Marker", readMarkerValue)
	var response LeaderboardMetadata
	if err := c.doJSON(request, &response); err != nil {
		return LeaderboardMetadata{}, err
	}
	if err := validateLeaderboardMetadata(response); err != nil {
		return LeaderboardMetadata{}, err
	}
	return response, nil
}

func validateLeaderboardMetadata(metadata LeaderboardMetadata) error {
	if metadata.ProtocolVersion != 1 || metadata.MetricsVersion != 1 || metadata.AvatarCatalogVersion != 1 || metadata.AvatarCount != 66 || metadata.ReadMarkerVersion != 1 {
		return fmt.Errorf("%w: fixed metadata fields do not match", ErrIncompatibleCenter)
	}
	if metadata.PeriodTimezone == "" {
		return fmt.Errorf("%w: period timezone is missing", ErrIncompatibleCenter)
	}
	if _, err := time.LoadLocation(metadata.PeriodTimezone); err != nil {
		return fmt.Errorf("%w: period timezone is invalid", ErrIncompatibleCenter)
	}
	expectedPeriods := map[LeaderboardPeriod]struct{}{
		LeaderboardToday: {}, LeaderboardYesterday: {}, LeaderboardCurrentMonth: {}, LeaderboardPreviousMonth: {},
	}
	if len(metadata.Periods) != len(expectedPeriods) {
		return fmt.Errorf("%w: leaderboard period set does not match", ErrIncompatibleCenter)
	}
	seenPeriods := make(map[LeaderboardPeriod]struct{}, len(metadata.Periods))
	for _, period := range metadata.Periods {
		if _, ok := expectedPeriods[period.Period]; !ok || strings.TrimSpace(period.PeriodKey) == "" {
			return fmt.Errorf("%w: leaderboard period set does not match", ErrIncompatibleCenter)
		}
		if _, duplicate := seenPeriods[period.Period]; duplicate {
			return fmt.Errorf("%w: leaderboard period set contains duplicates", ErrIncompatibleCenter)
		}
		seenPeriods[period.Period] = struct{}{}
	}
	expectedMetrics := map[LeaderboardMetric]struct{}{
		MetricTotalTokens: {}, MetricRequestCount: {}, MetricCacheReadRate: {}, MetricTTFTAverage: {},
		MetricLatencyAverage: {}, MetricPeakTPM: {}, MetricPeakRPM: {}, MetricOverall: {},
	}
	if len(metadata.Metrics) != len(expectedMetrics) {
		return fmt.Errorf("%w: leaderboard metric set does not match", ErrIncompatibleCenter)
	}
	seenMetrics := make(map[LeaderboardMetric]struct{}, len(metadata.Metrics))
	for _, metric := range metadata.Metrics {
		if _, ok := expectedMetrics[metric]; !ok {
			return fmt.Errorf("%w: leaderboard metric set does not match", ErrIncompatibleCenter)
		}
		if _, duplicate := seenMetrics[metric]; duplicate {
			return fmt.Errorf("%w: leaderboard metric set contains duplicates", ErrIncompatibleCenter)
		}
		seenMetrics[metric] = struct{}{}
	}
	return nil
}

func (c *Client) newSignedRequest(ctx context.Context, method, path string, body []byte, credentials Credentials, sequence *int64, idempotencyKey string, timestamp time.Time, registration bool) (*http.Request, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("ranking center client is not configured")
	}
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create signed ranking request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-Keeper-Protocol-Version", "1")
	subject := "participant:" + credentials.ParticipantID
	if registration {
		subject = "public-key:" + credentials.PublicKey
		request.Header.Set("X-Keeper-Public-Key", credentials.PublicKey)
	} else {
		if credentials.ParticipantID == "" {
			return nil, fmt.Errorf("ranking participant_id is required")
		}
		request.Header.Set("X-Keeper-Participant-ID", credentials.ParticipantID)
	}
	if sequence != nil {
		request.Header.Set("X-Keeper-Sequence", strconv.FormatInt(*sequence, 10))
	}
	if idempotencyKey != "" {
		request.Header.Set("X-Keeper-Idempotency-Key", idempotencyKey)
	}
	timestamp = timestamp.UTC()
	request.Header.Set("X-Keeper-Timestamp", timestamp.Format(time.RFC3339Nano))
	payload, err := CanonicalSigningPayload(SigningInput{
		Method: method, Path: path, Subject: subject, Sequence: sequence,
		IdempotencyKey: idempotencyKey, Timestamp: timestamp,
	}, body)
	if err != nil {
		return nil, fmt.Errorf("build ranking request signature: %w", err)
	}
	signature, err := (Identity{PublicKey: credentials.PublicKey, PrivateKey: credentials.PrivateKey}).Sign(payload)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Keeper-Signature", base64.RawURLEncoding.EncodeToString(signature))
	return request, nil
}

func (c *Client) doJSON(request *http.Request, target any) error {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request ranking center: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read ranking center response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("ranking center response exceeds %d bytes", maxResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &payload)
		return &CenterError{StatusCode: response.StatusCode, Code: payload.Error, RetryAfter: response.Header.Get("Retry-After")}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode ranking center response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode ranking center response: trailing JSON value")
	}
	return nil
}

func validLeaderboardPeriod(period LeaderboardPeriod) bool {
	switch period {
	case LeaderboardToday, LeaderboardYesterday, LeaderboardCurrentMonth, LeaderboardPreviousMonth:
		return true
	default:
		return false
	}
}

func validLeaderboardMetric(metric LeaderboardMetric) bool {
	switch metric {
	case MetricTotalTokens, MetricRequestCount, MetricCacheReadRate, MetricTTFTAverage, MetricLatencyAverage, MetricPeakTPM, MetricPeakRPM, MetricOverall:
		return true
	default:
		return false
	}
}
