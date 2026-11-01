package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

const completeClaudeBreakdown = `{"schema_version":2,"quality":"complete","total_tokens":470,"input":{"total_tokens":450,"uncached_tokens":100,"cache_read_tokens":300,"cache_write_tokens":50},"output":{"total_tokens":20,"non_reasoning_tokens":15,"reasoning_tokens":5},"unclassified_tokens":0}`

func breakdownInboxMessage(executor, accountingVersion, breakdown string) string {
	return fmt.Sprintf(`{"timestamp":"2026-10-23T08:00:00Z","model":"breakdown-model","provider":"plugin-provider","auth_type":"apikey","auth_index":"plugin-auth","executor_type":%q,"execution_id":"breakdown-execution","request_id":"breakdown-event","accounting_version":%s,"token_breakdown":%s,"tokens":{"input_tokens":100,"output_tokens":20,"reasoning_tokens":5,"cached_tokens":300,"cache_read_tokens":300,"cache_read_tokens_present":true,"cache_creation_tokens":50,"total_tokens":470}}`, executor, accountingVersion, breakdown)
}

func TestUnknownExecutorBreakdownNormalizesBeforePricingAndAggregation(t *testing.T) {
	for _, executor := range []string{"executorAdapter", "FutureExecutor", "", "unknown"} {
		t.Run(executor, func(t *testing.T) {
			db := openUsageServiceTestDatabase(t)
			provider, catalog := newCatalogPricingService(t, db)
			if _, err := provider.SavePricingModel(context.Background(), completeServiceConfig("breakdown-model", 1)); err != nil {
				t.Fatal(err)
			}
			// 即使 identity 带有不同协议的历史提示，完整 accounting 也必须先于 identity 生效。
			if err := db.Create(&entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "plugin-auth", Type: "gemini"}).Error; err != nil {
				t.Fatal(err)
			}
			lookups := registerTokenIdentityTypeLookupCallback(t, db, nil)
			seedRedisInboxMessagesForTest(t, db, breakdownInboxMessage(executor, "2", completeClaudeBreakdown))
			cache := &recordingRecentUsageAppender{allowed: true}
			notifier := &recordingUsageAggregationNotifier{}
			result, err := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
				PricingCatalog: catalog, RecentUsageEvents: cache, UsageAggregationNotifier: notifier,
			}).ProcessRedisUsageInbox(context.Background())
			if err != nil || result == nil || result.InsertedEvents != 1 {
				t.Fatalf("process breakdown: result=%+v err=%v", result, err)
			}
			event := loadTokenProcessorSyncEvent(t, db, "breakdown-event")
			if event.InputTokens != 450 || event.OutputTokens != 20 || event.ReasoningTokens != 5 || event.CacheReadTokens != 300 || event.CacheCreationTokens != 50 || event.TotalTokens != 470 {
				t.Fatalf("expected canonical breakdown tokens, got %+v", event)
			}
			if *lookups != 0 {
				t.Fatalf("complete breakdown must bypass identity lookup, got %d", *lookups)
			}
			assertStoredSyncFee(t, event, 100.0/1_000_000, true)
			if len(cache.events) != 1 || len(notifier.events) != 1 || cache.events[0].InputTokens != 450 || notifier.events[0].InputTokens != 450 {
				t.Fatalf("downstream notifications did not receive canonical input")
			}
			if len(cache.events[0].TokenBreakdown) != 0 || len(cache.events[0].AccountingVersion) != 0 || len(notifier.events[0].TokenBreakdown) != 0 {
				t.Fatal("consumed accounting must not be retained by downstream caches")
			}
			if err := repository.AggregateUsageOverviewStats(context.Background(), db, event.Timestamp.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			var hourly entities.UsageOverviewHourlyStat
			if err := db.Where("model = ?", "breakdown-model").First(&hourly).Error; err != nil {
				t.Fatal(err)
			}
			if hourly.InputTokens != 450 || hourly.CacheReadTokens != 300 || hourly.TotalTokens != 470 {
				t.Fatalf("overview did not retain canonical tokens: %+v", hourly)
			}
		})
	}
}

func TestUnavailableBreakdownKeepsIdentityAndStrictFallback(t *testing.T) {
	for _, test := range []struct{ name, version, breakdown string }{
		{"missing", "2", "null"},
		{"wrong type", "2", `"invalid"`},
		{"incomplete", "2", `{"schema_version":2,"quality":"complete","total_tokens":470}`},
		{"unclassified", "2", strings.Replace(completeClaudeBreakdown, `"complete"`, `"unclassified"`, 1)},
		{"inconsistent", "2", strings.Replace(completeClaudeBreakdown, `"complete"`, `"inconsistent"`, 1)},
		{"version null", "null", completeClaudeBreakdown},
		{"version string", `"invalid"`, completeClaudeBreakdown},
		{"version array", "[]", completeClaudeBreakdown},
		{"version unsupported", "3", completeClaudeBreakdown},
	} {
		for _, identity := range []string{"", "claude"} {
			// 版本异常保留原有 strict 场景；breakdown 异常同时验证 identity 回退。
			if test.version != "2" && identity != "" {
				continue
			}
			t.Run(test.name+"/"+identity, func(t *testing.T) {
				db := openUsageServiceTestDatabase(t)
				if identity != "" {
					if err := db.Create(&entities.UsageIdentity{AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "plugin-auth", Type: identity}).Error; err != nil {
						t.Fatal(err)
					}
				}
				seedRedisInboxMessagesForTest(t, db, breakdownInboxMessage("executorAdapter", test.version, test.breakdown))
				result, err := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{PricingCatalog: emptyPricingCatalogForTest()}).ProcessRedisUsageInbox(context.Background())
				if err != nil || result == nil || result.InsertedEvents != 1 {
					t.Fatalf("unavailable optional accounting must not reject the event: result=%+v err=%v", result, err)
				}
				event := loadTokenProcessorSyncEvent(t, db, "breakdown-event")
				wantInput := int64(100)
				if identity == "claude" {
					wantInput = 450
				}
				if event.InputTokens != wantInput || event.OutputTokens != 20 || event.TotalTokens != 470 {
					t.Fatalf("legacy fallback changed: %+v", event)
				}
			})
		}
	}
}
