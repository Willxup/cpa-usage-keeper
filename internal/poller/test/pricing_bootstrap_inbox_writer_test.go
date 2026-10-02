package poller_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
)

func TestPricingBootstrapWriterFiltersControlWithoutMetadataObserver(t *testing.T) {
	db, err := repository.OpenDatabaseConnection(config.Config{SQLitePath: filepath.Join(t.TempDir(), "bootstrap.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if _, err := repository.BootstrapPricingInitialization(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsurePricingBootstrapInbox(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	writer := poller.NewControlAwareRedisInboxWriter(poller.NewPricingBootstrapInboxWriter(db), nil)
	count, err := writer.Insert(context.Background(), "redis_subscribe:usage", []string{
		`{"support_refresh":true}`, `{"request_id":"one"}`, `{"request_id":"one"}`,
	}, time.Now())
	if err != nil || count != 2 {
		t.Fatalf("bootstrap insert = %d, %v", count, err)
	}
	var stored int64
	if err := db.Table("redis_usage_inboxes").Count(&stored).Error; err != nil || stored != 2 {
		t.Fatalf("stored raw messages = %d, %v", stored, err)
	}
}
