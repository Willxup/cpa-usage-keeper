package test

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func openUsageServiceTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "usage-service-test.db")})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}

func withUsageServiceLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	return location
}

func seedUsageFilterAPIKeys(t *testing.T, db *gorm.DB) string {
	t.Helper()
	keys := []entities.CPAAPIKey{
		{APIKey: "sk-target-key", DisplayKey: "sk-***target"},
		{APIKey: "sk-other-key", DisplayKey: "sk-***other"},
	}
	if err := db.Create(&keys).Error; err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	return strconv.FormatInt(keys[0].ID, 10)
}
