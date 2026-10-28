package test

import (
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

func TestOIDCStartupCleanupIdentityAndSubjectBoundaries(t *testing.T) {
	db := openSessionDatabase(t)
	store := auth.NewGormSessionStore(db)
	policy := auth.OIDCSessionPolicy{Enabled: true, Issuer: "https://issuer.example/realm", ClientID: "keeper", AllowedSubjects: []string{"User,A", " spaced "}}
	for _, tc := range []struct {
		name   string
		change func(*auth.Session)
		keep   bool
	}{
		{"valid", func(*auth.Session) {}, true},
		{"spaces", func(s *auth.Session) { s.OIDCSubject = " spaced " }, true},
		{"case", func(s *auth.Session) { s.OIDCSubject = "user,A" }, false},
		{"trimmed", func(s *auth.Session) { s.OIDCSubject = "spaced" }, false},
		{"issuer", func(s *auth.Session) { s.OIDCIssuer += "/" }, false},
		{"client", func(s *auth.Session) { s.OIDCClientID = "Keeper" }, false},
		{"missing-subject", func(s *auth.Session) { s.OIDCSubject = "" }, false},
		{"missing-issuer", func(s *auth.Session) { s.OIDCIssuer = "" }, false},
		{"missing-client", func(s *auth.Session) { s.OIDCClientID = "" }, false},
		{"viewer", func(s *auth.Session) { s.Role = auth.RoleAPIKeyViewer }, false},
		{"standard", func(s *auth.Session) { s.Source = auth.SessionSourceStandard; s.OIDCIssuer = "" }, true},
		{"embed", func(s *auth.Session) { s.Source = auth.SessionSourceEmbed; s.OIDCIssuer = "" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := auth.Session{Role: auth.RoleAdmin, Source: auth.SessionSourceOIDC, OIDCIssuer: policy.Issuer, OIDCClientID: policy.ClientID, OIDCSubject: "User,A", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
			tc.change(&session)
			if err := store.Save(tc.name, session); err != nil {
				t.Fatal(err)
			}
			if _, err := store.RevokeInvalidOIDCSessions(policy); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&entities.AuthSession{}).Where("token_hash = ?", auth.SessionTokenHash(tc.name)).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if (count == 1) != tc.keep {
				t.Fatalf("remaining rows = %d, keep=%v", count, tc.keep)
			}
		})
	}
}

func TestOIDCStartupCleanupBatchesAndRollsBack(t *testing.T) {
	originalHooks := logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))
	hook := logtest.NewGlobal()
	t.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(originalHooks) })
	db := openSessionDatabase(t)
	store := auth.NewGormSessionStore(db)
	policy := auth.OIDCSessionPolicy{Enabled: true, Issuer: "issuer", ClientID: "keeper", AllowedSubjects: []string{"allowed"}}
	for index := range 450 {
		subject := "denied"
		if index == 449 {
			subject = "allowed"
		}
		if err := store.Save(fmt.Sprint(index), auth.Session{Role: auth.RoleAdmin, Source: auth.SessionSourceOIDC, OIDCIssuer: "issuer", OIDCClientID: "keeper", OIDCSubject: subject, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Save("bad-identity", auth.Session{Role: auth.RoleAdmin, Source: auth.SessionSourceOIDC, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_subject_cleanup BEFORE DELETE ON auth_sessions WHEN OLD.oidc_subject = 'denied' BEGIN SELECT RAISE(ABORT, 'cleanup unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.RevokeInvalidOIDCSessions(policy); err == nil || deleted != 0 {
		t.Fatalf("cleanup = %d, %v", deleted, err)
	}
	for _, entry := range hook.AllEntries() {
		if entry.Message == "Revoked OIDC sessions during startup" {
			t.Fatal("rolled-back cleanup was logged as successful revocation")
		}
	}
	var count int64
	if err := db.Model(&entities.AuthSession{}).Count(&count).Error; err != nil || count != 451 {
		t.Fatalf("failed cleanup did not roll back: %d, %v", count, err)
	}
	if err := db.Exec("DROP TRIGGER fail_subject_cleanup").Error; err != nil {
		t.Fatal(err)
	}
	if deleted, err := store.RevokeInvalidOIDCSessions(policy); err != nil || deleted != 450 {
		t.Fatalf("cleanup = %d, %v", deleted, err)
	}
	if deleted, err := store.RevokeInvalidOIDCSessions(policy); err != nil || deleted != 0 {
		t.Fatalf("repeated cleanup = %d, %v", deleted, err)
	}
	revocations := 0
	for _, entry := range hook.AllEntries() {
		if entry.Message != "Revoked OIDC sessions during startup" {
			continue
		}
		revocations++
		if entry.Data["revoked"] != int64(450) || entry.Data["identity_invalid"] != int64(1) || entry.Data["subject_denied"] != int64(449) {
			t.Fatalf("unexpected cleanup summary: %+v", entry.Data)
		}
	}
	if revocations != 1 {
		t.Fatalf("cleanup summary count = %d, want one across rollback, success and no-op", revocations)
	}
	// 恢复原准入规则也不能复活已撤销 token；无需调用管理列表或旧 token 的鉴权路径来触发删除。
	manager := auth.NewPersistentSessionManager(time.Hour, store)
	policy.AllowedSubjects = nil
	manager.SetOIDCPolicy(policy)
	if manager.Validate("0") || !manager.Validate("449") {
		t.Fatal("revoked token revived or valid token lost")
	}
}
