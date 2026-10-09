package test

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageIdentityAggregationPreservesStoredTimeBounds(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "current"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			db := openTestDatabase(t)
			identity := entities.UsageIdentity{Identity: "time-bounds", AuthType: entities.UsageIdentityAuthTypeAuthFile, Type: "codex"}
			if err := db.Create(&identity).Error; err != nil {
				t.Fatal(err)
			}
			base := time.Date(2026, 9, 12, 23, 0, 0, 123456789, time.Local)
			first, last := base.Add(-time.Hour), base.Add(2*time.Hour)
			// 插入顺序与时间顺序不同，并跨自然日，首尾时间不能按事件 ID 推断。
			events := []entities.UsageEvent{
				{AuthType: "oauth", AuthIndex: identity.Identity, Timestamp: last, TotalTokens: 30},
				{AuthType: "oauth", AuthIndex: identity.Identity, Timestamp: first, TotalTokens: 10},
				{AuthType: "oauth", AuthIndex: identity.Identity, Timestamp: base, TotalTokens: 20},
			}
			if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
				t.Fatal(err)
			}
			if legacy {
				for _, event := range events {
					if err := db.Model(&entities.UsageEvent{}).Where("id = ?", event.ID).
						Update("timestamp", event.Timestamp.Format("2006-01-02 15:04:05.999999999")).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := repository.AggregateUsageIdentityStats(context.Background(), db, last.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if err := db.First(&identity, identity.ID).Error; err != nil {
				t.Fatal(err)
			}
			if identity.FirstUsedAt == nil || !identity.FirstUsedAt.Equal(first) || identity.LastUsedAt == nil || !identity.LastUsedAt.Equal(last) {
				t.Fatalf("unexpected time bounds: first=%v last=%v; want %v / %v", identity.FirstUsedAt, identity.LastUsedAt, first, last)
			}
			if identity.TotalRequests != 3 || identity.TotalTokens != 60 || identity.LastAggregatedUsageEventID != events[2].ID {
				t.Fatalf("aggregation totals or cursor changed: requests=%d tokens=%d cursor=%d", identity.TotalRequests, identity.TotalTokens, identity.LastAggregatedUsageEventID)
			}
		})
	}
}
