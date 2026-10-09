package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/timeutil"
)

func TestSessionManagerCreatesAndListsSessionSources(t *testing.T) {
	for _, tc := range []struct {
		source auth.SessionSource
		create func(*auth.SessionManager) (string, time.Time, error)
	}{
		{auth.SessionSourceStandard, (*auth.SessionManager).Create},
		{auth.SessionSourceEmbed, func(manager *auth.SessionManager) (string, time.Time, error) {
			return manager.CreateWithSource(auth.SessionSourceEmbed)
		}},
	} {
		t.Run(string(tc.source), func(t *testing.T) {
			manager := auth.NewSessionManager(time.Hour)
			token, _, err := tc.create(manager)
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			session, ok := manager.Get(token)
			if !ok || session.Source != tc.source {
				t.Fatalf("Get = %+v, %v; want source %q", session, ok, tc.source)
			}
			records := manager.List()
			if len(records) != 1 || records[0].Source != tc.source {
				t.Fatalf("List = %+v, want source %q", records, tc.source)
			}
		})
	}
}

func TestPersistentSessionManagerPreservesSessionSource(t *testing.T) {
	db := openSessionDatabase(t)
	store := auth.NewGormSessionStore(db)
	manager := auth.NewPersistentSessionManager(time.Hour, store)

	token, _, err := manager.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}

	restarted := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	session, ok := restarted.Get(token)
	if !ok {
		t.Fatal("expected persisted session to validate after restart")
	}
	if session.Source != auth.SessionSourceEmbed {
		t.Fatalf("expected persisted session source %q, got %q", auth.SessionSourceEmbed, session.Source)
	}
	records := restarted.List()
	if len(records) != 1 || records[0].Source != auth.SessionSourceEmbed {
		t.Fatalf("expected persisted list to expose embed source, got %+v", records)
	}
}

func TestPersistentSessionManagerNormalizesBlankSessionSource(t *testing.T) {
	db := openSessionDatabase(t)
	now := timeutil.NormalizeStorageTime(time.Now())
	expiresAt := now.Add(time.Hour)
	token := "blank-source-token"
	if err := db.Exec(
		"INSERT INTO auth_sessions (token_hash, role, source, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		auth.SessionTokenHash(token),
		string(auth.RoleAdmin),
		"",
		expiresAt,
		now,
		now,
	).Error; err != nil {
		t.Fatalf("insert blank source session: %v", err)
	}

	manager := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	session, ok := manager.Get(token)
	if !ok {
		t.Fatal("expected blank-source persisted session to validate")
	}
	if session.Source != auth.SessionSourceStandard {
		t.Fatalf("expected blank source to normalize to %q, got %q", auth.SessionSourceStandard, session.Source)
	}
}
