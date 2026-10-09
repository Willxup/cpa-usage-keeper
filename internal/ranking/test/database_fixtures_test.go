package test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func rankingSQLPool(t *testing.T, db *gorm.DB) *sql.DB {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("get ranking database pool: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close ranking database pool: %v", err)
		}
	})
	return pool
}

func openRankingDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "ranking.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	rankingSQLPool(t, db)
	return db
}

func openRankingDatabasePools(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()
	writer, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "ranking.db")})
	if err != nil {
		t.Fatalf("OpenDatabasePools: %v", err)
	}
	rankingSQLPool(t, reader)
	return writer, rankingSQLPool(t, writer)
}
