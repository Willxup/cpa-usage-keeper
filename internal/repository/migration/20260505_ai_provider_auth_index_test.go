package migration

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseRemovesPrefixGeneratedUsageIdentities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "prefix-identities.db")
	seedPrefixGeneratedUsageIdentities(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	for _, prefix := range []string{"gemini", "claude", "codex", "vertex", "openai"} {
		var prefixCount int64
		if err := db.Model(&models.UsageIdentity{}).Where("auth_type = ? AND identity = ?", models.UsageIdentityAuthTypeAIProvider, prefix).Count(&prefixCount).Error; err != nil {
			t.Fatalf("count prefix usage identity %q: %v", prefix, err)
		}
		if prefixCount != 0 {
			t.Fatalf("expected fixed prefix usage identity %q to be removed, got %d", prefix, prefixCount)
		}
	}

	if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, "claude-key") != 0 {
		t.Fatal("expected raw api key identity to be migrated away")
	}
	apiKey := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAIProvider, "claude-auth-index")
	if apiKey.LookupKey != "claude-key" || apiKey.TotalRequests != 1 || apiKey.LastAggregatedUsageEventID != 1 {
		t.Fatalf("expected real api key identity to migrate to auth-index with stats, got %+v", apiKey)
	}

	for _, identity := range []string{"claude-unused-key", "gemini-unused-key", "https://proxy.internal/v1"} {
		if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, identity) != 0 {
			t.Fatalf("expected unmatched raw provider identity %q to be deleted", identity)
		}
	}
}

func TestOpenDatabaseMigratesAIProviderRawIdentitiesToAuthIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ai-provider-auth-index.db")
	seedAIProviderAuthIndexMigrationDatabase(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, "sk-claude-old") != 0 {
		t.Fatal("expected raw API key identity to be removed after auth-index migration")
	}
	migrated := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAIProvider, "authidx-claude-1")
	if migrated.LookupKey != "sk-claude-old" || migrated.Name != "Claude" || migrated.Type != "claude" || migrated.Provider != "Claude" {
		t.Fatalf("unexpected migrated AI provider identity metadata: %+v", migrated)
	}
	if migrated.TotalRequests != 2 || migrated.SuccessCount != 1 || migrated.FailureCount != 1 || migrated.InputTokens != 12 || migrated.OutputTokens != 14 || migrated.ReasoningTokens != 3 || migrated.CachedTokens != 4 || migrated.TotalTokens != 33 || migrated.LastAggregatedUsageEventID != 2 {
		t.Fatalf("expected migrated identity stats to be rebuilt by auth_index, got %+v", migrated)
	}
	if migrated.FirstUsedAt == nil || !migrated.FirstUsedAt.Equal(time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first_used_at after migration: %+v", migrated.FirstUsedAt)
	}
	if migrated.LastUsedAt == nil || !migrated.LastUsedAt.Equal(time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected last_used_at after migration: %+v", migrated.LastUsedAt)
	}

	authFile := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAuthFile, "auth-file-index")
	if authFile.Identity != "auth-file-index" || authFile.AuthTypeName != "oauth" {
		t.Fatalf("expected auth file identity to remain untouched, got %+v", authFile)
	}
	nonAPIKey := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAIProvider, "non-apikey-identity")
	if nonAPIKey.AuthTypeName != "oauth" {
		t.Fatalf("expected non-apikey usage identity not to be converted by auth-index migration, got %+v", nonAPIKey)
	}
}

func TestOpenDatabaseMergesAIProviderRawIdentityIntoExistingAuthIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ai-provider-auth-index-merge.db")
	seedAIProviderAuthIndexMigrationDatabase(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, "sk-duplicate") != 0 {
		t.Fatal("expected duplicate raw API key identity to be physically deleted")
	}
	merged := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAIProvider, "authidx-existing")
	if merged.LookupKey != "sk-duplicate" || merged.Name != "Gemini" || merged.Type != "gemini" || merged.Provider != "Gemini" {
		t.Fatalf("expected existing auth-index row to be filled from old raw row, got %+v", merged)
	}
	if merged.TotalRequests != 1 || merged.TotalTokens != 21 || merged.LastAggregatedUsageEventID != 3 {
		t.Fatalf("expected merged row stats to be rebuilt by auth_index, got %+v", merged)
	}
}

