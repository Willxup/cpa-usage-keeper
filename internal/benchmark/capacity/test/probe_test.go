package capacity_test

import (
	"encoding/json"
	"testing"
	"time"

	"cpa-usage-keeper/internal/benchmark/capacity"
	"cpa-usage-keeper/internal/service/tokenprocessor"
)

func TestBuildUsagePayloadUsesValidSyntheticMetadata(t *testing.T) {
	profiles, err := capacity.BuildAPIKeyProfiles(100, []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}, 20260806)
	if err != nil {
		t.Fatalf("BuildAPIKeyProfiles returned error: %v", err)
	}
	payload, metadata, err := capacity.BuildUsagePayload(17, time.Date(2026, 8, 6, 15, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)), capacity.Cardinality{Identities: 1000, Models: 100, APIKeys: 100}, profiles, 9)
	if err != nil {
		t.Fatalf("BuildUsagePayload returned error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if metadata.APIKeyIndex < 1 || metadata.APIKeyIndex > 100 || metadata.ModelIndex < 1 || metadata.ModelIndex > 100 || metadata.IdentityIndex < 1 || metadata.IdentityIndex > 1000 {
		t.Fatalf("metadata outside configured cardinality: %+v", metadata)
	}
	if decoded["api_key"] != metadata.APIGroupKey || decoded["model"] != metadata.Model || decoded["auth_index"] != metadata.AuthIndex {
		t.Fatalf("payload metadata mismatch: payload=%v metadata=%+v", decoded, metadata)
	}
}

func TestBuildUsagePayloadUsesCanonicalTokenSemantics(t *testing.T) {
	profiles, err := capacity.BuildAPIKeyProfiles(4, []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}, 20260806)
	if err != nil {
		t.Fatalf("BuildAPIKeyProfiles returned error: %v", err)
	}
	cardinality := capacity.Cardinality{Identities: 12, Models: 6, APIKeys: 4}
	for sequence := int64(1); sequence <= 100; sequence++ {
		payload, _, err := capacity.BuildUsagePayload(sequence, time.Unix(sequence, 0), cardinality, profiles, 9)
		if err != nil {
			t.Fatalf("BuildUsagePayload(%d): %v", sequence, err)
		}
		var decoded struct {
			Provider string `json:"provider"`
			Tokens   struct {
				InputTokens         int64 `json:"input_tokens"`
				OutputTokens        int64 `json:"output_tokens"`
				ReasoningTokens     int64 `json:"reasoning_tokens"`
				CachedTokens        int64 `json:"cached_tokens"`
				CacheReadTokens     int64 `json:"cache_read_tokens"`
				CacheCreationTokens int64 `json:"cache_creation_tokens"`
				TotalTokens         int64 `json:"total_tokens"`
			} `json:"tokens"`
		}
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("decode payload %d: %v", sequence, err)
		}
		resolution, err := tokenprocessor.ResolveIdentity("", decoded.Provider)
		if err != nil {
			t.Fatalf("ResolveIdentity(%q): %v", decoded.Provider, err)
		}
		result := tokenprocessor.Process(tokenprocessor.TokenValues{
			InputTokens:         decoded.Tokens.InputTokens,
			OutputTokens:        decoded.Tokens.OutputTokens,
			ReasoningTokens:     decoded.Tokens.ReasoningTokens,
			CachedTokens:        decoded.Tokens.CachedTokens,
			CacheReadTokens:     decoded.Tokens.CacheReadTokens,
			CacheReadPresent:    true,
			CacheCreationTokens: decoded.Tokens.CacheCreationTokens,
			TotalTokens:         decoded.Tokens.TotalTokens,
		}, resolution)
		if len(result.Actions) != 0 || len(result.Violations) != 0 || result.Outcome != tokenprocessor.TokenOutcomeValid {
			t.Fatalf("payload %d provider=%q is not canonical: tokens=%+v actions=%+v violations=%+v outcome=%q", sequence, decoded.Provider, result.Tokens, result.Actions, result.Violations, result.Outcome)
		}
	}
}

