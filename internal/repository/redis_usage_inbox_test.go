package repository

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/models"
)

func TestInsertRedisUsageInboxMessagesPersistsPendingRows(t *testing.T) {
	db := openTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

	rows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{
		{QueueKey: "queue", RawMessage: `{"request_id":"one"}`, PoppedAt: poppedAt},
		{QueueKey: "queue", RawMessage: `{"request_id":"two"}`, PoppedAt: poppedAt.Add(time.Second)},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	var stored []models.RedisUsageInbox
	if err := db.Order("id asc").Find(&stored).Error; err != nil {
		t.Fatalf("load inbox rows: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected 2 stored rows, got %d", len(stored))
	}
	if stored[0].Status != RedisUsageInboxStatusPending {
		t.Fatalf("expected pending status, got %q", stored[0].Status)
	}
	if stored[0].AttemptCount != 0 {
		t.Fatalf("expected initial attempt count 0, got %d", stored[0].AttemptCount)
	}
	if stored[0].RawMessage != `{"request_id":"one"}` {
		t.Fatalf("unexpected raw message: %q", stored[0].RawMessage)
	}
	if stored[0].MessageHash != fmt.Sprintf("%x", sha256.Sum256([]byte(stored[0].RawMessage))) {
		t.Fatalf("unexpected message hash: %q", stored[0].MessageHash)
	}
	if !stored[1].PoppedAt.Equal(poppedAt.Add(time.Second)) {
		t.Fatalf("expected popped_at to be stored, got %s", stored[1].PoppedAt)
	}
}

func TestInsertRedisUsageInboxMessagesAllowsEmptyInput(t *testing.T) {
	db := openTestDatabase(t)

	rows, err := InsertRedisUsageInboxMessages(db, nil)
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %d", len(rows))
	}
}

func TestRedisUsageInboxStatusTransitions(t *testing.T) {
	db := openTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	processedAt := poppedAt.Add(time.Minute)

	rows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{{QueueKey: "queue", RawMessage: `{"request_id":"one"}`, PoppedAt: poppedAt}})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}

	if err := MarkRedisUsageInboxProcessed(db, rows[0].ID, 42, "event-1", processedAt); err != nil {
		t.Fatalf("MarkRedisUsageInboxProcessed returned error: %v", err)
	}

	var stored models.RedisUsageInbox
	if err := db.First(&stored, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if stored.Status != RedisUsageInboxStatusProcessed {
		t.Fatalf("expected processed status, got %q", stored.Status)
	}
	if stored.SnapshotRunID == nil || *stored.SnapshotRunID != 42 {
		t.Fatalf("expected snapshot id 42, got %+v", stored.SnapshotRunID)
	}
	if stored.UsageEventKey != "event-1" {
		t.Fatalf("expected event key to be stored, got %q", stored.UsageEventKey)
	}
	if stored.ProcessedAt == nil || !stored.ProcessedAt.Equal(processedAt) {
		t.Fatalf("expected processed_at %s, got %+v", processedAt, stored.ProcessedAt)
	}
}

func TestRedisUsageInboxFailureTransitionsBoundErrors(t *testing.T) {
	db := openTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

	rows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{
		{QueueKey: "queue", RawMessage: `{bad`, PoppedAt: poppedAt},
		{QueueKey: "queue", RawMessage: `{"request_id":"two"}`, PoppedAt: poppedAt},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}

	longErr := fmt.Errorf("%s", strings.Repeat("界", 5000))
	if err := MarkRedisUsageInboxDecodeFailed(db, rows[0].ID, longErr); err != nil {
		t.Fatalf("MarkRedisUsageInboxDecodeFailed returned error: %v", err)
	}
	if err := MarkRedisUsageInboxProcessFailed(db, rows[1].ID, fmt.Errorf("insert failed")); err != nil {
		t.Fatalf("MarkRedisUsageInboxProcessFailed returned error: %v", err)
	}

	var stored []models.RedisUsageInbox
	if err := db.Order("id asc").Find(&stored).Error; err != nil {
		t.Fatalf("load inbox rows: %v", err)
	}
	if stored[0].Status != RedisUsageInboxStatusDecodeFailed {
		t.Fatalf("expected decode_failed, got %q", stored[0].Status)
	}
	if stored[0].AttemptCount != 1 {
		t.Fatalf("expected decode attempt count 1, got %d", stored[0].AttemptCount)
	}
	if len(stored[0].LastError) > redisUsageInboxMaxErrorLength {
		t.Fatalf("expected bounded decode error, got length %d", len(stored[0].LastError))
	}
	if !strings.HasSuffix(stored[0].LastError, "界") {
		t.Fatalf("expected bounded decode error to preserve valid utf-8, got %q", stored[0].LastError)
	}
	if stored[1].Status != RedisUsageInboxStatusProcessFailed {
		t.Fatalf("expected process_failed, got %q", stored[1].Status)
	}
	if stored[1].AttemptCount != 1 || stored[1].LastError != "insert failed" {
		t.Fatalf("unexpected process failure fields: %+v", stored[1])
	}
}

