package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type requestLogClientStub struct {
	mu     sync.Mutex
	calls  int
	result *cpa.RequestLogResult
	err    error

	downloadCalls  int
	downloadResult *cpa.RequestLogStream
	downloadErr    error
	started        chan struct{}
	block          chan struct{}
	fetchCanceled  chan struct{}
}

func (s *requestLogClientStub) FetchRequestLogByID(ctx context.Context, _ string) (*cpa.RequestLogResult, error) {
	s.mu.Lock()
	s.calls++
	result := s.result
	err := s.err
	started := s.started
	block := s.block
	fetchCanceled := s.fetchCanceled
	s.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			if fetchCanceled != nil {
				select {
				case fetchCanceled <- struct{}{}:
				default:
				}
			}
			return nil, ctx.Err()
		}
	}
	return result, err
}

func (s *requestLogClientStub) fetchCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *requestLogClientStub) OpenRequestLogByID(context.Context, string) (*cpa.RequestLogStream, error) {
	s.mu.Lock()
	s.downloadCalls++
	result := s.result
	err := s.err
	downloadResult := s.downloadResult
	downloadErr := s.downloadErr
	s.mu.Unlock()
	if downloadResult != nil || downloadErr != nil {
		return downloadResult, downloadErr
	}
	if result == nil {
		return nil, err
	}
	return &cpa.RequestLogStream{
		StatusCode:    result.StatusCode,
		Filename:      result.Filename,
		ContentType:   result.ContentType,
		ContentLength: int64(len(result.Body)),
		Body:          io.NopCloser(bytes.NewReader(result.Body)),
	}, err
}

func TestRequestLogServiceLoadsEventLogWithoutCachingByRequestID(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "event-1",
		RequestID:   "req-log-1",
		Timestamp:   time.Date(2026, 7, 8, 12, 0, 0, 0, time.Local),
		Model:       "claude-sonnet",
		Source:      "source",
		AuthIndex:   "auth-1",
		APIGroupKey: "group",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode: http.StatusOK,
		Filename:   "v1-responses-req-log-1.log",
		Body:       []byte("=== REQUEST INFO ===\nURL: /v1/responses\n=== API RESPONSE ===\n{\"ok\":true}\n"),
	}}
	provider := service.NewRequestLogService(db, client)

	first, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if first.RequestID != "req-log-1" || first.Filename != "v1-responses-req-log-1.log" {
		t.Fatalf("unexpected first response: %+v", first)
	}
	if len(first.Sections) != 2 || first.Sections[0].Title != "REQUEST INFO" || first.Sections[1].Title != "API RESPONSE" {
		t.Fatalf("unexpected sections: %+v", first.Sections)
	}

	_, err = provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("second GetUsageEventRequestLog returned error: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("expected repeated CPA calls without cache, got %d", client.calls)
	}
}

func TestRequestLogServiceMapsCPANotFoundToUnavailable(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-404",
		RequestID: "req-missing",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result: &cpa.RequestLogResult{StatusCode: http.StatusNotFound, Body: []byte(`{"error":"missing"}`)},
		err:    errors.New("management request log request returned status 404"),
	}
	provider := service.NewRequestLogService(db, client)

	_, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogUnavailable) {
		t.Fatalf("expected ErrRequestLogUnavailable, got %v", err)
	}

	_, err = provider.GetUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogUnavailable) {
		t.Fatalf("expected second ErrRequestLogUnavailable, got %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("expected repeated 404 responses to refetch without cache, got %d calls", client.calls)
	}
}

