package api

import (
	"testing"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
)

func TestApplyUsageSourceResolutionUsesSharedResolver(t *testing.T) {
	snapshot := &cpa.StatisticsSnapshot{
		APIs: map[string]cpa.APISnapshot{
			"provider-a": {
				Models: map[string]cpa.ModelSnapshot{
					"claude-sonnet": {
						Details: []cpa.RequestDetail{{
							Source:    "sk-provider-key",
							AuthIndex: "2",
						}},
					},
				},
			},
		},
	}

	resolver := newUsageSourceResolver(
		[]models.AuthFile{{
			AuthIndex: "2",
			Email:     "user@example.com",
			Type:      "codex",
		}},
		[]models.ProviderMetadata{{
			LookupKey:    "sk-provider-key",
			ProviderType: "openai",
			DisplayName:  "OpenAI Mirror",
			ProviderKey:  "openai:OpenAI Mirror",
		}},
	)

	applyUsageSourceResolution(snapshot, resolver)

	detail := snapshot.APIs["provider-a"].Models["claude-sonnet"].Details[0]
	if detail.Source != "OpenAI Mirror" {
		t.Fatalf("expected resolved source display, got %q", detail.Source)
	}
	if detail.SourceDisplay != "OpenAI Mirror" {
		t.Fatalf("expected source display field to be populated, got %q", detail.SourceDisplay)
	}
	if detail.SourceType != "openai" {
		t.Fatalf("expected provider metadata type to win, got %q", detail.SourceType)
	}
	if detail.SourceKey != "openai:OpenAI Mirror" {
		t.Fatalf("expected provider key to be used for grouping, got %q", detail.SourceKey)
	}
	if detail.SourceRaw != "sk-provider-key" {
		t.Fatalf("expected raw source to be preserved, got %q", detail.SourceRaw)
	}
}
