package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	_ "unsafe"

	. "cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"gorm.io/gorm"
)

type usageEventsStub struct {
	events             []servicedto.UsageEventRecord
	eventsPage         *servicedto.UsageEventsPage
	exportEvents       []servicedto.UsageEventRecord
	eventFilterOptions *servicedto.UsageEventFilterOptions
	err                error
	lastFilter         servicedto.UsageFilter
	overviewCalls      int
	filterCalls        int
	filterOptionCalls  int
	exportCalls        int
}

type blockingUsageEventsExportStub struct {
	*usageEventsStub
	started chan struct{}
	release <-chan struct{}
	calls   atomic.Int32
}

type requestLogProviderStub struct {
	response         service.RequestLogResponse
	err              error
	eventID          int64
	calls            int
	downloadResponse service.RequestLogDownload
	downloadErr      error
	downloadEventID  int64
	downloadCalls    int
	downloadClosed   *bool
}

type requestLogDownloadTokenPayload struct {
	DownloadURL string `json:"download_url"`
}

func (s *requestLogProviderStub) GetUsageEventRequestLog(_ context.Context, eventID int64) (service.RequestLogResponse, error) {
	s.eventID = eventID
	s.calls++
	return s.response, s.err
}

func (s *requestLogProviderStub) DownloadUsageEventRequestLog(_ context.Context, eventID int64) (service.RequestLogDownload, error) {
	s.downloadEventID = eventID
	s.downloadCalls++
	if s.downloadClosed != nil && s.downloadResponse.Body != nil {
		s.downloadResponse.Body = &trackedReadCloser{Reader: s.downloadResponse.Body, closed: s.downloadClosed}
	}
	return s.downloadResponse, s.downloadErr
}

type trackedReadCloser struct {
	io.Reader
	closed *bool
}

func (r *trackedReadCloser) Close() error {
	*r.closed = true
	return nil
}

func assertNoStoreHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("Pragma") != "no-cache" || response.Header().Get("Expires") != "0" {
		t.Fatalf("expected no-store companion headers, got Pragma=%q Expires=%q", response.Header().Get("Pragma"), response.Header().Get("Expires"))
	}
}

func (s *usageEventsStub) GetUsageOverview(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	s.lastFilter = filter
	s.overviewCalls++
	return nil, nil
}

func (s *usageEventsStub) GetUsageActivity(context.Context, servicedto.UsageFilter) (*servicedto.UsageActivitySnapshot, error) {
	return nil, s.err
}

func (s *usageEventsStub) GetUsageOverviewRealtime(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error) {
	return nil, nil
}

func (s *usageEventsStub) ListUsageEvents(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	s.lastFilter = filter
	s.filterCalls++
	if s.eventsPage != nil {
		return s.eventsPage, s.err
	}
	return &servicedto.UsageEventsPage{Events: s.events, TotalCount: int64(len(s.events)), Page: 1, PageSize: servicedto.DefaultUsageEventsLimit, TotalPages: 1}, s.err
}

func (s *usageEventsStub) StreamUsageEvents(_ context.Context, filter servicedto.UsageFilter, emit func(servicedto.UsageEventRecord) error) error {
	s.lastFilter = filter
	s.exportCalls++
	events := s.events
	if s.exportEvents != nil {
		events = s.exportEvents
	}
	for _, event := range events {
		if err := emit(event); err != nil {
			return err
		}
	}
	return s.err
}

func (s *blockingUsageEventsExportStub) StreamUsageEvents(ctx context.Context, _ servicedto.UsageFilter, _ func(servicedto.UsageEventRecord) error) error {
	s.calls.Add(1)
	s.started <- struct{}{}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *usageEventsStub) ListUsageEventFilterOptions(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	s.lastFilter = filter
	s.filterOptionCalls++
	if s.eventFilterOptions != nil {
		return s.eventFilterOptions, s.err
	}
	return &servicedto.UsageEventFilterOptions{}, s.err
}

func (s *usageEventsStub) GetAnalysis(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	return nil, s.err
}

func (s *usageEventsStub) GetAnalysisLatency(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisLatencyDiagnostics, error) {
	return nil, s.err
}

type authCPAAPIKeyStub struct {
	row entities.CPAAPIKey
}

func (s *authCPAAPIKeyStub) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return []entities.CPAAPIKey{s.row}, nil
}

func (s *authCPAAPIKeyStub) FindActiveCPAAPIKeyByValue(context.Context, string) (entities.CPAAPIKey, error) {
	return s.row, nil
}

func (s *authCPAAPIKeyStub) FindActiveCPAAPIKeyByID(context.Context, int64) (entities.CPAAPIKey, error) {
	return s.row, nil
}

func (s *authCPAAPIKeyStub) UpdateCPAAPIKeyAlias(context.Context, int64, string) (entities.CPAAPIKey, error) {
	return s.row, nil
}

type usageIdentitiesStub struct {
	items []entities.UsageIdentity
	err   error
}

func (s usageIdentitiesStub) ListUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	return s.items, s.err
}

func (s usageIdentitiesStub) ListActiveUsageIdentities(context.Context) ([]entities.UsageIdentity, error) {
	return s.items, s.err
}

func (s usageIdentitiesStub) ListActiveUsageIdentitiesPage(context.Context, service.ListUsageIdentitiesRequest) (service.ListUsageIdentitiesResponse, error) {
	return service.ListUsageIdentitiesResponse{Items: s.items, Total: int64(len(s.items))}, s.err
}

func (s usageIdentitiesStub) UpdateUsageIdentityAlias(context.Context, int64, string) (entities.UsageIdentity, error) {
	if len(s.items) == 0 {
		return entities.UsageIdentity{}, s.err
	}
	return s.items[0], s.err
}

func TestUsageEventsEndpointsAcceptNinetyDayCustomRange(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	startDay := today.AddDate(0, 0, -89)
	query := url.Values{
		"range": {"custom"},
		"unit":  {"day"},
		"start": {startDay.Format(time.DateOnly)},
		"end":   {today.Format(time.DateOnly)},
	}

	for _, tc := range []struct {
		name   string
		path   string
		export bool
	}{
		{name: "list", path: "/api/v1/usage/events?" + query.Encode()},
		{name: "export", path: "/api/v1/usage/events/export?format=csv&" + query.Encode(), export: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			response := serveAPIGet(router, tc.path)

			if response.Code != http.StatusOK {
				t.Fatalf("expected 90-day custom Events %s to return 200, got %d body=%s", tc.name, response.Code, response.Body.String())
			}
			if tc.export {
				if provider.exportCalls != 1 || provider.filterCalls != 0 {
					t.Fatalf("expected only export provider call, export=%d list=%d", provider.exportCalls, provider.filterCalls)
				}
			} else if provider.filterCalls != 1 || provider.exportCalls != 0 {
				t.Fatalf("expected only list provider call, list=%d export=%d", provider.filterCalls, provider.exportCalls)
			}
			if provider.lastFilter.StartTime == nil || !provider.lastFilter.StartTime.Equal(startDay) {
				t.Fatalf("expected custom day start %s, got %+v", startDay, provider.lastFilter)
			}
			expectedEnd := today.AddDate(0, 0, 1)
			if provider.lastFilter.EndTime == nil || !provider.lastFilter.EndTime.Equal(expectedEnd) || !provider.lastFilter.EndExclusive {
				t.Fatalf("expected exclusive custom day end %s, got %+v", expectedEnd, provider.lastFilter)
			}
			if provider.lastFilter.CustomUnit != "day" || provider.lastFilter.RangeCount != 90 {
				t.Fatalf("expected 90 complete custom day buckets, got %+v", provider.lastFilter)
			}
		})
	}
}

