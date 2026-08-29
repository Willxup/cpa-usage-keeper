package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

const (
	requestLogMaxBytes = int(cpa.RequestLogPreviewMaxBytes)
)

var (
	ErrRequestLogUnavailable = errors.New("request log unavailable")
	ErrRequestLogMissingID   = errors.New("usage event request id missing")
)

type RequestLogClient interface {
	FetchRequestLogByID(ctx context.Context, requestID string) (*cpa.RequestLogResult, error)
	OpenRequestLogByID(ctx context.Context, requestID string) (*cpa.RequestLogStream, error)
}

type RequestLogProvider interface {
	GetUsageEventRequestLog(ctx context.Context, eventID int64) (RequestLogResponse, error)
	DownloadUsageEventRequestLog(ctx context.Context, eventID int64) (RequestLogDownload, error)
}

type RequestLogResponse struct {
	EventID      int64
	RequestID    string
	Filename     string
	Available    bool
	Previewable  bool
	TooLarge     bool
	Downloadable bool
	Sections     []RequestLogSection
}

type RequestLogDownload struct {
	EventID       int64
	RequestID     string
	Filename      string
	ContentType   string
	ContentLength int64
	Body          io.ReadCloser
	Downloadable  bool
}

type RequestLogSection struct {
	Title   string
	Content string
}

type requestLogService struct {
	db       *gorm.DB
	client   RequestLogClient
	mu       sync.Mutex
	inflight map[string]*requestLogInflight
}

type requestLogInflight struct {
	done     chan struct{}
	fetchCtx context.Context
	cancel   context.CancelFunc
	waiters  int
	response RequestLogResponse
	err      error
}

func NewRequestLogService(db *gorm.DB, client RequestLogClient) RequestLogProvider {
	return &requestLogService{
		db:       db,
		client:   client,
		inflight: map[string]*requestLogInflight{},
	}
}

func (s *requestLogService) GetUsageEventRequestLog(ctx context.Context, eventID int64) (RequestLogResponse, error) {
	if s == nil {
		return RequestLogResponse{}, fmt.Errorf("request log service is nil")
	}
	if s.db == nil {
		return RequestLogResponse{}, fmt.Errorf("database is nil")
	}
	if s.client == nil {
		return RequestLogResponse{}, fmt.Errorf("request log client is not configured")
	}
	requestID, err := repository.FindUsageEventRequestIDByID(s.db.WithContext(ctx), eventID)
	if err != nil {
		return RequestLogResponse{}, err
	}
	if requestID == "" {
		return RequestLogResponse{EventID: eventID, Available: false}, ErrRequestLogMissingID
	}

	inflight, leader := s.beginFetch(requestID)
	if leader {
		go s.fetchRequestLog(requestID, inflight)
	}
	return s.waitForRequestLogFetch(ctx, eventID, requestID, inflight)
}

func (s *requestLogService) fetchRequestLog(requestID string, inflight *requestLogInflight) {
	result, err := s.client.FetchRequestLogByID(inflight.fetchCtx, requestID)
	if err != nil {
		if result != nil && result.StatusCode == http.StatusNotFound {
			response := RequestLogResponse{RequestID: requestID, Available: false}
			s.finishFetch(requestID, inflight, response, ErrRequestLogUnavailable)
			return
		}
		s.finishFetch(requestID, inflight, RequestLogResponse{}, err)
		return
	}
	if result == nil {
		err := fmt.Errorf("request log result is nil")
		s.finishFetch(requestID, inflight, RequestLogResponse{}, err)
		return
	}
	if result.BodyTruncated || len(result.Body) > requestLogMaxBytes {
		response := RequestLogResponse{
			RequestID:    requestID,
			Filename:     strings.TrimSpace(result.Filename),
			Available:    true,
			Previewable:  false,
			TooLarge:     true,
			Downloadable: true,
		}
		s.finishFetch(requestID, inflight, response, nil)
		return
	}

	raw := string(result.Body)
	response := RequestLogResponse{
		RequestID:    requestID,
		Filename:     strings.TrimSpace(result.Filename),
		Available:    true,
		Previewable:  true,
		Downloadable: true,
		Sections:     ParseRequestLogSections(raw),
	}
	s.finishFetch(requestID, inflight, response, nil)
}

