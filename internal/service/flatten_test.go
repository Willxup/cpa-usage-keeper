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

func TestFlattenUsageExportBuildsEvents(t *testing.T) {
	exportedAt := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	export := cpa.UsageExport{
		Version:    1,
		ExportedAt: exportedAt,
		Usage: cpa.StatisticsSnapshot{
			APIs: map[string]cpa.APISnapshot{
				"provider-a": {
					Models: map[string]cpa.ModelSnapshot{
						"claude-sonnet": {
							Details: []cpa.RequestDetail{
								{
									Timestamp: time.Date(2026, 4, 16, 9, 30, 0, 0, time.UTC),
									LatencyMS: 123,
									Source:    "codex-account-a",
									AuthIndex: "1",
									Failed:    false,
									Tokens: cpa.TokenStats{
										InputTokens:     10,
										OutputTokens:    20,
										ReasoningTokens: 5,
										CachedTokens:    0,
										TotalTokens:     35,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	events := FlattenUsageExport(42, export)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	event := events[0]
	if event.SnapshotRunID != 42 {
		t.Fatalf("expected snapshot run id 42, got %d", event.SnapshotRunID)
	}
	if event.APIGroupKey != "provider-a" || event.Model != "claude-sonnet" {
		t.Fatalf("unexpected event grouping: %+v", event)
	}
	if event.TotalTokens != 35 || event.InputTokens != 10 || event.OutputTokens != 20 || event.ReasoningTokens != 5 {
		t.Fatalf("unexpected token values: %+v", event)
	}
	if event.EventKey == "" {
		t.Fatal("expected event key to be generated")
	}
}

func TestFlattenUsageExportUsesExportedAtForZeroTimestamp(t *testing.T) {
	exportedAt := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	events := FlattenUsageExport(1, cpa.UsageExport{
		Version:    1,
		ExportedAt: exportedAt,
		Usage: cpa.StatisticsSnapshot{
			APIs: map[string]cpa.APISnapshot{
				"provider-a": {
					Models: map[string]cpa.ModelSnapshot{
						"claude-sonnet": {
							Details: []cpa.RequestDetail{{Tokens: cpa.TokenStats{InputTokens: 1, OutputTokens: 1}}},
						},
					},
				},
			},
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].Timestamp.Equal(exportedAt.UTC()) {
		t.Fatalf("expected exportedAt timestamp fallback, got %s", events[0].Timestamp)
	}
	if events[0].TotalTokens != 2 {
		t.Fatalf("expected normalized total tokens 2, got %d", events[0].TotalTokens)
	}
}
