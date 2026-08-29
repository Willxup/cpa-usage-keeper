package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
)

func TestUsageRecentEventCachePreservesAllPricingDimensions(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Now().Truncate(time.Second)
	alias := "alias-a"
	if err := db.Create(&entities.UsageEvent{
		EventKey:            "recent-pricing-dimensions",
		Timestamp:           now.Add(-time.Minute),
		APIGroupKey:         "group-a",
		Model:               "model-a",
		AuthIndex:           "auth-a",
		ModelAlias:          &alias,
		ServiceTier:         "priority",
		ResponseServiceTier: "default",
		ReasoningEffort:     "xhigh",
		Endpoint:            "/v1/responses",
		ExecutorType:        "openai",
	}).Error; err != nil {
		t.Fatalf("seed recent usage event: %v", err)
	}
	cache, err := repository.NewUsageRecentEventCache(db, repository.UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewUsageRecentEventCache: %v", err)
	}
	t.Cleanup(cache.Close)

	events, ok := cache.Events(now.Add(-2*time.Minute), now, false, "group-a")
	if !ok || len(events) != 1 {
		t.Fatalf("expected one cached event, got ok=%v events=%+v", ok, events)
	}
	event := events[0]
	if event.ServiceTier != "priority" || event.ResponseServiceTier != "default" || event.ReasoningEffort != "xhigh" || event.Endpoint != "/v1/responses" || event.ExecutorType != "openai" {
		t.Fatalf("cached pricing dimensions missing: %+v", event)
	}

	// 返回值是副本；调用方修改后不能污染缓存里的 interned 字符串。
	events[0].ServiceTier = "mutated"
	again, _ := cache.Events(now.Add(-2*time.Minute), now, false, "group-a")
	if len(again) != 1 || again[0].ServiceTier != "priority" {
		t.Fatalf("cached event exposed mutable pricing dimensions: %+v", again)
	}
}
