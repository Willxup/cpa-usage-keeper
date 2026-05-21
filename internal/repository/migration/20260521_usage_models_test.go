package migration

import (
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateUsageModelsMigrationBackfillsDistinctModelSources(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(testSQLiteDSN(filepath.Join(t.TempDir(), "usage-models.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer closeOpenedDatabase(t, db)

	if err := db.Exec(`CREATE TABLE usage_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		model TEXT,
		auth_type TEXT,
		auth_index TEXT,
		timestamp DATETIME
	)`).Error; err != nil {
		t.Fatalf("create usage_events: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO usage_events (model, auth_type, auth_index, timestamp) VALUES (' claude-opus ', 'apikey', 'auth-a', '2026-05-21T10:00:00+08:00')`,
		`INSERT INTO usage_events (model, auth_type, auth_index, timestamp) VALUES ('claude-opus', 'apikey', 'auth-a', '2026-05-21T11:00:00+08:00')`,
		`INSERT INTO usage_events (model, auth_type, auth_index, timestamp) VALUES ('claude-opus', 'oauth', 'auth-b', '2026-05-21T12:00:00+08:00')`,
		`INSERT INTO usage_events (model, auth_type, auth_index, timestamp) VALUES (' ', 'apikey', 'auth-c', '2026-05-21T13:00:00+08:00')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed usage_events: %v", err)
		}
	}

	if err := createUsageModelsMigration(db); err != nil {
		t.Fatalf("createUsageModelsMigration returned error: %v", err)
	}

	var rows []entities.UsageModel
	if err := db.Order("auth_type asc, auth_index asc").Find(&rows).Error; err != nil {
		t.Fatalf("load usage models: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two usage model rows, got %+v", rows)
	}
	if rows[0].Model != "claude-opus" || rows[0].AuthType != "apikey" || rows[0].AuthIndex != "auth-a" || rows[0].RequestCount != 2 || rows[0].LastUsageEventID != 2 {
		t.Fatalf("unexpected first usage model row: %+v", rows[0])
	}
	if rows[1].Model != "claude-opus" || rows[1].AuthType != "oauth" || rows[1].AuthIndex != "auth-b" || rows[1].RequestCount != 1 || rows[1].LastUsageEventID != 3 {
		t.Fatalf("unexpected second usage model row: %+v", rows[1])
	}

	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := db.Where("name = ?", "usage_models").First(&checkpoint).Error; err != nil {
		t.Fatalf("load usage model checkpoint: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID != 4 || checkpoint.StatsUpdatedAt == nil {
		t.Fatalf("unexpected usage model checkpoint: %+v", checkpoint)
	}
}
