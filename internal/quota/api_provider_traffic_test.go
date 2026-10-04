package quota

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type apiQuotaObservationFixture []cpa.QuotaObservation

func (f apiQuotaObservationFixture) FetchQuotaObservations(context.Context) ([]cpa.QuotaObservation, error) {
	return f, nil
}

func apiQuotaTestService(t *testing.T) (*Service, *gorm.DB, entities.UsageIdentity) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fixture.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(entities.All()...); err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&entities.CodexQuotaCycle{}, &entities.CodexQuotaPercentSegment{}); err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	identity := entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "api-fixture", Type: "codex", IsDeleted: false}
	if err = db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	service := NewService(db, nil, pricing.NewCatalog(pricing.EmptySnapshot()))
	t.Cleanup(service.StopRefreshTasks)
	return service, db, identity
}

func apiQuotaFixture(t *testing.T, at time.Time) (cpa.QuotaObservation, *UsageHeaderSnapshot) {
	t.Helper()
	item := cpa.QuotaObservation{AuthIndex: "api-fixture", Provider: "codex", Window: "primary", ObservedAt: at, Source: "api_response_headers", Headers: http.Header{"X-Codex-Primary-Used-Percent": {"25"}, "X-Codex-Primary-Window-Minutes": {"300"}, "X-Codex-Primary-Reset-After-Seconds": {"120"}}}
	snapshot, ok := BuildUsageHeaderSnapshot(UsageHeaderSnapshotInput{AuthType: "oauth", AuthIndex: item.AuthIndex, Provider: item.Provider, ObservedAt: at, Headers: item.Headers, Source: trafficQuotaSource(item.Source)})
	if !ok {
		t.Fatal("invalid fixture")
	}
	return item, snapshot
}

func TestAPIProviderTrafficOnlyUpdatesCacheAndDeduplicates(t *testing.T) {
	service, db, _ := apiQuotaTestService(t)
	at := time.Now().Add(-time.Second)
	item, _ := apiQuotaFixture(t, at)
	seen, err := service.syncTrafficQuota(context.Background(), apiQuotaObservationFixture{item, item}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatal(seen)
	}
	for _, captured := range seen {
		if !captured.Equal(at) {
			t.Fatal("duplicate observation lost its successful timestamp", seen)
		}
	}
	service.refreshMu.Lock()
	cache := service.refreshTasks[item.AuthIndex]
	service.refreshMu.Unlock()
	if cache == nil || cache.Quota == nil || len(cache.Quota.Quota) != 1 || !cache.Quota.Quota[0].CapturedAt.Equal(at) || cache.Quota.Quota[0].Source != "api_response_headers" {
		t.Fatalf("incorrect API cache: %+v", cache)
	}
	next, err := service.syncTrafficQuota(context.Background(), apiQuotaObservationFixture{item}, seen)
	if err != nil || len(next) != 1 {
		t.Fatal(next, err)
	}
	service.usageHeaderMu.Lock()
	pending := len(service.usageHeaderPending)
	service.usageHeaderMu.Unlock()
	if pending != 0 {
		t.Fatal("API provider entered Auth File pending worker")
	}
	service.StopRefreshTasks()
	for _, model := range []any{&entities.CodexQuotaCycle{}, &entities.CodexQuotaPercentSegment{}, &entities.QuotaCycle{}, &entities.QuotaPercentSegment{}} {
		var count int64
		if err = db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("API observation entered quota history: %T count=%d err=%v", model, count, err)
		}
	}
}

func TestAPIProviderTrafficRevalidatesChangedAccounts(t *testing.T) {
	for _, change := range []map[string]any{{"disabled": true}, {"is_deleted": true}, {"auth_type": entities.UsageIdentityAuthTypeAuthFile}, {"type": "claude"}, {"identity": "replacement"}} {
		service, db, identity := apiQuotaTestService(t)
		_, snapshot := apiQuotaFixture(t, time.Now())
		if err := db.Model(&entities.UsageIdentity{}).Where("id = ?", identity.ID).Updates(change).Error; err != nil {
			t.Fatal(err)
		}
		if service.applyTrafficAPIProviderQuota(context.Background(), snapshot, identity) {
			t.Fatalf("stale account accepted: %v", change)
		}
		service.refreshMu.Lock()
		if len(service.refreshTasks) != 0 {
			t.Fatal("stale account populated cache")
		}
		service.refreshMu.Unlock()
	}
}

func TestAPIProviderTrafficPreservesActiveAndNewerCache(t *testing.T) {
	service, _, identity := apiQuotaTestService(t)
	at := time.Now()
	_, snapshot := apiQuotaFixture(t, at)
	service.refreshMu.Lock()
	service.refreshTasks[identity.Identity] = &RefreshTaskRecord{AuthIndex: identity.Identity, Type: "codex", Status: RefreshTaskStatusRunning}
	service.refreshMu.Unlock()
	if service.applyTrafficAPIProviderQuota(context.Background(), snapshot, identity) {
		t.Fatal("active task replaced")
	}
	service.refreshMu.Lock()
	delete(service.refreshTasks, identity.Identity)
	service.refreshMu.Unlock()
	if !service.applyTrafficAPIProviderQuota(context.Background(), snapshot, identity) {
		t.Fatal("initial API cache missing")
	}
	_, older := apiQuotaFixture(t, at.Add(-time.Minute))
	if service.applyTrafficAPIProviderQuota(context.Background(), older, identity) {
		t.Fatal("older window replaced newer cache")
	}
}
