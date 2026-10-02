package capacity_test

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/benchmark/capacity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
		Path:              filepath.Join(t.TempDir(), "dataset.db"),
		HotEvents:         400,
		Recent30DayEvents: 250,
		ArchiveEvents:     50,
		HotDays:           90,
		ArchiveDays:       7,
		FailureRate:       0.041818,
		Seed:              20260806,
		Now:               time.Date(2026, 8, 6, 15, 0, 0, 0, location),
		Cardinality:       capacity.Cardinality{Identities: 12, Models: 6, APIKeys: 4},
		TrafficTiers:      capacityTestTrafficTiers(),
		InsertBatchSize:   100,
		AggregatePage:     200,
	}

	result, err := capacity.GenerateDataset(context.Background(), options)
	if err != nil {
		t.Fatalf("GenerateDataset returned error: %v", err)
	}
	if result.HotEvents != 400 || result.ArchiveEvents != 50 || result.TotalEvents != 450 {
		t.Fatalf("unexpected generated counts: %+v", result)
	}
	if result.Recent30DayEvents != 250 {
		t.Fatalf("recent 30-day events=%d, want 250", result.Recent30DayEvents)
	}
	if result.FailureRate != options.FailureRate || !slices.Equal(result.TrafficTiers, options.TrafficTiers) {
		t.Fatalf("generation config=%v/%+v, want %v/%+v", result.FailureRate, result.TrafficTiers, options.FailureRate, options.TrafficTiers)
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
	// 合成事件使用已存模型报价，热表和归档各抽样核对一次独立算式。
	db, err := gorm.Open(sqlite.Open(options.Path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open generated dataset: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load generated database handle: %v", err)
	}
	defer sqlDB.Close()
	var invalidEventCosts int64
	if err := db.Raw(`SELECT COUNT(*) FROM (
		SELECT cost_usd, cost_available FROM usage_events
		UNION ALL SELECT cost_usd, cost_available FROM usage_events_archive
	) WHERE cost_usd IS NULL OR cost_usd <= 0 OR cost_available IS NULL OR cost_available <> 1`).Scan(&invalidEventCosts).Error; err != nil {
		t.Fatalf("inspect generated event costs: %v", err)
	}
	if invalidEventCosts != 0 {
		t.Fatalf("generated dataset has %d events without an explicit positive price", invalidEventCosts)
	}
	assertGeneratedEventMatchesStoredPrice(t, db, "usage_events")
	assertGeneratedEventMatchesStoredPrice(t, db, "usage_events_archive")
	assertGeneratedOverviewMatchesStoredEventFees(t, db)
}

// assertGeneratedOverviewMatchesStoredEventFees 验证合成桶已写明细费用，而非仅填请求数与 Token。
func assertGeneratedOverviewMatchesStoredEventFees(t *testing.T, db *gorm.DB) {
	t.Helper()
	var eventCost float64
	if err := db.Raw(`SELECT SUM(cost_usd) FROM (SELECT cost_usd FROM usage_events UNION ALL SELECT cost_usd FROM usage_events_archive)`).Scan(&eventCost).Error; err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
		var result struct {
			Cost        float64
			Unavailable int64
			NullRows    int64
		}
		query := `SELECT SUM(cost_usd) AS cost, SUM(unavailable_cost_count) AS unavailable,
			SUM(CASE WHEN cost_usd IS NULL OR unavailable_cost_count IS NULL THEN 1 ELSE 0 END) AS null_rows FROM ` + table
		if err := db.Raw(query).Scan(&result).Error; err != nil {
			t.Fatalf("read %s generated fees: %v", table, err)
		}
		if result.NullRows != 0 || result.Unavailable != 0 || math.IsNaN(result.Cost) || math.IsInf(result.Cost, 0) || math.Abs(result.Cost-eventCost) > 1e-7 {
			t.Fatalf("%s fees differ from stored events: %+v event_cost=%v", table, result, eventCost)
		}
	}
}

func assertGeneratedEventMatchesStoredPrice(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var input, output, cacheRead, cacheCreation int64
	var inputPrice, outputPrice, readPrice, writePrice, multiplier, storedCost float64
	var available bool
	query := `SELECT e.input_tokens, e.output_tokens, e.cache_read_tokens, e.cache_creation_tokens,
		p.prompt_price_per1_m, p.completion_price_per1_m, p.cache_read_price_per1_m,
		p.cache_creation_price_per1_m, p.price_multiplier, e.cost_usd, e.cost_available
		FROM ` + table + ` e JOIN model_price_settings p ON p.model = e.model ORDER BY e.id LIMIT 1`
	if err := db.Raw(query).Row().Scan(&input, &output, &cacheRead, &cacheCreation,
		&inputPrice, &outputPrice, &readPrice, &writePrice, &multiplier, &storedCost, &available); err != nil {
		t.Fatalf("sample %s price: %v", table, err)
	}
	// 固定合成 Token 范围保证两类缓存总量不超过输入，因此按四段单价直接核对。
	want := (float64(input-cacheRead-cacheCreation)*inputPrice + float64(output)*outputPrice +
		float64(cacheRead)*readPrice + float64(cacheCreation)*writePrice) / 1_000_000 * multiplier
	if !available || math.Abs(storedCost-want) > 1e-12 {
		t.Fatalf("%s stored cost=%g available=%t, want %g", table, storedCost, available, want)
	}
}

