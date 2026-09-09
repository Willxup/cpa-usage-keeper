package poller_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestClassifyRedisControlMessage(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		control bool
		support bool
		refresh bool
	}{
		{name: "support refresh", raw: `{"support_refresh":true}`, control: true, support: true},
		{name: "support refresh with spaces", raw: `{ "support_refresh" : true }`, control: true, support: true},
		{name: "refresh", raw: `{"refresh":true}`, control: true, support: true, refresh: true},
		{name: "both ignored", raw: `{"support_refresh":true,"refresh":true}`, control: false},
		{name: "false ignored", raw: `{"support_refresh":false,"refresh":false}`, control: false},
		{name: "usage passthrough", raw: `{"request_id":"req-1","refresh":false}`, control: false},
		{name: "usage refresh true passthrough", raw: `{"request_id":"req-1","refresh":true}`, control: false},
		{name: "usage support true passthrough", raw: `{"request_id":"req-1","support_refresh":true}`, control: false},
		{name: "invalid passthrough", raw: `{not-json`, control: false},
		{name: "array passthrough", raw: `[{"refresh":true}]`, control: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := poller.ClassifyRedisControlMessage(tt.raw)
			if got.IsControl != tt.control || got.SupportRefresh != tt.support || got.Refresh != tt.refresh {
				t.Fatalf("unexpected classification: %+v", got)
			}
		})
	}
}

func TestRedisInboxWriterSkipsEmptyMessages(t *testing.T) {
	db := openPollerTestDB(t)
	writer := poller.NewRedisInboxWriter(db)

	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceSubscribe, nil, time.Now())
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if inserted != 0 {
		t.Fatalf("expected no inserted rows, got %d", inserted)
	}

	var count int64
	if err := db.Model(&entities.RedisUsageInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count redis inbox rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no redis inbox rows, got %d", count)
	}
}

func TestRedisInboxWriterPersistsMessagesWithSource(t *testing.T) {
	db := openPollerTestDB(t)
	receivedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	writer := poller.NewRedisInboxWriter(db)

	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceSubscribe, []string{`{"request_id":"one"}`}, receivedAt)
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected one inserted row, got %d", inserted)
	}

	var rows []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("list redis inbox rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one redis inbox row, got %d", len(rows))
	}
	if rows[0].Source != poller.RedisIngestSourceSubscribe {
		t.Fatalf("expected source %q, got %q", poller.RedisIngestSourceSubscribe, rows[0].Source)
	}
	if rows[0].RawMessage != `{"request_id":"one"}` {
		t.Fatalf("unexpected raw message %q", rows[0].RawMessage)
	}
	if !rows[0].PoppedAt.Equal(receivedAt) {
		t.Fatalf("expected received at %s, got %s", receivedAt, rows[0].PoppedAt)
	}
}

func TestControlAwareRedisInboxWriterFiltersControlMessages(t *testing.T) {
	db := openPollerTestDB(t)
	observer := &controlObserverStub{}
	writer := poller.NewControlAwareRedisInboxWriter(
		poller.NewRedisInboxWriter(db),
		observer,
	)

	messages := []string{`{"support_refresh":true}`, `{"refresh":true}`, `{"request_id":"usage"}`, `{"request_id":"usage-refresh","refresh":true}`}
	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceSubscribe, messages, time.Now())
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected two usage rows, got %d", inserted)
	}
	_, support, refresh, _ := observer.counts()
	if support != 2 || refresh != 1 {
		t.Fatalf("unexpected observer calls: %+v", observer)
	}

	var rows []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("list rows: %v", err)
	}
	if len(rows) != 2 || rows[0].RawMessage != `{"request_id":"usage"}` || rows[1].RawMessage != `{"request_id":"usage-refresh","refresh":true}` {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestControlAwareRedisInboxWriterSkipsControlOnlyBatch(t *testing.T) {
	db := openPollerTestDB(t)
	observer := &controlObserverStub{}
	writer := poller.NewControlAwareRedisInboxWriter(
		poller.NewRedisInboxWriter(db),
		observer,
	)

	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceHTTPPull, []string{`{"refresh":true}`}, time.Now())
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	_, _, refresh, _ := observer.counts()
	if inserted != 0 || refresh != 1 {
		t.Fatalf("expected filtered refresh only, inserted=%d observer=%+v", inserted, observer)
	}

	var count int64
	if err := db.Model(&entities.RedisUsageInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no inbox rows, got %d", count)
	}
}

func TestControlAwareRedisInboxWriterSkipsEmptyAndNullPayloads(t *testing.T) {
	db := openPollerTestDB(t)
	observer := &controlObserverStub{}
	writer := poller.NewControlAwareRedisInboxWriter(
		poller.NewRedisInboxWriter(db),
		observer,
	)

	inserted, err := writer.Insert(context.Background(), poller.RedisIngestSourceHTTPPull, []string{"", " \n\t", " null ", `{"request_id":"usage"}`}, time.Now())
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected one usage row, got %d", inserted)
	}
	_, support, refresh, _ := observer.counts()
	if support != 0 || refresh != 0 {
		t.Fatalf("expected empty/null payloads not to trigger control observer, got %+v", observer)
	}

	var rows []entities.RedisUsageInbox
	if err := db.Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("list rows: %v", err)
	}
	if len(rows) != 1 || rows[0].RawMessage != `{"request_id":"usage"}` {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

type controlObserverStub struct {
	mu        sync.Mutex
	connected int
	support   int
	refresh   int
	polling   int
}

func (s *controlObserverStub) NotifyIngestConnected() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected++
}

func (s *controlObserverStub) MarkRefreshSupported() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.support++
}

func (s *controlObserverStub) RequestMetadataRefresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh++
}

func (s *controlObserverStub) MarkRefreshPollingRequired(string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polling++
}

func (s *controlObserverStub) counts() (connected, support, refresh, polling int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected, s.support, s.refresh, s.polling
}

func openPollerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
