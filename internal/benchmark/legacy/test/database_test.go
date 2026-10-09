package legacy_test

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func openLegacyBenchmarkDB(b *testing.B) *gorm.DB {
	b.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(b.TempDir(), "benchmark.db")})
	if err != nil {
		b.Fatalf("open benchmark database: %v", err)
	}
	b.Cleanup(func() { closeLegacyBenchmarkDB(b, db) })
	return db
}

func closeLegacyBenchmarkDB(b *testing.B, db *gorm.DB) {
	b.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatalf("get benchmark database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		b.Fatalf("close benchmark database: %v", err)
	}
}
