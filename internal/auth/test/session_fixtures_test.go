package test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openSessionDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open session database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close session database: %v", err)
		}
	})
	if err := db.AutoMigrate(&entities.AuthSession{}); err != nil {
		t.Fatalf("migrate auth sessions: %v", err)
	}
	return db
}