func TestUsageEventsEndpointsRejectCustomRangeBeyondNinetyDays(t *testing.T) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	query := url.Values{
		"range": {"custom"},
		"unit":  {"day"},
		"start": {today.AddDate(0, 0, -90).Format(time.DateOnly)},
		"end":   {today.Format(time.DateOnly)},
	}

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "list", path: "/api/v1/usage/events?" + query.Encode()},
		{name: "export", path: "/api/v1/usage/events/export?format=csv&" + query.Encode()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			response := serveAPIGet(router, tc.path)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 91-day custom Events %s to return 400, got %d body=%s", tc.name, response.Code, response.Body.String())
			}
			if provider.filterCalls != 0 || provider.exportCalls != 0 {
				t.Fatalf("expected rejected range not to reach provider, list=%d export=%d", provider.filterCalls, provider.exportCalls)
			}
		})
	}
}

func TestUsageEventsReturnsFilteredRows(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:                  42,
		Timestamp:           time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:               "claude-sonnet",
		ModelAlias:          "sonnet-business",
		ResponseModel:       "actual-model",
		ReasoningEffort:     "medium",
		ServiceTier:         "auto",
		ResponseServiceTier: "default",
		ClientIP:            new("192.0.2.10"),
		XForwardedFor:       new("203.0.113.5, 198.51.100.8"),
		UserAgent:           new("test-client/1.0"),
		ExecutorType:        "responses",
		Endpoint:            "POST /v1/responses",
		AuthType:            "apikey",
		RequestID:           "req-log-42",
		Provider:            "OpenAI Mirror",
		Source:              "sk-provider-key",
		AuthIndex:           "2",
		Failed:              false,
		StatusCode:          new(200),
		Stream:              new(true),
		LatencyMS:           2000,
		TTFTMS:              new(int64(45)),
		InputTokens:         10,
		OutputTokens:        61,
		ReasoningTokens:     2,
		CacheReadTokens:     3,
		CacheCreationTokens: 4,
		TotalTokens:         18,
		CostUSD:             0.1234,
		CostAvailable:       true,
		PricingStyle:        "claude",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	for _, fragment := range []string{
		`"events":[`, `"model":"claude-sonnet"`, `"model_alias":"sonnet-business"`, `"response_model":"actual-model"`,
		`"id":"42"`, `"total_count":1`, `"page":1`,
		`"page_size":100`, `"total_pages":1`, `"source":"OpenAI Mirror"`,
		`"auth_index":"2"`, `"request_id":"req-log-42"`, `"timestamp":"2026-04-22T19:00:00+08:00"`,
		`"cache_read_tokens":3`, `"cache_creation_tokens":4`, `"reasoning_effort":"medium"`,
		`"service_tier":"auto"`, `"response_service_tier":"default"`, `"client_ip":"192.0.2.10"`,
		`"x_forwarded_for":"203.0.113.5, 198.51.100.8"`, `"user_agent":"test-client/1.0"`, `"endpoint":"POST /v1/responses"`,
		`"ttft_ms":45`, `"speed_tps":30.5`, `"executor_type":"responses"`, `"status_code":200`, `"stream":true`,
		`"cost_usd":0.1234`, `"cost_available":true`, `"pricing_style":"claude"`,
	} {
		if !contains(body, fragment) {
			t.Fatalf("missing JSON field/value %s in %s", fragment, body)
		}
	}
	for _, fragment := range []string{
		`sk-provider-key`, `sk-provider-prefix`, `"source_type"`,
		`"source_key"`, `"cached_tokens"`,
	} {
		if contains(body, fragment) {
			t.Fatalf("unexpected JSON field/value %s in %s", fragment, body)
		}
	}
	if provider.filterCalls != 1 {
		t.Fatalf("expected ListUsageEvents to be called once, got %d", provider.filterCalls)
	}
	if provider.lastFilter.Range != "24h" {
		t.Fatalf("expected range to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.Page != 1 || provider.lastFilter.PageSize != 100 || provider.lastFilter.Offset != 0 {
		t.Fatalf("expected default pagination to be passed through, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime == nil || provider.lastFilter.EndTime == nil {
		t.Fatalf("expected resolved time bounds in filter, got %+v", provider.lastFilter)
	}
}

func TestUsageEventRequestLogReturnsStructuredLog(t *testing.T) {
	provider := &usageEventsStub{}
	requestLogProvider := &requestLogProviderStub{response: service.RequestLogResponse{
		EventID:   42,
		RequestID: "req-log-42",
		Filename:  "error-v1-responses-req-log-42.log",
		Available: true,
		Sections: []service.RequestLogSection{{
			Title:   "REQUEST INFO",
			Content: "URL: /v1/responses",
		}},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{RequestLogs: requestLogProvider, Status: StatusRouteConfig{CPARequestLogAccessEnabled: true}})
	resp := serveAPIGet(router, "/api/v1/usage/events/42/request-log")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	assertNoStoreHeaders(t, resp)
	body := resp.Body.String()
	if !contains(body, `"event_id":"42"`) || !contains(body, `"request_id":"req-log-42"`) || !contains(body, `"filename":"error-v1-responses-req-log-42.log"`) {
		t.Fatalf("expected request log metadata in response: %s", body)
	}
	if !contains(body, `"title":"REQUEST INFO"`) || !contains(body, `"content":"URL: /v1/responses"`) {
		t.Fatalf("expected parsed request log sections in response: %s", body)
	}
	if contains(body, `"raw":`) {
		t.Fatalf("expected preview response not to include raw log body: %s", body)
	}
	if requestLogProvider.calls != 1 || requestLogProvider.eventID != 42 {
		t.Fatalf("expected provider call with event id 42, got calls=%d eventID=%d", requestLogProvider.calls, requestLogProvider.eventID)
	}
}

func TestUsageEventRequestLogRoutesRejectDisabledAccess(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/usage/events/42/request-log"},
		{http.MethodPost, "/api/v1/usage/events/42/request-log/download-token"},
		{http.MethodGet, "/api/v1/usage/events/42/request-log/download-file?token=stale"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			logs := &requestLogProviderStub{}
			router := NewRouter(nil, nil, &usageEventsStub{}, nil, AuthConfig{}, nil, "", OptionalProviders{RequestLogs: logs})
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.method == http.MethodPost {
				req.Header.Set(requestIntentHeaderName, requestIntentHeaderValueFetch)
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)
			if resp.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			if logs.calls != 0 || logs.downloadCalls != 0 {
				t.Fatalf("disabled access reached provider: preview=%d download=%d", logs.calls, logs.downloadCalls)
			}
		})
	}
}

func TestUsageEventRequestLogReturnsNotFoundWhenEventMissing(t *testing.T) {
	provider := &usageEventsStub{}
	requestLogProvider := &requestLogProviderStub{err: gorm.ErrRecordNotFound}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{RequestLogs: requestLogProvider, Status: StatusRouteConfig{CPARequestLogAccessEnabled: true}})
	resp := serveAPIGet(router, "/api/v1/usage/events/404/request-log")

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !contains(resp.Body.String(), `"error":"usage event not found"`) {
		t.Fatalf("expected not found error body, got %s", resp.Body.String())
	}
	if requestLogProvider.calls != 1 || requestLogProvider.eventID != 404 {
		t.Fatalf("expected provider call with event id 404, got calls=%d eventID=%d", requestLogProvider.calls, requestLogProvider.eventID)
	}
}

