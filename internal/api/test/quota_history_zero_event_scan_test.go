package test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/api"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type apiUsageEventsSQLRecorder struct {
	mu           sync.Mutex
	eventQueries []string
}

func (r *apiUsageEventsSQLRecorder) LogMode(gormlogger.LogLevel) gormlogger.Interface { return r }
func (r *apiUsageEventsSQLRecorder) Info(context.Context, string, ...any)            {}
func (r *apiUsageEventsSQLRecorder) Warn(context.Context, string, ...any)            {}
func (r *apiUsageEventsSQLRecorder) Error(context.Context, string, ...any)           {}
func (r *apiUsageEventsSQLRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	lower := strings.ToLower(sql)
	if strings.Contains(lower, "usage_events") {
		r.mu.Lock()
		r.eventQueries = append(r.eventQueries, sql)
		r.mu.Unlock()
	}
}

func (r *apiUsageEventsSQLRecorder) reset() {
	r.mu.Lock()
	r.eventQueries = nil
	r.mu.Unlock()
}

func (r *apiUsageEventsSQLRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.eventQueries...)
}

func TestAntigravityQuotaHistoryAPINeverQueriesUsageEventsTables(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "ag-history-api.db")})
	if err != nil {
		t.Fatalf("open api test database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, authIndex := range []string{"ag-empty", "ag-full"} {
		if err := db.Create(&entities.UsageIdentity{
			Identity: authIndex,
			Provider: "antigravity",
			Type:     "antigravity",
			AuthType: entities.UsageIdentityAuthTypeCredential,
		}).Error; err != nil {
			t.Fatalf("seed identity %s: %v", authIndex, err)
		}
	}

	fiveReset := now.Add(3 * time.Hour)
	weeklyReset := now.Add(5 * 24 * time.Hour)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{
		{Provider: "antigravity", AuthIndex: "ag-full", WindowRole: "gemini_5h", WindowSeconds: 5 * 3600, ResetAtSource: "absolute", ResetAt: fiveReset, RemainingPercent: 95, FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now.Add(-time.Hour), ObservationCount: 1},
		{Provider: "antigravity", AuthIndex: "ag-full", WindowRole: "claude_gpt_weekly", WindowSeconds: 7 * 24 * 3600, ResetAtSource: "absolute", ResetAt: weeklyReset, RemainingPercent: 80, FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now.Add(-time.Hour), ObservationCount: 1},
	}); err != nil {
		t.Fatalf("seed Antigravity observations: %v", err)
	}
	cost, unavailable := 0.5, false
	if err := db.Create(&entities.UsageEvent{
		EventKey: "ag-api-ev", APIGroupKey: "key-1", Provider: "antigravity", AuthType: "oauth", AuthIndex: "ag-full",
		Model: "gemini-4-carbon-high", Timestamp: now.Add(-30 * time.Minute), TotalTokens: 1000, CostUSD: &cost, CostAvailable: &unavailable, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ?", "antigravity").Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, cycles, now, 0, false); err != nil {
		t.Fatalf("recompute Antigravity cycles: %v", err)
	}
	// 把第二个窗口标为 cost_stale=true，验证缺价和 cost_stale 场景同样零明细表扫描。
	if err := db.Model(&entities.QuotaCycle{}).Where("quota_key = ?", "bucket.antigravity-claude-and-gpt-models.weekly").Update("cost_stale", true).Error; err != nil {
		t.Fatal(err)
	}

	recorder := &apiUsageEventsSQLRecorder{}
	observedDB := db.Session(&gorm.Session{Logger: recorder})
	service := quota.NewServiceWithRegistry(observedDB, quota.NewProviderRegistry(nil))
	t.Cleanup(service.StopRefreshTasks)
	router := api.NewRouter(nil, nil, nil, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{Quota: service})

	for _, path := range []string{
		"/api/v1/quota/history/ag-empty",
		"/api/v1/quota/history/ag-full",
		"/api/v1/quota/history/ag-full?window_role=gemini_5h",
		"/api/v1/quota/history/ag-full?window_role=claude_gpt_weekly",
	} {
		recorder.reset()
		resp := serveAPIGet(router, path)
		if resp.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, resp.Code, resp.Body.String())
		}
		if queries := recorder.snapshot(); len(queries) != 0 {
			t.Fatalf("GET %s must never query usage_events or usage_events_archive, got: %v", path, queries)
		}
		if strings.Contains(path, "ag-full") {
			var payload quota.CodexQuotaHistoryResponse
			if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode %s response: %v", path, err)
			}
			if payload.SelectedWindow == nil || payload.SelectedWindow.GroupKey == "" || payload.SelectedWindow.GroupLabel == "" {
				t.Fatalf("expected group_key/group_label in %s response, got %+v", path, payload.SelectedWindow)
			}
		}
	}
}

func TestXAIQuotaHistoryAPINeverQueriesUsageEventsTables(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "xai-history-api.db")})
	if err != nil {
		t.Fatalf("open api test database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, authIndex := range []string{"xai-empty", "xai-full"} {
		if err := db.Create(&entities.UsageIdentity{
			Identity: authIndex,
			Provider: "xai",
			Type:     "xai",
			AuthType: entities.UsageIdentityAuthTypeCredential,
		}).Error; err != nil {
			t.Fatalf("seed identity %s: %v", authIndex, err)
		}
	}

	weeklyReset := now.Add(5 * 24 * time.Hour)
	if err := repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{
		{Provider: "xai", AuthIndex: "xai-full", WindowRole: "weekly", WindowSeconds: 7 * 24 * 3600, ResetAtSource: "absolute", ResetAt: weeklyReset, RemainingPercent: 75, FirstObservedAt: now.Add(-time.Hour), LastObservedAt: now.Add(-time.Hour), ObservationCount: 1},
	}); err != nil {
		t.Fatalf("seed xAI observation: %v", err)
	}
	cost, unavailable := 0.25, false
	if err := db.Create(&entities.UsageEvent{
		EventKey: "xai-api-ev", APIGroupKey: "key-1", Provider: "xai", AuthType: "oauth", AuthIndex: "xai-full",
		Model: "grok-4", Timestamp: now.Add(-30 * time.Minute), TotalTokens: 1200, CostUSD: &cost, CostAvailable: &unavailable, CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed usage event: %v", err)
	}
	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ?", "xai").Find(&cycles).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RecomputeAndPersistQuotaCycles(ctx, db, cycles, now, 0, false); err != nil {
		t.Fatalf("recompute xAI cycles: %v", err)
	}

	recorder := &apiUsageEventsSQLRecorder{}
	observedDB := db.Session(&gorm.Session{Logger: recorder})
	service := quota.NewServiceWithRegistry(observedDB, quota.NewProviderRegistry(nil))
	t.Cleanup(service.StopRefreshTasks)
	router := api.NewRouter(nil, nil, nil, nil, api.AuthConfig{}, nil, "", api.OptionalProviders{Quota: service})

	for _, path := range []string{
		"/api/v1/quota/history/xai-empty",
		"/api/v1/quota/history/xai-full",
		"/api/v1/quota/history/xai-full?window_role=weekly",
	} {
		recorder.reset()
		resp := serveAPIGet(router, path)
		if resp.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, resp.Code, resp.Body.String())
		}
		if queries := recorder.snapshot(); len(queries) != 0 {
			t.Fatalf("GET %s must never query usage_events or usage_events_archive, got: %v", path, queries)
		}
	}
}