func TestRequestLogServiceCoalescesConcurrentPreviewMisses(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "event-singleflight-1", RequestID: "req-singleflight"},
		{EventKey: "event-singleflight-2", RequestID: "req-singleflight"},
	}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result:  &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\ncoalesced\n")},
		started: make(chan struct{}, 2),
		block:   make(chan struct{}),
	}
	provider := service.NewRequestLogService(db, client)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, eventID := range []int64{1, 2} {
		wg.Add(1)
		go func(eventID int64) {
			defer wg.Done()
			response, err := provider.GetUsageEventRequestLog(context.Background(), eventID)
			if err == nil && response.Sections[0].Content != "coalesced" {
				err = errors.New("unexpected response content")
			}
			if err == nil && response.EventID != eventID {
				err = fmt.Errorf("expected event id %d, got %d", eventID, response.EventID)
			}
			errs <- err
		}(eventID)
	}
	<-client.started
	time.Sleep(20 * time.Millisecond)
	close(client.block)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent GetUsageEventRequestLog returned error: %v", err)
		}
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected concurrent miss to fetch once, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceCancelsPreviewFetchWhenOnlyWaiterCancels(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-singleflight-only-waiter-cancel",
		RequestID: "req-singleflight-only-waiter-cancel",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result:        &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nunused\n")},
		started:       make(chan struct{}, 1),
		block:         make(chan struct{}),
		fetchCanceled: make(chan struct{}, 1),
	}
	provider := service.NewRequestLogService(db, client)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageEventRequestLog(ctx, 1)
		errCh <- err
	}()
	<-client.started
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled waiter to return context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		close(client.block)
		<-errCh
		t.Fatal("expected canceled waiter to return promptly")
	}

	select {
	case <-client.fetchCanceled:
	case <-time.After(time.Second):
		close(client.block)
		t.Fatal("expected the CPA fetch to be canceled after the last waiter left")
	}

	client.mu.Lock()
	client.block = nil
	client.mu.Unlock()
	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected a fresh fetch after cancellation, got %v", err)
	}
	if len(response.Sections) != 1 || response.Sections[0].Content != "unused" {
		t.Fatalf("unexpected retry response: %+v", response)
	}
	if client.fetchCalls() != 2 {
		t.Fatalf("expected cancellation cleanup to allow a new fetch, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceAllowsPreviewAtSixMiBLimit(t *testing.T) {
	const sixMiB = 6 * 1024 * 1024
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-six-mib",
		RequestID: "req-six-mib",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode: http.StatusOK,
		Body:       bytes.Repeat([]byte("x"), sixMiB),
	}}
	provider := service.NewRequestLogService(db, client)

	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if got := service.RequestLogPreviewMaxBytes(); got != sixMiB {
		t.Fatalf("expected preview limit %d, got %d", sixMiB, got)
	}
	if response.TooLarge || !response.Previewable || len(response.Sections) != 1 {
		t.Fatalf("expected six MiB response to remain previewable, got %+v", response)
	}
	if len(response.Sections[0].Content) != sixMiB {
		t.Fatalf("expected full six MiB preview content, got %d bytes", len(response.Sections[0].Content))
	}
}

func TestRequestLogServiceTreatsOversizedBodyAsTooLargeWithoutTruncatedFlag(t *testing.T) {
	const sixMiB = 6 * 1024 * 1024
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-six-mib-plus-one",
		RequestID: "req-six-mib-plus-one",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode:    http.StatusOK,
		Body:          bytes.Repeat([]byte("x"), sixMiB+1),
		BodyTruncated: false,
	}}
	provider := service.NewRequestLogService(db, client)

	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if !response.TooLarge || response.Previewable || !response.Downloadable || len(response.Sections) != 0 {
		t.Fatalf("expected oversized body fallback to require download, got %+v", response)
	}
}

