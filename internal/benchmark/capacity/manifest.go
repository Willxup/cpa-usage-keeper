package capacity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	maxIdentities = 1000
	maxModels     = 100
	maxAPIKeys    = 100
)

type Manifest struct {
	Version      string        `json:"version"`
	Target       Target        `json:"target"`
	Dataset      DatasetSpec   `json:"dataset"`
	TrafficTiers []TrafficTier `json:"traffic_tiers"`
	Resources    []Resource    `json:"resources"`
	Search       Search        `json:"search"`
	SourceSHA256 string        `json:"-"`
}

type Target struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type DatasetSpec struct {
	ID            string      `json:"id"`
	HotEvents     int64       `json:"hot_events"`
	ArchiveEvents int64       `json:"archive_events"`
	FailureRate   float64     `json:"failure_rate"`
	Seed          uint64      `json:"seed"`
	HotDays       int         `json:"hot_days"`
	ArchiveDays   int         `json:"archive_days"`
	BenchmarkNow  string      `json:"benchmark_now"`
	Cardinality   Cardinality `json:"cardinality"`
}

type Cardinality struct {
	Identities int `json:"identities"`
	Models     int `json:"models"`
	APIKeys    int `json:"api_keys"`
}

type TrafficTier struct {
	Name         string  `json:"name"`
	KeyShare     float64 `json:"key_share"`
	PerKeyWeight float64 `json:"per_key_weight"`
}

type Resource struct {
	ID        string  `json:"id"`
	CPU       float64 `json:"cpu"`
	MemoryMiB int     `json:"memory_mib"`
}

type Search struct {
	RatesPerSecond             []int   `json:"rates_per_second"`
	ProbeSeconds               int     `json:"probe_seconds"`
	BoundarySeconds            int     `json:"boundary_seconds"`
	BoundaryRepetitions        int     `json:"boundary_repetitions"`
	SkipBoundary               bool    `json:"skip_boundary,omitempty"`
	SoakSeconds                int     `json:"soak_seconds"`
	MaxRunSeconds              int     `json:"max_run_seconds"`
	DashboardCoreP95MS         int     `json:"dashboard_core_p95_ms"`
	DashboardOverallP99MS      int     `json:"dashboard_overall_p99_ms"`
	DashboardRequestsPerSecond int     `json:"dashboard_requests_per_second"`
	SearchDashboardCapacity    bool    `json:"search_dashboard_capacity,omitempty"`
	RecommendedCapacityRate    float64 `json:"recommended_capacity_ratio"`
}

