package test

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/plugin/dbresolver"
)

func TestOpenDatabaseRoutesGORMErrorsThroughKeeperLogging(t *testing.T) {
	previousStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = previousStderr
		_ = reader.Close()
		_ = writer.Close()
	})

	logCloser, err := logging.Configure(config.Config{LogLevel: "info"})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	loggingClosed := false
	t.Cleanup(func() {
		if !loggingClosed {
			_ = logCloser.Close()
		}
	})
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if execErr := db.Exec("INSERT INTO definitely_missing_table DEFAULT VALUES").Error; execErr == nil {
		t.Fatal("expected missing table query to fail")
	}
	if sqlDB, dbErr := db.DB(); dbErr == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := logCloser.Close(); err != nil {
		t.Fatalf("close logging: %v", err)
	}
	loggingClosed = true
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(string(content), "")
	if !strings.Contains(plain, "| error | gorm query failed |") || !strings.Contains(plain, "no such table: definitely_missing_table") {
		t.Fatalf("expected GORM error through Keeper logging, got %q", plain)
	}
}

func TestOpenDatabasePoolsRoutesReaderErrorsThroughKeeperLogging(t *testing.T) {
	previousStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = previousStderr
		_ = reader.Close()
		_ = writer.Close()
	})

	logCloser, err := logging.Configure(config.Config{LogLevel: "info"})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	loggingClosed := false
	t.Cleanup(func() {
		if !loggingClosed {
			_ = logCloser.Close()
		}
	})

	databasePath := filepath.Join(t.TempDir(), "app.db")
	db, readDB, err := repository.OpenDatabasePools(config.Config{SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	for _, handle := range []interface{ DB() (*sql.DB, error) }{db, readDB} {
		if sqlDB, dbErr := handle.DB(); dbErr == nil {
			t.Cleanup(func() { _ = sqlDB.Close() })
		}
	}

	var rows []struct{ ID int }
	if queryErr := readDB.Raw("SELECT id FROM definitely_missing_reader_table").Scan(&rows).Error; queryErr == nil {
		t.Fatal("expected direct reader query to fail")
	}
	if queryErr := db.Clauses(dbresolver.Read).Raw("SELECT id FROM definitely_missing_reader_table").Scan(&rows).Error; queryErr == nil {
		t.Fatal("expected dbresolver reader query to fail")
	}
	if err := logCloser.Close(); err != nil {
		t.Fatalf("close logging: %v", err)
	}
	loggingClosed = true
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(string(content), "")
	if count := strings.Count(plain, "| error | gorm query failed |"); count != 2 {
		t.Fatalf("expected direct and routed reader errors through Keeper logging, count=%d logs=%q", count, plain)
	}
}
