package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestAPIKeyAuthFileScopeServiceResolvesCurrentAuthIndexesByFileName(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "api-key-auth-file-scope.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	now := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	if err := repository.SyncCPAAPIKeys(db, []string{"viewer-key"}, now); err != nil {
		t.Fatalf("SyncCPAAPIKeys returned error: %v", err)
	}
	keys, err := repository.ListActiveCPAAPIKeys(db)
	if err != nil || len(keys) != 1 {
		t.Fatalf("ListActiveCPAAPIKeys returned rows=%+v err=%v", keys, err)
	}
	fileName := "viewer-auth.json"
	if err := db.Create(&entities.UsageIdentity{
		Name:         "Viewer auth",
		AuthType:     entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     "auth-index-v1",
		FileName:     &fileName,
		Type:         "codex",
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("seed auth file identity: %v", err)
	}
	provider := NewAPIKeyAuthFileScopeService(db)

	if _, err := provider.ResolveAPIKeyViewerScope(context.Background(), keys[0].ID); !errors.Is(err, ErrAPIKeyAuthFileScopeNotConfigured) {
		t.Fatalf("expected empty scope to fail closed, got %v", err)
	}
	names, err := provider.ReplaceAPIKeyAuthFileScopes(context.Background(), keys[0].ID, []string{"  viewer-auth.json  "})
	if err != nil {
		t.Fatalf("ReplaceAPIKeyAuthFileScopes returned error: %v", err)
	}
	if len(names) != 1 || names[0] != "viewer-auth.json" {
		t.Fatalf("expected normalized auth file name, got %+v", names)
	}
	scope, err := provider.ResolveAPIKeyViewerScope(context.Background(), keys[0].ID)
	if err != nil {
		t.Fatalf("ResolveAPIKeyViewerScope returned error: %v", err)
	}
	if scope.CPAAPIKeyID != keys[0].ID || scope.APIGroupKey != "viewer-key" || len(scope.AuthIndexes) != 1 || scope.AuthIndexes[0] != "auth-index-v1" {
		t.Fatalf("unexpected resolved viewer scope: %+v", scope)
	}

	if err := db.Model(&entities.UsageIdentity{}).Where("id = ?", 1).Update("is_deleted", true).Error; err != nil {
		t.Fatalf("mark auth file identity deleted: %v", err)
	}
	if _, err := provider.ResolveAPIKeyViewerScope(context.Background(), keys[0].ID); !errors.Is(err, ErrAPIKeyAuthFileScopeUnresolved) {
		t.Fatalf("expected deleted auth file configuration to fail closed, got %v", err)
	}
}
