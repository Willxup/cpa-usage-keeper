package test

import (
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestMarkRedisUsageInboxProcessedBatchMapsKeysAcrossChunks(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	const rowCount = 601
	processedAt := time.Date(2026, 7, 23, 9, 30, 0, 123456789, location)
	inputs := make([]repositorydto.RedisInboxInsert, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		inputs = append(inputs, repositorydto.RedisInboxInsert{
			Source:     "redis_subscribe:usage",
			RawMessage: fmt.Sprintf(`{"request_id":"batch-map-%d"}`, i),
			PoppedAt:   processedAt.Add(-time.Minute),
		})
	}
	rows, err := repository.InsertRedisUsageInboxMessages(db, inputs)
	if err != nil {
		t.Fatalf("seed redis usage inbox rows: %v", err)
	}
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id > 0").Update("last_error", "stale error").Error; err != nil {
		t.Fatalf("seed stale inbox errors: %v", err)
	}
	oldUpdatedAt := processedAt.Add(-24 * time.Hour)
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id = ?", rows[0].ID).UpdateColumn("updated_at", timeutil.FormatStorageTime(oldUpdatedAt)).Error; err != nil {
		t.Fatalf("seed old updated_at: %v", err)
	}

	updates := make([]repository.RedisUsageInboxProcessedUpdate, 0, len(rows))
	for i, row := range rows {
		updates = append(updates, repository.RedisUsageInboxProcessedUpdate{
			ID:       row.ID,
			EventKey: fmt.Sprintf("event-key-%d", i),
		})
	}
	if err := repository.MarkRedisUsageInboxProcessedBatch(db, updates, processedAt); err != nil {
		t.Fatalf("MarkRedisUsageInboxProcessedBatch returned error: %v", err)
	}

	var stored []entities.RedisUsageInbox
	if err := db.Order("id ASC").Find(&stored).Error; err != nil {
		t.Fatalf("load processed inbox rows: %v", err)
	}
	if len(stored) != rowCount {
		t.Fatalf("expected %d inbox rows, got %d", rowCount, len(stored))
	}
	for i, row := range stored {
		if row.Status != repository.RedisUsageInboxStatusProcessed || row.UsageEventKey != fmt.Sprintf("event-key-%d", i) {
			t.Fatalf("unexpected processed mapping at index %d: %+v", i, row)
		}
		if row.ProcessedAt == nil || !row.ProcessedAt.Equal(processedAt) {
			t.Fatalf("unexpected processed_at at index %d: %+v", i, row.ProcessedAt)
		}
		if row.LastError != "" {
			t.Fatalf("expected last_error cleared at index %d, got %q", i, row.LastError)
		}
		if row.UpdatedAt.IsZero() {
			t.Fatalf("expected updated_at at index %d", i)
		}
	}
	if stored[0].UpdatedAt.Equal(oldUpdatedAt) {
		t.Fatalf("expected batch update to advance updated_at, still %s", stored[0].UpdatedAt)
	}
	for _, field := range []string{"processed_at", "updated_at"} {
		rawValue := rawSQLiteTimeValue(t, db, "redis_usage_inboxes", field, fmt.Sprintf("id = %d", rows[0].ID))
		assertProjectTimezoneStorageValue(t, rawValue, "redis_usage_inboxes."+field)
		if field == "processed_at" && rawValue != timeutil.FormatStorageTime(processedAt) {
			t.Fatalf("expected processed_at %q, got %q", timeutil.FormatStorageTime(processedAt), rawValue)
		}
	}
}