func TestUsageEventRequestLogReturnsTooLargeMetadata(t *testing.T) {
	provider := &usageEventsStub{}
	requestLogProvider := &requestLogProviderStub{response: service.RequestLogResponse{
		EventID:      42,
		RequestID:    "req-large",
		Filename:     "large-request.log",
		Available:    true,
		Previewable:  false,
		TooLarge:     true,
		Downloadable: true,
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{RequestLogs: requestLogProvider, Status: StatusRouteConfig{CPARequestLogAccessEnabled: true}})
	resp := serveAPIGet(router, "/api/v1/usage/events/42/request-log")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !contains(body, `"too_large":true`) || !contains(body, `"downloadable":true`) {
		t.Fatalf("expected large log metadata in response: %s", body)
	}
	if contains(body, `"raw":`) || contains(body, `"sections":null`) {
		t.Fatalf("expected too large preview response without raw body: %s", body)
	}
}

func TestUsageEventRequestLogDownloadTokenStreamsAttachmentOnce(t *testing.T) {
	provider := &usageEventsStub{}
	body := "token raw log"
	closed := false
	requestLogProvider := &requestLogProviderStub{downloadResponse: service.RequestLogDownload{
		EventID:       42,
		RequestID:     "req-log-42",
		Filename:      "token-request.log",
		ContentType:   "text/plain; charset=utf-8",
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(bytes.NewBufferString(body)),
		Downloadable:  true,
	}, downloadClosed: &closed}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{RequestLogs: requestLogProvider, Status: StatusRouteConfig{CPARequestLogAccessEnabled: true}})
	issueResp := serveCredentialMutation(router, http.MethodPost, "/api/v1/usage/events/42/request-log/download-token", "")

	if issueResp.Code != http.StatusOK {
		t.Fatalf("expected token status 200, got %d body=%s", issueResp.Code, issueResp.Body.String())
	}
	assertNoStoreHeaders(t, issueResp)
	var payload requestLogDownloadTokenPayload
	if err := json.NewDecoder(issueResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if payload.DownloadURL == "" || !contains(payload.DownloadURL, "/api/v1/usage/events/42/request-log/download-file?token=") {
		t.Fatalf("unexpected download URL %q", payload.DownloadURL)
	}

	downloadResp := serveAPIGet(router, payload.DownloadURL)

	if downloadResp.Code != http.StatusOK {
		t.Fatalf("expected download status 200, got %d body=%s", downloadResp.Code, downloadResp.Body.String())
	}
	if !contains(downloadResp.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("expected text attachment content type, got %q", downloadResp.Header().Get("Content-Type"))
	}
	if !contains(downloadResp.Header().Get("Content-Disposition"), `filename="token-request.log"`) {
		t.Fatalf("expected request log attachment filename, got %q", downloadResp.Header().Get("Content-Disposition"))
	}
	if downloadResp.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatalf("expected content length %d, got %q", len(body), downloadResp.Header().Get("Content-Length"))
	}
	if downloadResp.Body.String() != body {
		t.Fatalf("unexpected download body %q", downloadResp.Body.String())
	}
	assertNoStoreHeaders(t, downloadResp)
	if !closed {
		t.Fatalf("expected download stream to be closed")
	}
	if requestLogProvider.downloadCalls != 1 || requestLogProvider.downloadEventID != 42 {
		t.Fatalf("expected download provider call with event id 42, got calls=%d eventID=%d", requestLogProvider.downloadCalls, requestLogProvider.downloadEventID)
	}

	reuseResp := serveAPIGet(router, payload.DownloadURL)
	if reuseResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected reused token status 401, got %d body=%s", reuseResp.Code, reuseResp.Body.String())
	}
	if requestLogProvider.downloadCalls != 1 {
		t.Fatalf("expected reused token not to call provider, got %d calls", requestLogProvider.downloadCalls)
	}
}

