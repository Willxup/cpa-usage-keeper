package test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

func openBootstrapDB(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if reader != db {
			if sqlDB, err := reader.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestPricingBootstrapPersistsFreshIdentityBeforeInbox(t *testing.T) {
	for _, zeroByte := range []bool{false, true} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprintf("zero_byte_%t_partial_%t", zeroByte, partial), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "app.db")
				if zeroByte {
					if err := os.WriteFile(path, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				db, err := repository.OpenDatabaseConnection(config.Config{SQLitePath: path})
				if err != nil {
					t.Fatal(err)
				}
				state, err := repository.BootstrapPricingInitialization(context.Background(), db)
				if err != nil || state.InitKind != repository.PricingInitKindFresh {
					t.Fatalf("bootstrap fresh state = %+v, %v", state, err)
				}
				if db.Migrator().HasTable("redis_usage_inboxes") || db.Migrator().HasTable("usage_events") {
					t.Fatal("identity bootstrap created inbox or business tables")
				}
				if partial {
					if err := repository.EnsurePricingBootstrapInbox(context.Background(), db); err != nil {
						t.Fatal(err)
					}
					if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), db, "redis_pull:usage", []string{`{"request_id":"pending"}`}, time.Now()); err != nil {
						t.Fatal(err)
					}
					// 模拟新库只建出一部分业务表后退出：重启不能按“已有表”改判旧库。
					if err := db.Exec("CREATE TABLE usage_events (id INTEGER PRIMARY KEY)").Error; err != nil {
						t.Fatal(err)
					}
				}
				if sqlDB, err := db.DB(); err != nil {
					t.Fatal(err)
				} else if err := sqlDB.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := repository.OpenDatabaseConnection(config.Config{SQLitePath: path})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if sqlDB, err := reopened.DB(); err == nil {
						_ = sqlDB.Close()
					}
				})
				again, err := repository.BootstrapPricingInitialization(context.Background(), reopened)
				if err != nil || again.InitKind != repository.PricingInitKindFresh {
					t.Fatalf("restarted fresh state = %+v, %v", again, err)
				}
				var inboxCount int64
				if partial {
					if err := reopened.Table("redis_usage_inboxes").Count(&inboxCount).Error; err != nil || inboxCount != 1 {
						t.Fatalf("retained inbox = %d, %v", inboxCount, err)
					}
				} else if reopened.Migrator().HasTable("redis_usage_inboxes") {
					t.Fatal("control-only restart unexpectedly created inbox")
				}
			})
		}
	}
}

