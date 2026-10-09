package test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func openTestDatabasePools(t *testing.T, name string) (*gorm.DB, *sql.DB, *sql.DB) {
	t.Helper()
	db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), name)})
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	writerSQL, err := db.DB()
	if err != nil {
		t.Fatalf("load writer pool: %v", err)
	}
	readerSQL, err := reader.DB()
	if err != nil {
		t.Fatalf("load reader pool: %v", err)
	}
	closeResolverTestPools(t, writerSQL, readerSQL)
	return db, writerSQL, readerSQL
}

func closeResolverTestPools(t *testing.T, writer, reader *sql.DB) {
	t.Helper()
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Errorf("close reader pool: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Errorf("close writer pool: %v", err)
		}
	})
}
