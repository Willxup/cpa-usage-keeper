package test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/migration"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	usageEventAPIGroupKeyTimestampIndexName = "idx_usage_events_api_group_key_timestamp"
	usageEventAPIGroupKeyTimestampMigration = "20260905_usage_event_api_group_key_timestamp_index"
)

func TestUsageEventAPIGroupKeyTimestampIndexMigration(t *testing.T) {
	for _, test := range []struct {
		name      string
		fresh     bool
		seedIndex string
	}{
		{name: "fresh database", fresh: true},
		{name: "single key index"},
		{name: "existing descending index", seedIndex: "timestamp DESC"},
		{name: "existing ascending index", seedIndex: "timestamp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "app.db")
			var db *gorm.DB
			var err error
			if test.fresh {
				db, err = repository.OpenDatabase(config.Config{SQLitePath: dbPath})
			} else {
				db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
			}
			if err != nil {
				t.Fatalf("open database: %v", err)
			}
			closeMigrationTestDatabase(t, db)

			if !test.fresh {
				// 旧库只保留本次迁移需要的列，历史版本标记为完成，避免其它迁移影响测试。
				if err := db.Exec(`CREATE TABLE usage_events (
					id INTEGER PRIMARY KEY,
					api_group_key TEXT NOT NULL,
					timestamp DATETIME NOT NULL,
					total_tokens INTEGER NOT NULL
				)`).Error; err != nil {
					t.Fatalf("create usage_events table: %v", err)
				}
				if err := db.Exec(`CREATE INDEX idx_usage_events_api_group_key ON usage_events(api_group_key)`).Error; err != nil {
					t.Fatalf("seed single key index: %v", err)
				}
				if test.seedIndex != "" {
					if err := db.Exec(`CREATE INDEX idx_usage_events_api_group_key_timestamp ON usage_events(api_group_key, ` + test.seedIndex + `)`).Error; err != nil {
						t.Fatalf("seed compound index: %v", err)
					}
				}
				if err := migration.MarkAllAsApplied(db); err != nil {
					t.Fatalf("mark historical migrations applied: %v", err)
				}
				makeAPIGroupKeyTimestampMigrationPending(t, db)
			}

			start := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
			end := start.Add(time.Minute)
			for _, event := range []struct {
				id     int
				key    string
				offset time.Duration
				tokens int
			}{
				{1, "target", -time.Second, 100},
				{2, "other", 30 * time.Second, 200},
				{3, "target", 0, 30},
				{4, "target", 30 * time.Second, 40},
				{5, "target", 30 * time.Second, 50},
				{6, "target", time.Minute, 60},
			} {
				if err := db.Exec(`INSERT INTO usage_events (id, api_group_key, timestamp, total_tokens) VALUES (?, ?, ?, ?)`,
					event.id, event.key, timeutil.FormatStorageTime(start.Add(event.offset)), event.tokens).Error; err != nil {
					t.Fatalf("seed usage event: %v", err)
				}
			}

			// 第二轮显式重跑原版本，验证已有最终索引时仍安全且不会重复记录版本。
			for pass := 0; pass < 2; pass++ {
				if pass > 0 {
					makeAPIGroupKeyTimestampMigrationPending(t, db)
				}
				if err := migration.Run(db); err != nil {
					t.Fatalf("Run returned error: %v", err)
				}
				assertAPIGroupKeyTimestampIndex(t, db)
				assertAPIGroupKeyTimestampQueries(t, db, start, end)
				var count int64
				if err := db.Table("schema_migrations").Where("version = ?", usageEventAPIGroupKeyTimestampMigration).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("expected one migration record, got %d, error %v", count, err)
				}
				if err := db.Table("usage_events").Count(&count).Error; err != nil || count != 6 {
					t.Fatalf("expected all six usage events preserved, got %d, error %v", count, err)
				}
			}
		})
	}
}

func makeAPIGroupKeyTimestampMigrationPending(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Table("schema_migrations").Where("version = ?", usageEventAPIGroupKeyTimestampMigration).Delete(nil).Error; err != nil {
		t.Fatalf("make index migration pending: %v", err)
	}
}

