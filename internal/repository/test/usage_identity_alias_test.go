package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func TestUsageIdentityAliasIsLocalOnlyAndPreservedAcrossSync(t *testing.T) {
	db := openUsageIdentityAliasRepositoryDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	authType := entities.UsageIdentityAuthTypeAuthFile

	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{{
		Name:     "Upstream Auth",
		Identity: "auth-1",
		Type:     "codex",
		Provider: "Codex",
	}}, authType, now); err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	if err := repository.UpdateUsageIdentityAlias(ctx, db, 1, "  Local Alias  "); err != nil {
		t.Fatalf("UpdateUsageIdentityAlias returned error: %v", err)
	}

	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{{
		Name:     "Renamed Upstream Auth",
		Identity: "auth-1",
		Type:     "codex",
		Provider: "Codex",
	}}, authType, now.Add(time.Hour)); err != nil {
		t.Fatalf("resync usage identity: %v", err)
	}

	row, err := repository.FindUsageIdentityByID(ctx, db, 1)
	if err != nil {
		t.Fatalf("FindUsageIdentityByID returned error: %v", err)
	}
	if row.Name != "Renamed Upstream Auth" {
		t.Fatalf("expected upstream name to refresh, got %q", row.Name)
	}
	if row.Alias == nil || *row.Alias != "Local Alias" {
		t.Fatalf("expected local alias to survive sync, got %+v", row.Alias)
	}

	if err := repository.UpdateUsageIdentityAlias(ctx, db, 1, ""); err != nil {
		t.Fatalf("clear alias returned error: %v", err)
	}
	row, err = repository.FindUsageIdentityByID(ctx, db, 1)
	if err != nil {
		t.Fatalf("FindUsageIdentityByID after clear returned error: %v", err)
	}
	if row.Alias != nil {
		t.Fatalf("expected empty alias update to clear to NULL, got %+v", row.Alias)
	}
}

func openUsageIdentityAliasRepositoryDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-identity-alias.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
