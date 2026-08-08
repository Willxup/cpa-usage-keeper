package capacity_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/benchmark/capacity"
)

func TestDatasetResultJSONOmitsLocalPath(t *testing.T) {
	data, err := json.Marshal(capacity.DatasetResult{Path: "/private/benchmark/reference.db", HotEvents: 1})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(data), "/private/") || strings.Contains(string(data), "path") {
		t.Fatalf("dataset JSON leaked a local path: %s", data)
	}
}

func TestGenerateDatasetBuildsValidatedSteadyState(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	options := capacity.GenerateOptions{
		Path:          filepath.Join(t.TempDir(), "dataset.db"),
		HotEvents:     400,
		ArchiveEvents: 50,
		HotDays:       90,
		ArchiveDays:   7,
		FailureRate:   0.041818,
		Seed:          20260806,
		Now:           time.Date(2026, 8, 6, 15, 0, 0, 0, location),
		Cardinality:   capacity.Cardinality{Identities: 12, Models: 6, APIKeys: 4},
		TrafficTiers: []capacity.TrafficTier{
			{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
			{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
			{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
		},
		InsertBatchSize: 100,
		AggregatePage:   200,
	}

	result, err := capacity.GenerateDataset(context.Background(), options)
	if err != nil {
		t.Fatalf("GenerateDataset returned error: %v", err)
	}
	if result.HotEvents != 400 || result.ArchiveEvents != 50 || result.TotalEvents != 450 {
		t.Fatalf("unexpected generated counts: %+v", result)
	}
	if result.Identities != 12 || result.Models != 6 || result.APIKeys != 4 {
		t.Fatalf("unexpected cardinality: %+v", result)
	}
	if result.UsedIdentities != result.Identities || result.UsedModels != result.Models || result.UsedAPIKeys != result.APIKeys {
		t.Fatalf("every valid metadata row must be exercised: %+v", result)
	}
	if result.OrphanIdentities != 0 || result.OrphanModels != 0 || result.OrphanAPIKeys != 0 {
		t.Fatalf("generated dataset contains orphan metadata: %+v", result)
	}
	if result.TokenSemanticViolations != 0 {
		t.Fatalf("generated dataset contains non-canonical token rows: %+v", result)
	}
	if result.DuplicateEventKeys == 0 {
		t.Fatal("generated dataset should preserve controlled duplicate event keys")
	}
	if result.OverviewHourlyRequests != 450 || result.OverviewDailyRequests != 450 || result.IdentityRequests != 450 {
		t.Fatalf("derived totals do not match raw events: %+v", result)
	}
	if result.QuickCheck != "ok" {
		t.Fatalf("quick check=%q", result.QuickCheck)
	}
}

func TestGenerateDatasetIsSemanticallyDeterministic(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	base := capacity.GenerateOptions{
		HotEvents:     120,
		ArchiveEvents: 12,
		HotDays:       30,
		ArchiveDays:   2,
		FailureRate:   0.05,
		Seed:          99,
		Now:           time.Date(2026, 8, 6, 15, 0, 0, 0, location),
		Cardinality:   capacity.Cardinality{Identities: 6, Models: 4, APIKeys: 4},
		TrafficTiers: []capacity.TrafficTier{
			{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
			{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
			{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
		},
		InsertBatchSize: 50,
		AggregatePage:   100,
	}
	firstOptions := base
	firstOptions.Path = filepath.Join(t.TempDir(), "first.db")
	secondOptions := base
	secondOptions.Path = filepath.Join(t.TempDir(), "second.db")
	first, err := capacity.GenerateDataset(context.Background(), firstOptions)
	if err != nil {
		t.Fatalf("first GenerateDataset returned error: %v", err)
	}
	second, err := capacity.GenerateDataset(context.Background(), secondOptions)
	if err != nil {
		t.Fatalf("second GenerateDataset returned error: %v", err)
	}
	if first.SemanticFingerprint != second.SemanticFingerprint {
		t.Fatalf("semantic fingerprints differ: %q != %q", first.SemanticFingerprint, second.SemanticFingerprint)
	}
	thirdOptions := base
	thirdOptions.Path = filepath.Join(t.TempDir(), "third.db")
	thirdOptions.Seed++
	third, err := capacity.GenerateDataset(context.Background(), thirdOptions)
	if err != nil {
		t.Fatalf("third GenerateDataset returned error: %v", err)
	}
	if first.SemanticFingerprint == third.SemanticFingerprint {
		t.Fatalf("different seeds must produce different fingerprints: %q", first.SemanticFingerprint)
	}
}