func TestUsageEventRequestLogDirectDownloadRouteIsUnavailable(t *testing.T) {
	provider := &usageEventsStub{}
	requestLogProvider := &requestLogProviderStub{downloadResponse: service.RequestLogDownload{
		EventID:      42,
		RequestID:    "req-log-42",
		Body:         io.NopCloser(bytes.NewBufferString("raw log")),
		Downloadable: true,
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{
		RequestLogs: requestLogProvider,
		Status:      StatusRouteConfig{CPARequestLogAccessEnabled: true},
	})
	resp := serveAPIGet(router, "/api/v1/usage/events/42/request-log/download")

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected removed direct download route status 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if requestLogProvider.downloadCalls != 0 {
		t.Fatalf("expected removed direct route not to call provider, got %d calls", requestLogProvider.downloadCalls)
	}
}

func TestUsageEventRequestLogDownloadTokenAuthBoundary(t *testing.T) {
	provider := &usageEventsStub{}
	requestLogProvider := &requestLogProviderStub{downloadResponse: service.RequestLogDownload{
		EventID:      42,
		RequestID:    "req-log-42",
		Filename:     "token-auth-request.log",
		ContentType:  "text/plain; charset=utf-8",
		Body:         io.NopCloser(bytes.NewBufferString("auth token raw log")),
		Downloadable: true,
	}}
	sessions := auth.NewSessionManager(time.Hour)
	config := AuthConfig{Enabled: true, LoginPassword: "secret", SessionTTL: time.Hour, BasePath: "/cpa"}
	router := NewRouter(nil, nil, provider, nil, config, NewAuthHandler(config, sessions), "/cpa", OptionalProviders{RequestLogs: requestLogProvider, Status: StatusRouteConfig{CPARequestLogAccessEnabled: true}})

	noSessionResp := serveCredentialMutation(router, http.MethodPost, "/cpa/api/v1/usage/events/42/request-log/download-token", "")
	if noSessionResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated token issue status 401, got %d body=%s", noSessionResp.Code, noSessionResp.Body.String())
	}

	viewerToken, _, err := sessions.CreateAPIKeyViewer(7)
	if err != nil {
		t.Fatalf("create viewer session: %v", err)
	}
	viewerResp := serveCredentialMutation(router, http.MethodPost, "/cpa/api/v1/usage/events/42/request-log/download-token", "", &http.Cookie{Name: "cpa_usage_keeper_session", Value: viewerToken})
	if viewerResp.Code != http.StatusForbidden {
		t.Fatalf("expected viewer token issue status 403, got %d body=%s", viewerResp.Code, viewerResp.Body.String())
	}

	adminToken, _, err := sessions.Create()
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	adminResp := serveCredentialMutation(router, http.MethodPost, "/cpa/api/v1/usage/events/42/request-log/download-token", "", &http.Cookie{Name: "cpa_usage_keeper_session", Value: adminToken})
	if adminResp.Code != http.StatusOK {
		t.Fatalf("expected admin token issue status 200, got %d body=%s", adminResp.Code, adminResp.Body.String())
	}
	var payload requestLogDownloadTokenPayload
	if err := json.NewDecoder(adminResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode admin token response: %v", err)
	}
	if !strings.HasPrefix(payload.DownloadURL, "/cpa/api/v1/usage/events/42/request-log/download-file?token=") {
		t.Fatalf("expected base path download URL, got %q", payload.DownloadURL)
	}

	downloadResp := serveAPIGet(router, payload.DownloadURL)
	if downloadResp.Code != http.StatusOK {
		t.Fatalf("expected token-only download status 200, got %d body=%s", downloadResp.Code, downloadResp.Body.String())
	}
	if downloadResp.Body.String() != "auth token raw log" {
		t.Fatalf("unexpected token-only download body %q", downloadResp.Body.String())
	}
	if requestLogProvider.downloadCalls != 1 {
		t.Fatalf("expected one token-only provider download call, got %d", requestLogProvider.downloadCalls)
	}
}

