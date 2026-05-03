package models

import "testing"

func TestAllIncludesCoreModels(t *testing.T) {
	items := All()
	if len(items) != 4 {
		t.Fatalf("expected 4 models after removing legacy metadata tables, got %d", len(items))
	}
	if _, ok := items[0].(*UsageEvent); !ok {
		t.Fatalf("expected UsageEvent to be first registered model, got %T", items[0])
	}
	if _, ok := items[1].(*RedisUsageInbox); !ok {
		t.Fatalf("expected RedisUsageInbox to be registered, got %T", items[1])
	}
}