func createOldPricingInbox(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE TABLE redis_usage_inboxes (
		id INTEGER PRIMARY KEY AUTOINCREMENT, queue_key TEXT NOT NULL,
		message_hash TEXT NOT NULL, raw_message TEXT NOT NULL, status TEXT NOT NULL,
		attempt_count INTEGER NOT NULL DEFAULT 0, last_error TEXT, usage_event_key TEXT,
		popped_at DATETIME NOT NULL, processed_at DATETIME, created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPricingBootstrapCreatesMissingInboxOnlyAfterLegacyIdentity(t *testing.T) {
	db := openBootstrapDB(t, filepath.Join(t.TempDir(), "legacy-without-inbox.db"))
	if err := repository.EnsurePricingBootstrapInbox(context.Background(), db); err == nil {
		t.Fatal("inbox was allowed before initialization identity")
	}
	if err := db.Exec("CREATE TABLE usage_events (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	state, err := repository.BootstrapPricingInitialization(context.Background(), db)
	if err != nil || state.InitKind != repository.PricingInitKindLegacy {
		t.Fatalf("legacy initialization = %+v, %v", state, err)
	}
	if err := repository.EnsurePricingBootstrapInbox(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasColumn("redis_usage_inboxes", "source") || db.Migrator().HasTable("schema_migrations") {
		t.Fatal("bootstrap did not create only current inbox structure")
	}
}

func TestPricingBootstrapOldInboxKeepsOriginalMessagesAcrossColumnSwitch(t *testing.T) {
	db := openBootstrapDB(t, filepath.Join(t.TempDir(), "old.db"))
	createOldPricingInbox(t, db)
	state, err := repository.BootstrapPricingInitialization(context.Background(), db)
	if err != nil || state.InitKind != repository.PricingInitKindLegacy {
		t.Fatalf("bootstrap legacy state = %+v, %v", state, err)
	}
	if err := repository.EnsurePricingBootstrapInbox(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 9, 23, 8, 30, 0, 0, time.UTC)
	duplicate := `{"request_id":"same"}`
	if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), db, "redis_pull:queue", []string{duplicate, duplicate}, when); err != nil {
		t.Fatal(err)
	}
	// 两列共存时旧 queue_key 仍为 NOT NULL，必须两列都写。
	if err := db.Exec("ALTER TABLE redis_usage_inboxes ADD COLUMN source TEXT NOT NULL DEFAULT 'unknown'").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), db, "redis_pull:usage", []string{`{"request_id":"middle"}`}, when); err != nil {
		t.Fatal(err)
	}
	var transitional struct{ Source, QueueKey string }
	if err := db.Table("redis_usage_inboxes").Select("source, queue_key").Where("id = 3").Scan(&transitional).Error; err != nil {
		t.Fatal(err)
	}
	if transitional.Source != "redis_pull:usage" || transitional.QueueKey != "usage" {
		t.Fatalf("two-column insert = %+v", transitional)
	}
	// 实际 DROP 失败必须保持原表和消息；重试仍可在双列状态接收。
	if err := db.Exec("CREATE INDEX idx_old_queue_key ON redis_usage_inboxes(queue_key)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec("ALTER TABLE redis_usage_inboxes DROP COLUMN queue_key").Error
	}); err == nil {
		t.Fatal("expected indexed queue_key DROP to fail")
	}
	if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), db, "http_pull:usage", []string{`{"request_id":"after-failure"}`}, when); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_old_queue_key").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE redis_usage_inboxes DROP COLUMN queue_key").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), db, "http_pull:usage", []string{`{"request_id":"after-switch"}`}, when); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID                                        int64
		RawMessage, MessageHash, Status, PoppedAt string
	}
	if err := db.Table("redis_usage_inboxes").Select("id, raw_message, message_hash, status, popped_at").Order("id").Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || rows[0].RawMessage != duplicate || rows[1].RawMessage != duplicate {
		t.Fatalf("inbox messages = %+v", rows)
	}
	for _, row := range rows {
		hash := sha256.Sum256([]byte(row.RawMessage))
		if row.MessageHash != fmt.Sprintf("%x", hash) || row.Status != repository.RedisUsageInboxStatusPending || row.PoppedAt != timeutil.FormatStorageTime(when) {
			t.Fatalf("inbox fact changed: %+v", row)
		}
	}
}

func TestPricingBootstrapWriterAndColumnDDLSerialize(t *testing.T) {
	db := openBootstrapDB(t, filepath.Join(t.TempDir(), "switch.db"))
	createOldPricingInbox(t, db)
	if _, err := repository.BootstrapPricingInitialization(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	const count = 30
	var wg sync.WaitGroup
	errors := make(chan error, count+1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range count {
			_, err := repository.InsertPricingBootstrapInboxRawMessages(context.Background(), db, "redis_pull:queue", []string{fmt.Sprintf(`{"n":%d}`, i)}, time.Now())
			if err != nil {
				errors <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("ALTER TABLE redis_usage_inboxes ADD COLUMN source TEXT NOT NULL DEFAULT 'unknown'").Error; err != nil {
				return err
			}
			return tx.Exec("ALTER TABLE redis_usage_inboxes DROP COLUMN queue_key").Error
		}); err != nil {
			errors <- err
		}
	}()
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var stored int64
	if err := db.Table("redis_usage_inboxes").Count(&stored).Error; err != nil || stored != count {
		t.Fatalf("serialized writes = %d, %v", stored, err)
	}
	var rows []struct{ RawMessage string }
	if err := db.Table("redis_usage_inboxes").Select("raw_message").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int, count)
	for _, row := range rows {
		seen[row.RawMessage]++
	}
	for i := range count {
		if got := seen[fmt.Sprintf(`{"n":%d}`, i)]; got != 1 {
			t.Fatalf("raw message %d stored %d times", i, got)
		}
	}
}