func (s *requestLogService) waitForRequestLogFetch(ctx context.Context, eventID int64, requestID string, inflight *requestLogInflight) (RequestLogResponse, error) {
	select {
	case <-inflight.done:
		if err := ctx.Err(); err != nil {
			return RequestLogResponse{}, err
		}
	case <-ctx.Done():
		s.releaseFetchWaiter(requestID, inflight)
		return RequestLogResponse{}, ctx.Err()
	}
	response := inflight.response
	response.EventID = eventID
	return response, inflight.err
}

func (s *requestLogService) DownloadUsageEventRequestLog(ctx context.Context, eventID int64) (RequestLogDownload, error) {
	if s == nil {
		return RequestLogDownload{}, fmt.Errorf("request log service is nil")
	}
	if s.db == nil {
		return RequestLogDownload{}, fmt.Errorf("database is nil")
	}
	if s.client == nil {
		return RequestLogDownload{}, fmt.Errorf("request log client is not configured")
	}
	requestID, err := repository.FindUsageEventRequestIDByID(s.db.WithContext(ctx), eventID)
	if err != nil {
		return RequestLogDownload{}, err
	}
	if requestID == "" {
		return RequestLogDownload{EventID: eventID, Downloadable: false}, ErrRequestLogMissingID
	}
	result, err := s.client.OpenRequestLogByID(ctx, requestID)
	if err != nil {
		if result != nil && result.StatusCode == http.StatusNotFound {
			return RequestLogDownload{EventID: eventID, RequestID: requestID, Downloadable: false}, ErrRequestLogUnavailable
		}
		return RequestLogDownload{}, err
	}
	if result == nil {
		return RequestLogDownload{}, fmt.Errorf("request log result is nil")
	}
	return RequestLogDownload{
		EventID:       eventID,
		RequestID:     requestID,
		Filename:      strings.TrimSpace(result.Filename),
		ContentType:   strings.TrimSpace(result.ContentType),
		ContentLength: result.ContentLength,
		Body:          result.Body,
		Downloadable:  true,
	}, nil
}

func (s *requestLogService) beginFetch(requestID string) (*requestLogInflight, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight == nil {
		s.inflight = map[string]*requestLogInflight{}
	}
	if inflight, ok := s.inflight[requestID]; ok {
		inflight.waiters++
		return inflight, false
	}
	fetchCtx, cancel := context.WithCancel(context.Background())
	inflight := &requestLogInflight{
		done:     make(chan struct{}),
		fetchCtx: fetchCtx,
		cancel:   cancel,
		waiters:  1,
	}
	s.inflight[requestID] = inflight
	return inflight, true
}

func (s *requestLogService) releaseFetchWaiter(requestID string, inflight *requestLogInflight) {
	s.mu.Lock()
	if s.inflight[requestID] != inflight || inflight.waiters <= 0 {
		s.mu.Unlock()
		return
	}
	inflight.waiters--
	if inflight.waiters > 0 {
		s.mu.Unlock()
		return
	}
	delete(s.inflight, requestID)
	cancel := inflight.cancel
	s.mu.Unlock()
	cancel()
}

func (s *requestLogService) finishFetch(requestID string, inflight *requestLogInflight, response RequestLogResponse, err error) {
	s.mu.Lock()
	inflight.response = response
	inflight.err = err
	// The last waiter may have removed this entry and a replacement fetch may already exist.
	if s.inflight[requestID] == inflight {
		delete(s.inflight, requestID)
	}
	s.mu.Unlock()
	inflight.cancel()
	close(inflight.done)
}

func RequestLogPreviewMaxBytes() int {
	return requestLogMaxBytes
}

func ParseRequestLogSections(raw string) []RequestLogSection {
	lines := strings.Split(raw, "\n")
	sections := make([]RequestLogSection, 0, 8)
	currentTitle := ""
	currentLines := []string{}
	flush := func() {
		if currentTitle == "" {
			return
		}
		sections = append(sections, RequestLogSection{
			Title:   currentTitle,
			Content: strings.TrimRight(strings.Join(currentLines, "\n"), "\n"),
		})
		currentLines = []string{}
	}
	for _, line := range lines {
		title, ok := parseRequestLogSectionTitle(line)
		if ok {
			flush()
			currentTitle = title
			continue
		}
		if currentTitle != "" {
			currentLines = append(currentLines, line)
		}
	}
	flush()
	if len(sections) == 0 && strings.TrimSpace(raw) != "" {
		return []RequestLogSection{{Title: "RAW LOG", Content: raw}}
	}
	return sections
}

func parseRequestLogSectionTitle(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "===") || !strings.HasSuffix(trimmed, "===") {
		return "", false
	}
	title := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "==="), "==="))
	if title == "" {
		return "", false
	}
	return title, true
}
