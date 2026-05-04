package service

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestBuildEventKeyIsStable(t *testing.T) {
	timestamp := time.Date(2026, 4, 16, 12, 0, 0, 123, time.UTC)
	tokens := cpa.TokenStats{InputTokens: 1, OutputTokens: 2, ReasoningTokens: 3, CachedTokens: 4, TotalTokens: 10}

	key1 := BuildEventKey("provider-a", "claude-sonnet", timestamp, "source-a", "0", false, tokens)
	key2 := BuildEventKey("provider-a", "claude-sonnet", timestamp, "source-a", "0", false, tokens)

	if key1 != key2 {
		t.Fatalf("expected stable event key, got %s and %s", key1, key2)
	}
}
