package test

import (
	"context"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"testing"
	"time"
)

func TestUsageIdentityStatsResetSortsByPeriodAndRetainsDeletedBaselines(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Now()
	for _, identity := range []entities.UsageIdentity{
		{ID: 1, Identity: "first", AuthType: 1, TotalRequests: 100, TotalTokens: 1000},
		{ID: 2, Identity: "second", AuthType: 1, TotalRequests: 20, TotalTokens: 200},
		{ID: 3, Identity: "provider", AuthType: 2, TotalRequests: 100, TotalTokens: 1000},
		{ID: 4, Identity: "other-provider", AuthType: 2, TotalRequests: 20, TotalTokens: 200},
	} {
		if err := db.Create(&identity).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{1, 3} {
		if err := repository.ResetUsageIdentityStats(ctx, db, id, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, authType := range []entities.UsageIdentityAuthType{1, 2} {
		for _, sort := range []string{"total_requests", "total_tokens"} {
			rows, total, _, err := repository.ListActiveUsageIdentitiesPage(ctx, db, repository.ListUsageIdentitiesPageRequest{AuthType: &authType, Sort: sort, Page: 1, PageSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			if total != 2 || len(rows) != 1 || rows[0].ID != int64(authType)*2 {
				t.Fatalf("sort %s type %d: %+v total %d", sort, authType, rows, total)
			}
		}
	}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, nil, 1, now); err != nil {
		t.Fatal(err)
	}
	if err := repository.ResetUsageIdentityStats(ctx, db, 1, now); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, []entities.UsageIdentity{{Identity: "first", Name: "Restored"}}, 1, now); err != nil {
		t.Fatal(err)
	}
	row, err := repository.FindUsageIdentityByID(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if row.IsDeleted || row.ResetTotalRequests != 100 || row.StatsResetAt == nil {
		t.Fatalf("restoration lost baseline: %+v", row)
	}
}

func TestUsageIdentityStatsResetAndAggregationShareAtomicSnapshot(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	now := time.Now()
	identity := entities.UsageIdentity{ID: 1, Identity: "concurrent", AuthType: 1, TotalRequests: 10, SuccessCount: 10, TotalTokens: 100, InputTokens: 80, CacheReadTokens: 20}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	event := entities.UsageEvent{EventKey: "concurrent", AuthType: "oauth", AuthIndex: identity.Identity, Timestamp: now, TotalTokens: 10, InputTokens: 8, CacheReadTokens: 2}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	go func() { <-start; results <- repository.ResetUsageIdentityStats(ctx, db, 1, now) }()
	go func() { <-start; results <- repository.AggregateUsageIdentityStats(ctx, db, now) }()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	row, err := repository.FindUsageIdentityByID(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if row.TotalRequests != 11 || row.TotalTokens != 110 || row.LastAggregatedUsageEventID != event.ID {
		t.Fatalf("lost aggregate: %+v", row)
	}
	if row.ResetTotalRequests != 10 && row.ResetTotalRequests != 11 {
		t.Fatalf("bad baseline: %+v", row)
	}
	if row.ResetSuccessCount != row.ResetTotalRequests || row.ResetTotalTokens != row.ResetTotalRequests*10 || row.ResetInputTokens != row.ResetTotalRequests*8 || row.ResetCacheReadTokens != row.ResetTotalRequests*2 {
		t.Fatalf("torn snapshot: %+v", row)
	}
}
