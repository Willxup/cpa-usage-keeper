package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestListErrorEventsByAuthIndexUsesStableCursorAndIsolation(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "error-event-pages.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.Local)
	rows := []entities.ErrorEvent{
		{AuthIndex: "auth-a", Timestamp: now, ReceivedAt: now, Body: "newest"},
		{AuthIndex: "auth-a", Timestamp: now, ReceivedAt: now, Body: "same-time-older-id"},
		{AuthIndex: "auth-a", Timestamp: now.Add(-time.Minute), ReceivedAt: now, Body: "oldest"},
		{AuthIndex: "auth-b", Timestamp: now.Add(time.Minute), ReceivedAt: now, Body: "other-identity"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed error events: %v", err)
	}

	first, err := repository.ListErrorEventsByAuthIndex(context.Background(), db, "auth-a", nil, 2)
	if err != nil {
		t.Fatalf("first page returned error: %v", err)
	}
	if len(first.Events) != 2 || !first.HasMore || first.Events[0].ID != rows[1].ID || first.Events[1].ID != rows[0].ID {
		t.Fatalf("unexpected first page: %+v", first)
	}
	cursor := &repository.ErrorEventCursor{Timestamp: first.Events[1].Timestamp, ID: first.Events[1].ID}
	second, err := repository.ListErrorEventsByAuthIndex(context.Background(), db, "auth-a", cursor, 2)
	if err != nil {
		t.Fatalf("second page returned error: %v", err)
	}
	if len(second.Events) != 1 || second.HasMore || second.Events[0].ID != rows[2].ID {
		t.Fatalf("unexpected second page: %+v", second)
	}
}
