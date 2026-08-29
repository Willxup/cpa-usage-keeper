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

func TestUsageIdentityXAIUserIDPersistsAndReads(t *testing.T) {
	db := openXAIUserIDRepositoryDatabase(t)
	ctx := context.Background()
	xaiUserID := "xai-user-123"

	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{{
		Name:      "xAI Auth",
		Identity:  "xai-auth",
		Type:      "xai",
		XAIUserID: &xaiUserID,
	}}, entities.UsageIdentityAuthTypeAuthFile, time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForAuthType returned error: %v", err)
	}

	row, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, db, "xai-auth")
	if err != nil {
		t.Fatalf("GetActiveAuthFileUsageIdentityByAuthIndex returned error: %v", err)
	}
	if row.XAIUserID == nil || *row.XAIUserID != xaiUserID {
		t.Fatalf("expected persisted xAI user id %q, got %+v", xaiUserID, row.XAIUserID)
	}
}

func TestUsageIdentityXAIUserIDRefreshesAndClearsAcrossSync(t *testing.T) {
	db := openXAIUserIDRepositoryDatabase(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	initialUserID := "xai-user-old"
	updatedUserID := "xai-user-new"

	replaceXAIUsageIdentity(t, ctx, db, &initialUserID, now)
	replaceXAIUsageIdentity(t, ctx, db, &updatedUserID, now.Add(time.Minute))
	row, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, db, "xai-auth")
	if err != nil {
		t.Fatalf("read updated xAI usage identity: %v", err)
	}
	if row.XAIUserID == nil || *row.XAIUserID != updatedUserID {
		t.Fatalf("expected refreshed xAI user id %q, got %+v", updatedUserID, row.XAIUserID)
	}

	replaceXAIUsageIdentity(t, ctx, db, nil, now.Add(2*time.Minute))
	row, err = repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, db, "xai-auth")
	if err != nil {
		t.Fatalf("read cleared xAI usage identity: %v", err)
	}
	if row.XAIUserID != nil {
		t.Fatalf("expected a successful sync without xAI user id to clear the column, got %q", *row.XAIUserID)
	}
}

func replaceXAIUsageIdentity(t *testing.T, ctx context.Context, db *gorm.DB, xaiUserID *string, now time.Time) {
	t.Helper()
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{{
		Name:      "xAI Auth",
		Identity:  "xai-auth",
		Type:      "xai",
		XAIUserID: xaiUserID,
	}}, entities.UsageIdentityAuthTypeAuthFile, now); err != nil {
		t.Fatalf("ReplaceUsageIdentitiesForAuthType returned error: %v", err)
	}
}

func openXAIUserIDRepositoryDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "xai-user-id.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close sql database: %v", err)
		}
	})
	return db
}
