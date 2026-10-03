package test

import (
	"context"
	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"
	"net/http"
	"testing"
	"time"
)

func TestTrafficFreshnessIsPerWindowAndNeverChangesOnRead(t *testing.T) {
	db := openQuotaTestDatabase(t)
	for _, index := range []string{"account-a", "account-b"} {
		seedUsageIdentity(t, db, entities.UsageIdentity{Identity: index, Name: index, Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile})
	}
	service := NewServiceWithRegistry(db, NewProviderRegistry(nil), emptyPricingCatalogForTest())
	defer service.StopRefreshTasks()
	at := time.Now().Truncate(time.Second)
	apply := func(index, window, used string, at time.Time, source RefreshSource) {
		h := http.Header{}
		prefix := "X-Codex-" + window + "-"
		h.Set(prefix+"Used-Percent", used)
		h.Set(prefix+"Window-Minutes", "300")
		h.Set(prefix+"Reset-After-Seconds", "120")
		snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: index, Provider: "codex", ObservedAt: at, Source: source, Headers: h})
		if !ok {
			t.Fatal("snapshot rejected")
		}
		applyUsageHeaderSnapshot(service, context.Background(), *snapshot)
	}
	apply("account-a", "Primary", "10", at, "websocket_event")
	apply("account-a", "Secondary", "20", at.Add(-time.Minute), "api_response_headers")
	apply("account-b", "Primary", "90", at, "api_response_headers")
	apply("account-a", "Primary", "99", at.Add(-time.Second), "api_response_headers")
	result, err := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"account-a", "account-b"}})
	if err != nil || len(result.Items) != 2 {
		t.Fatal("account caches missing")
	}
	rows := result.Items[0].Quota.Quota
	if len(rows) != 2 {
		t.Fatalf("independent older window lost: %d", len(rows))
	}
	for _, row := range rows {
		if row.Key == "rate_limit.primary_window" {
			if row.UsedPercent == nil || *row.UsedPercent != 10 || !row.CapturedAt.Equal(at) || row.Source != "websocket_event" {
				t.Fatalf("wrong primary provenance %+v", row)
			}
		} else if !row.CapturedAt.Equal(at.Add(-time.Minute)) {
			t.Fatal("older independent window capture time lost")
		}
	}
	again, _ := service.GetCachedQuota(context.Background(), CacheRequest{AuthIndexes: []string{"account-a"}})
	if !again.Items[0].Quota.Quota[0].CapturedAt.Equal(rows[0].CapturedAt) {
		t.Fatal("cache read refreshed timestamp")
	}
}
