package auth

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSessionManagerCreateValidateDelete(t *testing.T) {
	manager := NewSessionManager(2 * time.Hour)
	manager.now = func() time.Time { return time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC) }
	manager.generate = func() (string, error) { return "token-1", nil }

	token, expiresAt, err := manager.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if token != "token-1" {
		t.Fatalf("expected token token-1, got %q", token)
	}
	if !expiresAt.Equal(time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected expiry: %s", expiresAt)
	}
	if !manager.Validate(token) {
		t.Fatal("expected token to validate")
	}

	manager.Delete(token)
	if manager.Validate(token) {
		t.Fatal("expected deleted token to fail validation")
	}
}

func TestSessionManagerCreateReturnsAdminSessionMetadata(t *testing.T) {
	manager := NewSessionManager(2 * time.Hour)
	manager.now = func() time.Time { return time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC) }
	manager.generate = func() (string, error) { return "token-admin", nil }

	token, expiresAt, err := manager.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	session, ok := manager.Get(token)
	if !ok {
		t.Fatal("expected session metadata to be available")
	}
	if session.Role != RoleAdmin {
		t.Fatalf("expected admin role, got %q", session.Role)
	}
	if session.CPAAPIKeyID != 0 {
		t.Fatalf("expected admin session to have no API key binding, got %d", session.CPAAPIKeyID)
	}
	if !session.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected session expiry %s, got %s", expiresAt, session.ExpiresAt)
	}
}

func TestSessionManagerCreateAPIKeyViewerBindsKeyID(t *testing.T) {
	manager := NewSessionManager(2 * time.Hour)
	manager.now = func() time.Time { return time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC) }
	manager.generate = func() (string, error) { return "token-viewer", nil }

	token, expiresAt, err := manager.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}

	session, ok := manager.Get(token)
	if !ok {
		t.Fatal("expected viewer session metadata to be available")
	}
	if session.Role != RoleAPIKeyViewer {
		t.Fatalf("expected api key viewer role, got %q", session.Role)
	}
	if session.CPAAPIKeyID != 42 {
		t.Fatalf("expected API key binding 42, got %d", session.CPAAPIKeyID)
	}
	if !session.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected session expiry %s, got %s", expiresAt, session.ExpiresAt)
	}
}

func TestSessionManagerRejectsExpiredSessions(t *testing.T) {
	baseTime := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	manager := NewSessionManager(30 * time.Minute)
	manager.now = func() time.Time { return baseTime }
	manager.generate = func() (string, error) { return "token-2", nil }

	token, _, err := manager.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	manager.now = func() time.Time { return baseTime.Add(31 * time.Minute) }
	if manager.Validate(token) {
		t.Fatal("expected expired token to fail validation")
	}
}

func TestSessionManagerCleanupExpired(t *testing.T) {
	baseTime := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	manager := NewSessionManager(time.Hour)
	manager.now = func() time.Time { return baseTime }
	manager.generate = func() (string, error) { return "token-3", nil }

	if _, _, err := manager.Create(); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	manager.mu.Lock()
	manager.sessions["expired"] = Session{Role: RoleAdmin, ExpiresAt: baseTime.Add(-time.Minute)}
	manager.mu.Unlock()

	manager.CleanupExpired()

	manager.mu.RLock()
	_, expiredExists := manager.sessions["expired"]
	_, activeExists := manager.sessions["token-3"]
	manager.mu.RUnlock()

	if expiredExists {
		t.Fatal("expected expired token to be removed")
	}
	if !activeExists {
		t.Fatal("expected active token to remain")
	}
}

func TestPersistentSessionManagerLoadsSessionAfterRestart(t *testing.T) {
	db := openSessionStoreTestDatabase(t)
	store := NewGormSessionStore(db)
	baseTime := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	manager := NewPersistentSessionManager(2*time.Hour, store)
	manager.now = func() time.Time { return baseTime }
	manager.generate = func() (string, error) { return "persisted-token", nil }

	token, expiresAt, err := manager.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	var row entities.AuthSession
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("load persisted auth session: %v", err)
	}
	if row.TokenHash == token {
		t.Fatal("expected persisted session token hash not to equal raw token")
	}
	if row.TokenHash != sessionTokenHash(token) {
		t.Fatalf("expected persisted token hash %q, got %q", sessionTokenHash(token), row.TokenHash)
	}

	restarted := NewPersistentSessionManager(2*time.Hour, store)
	restarted.now = func() time.Time { return baseTime.Add(time.Minute) }
	session, ok := restarted.Get(token)
	if !ok {
		t.Fatal("expected persisted session to validate after manager restart")
	}
	if session.Role != RoleAPIKeyViewer || session.CPAAPIKeyID != 42 {
		t.Fatalf("unexpected persisted session metadata: %+v", session)
	}
	if !session.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected persisted expiry %s, got %s", expiresAt, session.ExpiresAt)
	}
}

func TestPersistentSessionManagerDeletesExpiredPersistedSession(t *testing.T) {
	db := openSessionStoreTestDatabase(t)
	store := NewGormSessionStore(db)
	baseTime := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	if err := store.Save("expired-token", Session{Role: RoleAdmin, ExpiresAt: baseTime.Add(-time.Minute)}); err != nil {
		t.Fatalf("save expired session: %v", err)
	}
	manager := NewPersistentSessionManager(time.Hour, store)
	manager.now = func() time.Time { return baseTime }

	if manager.Validate("expired-token") {
		t.Fatal("expected expired persisted session to fail validation")
	}
	var count int64
	if err := db.Model(&entities.AuthSession{}).Where("token_hash = ?", sessionTokenHash("expired-token")).Count(&count).Error; err != nil {
		t.Fatalf("count auth sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected expired persisted session to be deleted, got %d rows", count)
	}
}

func openSessionStoreTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
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
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close sqlite database: %v", err)
		}
	})
	return db
}