func TestUsageEventRequestLogDownloadSanitizesAttachmentFilename(t *testing.T) {
	provider := &usageEventsStub{}
	unsafeFilename := "bad\r\nX-Bad: yes; evil/../名.log"
	requestLogProvider := &requestLogProviderStub{downloadResponse: service.RequestLogDownload{
		EventID:      42,
		RequestID:    "req-log-42",
		Filename:     unsafeFilename,
		ContentType:  "text/plain",
		Body:         io.NopCloser(strings.NewReader("raw")),
		Downloadable: true,
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{RequestLogs: requestLogProvider, Status: StatusRouteConfig{CPARequestLogAccessEnabled: true}})
	issueResp := serveCredentialMutation(router, http.MethodPost, "/api/v1/usage/events/42/request-log/download-token", "")

	if issueResp.Code != http.StatusOK {
		t.Fatalf("expected token status 200, got %d body=%s", issueResp.Code, issueResp.Body.String())
	}
	var payload requestLogDownloadTokenPayload
	if err := json.NewDecoder(issueResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	resp := serveAPIGet(router, payload.DownloadURL)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	disposition := resp.Header().Get("Content-Disposition")
	if strings.ContainsAny(disposition, "\r\n") {
		t.Fatalf("content disposition must not contain raw CR/LF: %q", disposition)
	}
	match := regexp.MustCompile(`filename="([^"]+)"`).FindStringSubmatch(disposition)
	if len(match) != 2 {
		t.Fatalf("expected sanitized filename parameter, got %q", disposition)
	}
	if strings.ContainsAny(match[1], "\r\n;:/\\\"") {
		t.Fatalf("fallback filename contains unsafe header/path characters: %q in %q", match[1], disposition)
	}
	if strings.Contains(disposition, "%0D") || strings.Contains(disposition, "%0A") || strings.Contains(disposition, "%2F") || strings.Contains(disposition, "%3B") {
		t.Fatalf("filename* must not preserve encoded control/path/header separators, got %q", disposition)
	}
	if !contains(disposition, `filename*=UTF-8''bad__X-Bad_%20yes_%20evil_.._%E5%90%8D.log`) {
		t.Fatalf("expected RFC5987 filename* parameter to preserve safe Unicode only, got %q", disposition)
	}
}

func TestUsageEventsExportCSVReturnsFilteredRowsWithoutPagination(t *testing.T) {
	provider := &usageEventsStub{exportEvents: []servicedto.UsageEventRecord{{
		ID:                  52,
		Timestamp:           time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		APIGroupKey:         "sk-export123456",
		Model:               "claude-sonnet",
		ModelAlias:          "sonnet-export",
		ResponseModel:       "actual-export-model",
		ReasoningEffort:     "medium",
		ServiceTier:         "auto",
		ResponseServiceTier: "default",
		ClientIP:            new("192.0.2.10"),
		XForwardedFor:       new("203.0.113.5, 198.51.100.8"),
		UserAgent:           new("test-client/1.0"),
		ExecutorType:        "responses",
		Endpoint:            "POST /v1/responses",
		AuthType:            "apikey",
		Provider:            "Provider Fallback",
		AuthIndex:           "authidx-export-main",
		Failed:              true,
		StatusCode:          new(429),
		Stream:              new(false),
		LatencyMS:           2000,
		TTFTMS:              new(int64(45)),
		InputTokens:         10,
		OutputTokens:        61,
		ReasoningTokens:     2,
		CacheReadTokens:     3,
		CacheCreationTokens: 4,
		TotalTokens:         18,
		CostUSD:             0.1234,
		CostAvailable:       true,
		PricingStyle:        "claude",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{
		CPAAPIKeys: &authCPAAPIKeyStub{row: entities.CPAAPIKey{
			ID:         7,
			APIKey:     "sk-export123456",
			DisplayKey: "sk-*********123456",
			KeyAlias:   "Export Key",
		}},
		UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
			ID:           12,
			Name:         "Export Provider",
			AuthType:     entities.UsageIdentityAuthTypeAIProvider,
			AuthTypeName: "apikey",
			Identity:     "authidx-export-main",
			Type:         "openai",
			Provider:     "Provider",
		}}},
	})
	resp := serveAPIGet(router, "/api/v1/usage/events/export?range=24h&page=3&page_size=100&model=claude-sonnet&source=authidx-export-main&result=failed&format=csv")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if provider.exportCalls != 1 || provider.filterCalls != 0 {
		t.Fatalf("expected export only, export=%d list=%d", provider.exportCalls, provider.filterCalls)
	}
	if provider.lastFilter.Page != 0 || provider.lastFilter.PageSize != 0 || provider.lastFilter.Limit != 0 || provider.lastFilter.Offset != 0 {
		t.Fatalf("expected export to drop pagination, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.Model != "claude-sonnet" || provider.lastFilter.AuthIndex != "authidx-export-main" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "failed" {
		t.Fatalf("expected export filters to match list filters, got %+v", provider.lastFilter)
	}
	if !contains(resp.Header().Get("Content-Type"), "text/csv") || !contains(resp.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("expected csv attachment headers, got content-type=%q disposition=%q", resp.Header().Get("Content-Type"), resp.Header().Get("Content-Disposition"))
	}
	if !regexp.MustCompile(`filename="usage-events-\d{8}-\d{6}\.csv"`).MatchString(resp.Header().Get("Content-Disposition")) {
		t.Fatalf("expected timestamped csv filename, got %q", resp.Header().Get("Content-Disposition"))
	}
	if !contains(body, "cpa_api_key_id") || !contains(body, "auth_index") || !contains(body, "model_alias") || !contains(body, "response_model") || !contains(body, "response_service_tier") || !contains(body, "executor_type") || !contains(body, "is_identity_deleted") {
		t.Fatalf("expected cpa_api_key_id, auth_index, model_alias, response_model, response_service_tier, executor_type, and is_identity_deleted columns, got %s", body)
	}
	if !contains(body, "cache_read_tokens,cache_creation_tokens,cache_read_rate") || !contains(body, ",3,4,30,") || contains(body, "cached_tokens") {
		t.Fatalf("expected canonical cache token fields in csv export, got %s", body)
	}
	if !regexp.MustCompile(`(?m)^id,timestamp,api_key,cpa_api_key_id,source,source_type,auth_index,is_identity_deleted,model,model_alias,response_model,reasoning_effort,`).MatchString(body) {
		t.Fatalf("expected response_model to follow model_alias in csv header, got %s", body)
	}
	if !contains(body, "speed_tps,client_ip,x_forwarded_for,user_agent,input_tokens") || !contains(body, ",30.5,192.0.2.10,\"203.0.113.5, 198.51.100.8\",test-client/1.0,10,") {
		t.Fatalf("expected client metadata after speed in csv export, got %s", body)
	}
	if !contains(body, "service_tier,response_service_tier,executor_type") || !contains(body, ",auto,default,responses,") {
		t.Fatalf("expected separate request and response service tiers in csv export, got %s", body)
	}
	if !contains(body, "result,status_code,stream,endpoint") || !contains(body, "failed,429,false,POST /v1/responses") {
		t.Fatalf("expected status_code and stream in csv export, got %s", body)
	}
	if contains(body, "is_deleted") {
		t.Fatalf("expected export to use is_identity_deleted instead of is_deleted, got %s", body)
	}
	if contains(body, "cost_available") || contains(body, "pricing_style") {
		t.Fatalf("expected csv export to omit cost availability metadata, got %s", body)
	}
	if !contains(body, "Export Key") || !contains(body, ",7,") || !contains(body, "authidx-export-main") || !contains(body, "sonnet-export") || !contains(body, "actual-export-model") || !contains(body, "responses") || !contains(body, "failed") {
		t.Fatalf("expected exported row values, got %s", body)
	}
}

func TestUsageEventsExportCSVFormatsClientMetadataAsText(t *testing.T) {
	provider := &usageEventsStub{exportEvents: []servicedto.UsageEventRecord{{
		ID:            54,
		Timestamp:     time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC),
		ClientIP:      new("=1+1"),
		XForwardedFor: new("+SUM(1,1)"),
		UserAgent:     new("@HYPERLINK(\"https://example.invalid\")"),
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events/export?range=24h&format=csv")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, resp.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(resp.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv export: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header and one row, got %d records", len(records))
	}
	columnIndexes := map[string]int{}
	for index, column := range records[0] {
		columnIndexes[column] = index
	}
	for column, want := range map[string]string{
		"client_ip":       "'=1+1",
		"x_forwarded_for": "'+SUM(1,1)",
		"user_agent":      "'@HYPERLINK(\"https://example.invalid\")",
	} {
		index, ok := columnIndexes[column]
		if !ok {
			t.Fatalf("expected %s column in csv header", column)
		}
		if got := records[1][index]; got != want {
			t.Fatalf("expected %s to be exported as text %q, got %q", column, want, got)
		}
	}
}

func TestUsageEventsExportAllowsTwoConcurrentStreamsAndRejectsThird(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseAll)

	provider := &blockingUsageEventsExportStub{
		usageEventsStub: &usageEventsStub{},
		started:         make(chan struct{}, 3),
		release:         release,
	}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	type exportResult struct {
		status int
		body   string
	}
	results := make(chan exportResult, 3)
	for range 3 {
		go func() {
			response := serveAPIGet(router, "/api/v1/usage/events/export?range=24h&format=csv")
			results <- exportResult{status: response.Code, body: response.Body.String()}
		}()
	}

	started := 0
	rejected := false
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for started < 2 || !rejected {
		select {
		case <-provider.started:
			started++
			if started > 2 {
				releaseAll()
				t.Fatalf("expected at most two export streams to reach provider, got %d", started)
			}
		case result := <-results:
			if result.status != http.StatusTooManyRequests {
				releaseAll()
				t.Fatalf("expected concurrent overflow status 429, got %d body=%s", result.status, result.body)
			}
			rejected = true
		case <-deadline.C:
			releaseAll()
			t.Fatal("timed out waiting for two active exports and one rejection")
		}
	}

	releaseAll()
	for range 2 {
		result := <-results
		if result.status != http.StatusOK {
			t.Fatalf("expected active export status 200, got %d body=%s", result.status, result.body)
		}
	}

	response := serveAPIGet(router, "/api/v1/usage/events/export?range=24h&format=json")
	if response.Code != http.StatusOK {
		t.Fatalf("expected a later export to reuse a released slot, got %d body=%s", response.Code, response.Body.String())
	}
	if provider.calls.Load() != 3 {
		t.Fatalf("expected exactly three provider streams after slot reuse, got %d", provider.calls.Load())
	}
}

func TestUsageEventsExportJSONIncludesAllExportFields(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:                  53,
		Timestamp:           time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		APIGroupKey:         "sk-json-export",
		Model:               "gpt-5",
		ModelAlias:          "gpt-json-alias",
		ServiceTier:         "auto",
		ResponseServiceTier: "default",
		ClientIP:            new("192.0.2.11"),
		XForwardedFor:       new("203.0.113.6"),
		UserAgent:           new("json-client/1.0"),
		ExecutorType:        "chat_completions",
		Endpoint:            "GET /v1/responses",
		AuthType:            "oauth",
		Source:              "claude-code",
		AuthIndex:           "auth-file-export",
		Failed:              false,
		StatusCode:          new(200),
		Stream:              new(true),
		LatencyMS:           500,
		InputTokens:         9,
		OutputTokens:        5,
		CacheReadTokens:     3,
		CacheCreationTokens: 4,
		TotalTokens:         14,
		CostAvailable:       true,
		PricingStyle:        "openai",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{
		CPAAPIKeys: &authCPAAPIKeyStub{row: entities.CPAAPIKey{
			ID:       9,
			APIKey:   "sk-json-export",
			KeyAlias: "Team <Ops> & Co",
		}},
	})
	resp := serveAPIGet(router, "/api/v1/usage/events/export?range=24h&format=json")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(resp.Header().Get("Content-Type"), "application/json") || !contains(resp.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("expected json attachment headers, got content-type=%q disposition=%q", resp.Header().Get("Content-Type"), resp.Header().Get("Content-Disposition"))
	}
	if !regexp.MustCompile(`filename="usage-events-\d{8}-\d{6}\.json"`).MatchString(resp.Header().Get("Content-Disposition")) {
		t.Fatalf("expected timestamped json filename, got %q", resp.Header().Get("Content-Disposition"))
	}
	for _, fragment := range []string{
		`"total_count":1`, `"auth_index":"auth-file-export"`, `"model_alias":"gpt-json-alias"`,
		`"executor_type":"chat_completions"`, `"endpoint":"GET /v1/responses"`, `"is_identity_deleted":true`,
		`"api_key":"Team <Ops> & Co"`, `"cpa_api_key_id":"9"`, `"source_type":""`,
		`"reasoning_effort":""`, `"ttft_ms":null`, `"speed_tps":10`,
		`"service_tier":"auto"`, `"response_service_tier":"default"`, `"client_ip":"192.0.2.11"`,
		`"x_forwarded_for":"203.0.113.6"`, `"user_agent":"json-client/1.0"`, `"cache_read_tokens":3`,
		`"cache_creation_tokens":4`, `"cache_read_rate":33.33333333333333`, `"status_code":200`, `"stream":true`,
	} {
		if !contains(body, fragment) {
			t.Fatalf("missing JSON field/value %s in %s", fragment, body)
		}
	}
	for _, fragment := range []string{
		`"page"`, `"page_size"`, `\u003c`,
		`\u0026`, `\u003e`, `"cached_tokens"`,
		`"is_deleted"`, `"cost_available"`, `"pricing_style"`,
	} {
		if contains(body, fragment) {
			t.Fatalf("unexpected JSON field/value %s in %s", fragment, body)
		}
	}

}