func TestMarkRedisUsageInboxProcessFailedKeepsRowsRetryableAfterRepeatedFailures(t *testing.T) {
	db := openTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

	rows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{{QueueKey: "queue", RawMessage: `{"request_id":"retry"}`, PoppedAt: poppedAt}})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := MarkRedisUsageInboxProcessFailed(db, rows[0].ID, fmt.Errorf("insert failed %d", i+1)); err != nil {
			t.Fatalf("MarkRedisUsageInboxProcessFailed attempt %d returned error: %v", i+1, err)
		}
	}

	var stored models.RedisUsageInbox
	if err := db.First(&stored, rows[0].ID).Error; err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if stored.Status != RedisUsageInboxStatusProcessFailed {
		t.Fatalf("expected process_failed after repeated process failures, got %q", stored.Status)
	}
	if stored.AttemptCount != 5 {
		t.Fatalf("expected 5 attempts, got %d", stored.AttemptCount)
	}
	if stored.LastError != "insert failed 5" {
		t.Fatalf("expected last error from final attempt, got %q", stored.LastError)
	}

	processable, err := ListProcessableRedisUsageInbox(db, 10)
	if err != nil {
		t.Fatalf("ListProcessableRedisUsageInbox returned error: %v", err)
	}
	if len(processable) != 1 || processable[0].ID != rows[0].ID {
		t.Fatalf("expected repeated process failure row to remain processable, got %+v", processable)
	}
}

