package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageIdentityServiceAddsCredentialHealthToPagedRows(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-identity-health-service.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)

	now := time.Now().In(time.Local).Add(-time.Minute).Truncate(time.Second)
	if err := db.Create(&[]entities.UsageIdentity{
		{Name: "Provider Team", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "shared-auth", Type: "claude", Provider: "Claude", CreatedAt: now, UpdatedAt: now},
		{Name: "Auth File", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "shared-auth", Type: "codex", Provider: "Codex", CreatedAt: now, UpdatedAt: now},
	}).Error; err != nil {
		t.Fatalf("seed usage identities: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "provider-success", AuthType: "apikey", AuthIndex: "shared-auth", Timestamp: now.Add(-3 * time.Minute), Failed: false},
		{EventKey: "provider-failure", AuthType: "apikey", AuthIndex: "shared-auth", Timestamp: now.Add(-4 * time.Minute), Failed: true},
		{EventKey: "auth-file-success", AuthType: "oauth", AuthIndex: "shared-auth", Timestamp: now.Add(-3 * time.Minute), Failed: false},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache returned error: %v", err)
	}
	t.Cleanup(cache.Close)

	authType := entities.UsageIdentityAuthTypeAIProvider
	provider := NewUsageIdentityServiceWithRecentCache(db, cache)
	result, err := provider.ListActiveUsageIdentitiesPage(context.Background(), ListUsageIdentitiesRequest{AuthType: &authType, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListActiveUsageIdentitiesPage returned error: %v", err)
	}

	if len(result.Items) != 1 || result.Items[0].Identity != "shared-auth" {
		t.Fatalf("expected one AI provider identity, got %+v", result.Items)
	}
	if len(result.CredentialHealth) != 1 {
		t.Fatalf("expected one credential health snapshot, got %+v", result.CredentialHealth)
	}
	health := result.CredentialHealth[0]
	if health.WindowSeconds != int64(5*time.Hour/time.Second) || health.BucketSeconds != int64(10*time.Minute/time.Second) || len(health.Buckets) != 30 {
		t.Fatalf("unexpected credential health metadata: %+v", health)
	}
	if health.TotalSuccess != 1 || health.TotalFailure != 1 {
		t.Fatalf("expected AI provider health to ignore oauth rows with the same auth_index, got %+v", health)
	}
}

func TestUsageIdentityServiceRunsDisplayNameChangeCallbackAfterAliasUpdate(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-identity-callback.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeTestDatabase(t, db)
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	if err := repository.ReplaceUsageIdentitiesForAuthType(context.Background(), db, []entities.UsageIdentity{{
		Name:     "Upstream Auth",
		Identity: "auth-1",
		Type:     "claude",
		Provider: "Claude",
	}}, entities.UsageIdentityAuthTypeAuthFile, now); err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
	var callbackRows []entities.UsageIdentity
	provider := NewUsageIdentityServiceWithOptions(db, nil, UsageIdentityServiceOptions{
		OnDisplayNameChanged: func(identity entities.UsageIdentity) {
			callbackRows = append(callbackRows, identity)
		},
	})

	updated, err := provider.UpdateUsageIdentityAlias(context.Background(), 1, "Friendly Auth")
	if err != nil {
		t.Fatalf("UpdateUsageIdentityAlias returned error: %v", err)
	}

	if updated.Alias == nil || *updated.Alias != "Friendly Auth" {
		t.Fatalf("expected updated alias in response, got %+v", updated)
	}
	if len(callbackRows) != 1 {
		t.Fatalf("expected one display name callback, got %+v", callbackRows)
	}
	if callbackRows[0].Identity != "auth-1" || callbackRows[0].Alias == nil || *callbackRows[0].Alias != "Friendly Auth" {
		t.Fatalf("expected callback to receive updated identity, got %+v", callbackRows[0])
	}
}
