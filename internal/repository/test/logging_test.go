package test

import (
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
	plain := captureConfiguredRepositoryLogs(t, func() {
		db := openTestDatabase(t)
		if err := db.Exec("INSERT INTO definitely_missing_table DEFAULT VALUES").Error; err == nil {
			t.Fatal("expected missing table query to fail")
		}
	})
	if !strings.Contains(plain, "| error | gorm query failed |") || !strings.Contains(plain, "no such table: definitely_missing_table") {
		t.Fatalf("expected GORM error through Keeper logging, got %q", plain)
	}
}

func TestOpenDatabasePoolsRoutesReaderErrorsThroughKeeperLogging(t *testing.T) {
	plain := captureConfiguredRepositoryLogs(t, func() {
		db, reader, err := repository.OpenDatabasePools(config.Config{SQLitePath: filepath.Join(t.TempDir(), "app.db")})
		if err != nil {
			t.Fatalf("open database pools: %v", err)
		}
		closeTestDatabase(t, db)
		closeTestDatabase(t, reader)
		var rows []struct{ ID int }
		if err := reader.Raw("SELECT id FROM definitely_missing_reader_table").Scan(&rows).Error; err == nil {
			t.Fatal("expected direct reader query to fail")
		}
		if err := db.Clauses(dbresolver.Read).Raw("SELECT id FROM definitely_missing_reader_table").Scan(&rows).Error; err == nil {
			t.Fatal("expected dbresolver reader query to fail")
		}
	})
	if count := strings.Count(plain, "| error | gorm query failed |"); count != 2 {
		t.Fatalf("expected direct and routed reader errors through Keeper logging, count=%d logs=%q", count, plain)
	}
}

func captureConfiguredRepositoryLogs(t *testing.T, run func()) string {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), "stderr.log"))
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	previousStderr := os.Stderr
	os.Stderr = file
	defer func() { os.Stderr = previousStderr; _ = file.Close() }()
	closer, err := logging.Configure(config.Config{LogLevel: "info"})
	if err != nil {
		t.Fatalf("configure logging: %v", err)
	}
	defer closer.Close()
	run()
	if err := closer.Close(); err != nil {
		t.Fatalf("close logging: %v", err)
	}
	content, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(string(content), "")
}