func TestLatencyPercentilesUseNearestRank(t *testing.T) {
	percentiles := capacity.LatencyPercentiles([]time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
		40 * time.Millisecond,
		100 * time.Millisecond,
	})
	if percentiles.P50MS != 30 || percentiles.P95MS != 100 || percentiles.P99MS != 100 {
		t.Fatalf("unexpected percentiles: %+v", percentiles)
	}
}

func TestSummarizeDashboardLatenciesSeparatesHeavyDiagnosticEndpoint(t *testing.T) {
	samples := []capacity.DashboardLatencySample{
		{Path: "/api/v1/usage/overview?range=30d", Duration: 120 * time.Millisecond},
		{Path: "/api/v1/usage/overview/realtime?range=1h", Duration: 2 * time.Millisecond},
		{Path: "/api/v1/usage/activity?range=30d", Duration: 210 * time.Millisecond},
		{Path: "/api/v1/usage/analysis?range=30d", Duration: 3 * time.Millisecond},
		{Path: "/api/v1/usage/events?range=30d&page=1&page_size=50", Duration: 140 * time.Millisecond},
		{Path: "/api/v1/usage/analysis/latency?range=30d", Duration: 1900 * time.Millisecond},
	}
	overall, core, byPath := capacity.SummarizeDashboardLatencies(samples)
	if overall.P95MS != 1900 || core.P95MS != 210 {
		t.Fatalf("unexpected overall/core latency: overall=%+v core=%+v", overall, core)
	}
	if byPath["/api/v1/usage/analysis/latency?range=30d"].P99MS != 1900 {
		t.Fatalf("heavy endpoint summary missing: %+v", byPath)
	}
}

func TestBuildUsagePayloadKeepsProductionCorrelations(t *testing.T) {
	tiers := []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}
	profiles, err := capacity.BuildAPIKeyProfiles(100, tiers, 20260806)
	if err != nil {
		t.Fatalf("BuildAPIKeyProfiles returned error: %v", err)
	}
	cardinality := capacity.Cardinality{Identities: 1000, Models: 100, APIKeys: 100}
	identitiesByKey := map[int]map[int]bool{}
	modelsByIdentity := map[int]map[int]bool{}
	for sequence := int64(1); sequence <= 20_000; sequence++ {
		_, metadata, err := capacity.BuildUsagePayload(sequence, time.Unix(sequence, 0), cardinality, profiles, 77)
		if err != nil {
			t.Fatalf("BuildUsagePayload(%d): %v", sequence, err)
		}
		if identitiesByKey[metadata.APIKeyIndex] == nil {
			identitiesByKey[metadata.APIKeyIndex] = map[int]bool{}
		}
		identitiesByKey[metadata.APIKeyIndex][metadata.IdentityIndex] = true
		if modelsByIdentity[metadata.IdentityIndex] == nil {
			modelsByIdentity[metadata.IdentityIndex] = map[int]bool{}
		}
		modelsByIdentity[metadata.IdentityIndex][metadata.ModelIndex] = true
	}
	for key, identities := range identitiesByKey {
		if len(identities) > 30 {
			t.Fatalf("API key %d reached %d identities; workload regressed toward a full cross product", key, len(identities))
		}
	}
	mediumKeys := 0
	lowKeys := 0
	for key := range identitiesByKey {
		if key >= 31 && key <= 80 {
			mediumKeys++
		}
		if key >= 81 {
			lowKeys++
		}
	}
	if mediumKeys > 18 || lowKeys > 2 {
		t.Fatalf("temporal activity window expanded: medium=%d low=%d", mediumKeys, lowKeys)
	}
	for identity, models := range modelsByIdentity {
		if len(models) > 3 {
			t.Fatalf("identity %d reached %d models, want at most 3 correlated choices", identity, len(models))
		}
	}
}
