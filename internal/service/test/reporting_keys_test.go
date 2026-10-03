package test

import (
	"context"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"testing"
	"time"
)

func TestHistoricalReportingDoesNotReactivateRevokedKey(t *testing.T) {
	db := openUsageServiceTestDatabase(t)
	now := time.Now()
	if err := repository.SyncCPAAPIKeys(db, []string{"active-key", "retired-key"}, now); err != nil {
		t.Fatal(err)
	}
	if err := repository.SyncCPAAPIKeys(db, []string{"active-key"}, now); err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"retired-key", "orphan-key", "active-key"} {
		if err := db.Create(&entities.UsageEvent{ID: int64(i + 1), APIGroupKey: key, Model: "model", Timestamp: now, EventKey: key, TotalTokens: int64((i + 1) * 100)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	keys := service.NewCPAAPIKeyService(db)
	rows, err := keys.(service.ReportingAPIKeyProvider).ListReportingAPIKeys(context.Background())
	if err != nil || len(rows) != 3 {
		t.Fatalf("historical reporting keys: %d %v", len(rows), err)
	}
	if _, err := keys.FindActiveCPAAPIKeyByValue(context.Background(), "retired-key"); err == nil {
		t.Fatal("reporting reactivated retired authentication")
	}
	usage := service.NewUsageService(db, emptyPricingCatalogForTest())
	for _, key := range rows {
		if !key.Historical {
			continue
		}
		events, err := usage.ListUsageEvents(context.Background(), servicedto.UsageFilter{ReportingAPIGroupKey: key.APIKey, Page: 1, PageSize: 50, Limit: 50})
		if err != nil || len(events.Events) != 1 || events.Events[0].APIGroupKey != key.APIKey {
			t.Fatalf("historical scope failed: %v", err)
		}
	}
}
