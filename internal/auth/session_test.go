package auth

import (
	"testing"
	"time"
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
	manager.sessions["expired"] = baseTime.Add(-time.Minute)
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
