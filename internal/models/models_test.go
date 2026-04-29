package models

import "testing"

func TestAllIncludesCoreModels(t *testing.T) {
	items := All()
	if len(items) != 6 {
		t.Fatalf("expected 6 models, got %d", len(items))
	}
	if _, ok := items[2].(*RedisUsageInbox); !ok {
		t.Fatalf("expected RedisUsageInbox to be registered, got %T", items[2])
	}
}