func TestRequestLogServiceCoalescedPreviewReturnsLeaderCancellationWithoutAbortingFollower(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-singleflight-leader-cancel",
		RequestID: "req-singleflight-leader-cancel",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result:  &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nleader survived\n")},
		started: make(chan struct{}, 1),
		block:   make(chan struct{}),
	}
	provider := service.NewRequestLogService(db, client)
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		response, err := provider.GetUsageEventRequestLog(leaderCtx, 1)
		if err == nil && response.Sections[0].Content != "leader survived" {
			err = errors.New("unexpected leader response content")
		}
		leaderErr <- err
	}()
	<-client.started

	followerErr := make(chan error, 1)
	go func() {
		response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
		if err == nil && response.Sections[0].Content != "leader survived" {
			err = errors.New("unexpected follower response content")
		}
		followerErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancelLeader()
	select {
	case err := <-leaderErr:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected canceled leader to return context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("expected canceled leader to return promptly while the shared fetch continues")
	}
	close(client.block)

	if err := <-followerErr; err != nil {
		t.Fatalf("expected follower to receive shared fetch result, got %v", err)
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected shared fetch to run once, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceCancelsSharedPreviewAfterLastConcurrentWaiterLeaves(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "event-singleflight-all-cancel-1", RequestID: "req-singleflight-all-cancel"},
		{EventKey: "event-singleflight-all-cancel-2", RequestID: "req-singleflight-all-cancel"},
	}); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}

	client := &requestLogClientStub{
		result:        &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nunused\n")},
		started:       make(chan struct{}, 1),
		block:         make(chan struct{}),
		fetchCanceled: make(chan struct{}, 1),
	}
	provider := service.NewRequestLogService(db, client)
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	followerCtx, cancelFollower := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	followerErr := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageEventRequestLog(leaderCtx, 1)
		leaderErr <- err
	}()
	<-client.started
	go func() {
		_, err := provider.GetUsageEventRequestLog(followerCtx, 2)
		followerErr <- err
	}()
	time.Sleep(20 * time.Millisecond)

	cancelLeader()
	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected leader cancellation, got %v", err)
	}
	select {
	case <-client.fetchCanceled:
		t.Fatal("expected shared fetch to remain active while the follower is waiting")
	case <-time.After(50 * time.Millisecond):
	}

	cancelFollower()
	if err := <-followerErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected follower cancellation, got %v", err)
	}
	select {
	case <-client.fetchCanceled:
	case <-time.After(time.Second):
		close(client.block)
		t.Fatal("expected the last waiter to cancel the shared fetch")
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected concurrent waiters to share one fetch, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceCoalescedPreviewFollowerCanCancelIndependently(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-singleflight-follower-cancel",
		RequestID: "req-singleflight-follower-cancel",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result:  &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nfollower survived\n")},
		started: make(chan struct{}, 1),
		block:   make(chan struct{}),
	}
	provider := service.NewRequestLogService(db, client)
	leaderErr := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageEventRequestLog(context.Background(), 1)
		leaderErr <- err
	}()
	<-client.started

	followerCtx, cancelFollower := context.WithCancel(context.Background())
	followerErr := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageEventRequestLog(followerCtx, 1)
		followerErr <- err
	}()
	cancelFollower()
	if err := <-followerErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected follower to stop waiting on context cancellation, got %v", err)
	}

	close(client.block)
	if err := <-leaderErr; err != nil {
		t.Fatalf("expected leader fetch to complete after follower cancellation, got %v", err)
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected shared fetch to run once, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceHandlesLargePreviewAsDownloadable(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-large",
		RequestID: "req-large",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode:    http.StatusOK,
		Filename:      "large-request.log",
		Body:          make([]byte, service.RequestLogPreviewMaxBytes()+1),
		BodyTruncated: true,
		ContentType:   "text/plain",
		ContentLength: int64(service.RequestLogPreviewMaxBytes() + 1),
	}}
	provider := service.NewRequestLogService(db, client)

	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if !response.TooLarge || !response.Downloadable || response.Previewable || len(response.Sections) != 0 {
		t.Fatalf("unexpected large preview response: %+v", response)
	}
	if response.Filename != "large-request.log" {
		t.Fatalf("unexpected filename %q", response.Filename)
	}
}

func TestRequestLogServiceTooLargePreviewRefetchesWithoutCache(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-large-expire",
		RequestID: "req-large-expire",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode:    http.StatusOK,
		Filename:      "large-request.log",
		BodyTruncated: true,
		ContentType:   "text/plain",
		ContentLength: int64(service.RequestLogPreviewMaxBytes() + 1),
	}}
	provider := service.NewRequestLogService(db, client)

	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if !response.TooLarge {
		t.Fatalf("expected initial too-large response, got %+v", response)
	}

	client.mu.Lock()
	client.result = &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nsmall again\n")}
	client.mu.Unlock()
	response, err = provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected too-large response to refetch successfully, got %v", err)
	}
	if response.TooLarge || len(response.Sections) != 1 || response.Sections[0].Content != "small again" {
		t.Fatalf("expected refetched preview response, got %+v", response)
	}
	if client.fetchCalls() != 2 {
		t.Fatalf("expected too-large response to refetch without cache, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceDownloadFetchesRawBody(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-download",
		RequestID: "req-download",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{downloadResult: &cpa.RequestLogStream{
		StatusCode:    http.StatusOK,
		Filename:      "download.log",
		ContentType:   "text/plain; charset=utf-8",
		ContentLength: 7,
		Body:          io.NopCloser(bytes.NewBufferString("raw log")),
	}}
	provider := service.NewRequestLogService(db, client)
	downloader, ok := provider.(interface {
		DownloadUsageEventRequestLog(context.Context, int64) (service.RequestLogDownload, error)
	})
	if !ok {
		t.Fatalf("request log provider does not support downloads")
	}

	download, err := downloader.DownloadUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("DownloadUsageEventRequestLog returned error: %v", err)
	}
	body, err := io.ReadAll(download.Body)
	if err != nil {
		t.Fatalf("read download body: %v", err)
	}
	_ = download.Body.Close()
	if string(body) != "raw log" || download.Filename != "download.log" || download.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected download response: %+v", download)
	}
	if client.downloadCalls != 1 {
		t.Fatalf("expected one raw download call, got %d", client.downloadCalls)
	}
}

func openRequestLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "request-log.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}
