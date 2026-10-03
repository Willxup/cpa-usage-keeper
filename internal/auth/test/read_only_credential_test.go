package test

import (
	"strings"
	"testing"
	"time"

	. "cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
)

func TestReadOnlySessionsPersistAndRevokeOnPasswordChange(t *testing.T) {
	db := openSessionStoreTestDatabase(t)
	if err := db.AutoMigrate(&entities.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	store := NewGormSessionStore(db)
	manager := NewPersistentSessionManager(7*24*time.Hour, store)
	password := "read-only-test-password"
	if err := manager.BindReadOnlyPassword(password); err != nil {
		t.Fatal(err)
	}
	token, expiry, err := manager.CreateReadOnlyWithSourceAndMetadata(SessionSourceStandard, SessionClientMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	admin, _, _ := manager.Create()
	viewer, _, _ := manager.CreateAPIKeyViewer(1)
	if err := manager.BindReadOnlyPassword(password); err != nil {
		t.Fatal(err)
	}
	var setting entities.AppSetting
	if err := db.Where("setting_key = ?", "auth.read_only.credential_hash").First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.Value == nil || !strings.HasPrefix(*setting.Value, "$2") || strings.Contains(*setting.Value, password) {
		t.Fatal("credential binding is not a salted hash")
	}
	for i := 0; i < 2; i++ {
		manager = NewPersistentSessionManager(7*24*time.Hour, store)
		if err := manager.BindReadOnlyPassword(password); err != nil {
			t.Fatal(err)
		}
		session, ok := manager.Get(token)
		if !ok || session.Role != RoleReadOnly || !session.ExpiresAt.Equal(expiry) {
			t.Fatal("restart lost session or changed expiry")
		}
		manager.Touch(token, "127.0.0.1")
		if session, _ := manager.Get(token); !session.ExpiresAt.Equal(expiry) {
			t.Fatal("activity extended expiry")
		}
	}
	if err := manager.BindReadOnlyPassword("another-read-only-password"); err != nil {
		t.Fatal(err)
	}
	if manager.Validate(token) || !manager.Validate(admin) || !manager.Validate(viewer) {
		t.Fatal("password change revoked incorrect roles")
	}
	if _, ok, err := store.Get(token); err != nil || ok {
		t.Fatal("password revocation was not persisted")
	}
	newToken, _, _ := manager.CreateReadOnlyWithSourceAndMetadata(SessionSourceStandard, SessionClientMetadata{})
	manager.DeleteByTokenHash(SessionTokenHash(newToken))
	restarted := NewPersistentSessionManager(7*24*time.Hour, store)
	if err := restarted.BindReadOnlyPassword("another-read-only-password"); err != nil {
		t.Fatal(err)
	}
	if restarted.Validate(newToken) {
		t.Fatal("restart resurrected revoked session")
	}
	disabledToken, _, _ := restarted.CreateReadOnlyWithSourceAndMetadata(SessionSourceStandard, SessionClientMetadata{})
	if err := restarted.BindReadOnlyPassword(""); err != nil {
		t.Fatal(err)
	}
	if restarted.Validate(disabledToken) {
		t.Fatal("disable did not revoke session")
	}
	if err := restarted.BindReadOnlyPassword("another-read-only-password"); err != nil {
		t.Fatal(err)
	}
	if restarted.Validate(disabledToken) {
		t.Fatal("reenable resurrected session")
	}
}

func TestReadOnlyPersistentSessionStillExpires(t *testing.T) {
	db := openSessionStoreTestDatabase(t)
	if err := db.AutoMigrate(&entities.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	store := NewGormSessionStore(db)
	manager := NewPersistentSessionManager(time.Hour, store)
	if err := manager.BindReadOnlyPassword("read-only-test-password"); err != nil {
		t.Fatal(err)
	}
	token, _, _ := manager.CreateReadOnlyWithSourceAndMetadata(SessionSourceStandard, SessionClientMetadata{})
	if err := db.Model(&entities.AuthSession{}).Where("token_hash = ?", SessionTokenHash(token)).Update("expires_at", timeutil.FormatStorageTime(time.Now().Add(-time.Hour))).Error; err != nil {
		t.Fatal(err)
	}
	restarted := NewPersistentSessionManager(time.Hour, store)
	if err := restarted.BindReadOnlyPassword("read-only-test-password"); err != nil {
		t.Fatal(err)
	}
	if restarted.Validate(token) {
		t.Fatal("expired session accepted after restart")
	}
}

func TestReadOnlyFirstBindingRevokesUnboundSessions(t *testing.T) {
	db := openSessionStoreTestDatabase(t)
	if err := db.AutoMigrate(&entities.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	manager := NewPersistentSessionManager(time.Hour, NewGormSessionStore(db))
	old, _, _ := manager.CreateReadOnlyWithSourceAndMetadata(SessionSourceStandard, SessionClientMetadata{})
	admin, _, _ := manager.Create()
	if err := manager.BindReadOnlyPassword("new-read-only-password"); err != nil {
		t.Fatal(err)
	}
	if manager.Validate(old) || !manager.Validate(admin) {
		t.Fatal("unbound sessions were trusted or the admin role was revoked")
	}
}
