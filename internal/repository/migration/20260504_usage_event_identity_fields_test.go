package migration

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDatabaseBackfillsUsageEventIdentityFieldsFromUsageIdentities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-identities.db")
	seedLegacyUsageIdentityTables(t, dbPath)

	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open seeded legacy database: %v", err)
	}
	if err := db.Exec("UPDATE usage_events SET provider = '' WHERE event_key IN (?, ?)", "legacy-apikey", "legacy-oauth").Error; err != nil {
		t.Fatalf("blank legacy usage event providers: %v", err)
	}
	if err := db.Exec("UPDATE usage_events SET provider = ? WHERE event_key = ?", "existing-provider", "apikey-success").Error; err != nil {
		t.Fatalf("set existing provider: %v", err)
	}
	closeOpenedDatabase(t, db)

	db = openMigratedDatabase(t, dbPath)
	defer closeOpenedDatabase(t, db)

	var legacyProvider models.UsageEvent
	if err := db.Where("event_key = ?", "legacy-apikey").First(&legacyProvider).Error; err != nil {
		t.Fatalf("load legacy provider event: %v", err)
	}
	if legacyProvider.AuthType != "apikey" || legacyProvider.Provider != "Claude API" {
		t.Fatalf("expected legacy provider event identity fields to be backfilled, got %+v", legacyProvider)
	}

	var legacyOAuth models.UsageEvent
	if err := db.Where("event_key = ?", "legacy-oauth").First(&legacyOAuth).Error; err != nil {
		t.Fatalf("load legacy oauth event: %v", err)
	}
	if legacyOAuth.AuthType != "oauth" {
		t.Fatalf("expected legacy oauth event auth_type to be backfilled, got %+v", legacyOAuth)
	}

	var existingProvider models.UsageEvent
	if err := db.Where("event_key = ?", "apikey-success").First(&existingProvider).Error; err != nil {
		t.Fatalf("load existing provider event: %v", err)
	}
	if existingProvider.Provider != "existing-provider" {
		t.Fatalf("expected existing provider field to remain unchanged, got %+v", existingProvider)
	}

	var providerFilterCount int64
	if err := db.Model(&models.UsageEvent{}).Where("auth_type = ? AND provider = ?", "apikey", "Claude API").Count(&providerFilterCount).Error; err != nil {
		t.Fatalf("count provider-filtered usage events: %v", err)
	}
	if providerFilterCount != 1 {
		t.Fatalf("expected provider filter to match migrated legacy event, got %d", providerFilterCount)
	}
}
