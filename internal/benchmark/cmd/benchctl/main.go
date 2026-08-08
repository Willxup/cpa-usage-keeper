package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cpa-usage-keeper/internal/benchmark/capacity"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "plan":
		return runPlan(args[1:])
	case "generate":
		return runGenerate(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "probe":
		return runProbe(args[1:])
	case "clone":
		return runClone(args[1:])
	case "compress":
		return runCompress(args[1:])
	case "run":
		return runSuite(args[1:], false)
	case "resume":
		return runSuite(args[1:], true)
	case "summarize":
		return runSummarize(args[1:])
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: benchctl <plan|generate|validate|probe|clone|compress|run|resume|summarize> [flags]")
}

func runPlan(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "internal/benchmark/manifest/capacity-v1.json", "suite manifest path")
	outputPath := flags.String("output", "", "plan output path; stdout when empty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manifest, err := capacity.LoadManifest(*manifestPath)
	if err != nil {
		return err
	}
	plan, err := capacity.ExpandPlan(manifest)
	if err != nil {
		return err
	}
	data, err := capacity.MarshalPlan(plan)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*outputPath) == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return writeAtomic(*outputPath, data)
}

func runGenerate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "internal/benchmark/manifest/capacity-v1.json", "suite manifest path")
	databasePath := flags.String("database", "", "output SQLite database")
	nowText := flags.String("now", "", "RFC3339 benchmark end time; manifest benchmark_now when empty")
	vacuum := flags.Bool("vacuum", true, "vacuum canonical database after archiving")
	insertBatch := flags.Int("insert-batch", 10_000, "raw event insert batch")
	aggregatePage := flags.Int("aggregate-page", 5_000, "derived aggregation page used only during unrestricted preparation")
	resultPath := flags.String("result", "", "dataset metadata output; defaults to <database-dir>/dataset.json")
	compress := flags.Bool("compress", false, "zstd-compress canonical database and remove raw file after verification")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*databasePath) == "" {
		return fmt.Errorf("generate requires --database")
	}
	manifest, err := capacity.LoadManifest(*manifestPath)
	if err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	effectiveNow := strings.TrimSpace(*nowText)
	if effectiveNow == "" {
		effectiveNow = manifest.Dataset.BenchmarkNow
	}
	now, err := time.Parse(time.RFC3339, effectiveNow)
	if err != nil {
		return fmt.Errorf("parse benchmark now: %w", err)
	}
	options := capacity.GenerateOptions{
		Path: *databasePath, HotEvents: manifest.Dataset.HotEvents,
		ArchiveEvents: manifest.Dataset.ArchiveEvents, HotDays: manifest.Dataset.HotDays,
		ArchiveDays: manifest.Dataset.ArchiveDays, FailureRate: manifest.Dataset.FailureRate,
		Seed: manifest.Dataset.Seed, Now: now, Cardinality: manifest.Dataset.Cardinality,
		TrafficTiers: manifest.TrafficTiers, InsertBatchSize: *insertBatch, AggregatePage: *aggregatePage, Vacuum: *vacuum,
	}
	result, err := capacity.GenerateDataset(context.Background(), options)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*resultPath) == "" {
		*resultPath = filepath.Join(filepath.Dir(*databasePath), "dataset.json")
	}
	if err := capacity.WriteJSONAtomic(*resultPath, result); err != nil {
		return err
	}
	if *compress {
		if err := capacity.CompressDataset(context.Background(), *databasePath, *databasePath+".zst", true); err != nil {
			return err
		}
	}
	return writeJSON(os.Stdout, result)
}

func runValidate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	databasePath := flags.String("database", "", "SQLite database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*databasePath) == "" {
		return fmt.Errorf("validate requires --database")
	}
	db, err := repository.OpenReadDatabase(config.Config{SQLitePath: *databasePath})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	result, err := capacity.ValidateDataset(db, *databasePath)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, result)
}

