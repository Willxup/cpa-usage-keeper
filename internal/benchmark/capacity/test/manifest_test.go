package capacity_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/benchmark/capacity"
)

func TestLoadCapacityManifestAndExpandStablePlan(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "manifest", "capacity-v1.json")
	manifest, err := capacity.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest returned error: %v", err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	plan, err := capacity.ExpandPlan(manifest)
	if err != nil {
		t.Fatalf("ExpandPlan returned error: %v", err)
	}
	if len(plan.Cells) != 3 {
		t.Fatalf("plan cells=%d, want 3", len(plan.Cells))
	}
	wantIDs := []string{
		"capacity-reference-2m-1c-unlimited",
		"capacity-reference-2m-2c-unlimited",
		"capacity-reference-2m-4c-unlimited",
	}
	wantCPUs := []float64{1, 2, 4}
	for index, cell := range plan.Cells {
		if cell.ID != wantIDs[index] || cell.DatasetID != "reference-2m" || cell.HotEvents != 1_946_550 || cell.ArchiveEvents != 89_190 {
			t.Fatalf("unexpected cell %d: %+v", index, cell)
		}
		if cell.Cardinality != (capacity.Cardinality{Identities: 323, Models: 52, APIKeys: 27}) || cell.Resource.CPU != wantCPUs[index] || cell.Resource.MemoryMiB != 0 {
			t.Fatalf("unexpected cell capacity profile %d: %+v", index, cell)
		}
	}
	if manifest.Search.DashboardCoreP95MS != 0 || manifest.Search.DashboardOverallP99MS != 3000 || manifest.Search.SoakSeconds != 300 || !manifest.Search.SearchDashboardCapacity {
		t.Fatalf("unexpected dashboard policy: %+v", manifest.Search)
	}
	second, err := capacity.ExpandPlan(manifest)
	if err != nil {
		t.Fatalf("second ExpandPlan returned error: %v", err)
	}
	if !reflect.DeepEqual(plan, second) {
		t.Fatal("same manifest must expand to an identical plan")
	}
}

func TestLoadManifestRejectsHostSpecificFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	data := []byte(`{"version":"capacity-v1","target":{"machine_label":"private-machine","os":"linux","arch":"amd64"}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := capacity.LoadManifest(path); err == nil {
		t.Fatal("host-specific manifest field must be rejected")
	}
}

func TestAllocateTrafficTiersUsesKeyShares(t *testing.T) {
	tiers := []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}
	profiles, err := capacity.BuildAPIKeyProfiles(100, tiers, 20260806)
	if err != nil {
		t.Fatalf("BuildAPIKeyProfiles returned error: %v", err)
	}
	counts := map[string]int{}
	for _, profile := range profiles {
		counts[profile.Tier]++
	}
	if !reflect.DeepEqual(counts, map[string]int{"high": 30, "medium": 50, "low": 20}) {
		t.Fatalf("tier counts=%v", counts)
	}
	if !(profiles[0].Weight > profiles[30].Weight && profiles[30].Weight > profiles[80].Weight) {
		t.Fatalf("tier weights are not ordered: high=%f medium=%f low=%f", profiles[0].Weight, profiles[30].Weight, profiles[80].Weight)
	}
}

func TestAllocateTrafficTiersRoundsSmallKeySetsDeterministically(t *testing.T) {
	tiers := []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}
	profiles, err := capacity.BuildAPIKeyProfiles(4, tiers, 7)
	if err != nil {
		t.Fatalf("BuildAPIKeyProfiles returned error: %v", err)
	}
	counts := map[string]int{}
	for _, profile := range profiles {
		counts[profile.Tier]++
	}
	if !reflect.DeepEqual(counts, map[string]int{"high": 1, "medium": 2, "low": 1}) {
		t.Fatalf("tier counts=%v", counts)
	}
}

func TestAllocateEventsPreservesExactTotalAndTierOrdering(t *testing.T) {
	profiles, err := capacity.BuildAPIKeyProfiles(100, []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}, 42)
	if err != nil {
		t.Fatalf("BuildAPIKeyProfiles returned error: %v", err)
	}
	allocations, err := capacity.AllocateEvents(2_035_740, profiles)
	if err != nil {
		t.Fatalf("AllocateEvents returned error: %v", err)
	}
	var total int64
	tierTotals := map[string]int64{}
	for index, count := range allocations {
		total += count
		tierTotals[profiles[index].Tier] += count
	}
	if total != 2_035_740 {
		t.Fatalf("allocated total=%d", total)
	}
	if !(tierTotals["high"] > tierTotals["medium"] && tierTotals["medium"] > tierTotals["low"]) {
		t.Fatalf("tier totals=%v", tierTotals)
	}
}

func TestManifestRejectsInvalidCapacityBounds(t *testing.T) {
	manifest := capacity.Manifest{
		Version: "capacity-v1",
		Target:  capacity.Target{OS: "linux", Arch: "amd64"},
		Dataset: capacity.DatasetSpec{
			ID: "reference-2m", HotEvents: 1, HotDays: 1, BenchmarkNow: "2026-08-06T16:00:00+08:00",
			Cardinality: capacity.Cardinality{Identities: 1001, Models: 101, APIKeys: 101},
		},
		TrafficTiers: []capacity.TrafficTier{{Name: "all", KeyShare: 1, PerKeyWeight: 1}},
		Resources:    []capacity.Resource{{ID: "1c-unlimited", CPU: 1, MemoryMiB: 0}},
		Search: capacity.Search{
			RatesPerSecond: []int{1}, ProbeSeconds: 1, BoundarySeconds: 1, BoundaryRepetitions: 1,
			SoakSeconds: 1, MaxRunSeconds: 1, DashboardOverallP99MS: 3000, DashboardRequestsPerSecond: 1, RecommendedCapacityRate: 0.7,
		},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate should reject cardinality beyond capacity bounds")
	}
}
