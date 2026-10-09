package test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
)

// 用逐事件结果校验拆分后的区间汇总和三个趋势聚合，覆盖跨时间、分类及计费维度。
func TestOverviewComparisonSplitAggregationMatchesEvents(t *testing.T) {
	for _, unit := range []string{"hour", "day"} {
		for _, scope := range []string{"", "key-a"} {
			t.Run(unit+"/"+scope, func(t *testing.T) {
				db := openTestDatabase(t)
				start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.Local)
				end := start.AddDate(0, 0, 3)
				if unit == "hour" {
					end = start.Add(3 * time.Hour)
				}
				ids := []entities.UsageIdentity{
					{AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: "file-a", Name: "same"},
					{AuthType: entities.UsageIdentityAuthTypeAIProvider, Identity: "provider-a", Name: "same"},
				}
				if err := db.Create(&ids).Error; err != nil {
					t.Fatal(err)
				}
				var events []entities.UsageEvent
				for bucket := 0; bucket < 3; bucket++ {
					stamp := start.Add(time.Duration(bucket) * time.Hour)
					if unit == "day" {
						stamp = start.AddDate(0, 0, bucket)
					}
					for _, key := range []string{"key-a", "key-b"} {
						for _, model := range []string{"model-a", "model-b"} {
							for _, auth := range []string{"file-a", "provider-a", "unresolved"} {
								for _, tier := range []string{"", "priority"} {
									n := int64(len(events) + 1)
									events = append(events, entities.UsageEvent{EventKey: fmt.Sprint(n), Timestamp: stamp.Add(time.Minute), APIGroupKey: key, Model: model, AuthIndex: auth, ServiceTier: tier, ModelAlias: &model, InputTokens: 100 + n, OutputTokens: 20, CacheReadTokens: n % 7, CacheCreationTokens: n % 3, ReasoningTokens: 3, TotalTokens: 120 + n, Failed: n%5 == 0})
								}
							}
						}
					}
				}
				// 非可加的历史计费值也必须继续逐行归一化，不能以总 Token 推导价格。
				events[0].InputTokens = -3
				events[1].CacheReadTokens = 999
				if err := db.Create(&events).Error; err != nil {
					t.Fatal(err)
				}
				if err := repository.AggregateUsageOverviewStats(context.Background(), db, end); err != nil {
					t.Fatal(err)
				}
				resolver := repositoryPricingResolver(t, []pricing.RuleConfig{{Key: "api_group_key", Value: "key-a", Multiplier: 2}, {Key: "service_tier", Value: "priority", Multiplier: 3}})
				filter := dto.UsageQueryFilter{Range: "custom", CustomUnit: unit, StartTime: &start, EndTime: &end, EndExclusive: true, QueryNow: &end, ComparisonOnly: true, APIGroupKey: scope}
				got, err := repository.BuildUsageOverviewWithFilter(db, filter, resolver)
				if err != nil {
					t.Fatal(err)
				}
				dimensions := []map[string]*dto.UsageComparisonItemRecord{got.Comparisons.Models, got.Comparisons.APIKeys, got.Comparisons.AuthFiles, got.Comparisons.AIProviders}
				for dim, items := range dimensions {
					expected := make(map[string]*dto.UsageComparisonItemRecord)
					for _, e := range events {
						if scope != "" && e.APIGroupKey != scope {
							continue
						}
						key := e.Model
						switch dim {
						case 1:
							key = e.APIGroupKey
						case 2:
							if e.AuthIndex != "file-a" {
								continue
							}
							key = e.AuthIndex
						case 3:
							if e.AuthIndex != "provider-a" {
								continue
							}
							key = e.AuthIndex
						}
						item := expected[key]
						if item == nil {
							item = &dto.UsageComparisonItemRecord{CostAvailable: true, TokenBuckets: map[string]int64{}}
							expected[key] = item
						}
						item.Requests++
						if e.Failed {
							item.Failures++
						}
						item.InputTokens += e.InputTokens
						item.OutputTokens += e.OutputTokens
						item.CacheReadTokens += e.CacheReadTokens
						item.CacheCreationTokens += e.CacheCreationTokens
						item.ReasoningTokens += e.ReasoningTokens
						item.TotalTokens += e.TotalTokens
						price := resolver.Calculate(repository.UsageEventCostSubject(e))
						item.CostUSD += price.Cost.TotalCostUSD
						item.CostAvailable = item.CostAvailable && price.Available
						bucket := e.Timestamp.Truncate(time.Hour).Format(time.RFC3339Nano)
						if unit == "day" {
							bucket = e.Timestamp.Format(time.DateOnly)
						}
						item.TokenBuckets[bucket] += e.TotalTokens
					}
					if len(items) != len(expected) {
						t.Fatalf("dimension %d items=%d want %d", dim, len(items), len(expected))
					}
					for key, want := range expected {
						item := items[key]
						if item == nil {
							t.Fatalf("missing dimension %d key %s", dim, key)
						}
						if item.Requests != want.Requests || item.Failures != want.Failures || item.TotalTokens != want.TotalTokens || item.InputTokens != want.InputTokens || item.OutputTokens != want.OutputTokens || item.CacheReadTokens != want.CacheReadTokens || item.CacheCreationTokens != want.CacheCreationTokens || item.ReasoningTokens != want.ReasoningTokens || item.CostAvailable != want.CostAvailable || math.Abs(item.CostUSD-want.CostUSD) > 1e-9 {
							t.Fatalf("dimension %d key %s totals: got %+v want %+v", dim, key, item, want)
						}
						if len(item.TokenBuckets) != len(want.TokenBuckets) {
							t.Fatalf("dimension %d key %s unexpected buckets: %v", dim, key, item.TokenBuckets)
						}
						for bucket, tokens := range want.TokenBuckets {
							if item.TokenBuckets[bucket] != tokens {
								t.Fatalf("dimension %d key %s bucket %s got %d want %d", dim, key, bucket, item.TokenBuckets[bucket], tokens)
							}
						}
					}
				}
			})
		}
	}
}
