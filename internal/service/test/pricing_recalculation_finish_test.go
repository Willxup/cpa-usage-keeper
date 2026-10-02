package test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type recalculationFinishProvider struct{}

func (recalculationFinishProvider) Check(context.Context, quota.ProviderInput) (quota.ProviderOutput, error) {
	return quota.ProviderOutput{Provider: "gemini-cli", Result: quota.GeminiCLIResult{
		Quota: &quota.GeminiCliQuotaPayload{Buckets: []quota.GeminiCliQuotaBucket{{ModelID: "gemini", TokenType: "PROMPT", RemainingAmount: 42}}},
	}}, nil
}

// TestFinishPricingRecalculationRefreshesCachesBeforeReleasingRealPermits 验证成功、读库失败和生命周期取消共用同一退出顺序。
func TestFinishPricingRecalculationRefreshesCachesBeforeReleasingRealPermits(t *testing.T) {
	for _, scenario := range []string{"success", "database_failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			db := openSyncTestDatabase(t)
			now := time.Now()
			cost, available := 0.5, true
			event := entities.UsageEvent{EventKey: "finish-event", Model: "gemini", APIGroupKey: "key", AuthType: "oauth", AuthIndex: "finish-auth", Timestamp: now.Add(-time.Minute), CostUSD: &cost, CostAvailable: &available}
			if err := db.Create(&event).Error; err != nil {
				t.Fatal(err)
			}
			recent, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(recent.Close)
			identity := entities.UsageIdentity{Identity: "finish-auth", AuthType: entities.UsageIdentityAuthTypeAuthFile, Type: "gemini-cli", Provider: "gemini-cli"}
			if err := db.Create(&identity).Error; err != nil {
				t.Fatal(err)
			}
			quotas := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"gemini-cli": recalculationFinishProvider{}}))
			t.Cleanup(quotas.StopRefreshTasks)
			request := quota.CacheRequest{AuthIndexes: []string{"finish-auth"}}
			refresh, err := quotas.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: request.AuthIndexes, Source: quota.RefreshSourceManual})
			if err != nil || refresh.Accepted != 1 {
				t.Fatalf("seed quota refresh: %+v, %v", refresh, err)
			}
			quotas.WaitRefreshTasks()
			before, err := quotas.GetCachedQuota(context.Background(), request)
			if err != nil || len(before.Items) != 1 {
				t.Fatalf("seed quota cache: %+v, %v", before, err)
			}

			syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{RecentUsageEvents: recent, PricingCatalog: emptyPricingCatalogForTest()})
			runner := poller.NewUsageAggregationRunner(db)
			reads := service.NewCostReadGate()
			resumeUsage, err := syncer.PauseUsageWork(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer resumeUsage()
			resumeAggregation, err := runner.Pause(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer resumeAggregation()
			resumeReads, err := reads.BlockAndDrain(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer resumeReads()
			if err := db.Model(&entities.UsageEvent{}).Where("id = ?", event.ID).Update("cost_usd", 2).Error; err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			databaseErr := errors.New("injected recent reload failure")
			if scenario == "database_failure" {
				if err := db.Callback().Query().Before("gorm:query").Register("test:finish_reload_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "usage_events" {
						tx.AddError(databaseErr)
					}
				}); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			released := 0
			err = service.FinishPricingRecalculation(ctx, db, recent, quotas, func() {
				released++
				// 退出回调开始时费用读取仍关闭，缓存清理已结束；错误路径不能略过 quota。
				if release, ok := reads.Acquire(); ok {
					release()
					t.Error("fee reads resumed before cache cleanup finished")
				}
				cached, cacheErr := quotas.GetCachedQuota(context.Background(), request)
				if cacheErr != nil || len(cached.Items) != 0 {
					t.Errorf("quota cache survived cleanup: %+v, %v", cached, cacheErr)
				}
				events, ok := recent.EventsSince(now.Add(-time.Hour), "")
				if scenario == "success" {
					if !ok || len(events) != 1 || events[0].CostUSD == nil || math.Abs(*events[0].CostUSD-2) > 1e-12 {
						t.Errorf("recent cache not refreshed before release: %+v, ok=%v", events, ok)
					}
				} else if ok {
					t.Error("failed reload left stale recent fee reads enabled")
				}
				resumeAggregation()
				resumeUsage()
				resumeReads()
			})
			if released != 1 {
				t.Fatalf("release callback calls=%d, want 1", released)
			}
			switch scenario {
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			case "database_failure":
				if !errors.Is(err, databaseErr) {
					t.Fatalf("lost database error: %v", err)
				}
				if err := db.Callback().Query().Remove("test:finish_reload_failure"); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost lifecycle cancellation: %v", err)
				}
			}
			health, ok := recent.CredentialHealth("oauth", "finish-auth", now)
			if !ok || health.TotalSuccess != 1 {
				t.Fatalf("fee cleanup modified health: %+v, ok=%v", health, ok)
			}
			if release, ok := reads.Acquire(); !ok {
				t.Fatal("fee reads remain blocked")
			} else {
				release()
			}
			// 用实际入口证明暂停已释放，而不只检查回调次数。无新 inbox，不会影响保留的费用。
			workCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := syncer.CleanupRedisUsageInbox(workCtx); err != nil {
				t.Fatalf("maintenance remains paused: %v", err)
			}
			if _, err := syncer.ProcessRedisUsageInbox(workCtx); err != nil {
				t.Fatalf("event processing remains paused: %v", err)
			}
			if _, err := runner.RunOnce(workCtx); err != nil {
				t.Fatalf("aggregation remains paused: %v", err)
			}
		})
	}
}
