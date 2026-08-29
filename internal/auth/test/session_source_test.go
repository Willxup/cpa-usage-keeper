package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSessionManagerCreatesStandardSessionByDefault(t *testing.T) {
	manager := auth.NewSessionManager(time.Hour)

	token, _, err := manager.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	session, ok := manager.Get(token)
	if !ok {
		t.Fatal("expected created session to validate")
	}
	if session.Source != auth.SessionSourceStandard {
		t.Fatalf("expected default session source %q, got %q", auth.SessionSourceStandard, session.Source)
	}
	records := manager.List()
	if len(records) != 1 || records[0].Source != auth.SessionSourceStandard {
		t.Fatalf("expected listed session to expose standard source, got %+v", records)
	}
}

func TestSessionManagerCreatesEmbedSessionWithSource(t *testing.T) {
	manager := auth.NewSessionManager(time.Hour)

	token, _, err := manager.CreateWithSource(auth.SessionSourceEmbed)
	if err != nil {
		t.Fatalf("CreateWithSource returned error: %v", err)
	}
	session, ok := manager.Get(token)
	if !ok {
		t.Fatal("expected created session to validate")
	}
	if session.Source != auth.SessionSourceEmbed {
		t.Fatalf("expected embed session source, got %q", session.Source)
	}
	records := manager.List()
	if len(records) != 1 || records[0].Source != auth.SessionSourceEmbed {
		t.Fatalf("expected listed session to expose embed source, got %+v", records)
	}
}

func TestPersistentSessionManagerPreservesSessionSource(t *testing.T) {
	db := openAuthSourceDatabase(t)
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
	db := openAuthSourceDatabase(t)
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

func openAuthSourceDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth-source.db")), &gorm.Config{})
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
