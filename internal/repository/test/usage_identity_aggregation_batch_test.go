package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestAggregateUsageIdentityStatsBatchCommitsBoundedIdentityPages(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	db := openTestDatabase(t)

	// 构造比单批上限多 2 个 identities，确保必须分两个事务完成。
	identityCount := repository.UsageIdentityAggregationBatchSize + 2
	identities := make([]entities.UsageIdentity, 0, identityCount)
	events := make([]entities.UsageEvent, 0, identityCount)
	for index := 1; index <= identityCount; index++ {
		identity := fmt.Sprintf("batch-auth-%02d", index)
		identities = append(identities, entities.UsageIdentity{
			Name: identity, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: identity, Type: "codex",
		})
		events = append(events, entities.UsageEvent{
			EventKey: fmt.Sprintf("batch-event-%02d", index), AuthType: "oauth", AuthIndex: identity,
			Timestamp: now.Add(-time.Duration(index) * time.Minute), InputTokens: int64(index),
			CachedTokens: int64(index * 10), CacheReadTokens: int64(index), TotalTokens: int64(index),
		})
	}
	if err := db.Create(&identities).Error; err != nil {
		t.Fatalf("seed batched identities: %v", err)
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert batched identity events: %v", err)
	}

	first, err := repository.AggregateUsageIdentityStatsBatch(context.Background(), db, now, 0)
	if err != nil {
		t.Fatalf("first AggregateUsageIdentityStatsBatch: %v", err)
	}
	if first.ProcessedIdentities != repository.UsageIdentityAggregationBatchSize || first.ReachedEnd {
		t.Fatalf("unexpected first batch result: %+v", first)
	}
	if first.LastIdentityID != identities[repository.UsageIdentityAggregationBatchSize-1].ID {
		t.Fatalf("unexpected first batch cursor: got=%d want=%d", first.LastIdentityID, identities[repository.UsageIdentityAggregationBatchSize-1].ID)
	}
	assertAggregatedUsageIdentityCount(t, db, int64(repository.UsageIdentityAggregationBatchSize))

	second, err := repository.AggregateUsageIdentityStatsBatch(context.Background(), db, now, first.LastIdentityID)
	if err != nil {
		t.Fatalf("second AggregateUsageIdentityStatsBatch: %v", err)
	}
	if second.ProcessedIdentities != 2 || !second.ReachedEnd {
		t.Fatalf("unexpected second batch result: %+v", second)
	}
	if second.LastIdentityID != identities[len(identities)-1].ID {
		t.Fatalf("unexpected second batch cursor: got=%d want=%d", second.LastIdentityID, identities[len(identities)-1].ID)
	}
	assertAggregatedUsageIdentityCount(t, db, int64(identityCount))

	empty, err := repository.AggregateUsageIdentityStatsBatch(context.Background(), db, now, second.LastIdentityID)
	if err != nil {
		t.Fatalf("empty AggregateUsageIdentityStatsBatch: %v", err)
	}
	if empty.ProcessedIdentities != 0 || !empty.ReachedEnd || empty.LastIdentityID != second.LastIdentityID {
		t.Fatalf("unexpected empty batch result: %+v", empty)
	}
}

func assertAggregatedUsageIdentityCount(t *testing.T, db *gorm.DB, want int64) {
	// total_requests=1 精确表示该 identity 已经完成本轮唯一事件聚合。
	t.Helper()
	var count int64
	if err := db.Model(&entities.UsageIdentity{}).Where("total_requests = ?", 1).Count(&count).Error; err != nil {
		t.Fatalf("count aggregated identities: %v", err)
	}
	if count != want {
		t.Fatalf("expected %d aggregated identities, got %d", want, count)
	}
}
