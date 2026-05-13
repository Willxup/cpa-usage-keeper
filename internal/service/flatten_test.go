package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

func TestBuildEventKeyIsStable(t *testing.T) {
	timestamp := time.Date(2026, 4, 16, 12, 0, 0, 123, time.UTC)
	tokens := dto.TokenStats{InputTokens: 1, OutputTokens: 2, ReasoningTokens: 3, CachedTokens: 4, TotalTokens: 10}

	key1 := BuildEventKey("provider-a", "claude-sonnet", timestamp, "source-a", "0", false, tokens)
	key2 := BuildEventKey("provider-a", "claude-sonnet", timestamp, "source-a", "0", false, tokens)

	if key1 != key2 {
		t.Fatalf("expected stable event key, got %s and %s", key1, key2)
	}
}

func TestBuildEventKeyUsesStorageTimeFormat(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	t.Cleanup(func() { time.Local = previousLocal })
	time.Local = location

	timestamp := time.Date(2026, 4, 16, 12, 0, 0, 123, time.UTC)
	tokens := dto.TokenStats{InputTokens: 1, OutputTokens: 2, ReasoningTokens: 3, CachedTokens: 4, TotalTokens: 10}

	payload := fmt.Sprintf(
		"%s|%s|%s|%s|%s|%t|%d|%d|%d|%d|%d",
		strings.TrimSpace("provider-a"),
		strings.TrimSpace("claude-sonnet"),
		timeutil.FormatStorageTime(timestamp),
		strings.TrimSpace("source-a"),
		strings.TrimSpace("0"),
		false,
		tokens.InputTokens,
		tokens.OutputTokens,
		tokens.ReasoningTokens,
		tokens.CachedTokens,
		tokens.TotalTokens,
	)
	sum := sha256.Sum256([]byte(payload))
	expected := hex.EncodeToString(sum[:])

	if key := BuildEventKey("provider-a", "claude-sonnet", timestamp, "source-a", "0", false, tokens); key != expected {
		t.Fatalf("expected storage-time event key %s, got %s", expected, key)
	}
}
