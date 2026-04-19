package redact

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

func TestAPIAliasIsStableAndIdempotent(t *testing.T) {
	raw := "sk-live-secret-value"
	first := APIAlias(raw)
	second := APIAlias(raw)

	if first != second {
		t.Fatalf("expected stable alias, got %q and %q", first, second)
	}
	if first == raw {
		t.Fatalf("expected raw key to be redacted")
	}
	if again := APIAlias(first); again != first {
		t.Fatalf("expected redacted alias to remain unchanged, got %q", again)
	}
}

func TestAPIKeyDisplayNameMasksMiddle(t *testing.T) {
	if got := APIKeyDisplayName("provider-a"); got != "prov**er-a" {
		t.Fatalf("expected masked display name, got %q", got)
	}
	if got := APIKeyDisplayName("abcd"); got != "****" {
		t.Fatalf("expected short key to be fully masked, got %q", got)
	}
}

func TestUsageSnapshotRedactsOnlyAPIKeys(t *testing.T) {
	snapshot := &cpa.StatisticsSnapshot{
		TotalRequests: 1,
		SuccessCount:  1,
		TotalTokens:   42,
		APIs: map[string]cpa.APISnapshot{
			"sk-live-secret-value": {
				TotalRequests: 1,
				SuccessCount:  1,
				TotalTokens:   42,
				Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {
						TotalRequests: 1,
						SuccessCount:  1,
						TotalTokens:   42,
						Details: []cpa.RequestDetail{{
							Timestamp: time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC),
							LatencyMS: 150,
							Source:    "source-a",
							AuthIndex: "3",
							Failed:    false,
							Tokens: cpa.TokenStats{TotalTokens: 42},
						}},
					},
				},
			},
		},
		RequestsByDay:  map[string]int64{"2026-04-20": 1},
		RequestsByHour: map[string]int64{"2026-04-20T12:00:00Z": 1},
		TokensByDay:    map[string]int64{"2026-04-20": 42},
		TokensByHour:   map[string]int64{"2026-04-20T12:00:00Z": 42},
	}

	redacted := UsageSnapshot(snapshot)
	alias := APIAlias("sk-live-secret-value")
	apiSnapshot, ok := redacted.APIs[alias]
	if !ok {
		t.Fatalf("expected redacted API alias %q in snapshot", alias)
	}
	if _, ok := redacted.APIs["sk-live-secret-value"]; ok {
		t.Fatalf("expected raw API key to be absent from response snapshot")
	}
	if apiSnapshot.DisplayName != "sk-l************alue" {
		t.Fatalf("expected masked display name, got %q", apiSnapshot.DisplayName)
	}
	if got := apiSnapshot.Models["claude-sonnet"].Details[0].Source; got != "source-a" {
		t.Fatalf("expected source to remain unchanged, got %q", got)
	}
	if got := apiSnapshot.Models["claude-sonnet"].Details[0].AuthIndex; got != "3" {
		t.Fatalf("expected auth index to remain unchanged, got %q", got)
	}
	if _, ok := snapshot.APIs["sk-live-secret-value"]; !ok {
		t.Fatalf("expected source snapshot to remain unchanged")
	}
}
