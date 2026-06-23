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

func TestSessionManagerListsSessionsAndRevokesAdminGroup(t *testing.T) {
	baseTime := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	tokens := []string{"admin-token-1", "admin-token-2", "viewer-token"}
	manager := NewSessionManager(2 * time.Hour)
	manager.now = func() time.Time { return baseTime }
	manager.generate = func() (string, error) {
		token := tokens[0]
		tokens = tokens[1:]
		return token, nil
	}

	adminToken1, _, err := manager.Create()
	if err != nil {
		t.Fatalf("Create admin 1 returned error: %v", err)
	}
	adminToken2, _, err := manager.Create()
	if err != nil {
		t.Fatalf("Create admin 2 returned error: %v", err)
	}
	viewerToken, _, err := manager.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}

	records := manager.List()
	if len(records) != 3 {
		t.Fatalf("expected three active sessions, got %+v", records)
	}
	for _, record := range records {
		if record.TokenHash == "" || record.TokenHash == adminToken1 || record.TokenHash == adminToken2 || record.TokenHash == viewerToken {
			t.Fatalf("expected list to expose only token hashes, got %+v", record)
		}
		if !record.CreatedAt.Equal(baseTime) {
			t.Fatalf("expected CreatedAt %s, got %s", baseTime, record.CreatedAt)
		}
	}

	result := manager.DeleteByRole(RoleAdmin)
	if result.Deleted != 2 {
		t.Fatalf("expected two admin sessions to be deleted, got %+v", result)
	}
	if manager.Validate(adminToken1) || manager.Validate(adminToken2) {
		t.Fatal("expected admin sessions to be invalid after role revoke")
	}
	if !manager.Validate(viewerToken) {
		t.Fatal("expected viewer session to remain valid after admin role revoke")
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

	restartedStore := &trackingSessionStore{store: store}
	restarted := NewPersistentSessionManager(2*time.Hour, restartedStore)
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
	if restartedStore.getCalls != 1 {
		t.Fatalf("expected first restart lookup to load from store once, got %d", restartedStore.getCalls)
	}

	session, ok = restarted.Get(token)
	if !ok {
		t.Fatal("expected cached persisted session to validate")
	}
	if session.CPAAPIKeyID != 42 {
		t.Fatalf("unexpected cached session metadata: %+v", session)
	}
	if restartedStore.getCalls != 1 {
		t.Fatalf("expected cached restart lookup not to hit store again, got %d calls", restartedStore.getCalls)
	}
}

func TestPersistentSessionManagerDeleteByTokenHashClearsStoreAndCache(t *testing.T) {
	db := openSessionStoreTestDatabase(t)
	store := NewGormSessionStore(db)
	baseTime := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	manager := NewPersistentSessionManager(2*time.Hour, store)
	manager.now = func() time.Time { return baseTime }
	manager.generate = func() (string, error) { return "persisted-viewer-token", nil }

	token, _, err := manager.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("CreateAPIKeyViewer returned error: %v", err)
	}
	if !manager.Validate(token) {
		t.Fatal("expected created session to validate before revoke")
	}

	result := manager.DeleteByTokenHash(sessionTokenHash(token))
	if result.Deleted != 1 {
		t.Fatalf("expected one session to be deleted, got %+v", result)
	}
	if manager.Validate(token) {
		t.Fatal("expected revoked persisted session to fail validation")
	}
	var count int64
	if err := db.Model(&entities.AuthSession{}).Where("token_hash = ?", sessionTokenHash(token)).Count(&count).Error; err != nil {
		t.Fatalf("count auth sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected revoked persisted session to be deleted from store, got %d rows", count)
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

type trackingSessionStore struct {
	store    SessionStore
	getCalls int
}

func (s *trackingSessionStore) Save(token string, session Session) error {
	return s.store.Save(token, session)
}

func (s *trackingSessionStore) Get(token string) (Session, bool, error) {
	s.getCalls++
	return s.store.Get(token)
}

func (s *trackingSessionStore) List(now time.Time) ([]SessionRecord, error) {
	return s.store.List(now)
}

func (s *trackingSessionStore) Delete(token string) error {
	return s.store.Delete(token)
}

func (s *trackingSessionStore) DeleteByTokenHash(tokenHash string) (int64, error) {
	return s.store.DeleteByTokenHash(tokenHash)
}

func (s *trackingSessionStore) DeleteByRole(role Role) (int64, error) {
	return s.store.DeleteByRole(role)
}

func (s *trackingSessionStore) DeleteExpired(now time.Time) error {
	return s.store.DeleteExpired(now)
}
