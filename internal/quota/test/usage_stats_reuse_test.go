package test

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	. "cpa-usage-keeper/internal/quota"
)

func TestHeaderManualAndScheduledRefreshReuseSameWindowUsageStats(t *testing.T) {
	// 三个真实入口使用相同事件、价格和固定窗口，结果差异会直接暴露重复查询或计价实现。
	for _, entry := range []string{"header", "manual", "scheduled"} {
		t.Run(entry, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			seedUsageIdentity(t, db, entities.UsageIdentity{
				Identity: "shared-usage-auth", Provider: "codex", Type: "codex", AuthType: entities.UsageIdentityAuthTypeAuthFile,
			})
			resetAt := time.Now().Add(-time.Hour).Truncate(time.Second)
			seedUsageEvent(t, db, entities.UsageEvent{
				EventKey: "shared-usage-event", AuthType: "oauth", AuthIndex: "shared-usage-auth", Model: "priced-model",
				Timestamp: resetAt.Add(-2 * time.Hour), InputTokens: 1_000_000, OutputTokens: 500_000, TotalTokens: 1_500_000,
			})

			// 固定价格快照避免测试依赖运行时价格表，同时让 cost 不是无意义的零值。
			snapshot, err := pricing.CompileSnapshot([]pricing.ModelConfig{{Pricing: entities.ModelPriceSetting{
				Model: "priced-model", PromptPricePer1M: 3, CompletionPricePer1M: 15,
			}}})
			if err != nil {
				t.Fatalf("compile quota pricing snapshot: %v", err)
			}
			providerOutput := ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{RateLimit: &CodexRateLimitInfo{
				PrimaryWindow: &CodexUsageWindow{UsedPercent: 4, LimitWindowSeconds: int64(5 * time.Hour / time.Second), ResetAt: resetAt.Unix()},
			}}}}
			handler := &refreshHandlerStub{output: providerOutput}
			service := NewServiceWithRegistry(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), pricing.NewCatalog(snapshot))
			t.Cleanup(service.StopRefreshTasks)
			setRefreshCooldown(service, func(time.Duration) {})

			switch entry {
			case "header":
				headers := codexUsageHeader("4")
				headers.Set("X-Codex-Primary-Reset-At", strconv.FormatInt(resetAt.Unix(), 10))
				header := codexUsageHeaderSnapshotWithHeaders("shared-usage-auth", resetAt.Add(time.Minute), headers)
				if !applyUsageHeaderSnapshot(service, context.Background(), header) {
					t.Fatal("expected Header path to populate quota cache")
				}
			case "manual":
				if _, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"shared-usage-auth"}, Source: RefreshSourceManual}); err != nil {
					t.Fatalf("queue manual quota refresh: %v", err)
				}
			case "scheduled":
				if err := service.RunAutoRefresh(context.Background()); err != nil {
					t.Fatalf("run scheduled quota refresh: %v", err)
				}
			}

			task := waitForRefreshTask(t, service, "shared-usage-auth", RefreshTaskStatusCompleted)
			if task.Quota == nil || len(task.Quota.Quota) != 1 {
				t.Fatalf("expected one quota window from %s path, got %+v", entry, task)
			}
			row := task.Quota.Quota[0]
			if row.WindowUsageTokens == nil || *row.WindowUsageTokens != 1_500_000 {
				t.Fatalf("expected shared token result from %s path, got %#v", entry, row.WindowUsageTokens)
			}
			const wantCost = 10.5
			if row.WindowUsageCost == nil || math.Abs(*row.WindowUsageCost-wantCost) > 1e-9 {
				t.Fatalf("expected shared cost %.2f from %s path, got %#v", wantCost, entry, row.WindowUsageCost)
			}
		})
	}
}
