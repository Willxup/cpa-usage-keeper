package test

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

func TestUsageEventsLiveTimeBatchCountsUnmatchedRows(t *testing.T) {
	db := openTestDatabase(t)
	start := time.Date(2026, 6, 12, 10, 0, 0, 123000000, time.Local)
	end := start.Add(time.Minute)
	events := requestEventFixtureWithZeroFees([]entities.UsageEvent{
		{Timestamp: start.Add(time.Second), Model: "other"},
		{Timestamp: start.Add(2 * time.Second), Model: "other"},
		{Timestamp: start.Add(3 * time.Second), Model: "match"},
	})
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatal(err)
	}
	filter := dto.UsageQueryFilter{StartTime: &start, EndTime: &end, Model: "match"}
	rows, nextTime, nextID, count, err := repository.UsageEventsLiveTimeBatch(db, filter, events[2].ID, start, end, nil, 0, 2, emptyPricingSnapshotForTest())
	if err != nil || len(rows) != 0 || count != 2 || nextID != events[1].ID || nextTime == nil {
		t.Fatalf("unmatched rows must count toward the bounded scan: rows=%+v count=%d next=%d err=%v", rows, count, nextID, err)
	}
	rows, _, _, count, err = repository.UsageEventsLiveTimeBatch(db, filter, events[2].ID, start, end, nextTime, nextID, 2, emptyPricingSnapshotForTest())
	if err != nil || count != 1 || len(rows) != 1 || rows[0].ID != events[2].ID {
		t.Fatalf("matched row after unmatched window missing: rows=%+v count=%d err=%v", rows, count, err)
	}
}

func TestUsageEventsLiveSnapshotUsesOneReaderSnapshotAcrossConcurrentCommit(t *testing.T) {
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "live-snapshot.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		readPool, _ := reader.DB()
		_ = readPool.Close()
		writePool, _ := db.DB()
		_ = writePool.Close()
	}()
	now := time.Now()
	first := requestEventFixtureWithZeroFees([]entities.UsageEvent{{Timestamp: now, Model: "before"}})
	if _, _, err := repository.InsertUsageEvents(db, first); err != nil {
		t.Fatal(err)
	}
	reachedCount := make(chan struct{})
	continueCount := make(chan struct{})
	var once sync.Once
	if err := db.Callback().Query().Before("gorm:query").Register("test:pause_live_snapshot_count", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*int64); ok {
			once.Do(func() { close(reachedCount); <-continueCount })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("test:pause_live_snapshot_count")
	type result struct {
		page *dto.UsageEventsPageRecord
		high int64
		err  error
	}
	results := make(chan result, 1)
	go func() {
		page, high, err := repository.UsageEventsLiveSnapshot(db, dto.UsageQueryFilter{CursorMode: true, PageSize: 20}, emptyPricingSnapshotForTest())
		results <- result{page, high, err}
	}()
	select {
	case <-reachedCount:
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot did not reach count after reading its high ID")
	}
	inserted := make(chan error, 1)
	second := requestEventFixtureWithZeroFees([]entities.UsageEvent{{Timestamp: now.Add(time.Second), Model: "after"}})
	go func() { _, _, err := repository.InsertUsageEvents(db, second); inserted <- err }()
	select {
	case err := <-inserted:
		if err != nil {
			close(continueCount)
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		close(continueCount)
		t.Fatal("snapshot transaction occupied the SQLite writer")
	}
	close(continueCount)
	got := <-results
	if got.err != nil || got.high != first[0].ID || got.page.TotalCount != 1 || len(got.page.Events) != 1 || got.page.Events[0].ID != first[0].ID {
		t.Fatalf("snapshot and high ID crossed commits: %+v", got)
	}
	rows, high, scanned, err := repository.UsageEventsLiveIDBatch(db, dto.UsageQueryFilter{}, got.high, 100, emptyPricingSnapshotForTest())
	if err != nil || scanned != 1 || high != second[0].ID || len(rows) != 1 || rows[0].ID != second[0].ID {
		t.Fatalf("committed event not recoverable after snapshot: rows=%+v high=%d scanned=%d err=%v", rows, high, scanned, err)
	}
}