func TestUsageEventsExportStreamSetupErrorReturnsServerError(t *testing.T) {
	for _, format := range []string{"csv", "json"} {
		t.Run(format, func(t *testing.T) {
			provider := &usageEventsStub{err: errors.New("stream setup failed")}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
			resp := serveAPIGet(router, "/api/v1/usage/events/export?range=24h&format="+format)

			if resp.Code != http.StatusInternalServerError {
				t.Fatalf("expected status 500 before export body is written, got %d: %s", resp.Code, resp.Body.String())
			}
			if contains(resp.Body.String(), "usage-events-") || contains(resp.Body.String(), `"events":[`) || contains(resp.Body.String(), "id,timestamp") {
				t.Fatalf("expected error response instead of partial export body, got %s", resp.Body.String())
			}
		})
	}
}

func TestUsageEventsResolvesCPAAPIKeyDisplay(t *testing.T) {
	for _, tc := range []struct {
		name, groupKey, alias, want string
		matched                     bool
		hiddenMask                  string
	}{
		{"alias", "sk-alpha123456", "Production Key", "Production Key", true, "sk-*********123456"},
		{"matched without alias", "sk-beta654321", "", "sk-*********654321", true, ""},
		{"unmatched", "sk-BabcdefghijklmnopqrstuvwxyzmaWyTA", "", "sk-*********maWyTA", false, "sk-B***************************WyTA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
				ID: 49, Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
				APIGroupKey: tc.groupKey, Model: "claude-sonnet", AuthType: "apikey", Provider: "Fallback Provider",
			}}}
			var keys service.CPAAPIKeyProvider
			if tc.matched {
				keys = &authCPAAPIKeyStub{row: entities.CPAAPIKey{ID: 7, APIKey: tc.groupKey, DisplayKey: "sk-*********" + tc.groupKey[len(tc.groupKey)-6:], KeyAlias: tc.alias}}
			}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{CPAAPIKeys: keys})
			resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")
			if resp.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			var payload struct {
				Events []struct {
					APIKey string `json:"api_key"`
				} `json:"events"`
			}
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Events) != 1 || payload.Events[0].APIKey != tc.want {
				t.Fatalf("unexpected API key display: %s", resp.Body.String())
			}
			if contains(resp.Body.String(), tc.groupKey) || (tc.hiddenMask != "" && contains(resp.Body.String(), tc.hiddenMask)) {
				t.Fatalf("key leaked in response: %s", resp.Body.String())
			}
		})
	}
}

func TestUsageEventsResolvesAPIKeySourceFromProviderIdentity(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        44,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "Fallback Provider",
		Source:    "sk-provider-key",
		AuthIndex: "provider-auth-index",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:            12,
		Name:          "Provider Name",
		Prefix:        "Team Prefix",
		AuthType:      entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName:  "apikey",
		Identity:      "provider-auth-index",
		Type:          "openai",
		Provider:      "Provider",
		TotalRequests: 1,
	}}}})
	resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if !contains(body, `"source":"Team Prefix"`) {
		t.Fatalf("expected source to use provider identity displayName, got %s", body)
	}
	if !contains(body, `"source_type":"openai"`) {
		t.Fatalf("expected source_type to use provider identity type, got %s", body)
	}
	if contains(body, `"source_key"`) {
		t.Fatalf("expected source_key to stay omitted, got %s", body)
	}
	if contains(body, `Fallback Provider`) || contains(body, `sk-provider-key`) {
		t.Fatalf("expected fallback and raw source to be hidden, got %s", body)
	}
}