func TestUsageEventAPIGroupKeyTimestampIndexMigrationRollsBackOnFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "invalid-schema.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	closeMigrationTestDatabase(t, db)
	// 缺少 timestamp 让重建失败，验证原索引删除和迁移版本记录都在同一事务中回滚。
	for _, statement := range []string{
		`CREATE TABLE usage_events (id INTEGER PRIMARY KEY, api_group_key TEXT NOT NULL)`,
		`CREATE INDEX idx_usage_events_api_group_key ON usage_events(api_group_key)`,
		`CREATE INDEX idx_usage_events_api_group_key_timestamp ON usage_events(api_group_key, id)`,
		`INSERT INTO usage_events (id, api_group_key) VALUES (1, 'target')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed invalid schema: %v", err)
		}
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	makeAPIGroupKeyTimestampMigrationPending(t, db)
	if err := migration.Run(db); err == nil {
		t.Fatal("expected missing timestamp column to fail migration")
	}
	var columns []string
	if err := db.Raw(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, usageEventAPIGroupKeyTimestampIndexName).Scan(&columns).Error; err != nil || !reflect.DeepEqual(columns, []string{"api_group_key", "id"}) {
		t.Fatalf("expected original compound index to survive rollback, got %v, error %v", columns, err)
	}
	if !db.Migrator().HasIndex(&entities.UsageEvent{}, "idx_usage_events_api_group_key") {
		t.Fatal("expected original single key index to survive rollback")
	}
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", usageEventAPIGroupKeyTimestampMigration).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expected failed migration not to be recorded, got %d, error %v", count, err)
	}
	if err := db.Table("usage_events").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("expected usage event to survive rollback, got %d, error %v", count, err)
	}
}

func assertAPIGroupKeyTimestampIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db.Migrator().HasIndex(&entities.UsageEvent{}, "idx_usage_events_api_group_key") {
		t.Fatal("expected redundant single key index to be removed")
	}
	var columns []struct {
		Name string
		Desc int
	}
	if err := db.Raw(`SELECT name, "desc" FROM pragma_index_xinfo(?) WHERE key = 1 ORDER BY seqno`, usageEventAPIGroupKeyTimestampIndexName).Scan(&columns).Error; err != nil {
		t.Fatalf("read compound index columns: %v", err)
	}
	if len(columns) != 2 || columns[0].Name != "api_group_key" || columns[1].Name != "timestamp" || columns[0].Desc != 0 || columns[1].Desc != 0 {
		t.Fatalf("expected ascending (api_group_key, timestamp) index, got %+v", columns)
	}
}

func assertAPIGroupKeyTimestampQueries(t *testing.T, db *gorm.DB, start, end time.Time) {
	t.Helper()
	args := []any{"target", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)}
	predicate := `api_group_key = ? AND timestamp >= ? AND timestamp < ?`
	listSQL := `SELECT id FROM usage_events WHERE ` + predicate + ` ORDER BY timestamp DESC, id DESC LIMIT 2`
	var ids []int64
	if err := db.Raw(listSQL, args...).Scan(&ids).Error; err != nil || !reflect.DeepEqual(ids, []int64{5, 4}) {
		t.Fatalf("expected descending timestamp/id page [5 4], got %v, error %v", ids, err)
	}
	var count int64
	countSQL := `SELECT COUNT(*) FROM usage_events WHERE ` + predicate
	if err := db.Raw(countSQL, args...).Scan(&count).Error; err != nil || count != 3 {
		t.Fatalf("expected three in-window target events, got %d, error %v", count, err)
	}
	boundarySQL := `SELECT total_tokens FROM usage_events WHERE ` + predicate + ` ORDER BY timestamp ASC`
	var tokens []int64
	if err := db.Raw(boundarySQL, args...).Scan(&tokens).Error; err != nil {
		t.Fatalf("query boundary tokens: %v", err)
	}
	var total int64
	for _, tokenCount := range tokens {
		total += tokenCount
	}
	if len(tokens) != 3 || total != 120 {
		t.Fatalf("expected only in-window target boundary tokens, got %v", tokens)
	}
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{listSQL, args},
		{countSQL, args},
		{boundarySQL, args},
		{`SELECT COUNT(*) FROM usage_events WHERE api_group_key = ?`, []any{"target"}},
	} {
		var plan []struct{ Detail string }
		if err := db.Raw("EXPLAIN QUERY PLAN "+query.sql, query.args...).Scan(&plan).Error; err != nil {
			t.Fatalf("explain query plan: %v", err)
		}
		usesIndex := false
		for _, step := range plan {
			usesIndex = usesIndex || strings.Contains(step.Detail, usageEventAPIGroupKeyTimestampIndexName)
			if strings.Contains(step.Detail, "TEMP B-TREE") {
				t.Fatalf("expected index ordering without temporary sorting: %+v", plan)
			}
		}
		if !usesIndex {
			t.Fatalf("expected compound index for %s, got %+v", query.sql, plan)
		}
	}
}
