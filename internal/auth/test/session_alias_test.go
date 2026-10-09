package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/entities"
)

func TestSessionManagerUpdatesAndPersistsAdminAliasByTokenHash(t *testing.T) {
	db := openSessionDatabase(t)
	manager := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	adminToken, _, err := manager.Create()
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	viewerToken, _, err := manager.CreateAPIKeyViewer(42)
	if err != nil {
		t.Fatalf("create API key session: %v", err)
	}

	if !manager.UpdateAdminAliasByTokenHash(auth.SessionTokenHash(adminToken), "Office Mac") {
		t.Fatal("expected admin alias update to succeed")
	}
	if manager.UpdateAdminAliasByTokenHash(auth.SessionTokenHash(viewerToken), "Must Not Apply") {
		t.Fatal("expected API key session alias update to be rejected")
	}

	restarted := auth.NewPersistentSessionManager(time.Hour, auth.NewGormSessionStore(db))
	records := restarted.List()
	if len(records) != 2 {
		t.Fatalf("expected two sessions after restart, got %+v", records)
	}
	aliases := map[auth.Role]string{}
	for _, record := range records {
		aliases[record.Role] = record.Alias
	}
	if aliases[auth.RoleAdmin] != "Office Mac" {
		t.Fatalf("expected persisted admin alias, got %q", aliases[auth.RoleAdmin])
	}
	if aliases[auth.RoleAPIKeyViewer] != "" {
		t.Fatalf("expected API key alias to remain empty, got %q", aliases[auth.RoleAPIKeyViewer])
	}

	if !restarted.UpdateAdminAliasByTokenHash(auth.SessionTokenHash(adminToken), "") {
		t.Fatal("expected clearing admin alias to succeed")
	}
	var row entities.AuthSession
	if err := db.Where("token_hash = ?", auth.SessionTokenHash(adminToken)).First(&row).Error; err != nil {
		t.Fatalf("reload admin session row: %v", err)
	}
	if row.Alias != "" {
		t.Fatalf("expected cleared persisted alias, got %q", row.Alias)
	}
}

func TestSessionManagerRejectsUnknownAdminAliasTarget(t *testing.T) {
	manager := auth.NewSessionManager(time.Hour)
	if manager.UpdateAdminAliasByTokenHash("missing", "Office Mac") {
		t.Fatal("expected unknown session alias update to be rejected")
	}
}