func TestUsageEventsDoesNotResolveProviderIdentityFromSource(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        45,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "Fallback Provider",
		Source:    "provider-auth-index",
		AuthIndex: "missing-auth-index",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
		ID:            12,
		Name:          "Provider Name",
		Prefix:        "Team Prefix",
		AuthType:      entities.UsageIdentityAuthTypeAIProvider,
		AuthTypeName:  "apikey",
		Identity:      "provider-auth-index",
		Type:          "openai",
		Provider:      "Provider",
		TotalRequests: 1,
	}}}})
	resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")

	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", resp.Code, body)
	}
	if contains(body, `"source":"Team Prefix"`) || contains(body, `"source_key"`) {
		t.Fatalf("expected event source not to resolve identity through usage event source, got %s", body)
	}
	if !contains(body, `"source":"Fallback Provider"`) {
		t.Fatalf("expected auth_index fallback when identity is missing, got %s", body)
	}
}

func TestUsageEventsMarksDeletedFromAuthIndexIdentityMatch(t *testing.T) {
	for _, authIndex := range []string{"missing-auth-index", "provider-auth-index"} {
		t.Run(authIndex, func(t *testing.T) {
			provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
				ID: 46, Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
				Model: "claude-sonnet", AuthType: "apikey", Provider: "Fallback Provider", AuthIndex: authIndex,
			}}}
			router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{
				ID: 12, Name: "Provider Name", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "provider-auth-index",
			}}}})
			resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")
			if resp.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
			}
			body := resp.Body.String()
			if contains(body, `"isDelete":true`) != (authIndex == "missing-auth-index") {
				t.Fatalf("unexpected identity deletion marker: %s", body)
			}
			if contains(body, `"source_key"`) {
				t.Fatalf("source_key exposed: %s", body)
			}
		})
	}
}

func TestUsageEventsKeepsFallbackSourceWhenAuthIndexIsMissing(t *testing.T) {
	provider := &usageEventsStub{events: []servicedto.UsageEventRecord{{
		ID:        43,
		Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC),
		Model:     "claude-sonnet",
		AuthType:  "apikey",
		Provider:  "OpenAI Mirror",
		Source:    "sk-provider-key",
	}}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !contains(body, `"source":"OpenAI Mirror"`) || contains(body, `"source_key"`) {
		t.Fatalf("expected provider source fallback without source_key, got %s", body)
	}
}

func TestUsageEventsPassesPaginationAndAuthIndexSourceFilter(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{Events: []servicedto.UsageEventRecord{}, TotalCount: 0, Page: 3, PageSize: 100, TotalPages: 0}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events?range=24h&page=3&page_size=100&model=claude-sonnet&source=authidx-openai-main&result=failed")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.lastFilter.Page != 3 || provider.lastFilter.PageSize != 100 || provider.lastFilter.Offset != 200 {
		t.Fatalf("expected pagination filter, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.Model != "claude-sonnet" || provider.lastFilter.AuthIndex != "authidx-openai-main" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "failed" {
		t.Fatalf("expected source filter to be translated to auth_index only, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.SkipTotalCount {
		t.Fatalf("expected ranged Request Events query to keep total count, got %+v", provider.lastFilter)
	}
	body := resp.Body.String()
	if !contains(body, `"page":3`) || !contains(body, `"page_size":100`) || !contains(body, `"total_count":0`) || !contains(body, `"total_pages":0`) {
		t.Fatalf("expected response pagination metadata, got %s", body)
	}
}

func TestUsageEventsPassesLatestIdentityTypeFilterWithoutRange(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{Events: []servicedto.UsageEventRecord{}, TotalCount: 0, Page: 1, PageSize: 50}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events?cursor_mode=true&page_size=50&source=shared-auth&auth_type=2")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.lastFilter.AuthIndex != "shared-auth" || provider.lastFilter.AuthType != "apikey" || provider.lastFilter.Source != "" {
		t.Fatalf("expected exact provider identity filter, got %+v", provider.lastFilter)
	}
	if provider.lastFilter.StartTime != nil || provider.lastFilter.EndTime != nil || !provider.lastFilter.CursorMode {
		t.Fatalf("expected unbounded latest cursor filter, got %+v", provider.lastFilter)
	}
	if !provider.lastFilter.SkipTotalCount {
		t.Fatalf("expected latest identity cursor filter to skip total count, got %+v", provider.lastFilter)
	}
}

func TestUsageEventsExportRejectsLatestIdentityQueryWithoutRange(t *testing.T) {
	provider := &usageEventsStub{}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events/export?format=csv&cursor_mode=true&source=shared-auth&auth_type=2")

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if provider.exportCalls != 0 {
		t.Fatalf("expected invalid unbounded export not to reach the service, got %d calls", provider.exportCalls)
	}
}

func TestUsageEventsReturnsAndAcceptsCursorPagination(t *testing.T) {
	eventTimestamp := time.Date(2026, 4, 22, 11, 0, 0, 123456789, time.UTC)
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{
		Events:     []servicedto.UsageEventRecord{{ID: 42, Timestamp: eventTimestamp, Model: "gpt-5"}},
		TotalCount: 3,
		Page:       1,
		PageSize:   20,
		HasMore:    true,
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	firstResponse := serveAPIGet(router, "/api/v1/usage/events?range=24h&page_size=20&cursor_mode=true")

	if firstResponse.Code != http.StatusOK {
		t.Fatalf("expected first cursor response status 200, got %d", firstResponse.Code)
	}
	var payload struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode cursor response: %v", err)
	}
	if !payload.HasMore || payload.NextCursor == "" {
		t.Fatalf("expected next cursor metadata, got %s", firstResponse.Body.String())
	}

	secondRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/usage/events?range=24h&page_size=20&cursor="+url.QueryEscape(payload.NextCursor),
		nil,
	)
	secondResponse := httptest.NewRecorder()
	router.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("expected continuation response status 200, got %d", secondResponse.Code)
	}
	if !provider.lastFilter.CursorMode || provider.lastFilter.CursorTimestamp == nil || provider.lastFilter.CursorID != 42 {
		t.Fatalf("expected decoded cursor filter, got %+v", provider.lastFilter)
	}
	if !provider.lastFilter.CursorTimestamp.Equal(eventTimestamp) {
		t.Fatalf("expected cursor timestamp %s, got %s", eventTimestamp, provider.lastFilter.CursorTimestamp)
	}
}

func TestUsageEventsPassesAuthFileIdentitySourceFilterAsAuthIndex(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{Events: []servicedto.UsageEventRecord{}, TotalCount: 0, Page: 1, PageSize: 100, TotalPages: 0}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events?range=24h&source=auth-file-index")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.lastFilter.AuthIndex != "auth-file-index" || provider.lastFilter.Source != "" {
		t.Fatalf("expected auth file identity source filter to use auth_index only, got %+v", provider.lastFilter)
	}
}

