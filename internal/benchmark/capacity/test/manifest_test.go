package capacity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"cpa-usage-keeper/internal/benchmark/capacity"
)

func TestPlanSchemaRequiresRecentThirtyDayEvents(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "plan.schema.json"))
	if err != nil {
		t.Fatalf("read plan schema: %v", err)
	}
	var schema struct {
		Properties map[string]struct {
			Items struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decode plan schema: %v", err)
	}
	cells := schema.Properties["cells"].Items
	required := false
	for _, field := range cells.Required {
		if field == "recent_30_day_events" {
			required = true
			break
		}
	}
	if !required {
		t.Fatal("plan schema must require recent_30_day_events")
	}
	var property struct {
		Type    string `json:"type"`
		Minimum int    `json:"minimum"`
	}
	if err := json.Unmarshal(cells.Properties["recent_30_day_events"], &property); err != nil {
		t.Fatalf("decode recent_30_day_events property: %v", err)
	}
	if property.Type != "integer" || property.Minimum != 1 {
		t.Fatalf("recent_30_day_events schema=%+v, want integer minimum 1", property)
	}
}

func TestSuiteSchemaRequiresDrainStabilityBudget(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "suite.schema.json"))
	if err != nil {
		t.Fatalf("read suite schema: %v", err)
	}
	var schema struct {
		Properties map[string]struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decode suite schema: %v", err)
	}
	search := schema.Properties["search"]
	if !slices.Contains(search.Required, "initial_rate_per_second") {
		t.Fatal("suite schema must require initial_rate_per_second")
	}
	var initialRate struct {
		Type    string `json:"type"`
		Minimum int    `json:"minimum"`
	}
	if err := json.Unmarshal(search.Properties["initial_rate_per_second"], &initialRate); err != nil {
		t.Fatalf("decode initial_rate_per_second property: %v", err)
	}
	if initialRate.Type != "integer" || initialRate.Minimum != 1 {
		t.Fatalf("initial_rate_per_second schema=%+v, want positive integer", initialRate)
	}
	if !slices.Contains(search.Required, "max_pass_drain_seconds") {
		t.Fatal("suite schema must require max_pass_drain_seconds")
	}
	if !slices.Contains(search.Required, "analysis_latency_interval_seconds") {
		t.Fatal("suite schema must require analysis_latency_interval_seconds")
	}
	var property struct {
		Type    string `json:"type"`
		Minimum int    `json:"minimum"`
		Maximum int    `json:"maximum"`
	}
	if err := json.Unmarshal(search.Properties["max_pass_drain_seconds"], &property); err != nil {
		t.Fatalf("decode max_pass_drain_seconds property: %v", err)
	}
	if property.Type != "integer" || property.Minimum != 1 || property.Maximum != 30 {
		t.Fatalf("max_pass_drain_seconds schema=%+v, want integer range 1..30", property)
	}
}

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
		"capacity-reference-3m-1c-unlimited",
		"capacity-reference-3m-2c-unlimited",
		"capacity-reference-3m-4c-unlimited",
	}
	wantCPUs := []float64{1, 2, 4}
	for index, cell := range plan.Cells {
		if cell.ID != wantIDs[index] || cell.DatasetID != "reference-3m" || cell.HotEvents != 3_205_740 || cell.Recent30DayEvents != 1_226_326 || cell.ArchiveEvents != 0 {
			t.Fatalf("unexpected cell %d: %+v", index, cell)
		}
		if cell.Cardinality != (capacity.Cardinality{Identities: 500, Models: 50, APIKeys: 50}) || cell.Resource.CPU != wantCPUs[index] || cell.Resource.MemoryMiB != 0 {
			t.Fatalf("unexpected cell capacity profile %d: %+v", index, cell)
		}
	}
	if manifest.Search.DashboardCoreP95MS != 0 || manifest.Search.DashboardCoreP99MS != 3000 || manifest.Search.AnalysisLatencyIntervalSeconds != 30 || manifest.Search.SoakSeconds != 300 || !manifest.Search.SearchDashboardCapacity {
		t.Fatalf("unexpected dashboard policy: %+v", manifest.Search)
	}
	if manifest.Search.RatesPerSecond[0] != 1 || manifest.Search.InitialRatePerSecond != 25 || !slices.Contains(manifest.Search.RatesPerSecond, 25) || manifest.Search.MaxPassDrainSeconds != 15 || manifest.Search.BoundarySeconds != 60 || manifest.Search.BoundaryRepetitions != 1 || manifest.Search.SkipBoundary {
		t.Fatalf("unexpected adaptive search policy: %+v", manifest.Search)
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

func TestExpandPlanUsesCustomDatasetAndResourceIDs(t *testing.T) {
	manifest := capacity.Manifest{
		Version: "capacity-v1",
		Target:  capacity.Target{OS: "linux", Arch: "amd64"},
		Dataset: capacity.DatasetSpec{
			ID: "custom-reference", HotEvents: 10, Recent30DayEvents: 10, HotDays: 1, BenchmarkNow: "2026-08-06T16:00:00+08:00",
			Cardinality: capacity.Cardinality{Identities: 1, Models: 1, APIKeys: 1},
		},
		TrafficTiers: []capacity.TrafficTier{{Name: "all", KeyShare: 1, PerKeyWeight: 1}},
		Resources:    []capacity.Resource{{ID: "single-core", CPU: 1, MemoryMiB: 0}},
		Search: capacity.Search{
			RatesPerSecond: []int{1}, InitialRatePerSecond: 1, ProbeSeconds: 1, BoundarySeconds: 1, BoundaryRepetitions: 1,
			SoakSeconds: 1, MaxPassDrainSeconds: 1, MaxRunSeconds: 1, DashboardCoreP99MS: 3000, DashboardRequestsPerSecond: 1, AnalysisLatencyIntervalSeconds: 30, RecommendedCapacityRate: 0.7,
		},
	}
	plan, err := capacity.ExpandPlan(manifest)
	if err != nil {
		t.Fatalf("ExpandPlan returned error: %v", err)
	}
	if len(plan.Cells) != 1 || plan.Cells[0].ID != "capacity-custom-reference-single-core" || plan.Cells[0].DatasetID != "custom-reference" {
		t.Fatalf("custom manifest identifiers were not preserved: %+v", plan.Cells)
	}
}

func TestResolveDatasetBenchmarkNowSupportsGenerationTime(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	resolved, err := capacity.ResolveDatasetBenchmarkNow("generation-time", now)
	if err != nil || !resolved.Equal(now) {
		t.Fatalf("generation-time resolved=%s err=%v, want %s", resolved, err, now)
	}
	fixed, err := capacity.ResolveDatasetBenchmarkNow("2026-08-06T16:00:00+08:00", now)
	if err != nil || fixed.Format(time.RFC3339) != "2026-08-06T16:00:00+08:00" {
		t.Fatalf("fixed benchmark time resolved=%s err=%v", fixed, err)
	}
}

func TestAllocateTrafficTiersUsesKeyShares(t *testing.T) {
	tiers := []capacity.TrafficTier{
		{Name: "high", KeyShare: 0.30, PerKeyWeight: 10},
		{Name: "medium", KeyShare: 0.50, PerKeyWeight: 3},
		{Name: "low", KeyShare: 0.20, PerKeyWeight: 1},
	}
	profiles, err := capacity.BuildAPIKeyProfiles(50, tiers, 20260806)
	if err != nil {
		t.Fatalf("BuildAPIKeyProfiles returned error: %v", err)
	}
	counts := map[string]int{}
	for _, profile := range profiles {
		counts[profile.Tier]++
	}
	if !reflect.DeepEqual(counts, map[string]int{"high": 15, "medium": 25, "low": 10}) {
		t.Fatalf("tier counts=%v", counts)
	}
	if !(profiles[0].Weight > profiles[15].Weight && profiles[15].Weight > profiles[40].Weight) {
		t.Fatalf("tier weights are not ordered: high=%f medium=%f low=%f", profiles[0].Weight, profiles[15].Weight, profiles[40].Weight)
	}
}

func TestManifestRejectsRecentWindowThatConsumesNinetyDayTotal(t *testing.T) {
	manifest, err := capacity.LoadManifest(filepath.Join("..", "..", "manifest", "capacity-v1.json"))
	if err != nil {
		t.Fatalf("LoadManifest returned error: %v", err)
	}
	manifest.Dataset.Recent30DayEvents = manifest.Dataset.HotEvents
	if err := manifest.Validate(); err == nil {
		t.Fatal("90-day dataset must retain events outside the latest 30 days")
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
			ID: "reference-3m", HotEvents: 1, Recent30DayEvents: 1, HotDays: 1, BenchmarkNow: "2026-08-06T16:00:00+08:00",
			Cardinality: capacity.Cardinality{Identities: 1001, Models: 101, APIKeys: 101},
		},
		TrafficTiers: []capacity.TrafficTier{{Name: "all", KeyShare: 1, PerKeyWeight: 1}},
		Resources:    []capacity.Resource{{ID: "1c-unlimited", CPU: 1, MemoryMiB: 0}},
		Search: capacity.Search{
			RatesPerSecond: []int{1}, InitialRatePerSecond: 1, ProbeSeconds: 1, BoundarySeconds: 1, BoundaryRepetitions: 1,
			SoakSeconds: 1, MaxPassDrainSeconds: 1, MaxRunSeconds: 1, DashboardCoreP99MS: 3000, DashboardRequestsPerSecond: 1, AnalysisLatencyIntervalSeconds: 30, RecommendedCapacityRate: 0.7,
		},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate should reject cardinality beyond capacity bounds")
	}
}