func runProbe(args []string) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "internal/benchmark/manifest/capacity-v1.json", "suite manifest path")
	databasePath := flags.String("database", "", "active benchmark SQLite database")
	redisAddress := flags.String("redis", "127.0.0.1:16379", "Redis address")
	redisPassword := flags.String("redis-password", "benchmark-only", "Redis password")
	applicationURL := flags.String("app", "http://127.0.0.1:18080", "Keeper base URL")
	rate := flags.Int("rate", 0, "offered usage events per second")
	duration := flags.Duration("duration", 30*time.Second, "probe duration")
	drainTimeout := flags.Duration("drain-timeout", 15*time.Second, "post-probe drain timeout")
	httpRate := flags.Int("http-rate", 8, "dashboard HTTP requests per second")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *rate <= 0 || strings.TrimSpace(*databasePath) == "" {
		return fmt.Errorf("probe requires --rate and --database")
	}
	manifest, err := capacity.LoadManifest(*manifestPath)
	if err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	apiProfiles, err := capacity.BuildAPIKeyProfiles(manifest.Dataset.Cardinality.APIKeys, manifest.TrafficTiers, manifest.Dataset.Seed)
	if err != nil {
		return err
	}
	report, err := capacity.RunProbe(context.Background(), capacity.ProbeOptions{
		RedisAddress: *redisAddress, RedisPassword: *redisPassword, RedisChannel: "usage",
		ApplicationURL: *applicationURL, DatabasePath: *databasePath, RatePerSecond: *rate,
		Duration: *duration, DrainTimeout: *drainTimeout, HTTPRatePerSecond: *httpRate,
		Cardinality: manifest.Dataset.Cardinality, APIKeyProfiles: apiProfiles, Seed: manifest.Dataset.Seed,
		Thresholds: capacity.ProbeThresholds{MinDurableRatio: 0.99, MaxBacklogGrowth: 0,
			InteractiveP95MS: float64(manifest.Search.DashboardCoreP95MS), InteractiveP99MS: float64(manifest.Search.DashboardOverallP99MS)},
	})
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, report)
}

func runClone(args []string) error {
	flags := flag.NewFlagSet("clone", flag.ContinueOnError)
	source := flags.String("source", "", "source SQLite database")
	destination := flags.String("destination", "", "destination SQLite database")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*source) == "" || strings.TrimSpace(*destination) == "" {
		return fmt.Errorf("clone requires --source and --destination")
	}
	return capacity.BackupSQLite(context.Background(), *source, *destination)
}

func runCompress(args []string) error {
	flags := flag.NewFlagSet("compress", flag.ContinueOnError)
	source := flags.String("source", "", "uncompressed canonical SQLite database")
	destination := flags.String("destination", "", "zstd canonical archive; defaults to <source>.zst")
	removeSource := flags.Bool("remove-source", false, "remove source only after zstd verification succeeds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*source) == "" {
		return fmt.Errorf("compress requires --source")
	}
	if strings.TrimSpace(*destination) == "" {
		*destination = *source + ".zst"
	}
	return capacity.CompressDataset(context.Background(), *source, *destination, *removeSource)
}

func runSuite(args []string, resume bool) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	manifestPath := flags.String("manifest", "internal/benchmark/manifest/capacity-v1.json", "suite manifest path")
	planPath := flags.String("plan", "", "expanded plan path")
	root := flags.String("root", "/var/tmp/cpa-usage-keeper-capacity", "benchmark root")
	runID := flags.String("run-id", "", "stable run identifier")
	keeperBinary := flags.String("keeper", "", "Keeper server binary")
	redisBinary := flags.String("redis-server", "redis-server", "Redis server binary")
	redisPort := flags.Int("redis-port", 16379, "task Redis port")
	appPort := flags.Int("app-port", 18080, "Keeper HTTP port")
	cells := flags.String("cells", "", "comma-separated cell IDs; all cells when empty")
	maxDuration := flags.Duration("max-duration", 8*time.Hour, "whole-run deadline")
	fixedRate := flags.Int("fixed-rate", 0, "run one fixed-rate soak instead of capacity search")
	fixedDuration := flags.Duration("fixed-duration", 0, "fixed-rate soak duration; manifest soak duration when empty")
	fixedPass := flags.String("fixed-pass", "interactive", "fixed-rate pass requirement: interactive or hard")
	searchDuration := flags.Duration("search-duration", 0, "override each capacity-search probe duration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*planPath) == "" || strings.TrimSpace(*keeperBinary) == "" {
		return fmt.Errorf("run requires --plan and --keeper")
	}
	if strings.TrimSpace(*runID) == "" {
		*runID = "capacity-v1-" + time.Now().UTC().Format("20060102-150405")
	}
	var cellIDs []string
	for _, value := range strings.Split(*cells, ",") {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cellIDs = append(cellIDs, trimmed)
		}
	}
	metadata, err := capacity.ExecuteRun(context.Background(), capacity.RunOptions{
		ManifestPath: *manifestPath, PlanPath: *planPath, Root: *root, RunID: *runID,
		KeeperBinary: *keeperBinary, RedisBinary: *redisBinary, RedisPort: *redisPort, AppPort: *appPort,
		CellIDs: cellIDs, Resume: resume, MaxDuration: *maxDuration,
		FixedRate: *fixedRate, FixedDuration: *fixedDuration, FixedPass: *fixedPass, SearchDuration: *searchDuration,
	})
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, metadata)
}

func runSummarize(args []string) error {
	flags := flag.NewFlagSet("summarize", flag.ContinueOnError)
	runDir := flags.String("run-dir", "", "benchmark run directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*runDir) == "" {
		return fmt.Errorf("summarize requires --run-dir")
	}
	summary, err := capacity.SummarizeRun(*runDir)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, summary)
}

func writeJSON(file *os.File, value any) error {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".benchctl-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary output: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish output: %w", err)
	}
	return nil
}