func TestUsageEventsDoesNotReturnFilterOptions(t *testing.T) {
	provider := &usageEventsStub{eventsPage: &servicedto.UsageEventsPage{
		Events: []servicedto.UsageEventRecord{{
			ID: 7, Timestamp: time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC), Model: "gpt-5", AuthType: "apikey", Provider: "Provider A", Source: "source-a", Failed: true,
		}},
		TotalCount: 2, Page: 1, PageSize: 20, TotalPages: 1,
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events?range=24h")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if contains(body, `"models":`) || contains(body, `"sources":`) {
		t.Fatalf("expected events response to omit filter options, got %s", body)
	}
}

func TestUsageEventModelFilterOptionsReturnsStableModels(t *testing.T) {
	provider := &usageEventsStub{eventFilterOptions: &servicedto.UsageEventFilterOptions{
		Models: []string{"claude-sonnet", "gpt-5"},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")
	resp := serveAPIGet(router, "/api/v1/usage/events/filters/models?range=24h&model=ignored&source=ignored&result=failed&page=3&page_size=20")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.filterOptionCalls != 1 || provider.filterCalls != 0 {
		t.Fatalf("expected model filter options endpoint only, events=%d filterOptions=%d", provider.filterCalls, provider.filterOptionCalls)
	}
	if provider.lastFilter.Range != "" || provider.lastFilter.StartTime != nil || provider.lastFilter.EndTime != nil || provider.lastFilter.Model != "" || provider.lastFilter.Source != "" || provider.lastFilter.Result != "" || provider.lastFilter.Page != 0 || provider.lastFilter.PageSize != 0 {
		t.Fatalf("expected model filters endpoint to ignore query filters, got %+v", provider.lastFilter)
	}
	body := resp.Body.String()
	if body != `{"models":["claude-sonnet","gpt-5"]}` {
		t.Fatalf("expected stable model filter options, got %s", body)
	}
}

func TestUsageEventSpeedTPS(t *testing.T) {
	tests := []struct {
		name string
		row  servicedto.UsageEventRecord
		want *float64
	}{
		{"total latency", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(45)), OutputTokens: 61}, new(30.5)},
		{"reasoning included", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(45)), OutputTokens: 61, ReasoningTokens: 2}, new(30.5)},
		{"missing ttft", servicedto.UsageEventRecord{LatencyMS: 2000, OutputTokens: 61}, new(30.5)},
		{"ttft equals latency", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(2000)), OutputTokens: 61}, new(30.5)},
		{"one output token", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(45)), OutputTokens: 1}, new(0.5)},
		{"mostly reasoning", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(45)), OutputTokens: 4, ReasoningTokens: 3}, new(float64(2))},
		{"zero output", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(45))}, nil},
		{"zero ttft", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(0)), OutputTokens: 61}, new(30.5)},
		{"negative ttft", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(-45)), OutputTokens: 61}, new(30.5)},
		{"ttft exceeds latency", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(3000)), OutputTokens: 61}, new(30.5)},
		{"ttft nearly equals latency", servicedto.UsageEventRecord{LatencyMS: 2000, TTFTMS: new(int64(1999)), OutputTokens: 61}, new(30.5)},
		{"zero latency", servicedto.UsageEventRecord{OutputTokens: 61}, nil},
		{"negative latency", servicedto.UsageEventRecord{LatencyMS: -1, OutputTokens: 61}, nil},
		{"negative output", servicedto.UsageEventRecord{LatencyMS: 2000, OutputTokens: -1}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := usageEventSpeedTPS(tc.row)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil speed, got %v", *got)
				}
				return
			}
			if got == nil || !(math.Abs(*got-*tc.want) <= 0.000001) {
				t.Fatalf("expected speed %.6f, got %v", *tc.want, got)
			}
		})
	}
}

func TestUsageEventSourceFilterOptionsReturnsIdentitySources(t *testing.T) {
	provider := &usageEventsStub{}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "", OptionalProviders{UsageIdentity: usageIdentitiesStub{items: []entities.UsageIdentity{{ID: 1, Name: "Claude Main", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-source-a", Type: "openai", Provider: "Provider A", TotalRequests: 3}, {ID: 2, Name: "Provider A", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-source-b", Type: "openai", Provider: "Provider A"}, {ID: 3, Name: "Auth User", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-1", Type: "claude", Provider: "Claude", TotalRequests: 2}, {ID: 4, Name: "Zero Request User", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-zero", Type: "claude", Provider: "Claude"}, {ID: 5, Name: "Zero Provider", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-source-zero", Type: "openai", Provider: "Zero Provider"}, {ID: 6, Name: "Deleted Source", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "authidx-deleted", Type: "openai", Provider: "Deleted Provider", TotalRequests: 5, IsDeleted: true}, {ID: 7, Name: "   ", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "auth-display", Type: "claude", Provider: "Claude Display", TotalRequests: 1}}}})
	resp := serveAPIGet(router, "/api/v1/usage/events/filters/sources?range=24h&model=ignored&source=ignored&result=failed&page=3&page_size=20")

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if provider.filterOptionCalls != 0 || provider.filterCalls != 0 {
		t.Fatalf("expected source filter options endpoint to use identities only, events=%d filterOptions=%d", provider.filterCalls, provider.filterOptionCalls)
	}
	body := resp.Body.String()
	if !contains(body, `"sources":[`) || !contains(body, `"value":"authidx-source-a"`) || !contains(body, `"label":"Claude Main"`) || !contains(body, `"displayName":"Claude Main"`) || !contains(body, `"value":"auth-1"`) || !contains(body, `"label":"Auth User"`) || !contains(body, `"value":"auth-display"`) || !contains(body, `"label":"Claude Display"`) || !contains(body, `"displayName":"Claude Display"`) {
		t.Fatalf("expected stable identity source filter options with display names, got %s", body)
	}
	if contains(body, `"models"`) {
		t.Fatalf("expected source filter options endpoint not to return models, got %s", body)
	}
	if contains(body, `"value":"auth:auth-1"`) || contains(body, `"value":"provider:Provider A"`) || contains(body, `"value":"provider:1"`) || contains(body, `"value":"provider:2"`) {
		t.Fatalf("expected source filter values without prefixes, got %s", body)
	}
	if contains(body, `Zero Request User`) || contains(body, `Zero Provider`) || contains(body, `auth-zero`) || contains(body, `authidx-source-zero`) {
		t.Fatalf("expected zero-request source filter options to be omitted, got %s", body)
	}
	if contains(body, `Deleted Source`) || contains(body, `Deleted Provider`) || contains(body, `authidx-deleted`) {
		t.Fatalf("expected deleted source filter options to be omitted, got %s", body)
	}
}

//go:linkname usageEventSpeedTPS cpa-usage-keeper/internal/api.usageEventSpeedTPS
func usageEventSpeedTPS(row servicedto.UsageEventRecord) *float64

func contains(s string, sub string) bool {
	return strings.Contains(s, sub)
}