func TestOpenDatabaseKeepsNewestAIProviderRawIdentityWhenMultipleRowsMapToSameAuthIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ai-provider-auth-index-newest.db")
	seedAIProviderAuthIndexMigrationDatabase(t, dbPath)

	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open seeded database: %v", err)
	}
	older := time.Date(2026, 5, 5, 6, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		name      string
		identity  string
		createdAt time.Time
	}{
		{name: "Older Shared", identity: "sk-shared-old", createdAt: older},
		{name: "Newer Shared", identity: "sk-shared-new", createdAt: newer},
	} {
		if err := db.Exec(`INSERT INTO usage_identities (name, auth_type, auth_type_name, identity, type, provider, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, row.name, models.UsageIdentityAuthTypeAIProvider, "apikey", row.identity, "claude", row.name, row.createdAt, row.createdAt).Error; err != nil {
			t.Fatalf("seed shared raw identity %q: %v", row.identity, err)
		}
		if err := db.Exec(`INSERT INTO usage_events (event_key, api_group_key, provider, endpoint, auth_type, request_id, model, timestamp, source, auth_index, failed, latency_ms, total_tokens, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "event-"+row.identity, "group", row.name, "/v1/messages", "apikey", "req-"+row.identity, "claude-sonnet", row.createdAt, row.identity, "authidx-shared", false, 100, 1, row.createdAt).Error; err != nil {
			t.Fatalf("seed shared usage event %q: %v", row.identity, err)
		}
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	migrated := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAIProvider, "authidx-shared")
	if migrated.Name != "Newer Shared" || migrated.LookupKey != "sk-shared-new" {
		t.Fatalf("expected newest raw identity to be retained for shared auth-index, got %+v", migrated)
	}
	if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, "sk-shared-old") != 0 || countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, "sk-shared-new") != 0 {
		t.Fatal("expected old raw identities to be removed after shared auth-index migration")
	}
}

func TestOpenDatabaseDeletesAIProviderRawIdentitiesWithoutUniqueProviderMatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ai-provider-auth-index-delete.db")
	seedAIProviderAuthIndexMigrationDatabase(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	for _, identity := range []string{"sk-ambiguous", "sk-provider-mismatch", "sk-no-events"} {
		if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, identity) != 0 {
			t.Fatalf("expected raw identity %q to be physically deleted", identity)
		}
	}
	for _, identity := range []string{"authidx-ambiguous-a", "authidx-ambiguous-b", "authidx-wrong-provider"} {
		if countUsageIdentities(t, db, models.UsageIdentityAuthTypeAIProvider, identity) != 0 {
			t.Fatalf("expected auth-index identity %q not to be created from ambiguous or mismatched events", identity)
		}
	}
}

func TestOpenDatabaseAIProviderAuthIndexMigrationIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ai-provider-auth-index-idempotent.db")
	seedAIProviderAuthIndexMigrationDatabase(t, dbPath)

	db := openMigratedDatabase(t, dbPath)
	first := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAIProvider, "authidx-claude-1")
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)
	second := loadUsageIdentity(t, db, models.UsageIdentityAuthTypeAIProvider, "authidx-claude-1")
	if second.TotalRequests != first.TotalRequests || second.TotalTokens != first.TotalTokens || second.LastAggregatedUsageEventID != first.LastAggregatedUsageEventID {
		t.Fatalf("expected idempotent stats, first %+v second %+v", first, second)
	}
	var migrationCount int64
	if err := db.Table("schema_migrations").Where("version = ?", "20260505_migrate_ai_provider_identities_to_auth_index").Count(&migrationCount).Error; err != nil {
		t.Fatalf("count auth-index migration: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected auth-index migration to be recorded once, got %d", migrationCount)
	}
	var identityCount int64
	if err := db.Model(&models.UsageIdentity{}).Count(&identityCount).Error; err != nil {
		t.Fatalf("count usage identities after reopen: %v", err)
	}
	if identityCount != 4 {
		t.Fatalf("expected stable usage identity count after reopen, got %d", identityCount)
	}
}