type Plan struct {
	Version        string `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Cells          []Cell `json:"cells"`
}

type Cell struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	DatasetID      string      `json:"dataset_id"`
	HotEvents      int64       `json:"hot_events"`
	ArchiveEvents  int64       `json:"archive_events"`
	Cardinality    Cardinality `json:"cardinality"`
	Resource       Resource    `json:"resource"`
	RatesPerSecond []int       `json:"rates_per_second"`
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read benchmark manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode benchmark manifest: %w", err)
	}
	digest := sha256.Sum256(data)
	manifest.SourceSHA256 = hex.EncodeToString(digest[:])
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Version != "capacity-v1" {
		return fmt.Errorf("unsupported benchmark manifest version %q", m.Version)
	}
	if m.Target.OS != "linux" || m.Target.Arch != "amd64" {
		return fmt.Errorf("capacity benchmark requires linux/amd64")
	}
	if !validPublicID(m.Dataset.ID) {
		return fmt.Errorf("invalid dataset ID %q", m.Dataset.ID)
	}
	if m.Dataset.HotEvents <= 0 || m.Dataset.ArchiveEvents < 0 || m.Dataset.HotDays <= 0 || m.Dataset.ArchiveDays < 0 {
		return fmt.Errorf("dataset counts and day windows must be positive")
	}
	if m.Dataset.FailureRate < 0 || m.Dataset.FailureRate > 1 {
		return fmt.Errorf("failure rate must be between zero and one")
	}
	if _, err := time.Parse(time.RFC3339, m.Dataset.BenchmarkNow); err != nil {
		return fmt.Errorf("dataset benchmark_now must be RFC3339: %w", err)
	}
	if err := validateCardinality(m.Dataset.ID, m.Dataset.Cardinality); err != nil {
		return err
	}
	if len(m.TrafficTiers) == 0 {
		return fmt.Errorf("traffic tiers are required")
	}
	shareTotal := 0.0
	seenTierNames := map[string]bool{}
	for _, tier := range m.TrafficTiers {
		if !validPublicID(tier.Name) || seenTierNames[tier.Name] || tier.KeyShare <= 0 || tier.PerKeyWeight <= 0 {
			return fmt.Errorf("invalid traffic tier %q", tier.Name)
		}
		seenTierNames[tier.Name] = true
		shareTotal += tier.KeyShare
	}
	if math.Abs(shareTotal-1) > 1e-9 {
		return fmt.Errorf("traffic tier key shares sum to %.12f, want 1", shareTotal)
	}
	if len(m.Resources) == 0 {
		return fmt.Errorf("at least one resource profile is required")
	}
	seenResources := map[string]bool{}
	for _, resource := range m.Resources {
		if !validPublicID(resource.ID) || seenResources[resource.ID] || resource.CPU <= 0 || resource.CPU > 4 || math.Trunc(resource.CPU) != resource.CPU || (resource.MemoryMiB != 0 && resource.MemoryMiB < 100) || resource.MemoryMiB > 4096 {
			return fmt.Errorf("invalid resource profile %q", resource.ID)
		}
		seenResources[resource.ID] = true
	}
	if len(m.Search.RatesPerSecond) == 0 || m.Search.ProbeSeconds <= 0 || m.Search.BoundarySeconds <= 0 || m.Search.BoundaryRepetitions <= 0 || m.Search.SoakSeconds <= 0 || m.Search.MaxRunSeconds <= 0 || m.Search.DashboardRequestsPerSecond <= 0 {
		return fmt.Errorf("capacity search durations and rates must be positive")
	}
	for index, rate := range m.Search.RatesPerSecond {
		if rate <= 0 || (index > 0 && rate <= m.Search.RatesPerSecond[index-1]) {
			return fmt.Errorf("capacity search rates must be positive and increasing")
		}
	}
	if m.Search.DashboardCoreP95MS < 0 || m.Search.DashboardOverallP99MS <= 0 {
		return fmt.Errorf("dashboard latency thresholds are invalid")
	}
	if m.Search.RecommendedCapacityRate <= 0 || m.Search.RecommendedCapacityRate > 1 {
		return fmt.Errorf("recommended capacity ratio must be in (0,1]")
	}
	return nil
}

func LoadPlan(path string) (Plan, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, "", fmt.Errorf("read benchmark plan: %w", err)
	}
	var plan Plan
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, "", fmt.Errorf("decode benchmark plan: %w", err)
	}
	if strings.TrimSpace(plan.Version) == "" || strings.TrimSpace(plan.ManifestSHA256) == "" || len(plan.Cells) == 0 {
		return Plan{}, "", fmt.Errorf("benchmark plan is incomplete")
	}
	seen := map[string]bool{}
	for _, cell := range plan.Cells {
		if strings.TrimSpace(cell.ID) == "" || !validPublicID(cell.DatasetID) || seen[cell.ID] {
			return Plan{}, "", fmt.Errorf("benchmark plan contains invalid or duplicate cell %q", cell.ID)
		}
		seen[cell.ID] = true
	}
	digest := sha256.Sum256(data)
	return plan, hex.EncodeToString(digest[:]), nil
}

func validateCardinality(name string, cardinality Cardinality) error {
	if cardinality.Identities <= 0 || cardinality.Identities > maxIdentities || cardinality.Models <= 0 || cardinality.Models > maxModels || cardinality.APIKeys <= 0 || cardinality.APIKeys > maxAPIKeys {
		return fmt.Errorf("cardinality %q exceeds benchmark bounds: %+v", name, cardinality)
	}
	return nil
}

func validPublicID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") || strings.Contains(value, "--") {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func ExpandPlan(manifest Manifest) (Plan, error) {
	if err := manifest.Validate(); err != nil {
		return Plan{}, err
	}
	plan := Plan{Version: manifest.Version, ManifestSHA256: manifest.SourceSHA256}
	for _, resource := range manifest.Resources {
		plan.Cells = append(plan.Cells, Cell{
			ID:             fmt.Sprintf("capacity-%s-%s", manifest.Dataset.ID, resource.ID),
			Kind:           "capacity",
			DatasetID:      manifest.Dataset.ID,
			HotEvents:      manifest.Dataset.HotEvents,
			ArchiveEvents:  manifest.Dataset.ArchiveEvents,
			Cardinality:    manifest.Dataset.Cardinality,
			Resource:       resource,
			RatesPerSecond: append([]int(nil), manifest.Search.RatesPerSecond...),
		})
	}
	return plan, nil
}

func MarshalPlan(plan Plan) ([]byte, error) {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode benchmark plan: %w", err)
	}
	return append(data, '\n'), nil
}

func SortCellsByID(cells []Cell) {
	sort.Slice(cells, func(left, right int) bool { return cells[left].ID < cells[right].ID })
}