func TestListProcessableRedisUsageInboxIncludesProcessFailedRows(t *testing.T) {
	db := openTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

	rows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{
		{QueueKey: "queue", RawMessage: `{"request_id":"pending"}`, PoppedAt: poppedAt},
		{QueueKey: "queue", RawMessage: `{"request_id":"retry"}`, PoppedAt: poppedAt},
		{QueueKey: "queue", RawMessage: `{bad`, PoppedAt: poppedAt},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if err := MarkRedisUsageInboxProcessFailed(db, rows[1].ID, fmt.Errorf("temporary insert failure")); err != nil {
		t.Fatalf("MarkRedisUsageInboxProcessFailed returned error: %v", err)
	}
	if err := MarkRedisUsageInboxDecodeFailed(db, rows[2].ID, fmt.Errorf("bad json")); err != nil {
		t.Fatalf("MarkRedisUsageInboxDecodeFailed returned error: %v", err)
	}

	processable, err := ListProcessableRedisUsageInbox(db, 10)
	if err != nil {
		t.Fatalf("ListProcessableRedisUsageInbox returned error: %v", err)
	}
	if len(processable) != 2 {
		t.Fatalf("expected 2 processable rows, got %d", len(processable))
	}
	if processable[0].ID != rows[0].ID || processable[1].ID != rows[1].ID {
		t.Fatalf("expected pending and process_failed rows in id order, got %+v", processable)
	}
}

func TestCleanupRedisUsageInboxRemovesOldProcessedAndFailedRows(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	db := openTestDatabase(t)
	now := time.Date(2026, 4, 27, 2, 30, 0, 0, time.UTC)

	rows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{
		{QueueKey: "queue", RawMessage: `{"request_id":"processed-old"}`, PoppedAt: now.Add(-48 * time.Hour)},
		{QueueKey: "queue", RawMessage: `{"request_id":"processed-today"}`, PoppedAt: now.Add(-time.Hour)},
		{QueueKey: "queue", RawMessage: `{"request_id":"failed-old"}`, PoppedAt: now.AddDate(0, 0, -8)},
		{QueueKey: "queue", RawMessage: `{"request_id":"failed-recent"}`, PoppedAt: now.AddDate(0, 0, -6)},
		{QueueKey: "queue", RawMessage: `{"request_id":"pending-old"}`, PoppedAt: now.AddDate(0, 0, -10)},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	oldProcessedAt := time.Date(2026, 4, 26, 15, 59, 59, 0, time.UTC)
	todayProcessedAt := time.Date(2026, 4, 26, 16, 0, 0, 0, time.UTC)
	if err := db.Model(&models.RedisUsageInbox{}).Where("id = ?", rows[0].ID).Updates(map[string]any{"status": RedisUsageInboxStatusProcessed, "processed_at": oldProcessedAt}).Error; err != nil {
		t.Fatalf("seed old processed row: %v", err)
	}
	if err := db.Model(&models.RedisUsageInbox{}).Where("id = ?", rows[1].ID).Updates(map[string]any{"status": RedisUsageInboxStatusProcessed, "processed_at": todayProcessedAt}).Error; err != nil {
		t.Fatalf("seed today processed row: %v", err)
	}
	if err := db.Model(&models.RedisUsageInbox{}).Where("id = ?", rows[2].ID).Updates(map[string]any{"status": RedisUsageInboxStatusProcessFailed, "updated_at": now.AddDate(0, 0, -8)}).Error; err != nil {
		t.Fatalf("seed old failed row: %v", err)
	}
	if err := db.Model(&models.RedisUsageInbox{}).Where("id = ?", rows[3].ID).Updates(map[string]any{"status": RedisUsageInboxStatusDecodeFailed, "updated_at": now.AddDate(0, 0, -6)}).Error; err != nil {
		t.Fatalf("seed recent failed row: %v", err)
	}

	result, err := CleanupRedisUsageInbox(db, now)
	if err != nil {
		t.Fatalf("CleanupRedisUsageInbox returned error: %v", err)
	}
	if result.ProcessedDeleted != 1 || result.FailedDeleted != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}

	var remaining []models.RedisUsageInbox
	if err := db.Order("id asc").Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining inbox rows: %v", err)
	}
	remainingIDs := make([]uint, 0, len(remaining))
	for _, row := range remaining {
		remainingIDs = append(remainingIDs, row.ID)
	}
	expectedIDs := []uint{rows[1].ID, rows[3].ID, rows[4].ID}
	if fmt.Sprint(remainingIDs) != fmt.Sprint(expectedIDs) {
		t.Fatalf("expected remaining ids %v, got %v", expectedIDs, remainingIDs)
	}
}

func TestListPendingRedisUsageInboxReturnsPendingRowsInIDOrder(t *testing.T) {
	db := openTestDatabase(t)
	poppedAt := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

	rows, err := InsertRedisUsageInboxMessages(db, []RedisInboxInsert{
		{QueueKey: "queue", RawMessage: `{"request_id":"one"}`, PoppedAt: poppedAt},
		{QueueKey: "queue", RawMessage: `{"request_id":"two"}`, PoppedAt: poppedAt},
		{QueueKey: "queue", RawMessage: `{"request_id":"three"}`, PoppedAt: poppedAt},
	})
	if err != nil {
		t.Fatalf("InsertRedisUsageInboxMessages returned error: %v", err)
	}
	if err := MarkRedisUsageInboxProcessed(db, rows[1].ID, 7, "event-2", poppedAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkRedisUsageInboxProcessed returned error: %v", err)
	}

	pending, err := ListPendingRedisUsageInbox(db, 10)
	if err != nil {
		t.Fatalf("ListPendingRedisUsageInbox returned error: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending rows, got %d", len(pending))
	}
	if pending[0].ID != rows[0].ID || pending[1].ID != rows[2].ID {
		t.Fatalf("expected pending rows in id order, got %+v", pending)
	}
}