func TestGenerateDatasetIsSemanticallyDeterministic(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	base := capacity.GenerateOptions{
		HotEvents:         120,
		Recent30DayEvents: 120,
		ArchiveEvents:     12,
		HotDays:           30,
		ArchiveDays:       2,
		FailureRate:       0.05,
		Seed:              99,
		Now:               time.Date(2026, 8, 6, 15, 0, 0, 0, location),
		Cardinality:       capacity.Cardinality{Identities: 6, Models: 4, APIKeys: 4},
		TrafficTiers:      capacityTestTrafficTiers(),
		InsertBatchSize:   50,
		AggregatePage:     100,
	}
	generate := func(options capacity.GenerateOptions) capacity.DatasetResult {
		options.Path = filepath.Join(t.TempDir(), "dataset.db")
		result, err := capacity.GenerateDataset(context.Background(), options)
		if err != nil {
			t.Fatalf("GenerateDataset seed %d: %v", options.Seed, err)
		}
		return result
	}
	first, second := generate(base), generate(base)
	if first.SemanticFingerprint != second.SemanticFingerprint {
		t.Fatalf("semantic fingerprints differ: %q != %q", first.SemanticFingerprint, second.SemanticFingerprint)
	}
	base.Seed++
	third := generate(base)
	if first.SemanticFingerprint == third.SemanticFingerprint {
		t.Fatalf("different seeds must produce different fingerprints: %q", first.SemanticFingerprint)
	}
}

func TestValidateDatasetAgainstManifestRejectsStaleOrMismatchedMetadata(t *testing.T) {
	queryAnchor := time.Date(2026, 8, 9, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	actual := capacity.DatasetResult{
		QueryAnchor: queryAnchor, EventTimeMax: queryAnchor.Add(-24 * time.Hour), Recent30DayEvents: 90,
		HotEvents: 90, ArchiveEvents: 10, TotalEvents: 100,
		Identities: 3, Models: 2, APIKeys: 2, UsedIdentities: 3, UsedModels: 2, UsedAPIKeys: 2,
		OverviewHourlyRequests: 100, OverviewDailyRequests: 100, IdentityRequests: 100,
		CheckpointMin: 100, CheckpointMax: 100, QuickCheck: "ok", SemanticFingerprint: "fingerprint",
	}
	metadata := actual
	metadata.GeneratorVersion = capacity.DatasetGeneratorVersion
	metadata.Seed = 42
	metadata.BenchmarkNow = queryAnchor.Add(-24 * time.Hour)
	metadata.FailureRate = 0.01
	metadata.TrafficTiers = []capacity.TrafficTier{{Name: "all", KeyShare: 1, PerKeyWeight: 1}}
	manifest := capacity.Manifest{Dataset: capacity.DatasetSpec{
		HotEvents: 90, Recent30DayEvents: 90, ArchiveEvents: 10, FailureRate: 0.01, Seed: 42, BenchmarkNow: "generation-time",
		Cardinality: capacity.Cardinality{Identities: 3, Models: 2, APIKeys: 2},
	}, TrafficTiers: []capacity.TrafficTier{{Name: "all", KeyShare: 1, PerKeyWeight: 1}}}
	if err := capacity.ValidateDatasetAgainstManifest(actual, metadata, manifest); err != nil {
		t.Fatalf("valid dataset rejected: %v", err)
	}
	staleGenerator := metadata
	staleGenerator.GeneratorVersion = "production-v9-event-status-stream"
	if err := capacity.ValidateDatasetAgainstManifest(actual, staleGenerator, manifest); err == nil {
		t.Fatal("dataset generated before pricing storage must fail validation")
	}

	mismatched := actual
	mismatched.OrphanAPIKeys = 1
	if err := capacity.ValidateDatasetAgainstManifest(mismatched, metadata, manifest); err == nil {
		t.Fatal("orphan API key must fail strict validation")
	}

	stale := actual
	stale.EventTimeMax = queryAnchor.Add(-8 * 24 * time.Hour)
	if err := capacity.ValidateDatasetAgainstManifest(stale, metadata, manifest); err == nil {
		t.Fatal("dataset older than the freshness window must fail")
	}

	wrongMetadata := metadata
	wrongMetadata.OverviewDailyRows++
	if err := capacity.ValidateDatasetAgainstManifest(actual, wrongMetadata, manifest); err == nil {
		t.Fatal("dataset statistics must match dataset.json")
	}

	wrongRecentWindow := metadata
	wrongRecentWindow.Recent30DayEvents--
	if err := capacity.ValidateDatasetAgainstManifest(actual, wrongRecentWindow, manifest); err == nil {
		t.Fatal("generation-time recent 30-day count must match the manifest")
	}

	wrongFailureRate := metadata
	wrongFailureRate.FailureRate = 0.02
	if err := capacity.ValidateDatasetAgainstManifest(actual, wrongFailureRate, manifest); err == nil {
		t.Fatal("dataset failure rate must match the manifest")
	}

	wrongTrafficTiers := metadata
	wrongTrafficTiers.TrafficTiers = []capacity.TrafficTier{{Name: "all", KeyShare: 1, PerKeyWeight: 2}}
	if err := capacity.ValidateDatasetAgainstManifest(actual, wrongTrafficTiers, manifest); err == nil {
		t.Fatal("dataset traffic tiers must match the manifest")
	}
}
