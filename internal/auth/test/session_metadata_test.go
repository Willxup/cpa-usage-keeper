package test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPersistentSessionManagerPreservesAndTouchesClientMetadata(t *testing.T) {
	db := openSessionMetadataDatabase(t)
	store := auth.NewGormSessionStore(db)
	manager := auth.NewPersistentSessionManager(time.Hour, store)
	metadata := auth.SessionClientMetadata{
		IP:        "203.0.113.9",
		UserAgent: "Keeper-Test/" + strings.Repeat("a", 600) + "/tail-marker",
	}

	token, _, err := manager.CreateWithSourceAndMetadata(auth.SessionSourceStandard, metadata)
	if err != nil {
		t.Fatalf("create session with metadata: %v", err)
	}
	session, ok := manager.Get(token)
	if !ok {
		t.Fatal("expected created session to validate")
	}
	if session.LoginIP != metadata.IP || session.LastSeenIP != metadata.IP || session.UserAgent != metadata.UserAgent {
		t.Fatalf("unexpected created metadata: %+v", session)
	}
	if session.LastSeenAt.IsZero() || !session.LastSeenAt.Equal(session.CreatedAt) {
		t.Fatalf("expected last seen to start at login time, got %+v", session)
	}

	restarted := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	persisted, ok := restarted.Get(token)
	if !ok {
		t.Fatal("expected persisted session to validate")
	}
	if persisted.LoginIP != metadata.IP || persisted.LastSeenIP != metadata.IP || persisted.UserAgent != metadata.UserAgent {
		t.Fatalf("unexpected persisted metadata: %+v", persisted)
	}

	if restarted.Touch(token, metadata.IP) {
		t.Fatal("expected an immediate request from the same IP to stay inside the write throttle window")
	}
	if !restarted.Touch(token, "198.51.100.42") {
		t.Fatal("expected an IP change to update recent activity immediately")
	}
	changed, ok := restarted.Get(token)
	if !ok || changed.LastSeenIP != "198.51.100.42" {
		t.Fatalf("expected changed recent IP, got %+v", changed)
	}
	waitForPersistedSessionActivity(t, db, token, "198.51.100.42", changed.LastSeenAt)

	oldSeenAt := timeutil.NormalizeStorageTime(time.Now().Add(-2 * time.Minute))
	if err := db.Model(&entities.AuthSession{}).
		Where("token_hash = ?", auth.SessionTokenHash(token)).
		Updates(map[string]any{"last_seen_at": oldSeenAt, "last_seen_ip": "198.51.100.42"}).Error; err != nil {
		t.Fatalf("age persisted session activity: %v", err)
	}
	aged := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	if _, ok := aged.Get(token); !ok {
		t.Fatal("expected aged session to validate")
	}
	if !aged.Touch(token, "198.51.100.42") {
		t.Fatal("expected activity outside the throttle window to persist")
	}
	touched, ok := aged.Get(token)
	if !ok || !touched.LastSeenAt.After(oldSeenAt) {
		t.Fatalf("expected last seen time to advance, got %+v", touched)
	}
	waitForPersistedSessionActivity(t, db, token, "198.51.100.42", touched.LastSeenAt)
}

func waitForPersistedSessionActivity(t *testing.T, db *gorm.DB, token, lastSeenIP string, lastSeenAt time.Time) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		var row entities.AuthSession
		err := db.Where("token_hash = ?", auth.SessionTokenHash(token)).First(&row).Error
		if err == nil && row.LastSeenIP == lastSeenIP && row.LastSeenAt != nil && !row.LastSeenAt.Before(lastSeenAt) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for persisted session activity: row=%+v err=%v", row, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func openSessionMetadataDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "session-metadata.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.AutoMigrate(&entities.AuthSession{}); err != nil {
		t.Fatalf("auto migrate auth sessions: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
