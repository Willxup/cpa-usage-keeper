package capacity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type RunOptions struct {
	ManifestPath   string
	PlanPath       string
	Root           string
	RunID          string
	KeeperBinary   string
	RedisBinary    string
	RedisPort      int
	AppPort        int
	CellIDs        []string
	Resume         bool
	MaxDuration    time.Duration
	FixedRate      int
	FixedDuration  time.Duration
	FixedPass      string
	SearchDuration time.Duration
}

type RunMetadata struct {
	Version               string    `json:"version"`
	RunID                 string    `json:"run_id"`
	State                 string    `json:"state"`
	StartedAt             time.Time `json:"started_at"`
	UpdatedAt             time.Time `json:"updated_at"`
	FinishedAt            time.Time `json:"finished_at,omitempty"`
	OS                    string    `json:"os"`
	Arch                  string    `json:"arch"`
	ManifestSHA256        string    `json:"manifest_sha256"`
	PlanSHA256            string    `json:"plan_sha256"`
	KeeperBinarySHA256    string    `json:"keeper_binary_sha256"`
	BenchctlBinarySHA256  string    `json:"benchctl_binary_sha256"`
	RedisPort             int       `json:"redis_port"`
	AppPort               int       `json:"app_port"`
	SelectedCells         []string  `json:"selected_cells"`
	CompletedCells        int       `json:"completed_cells"`
	FailedCells           int       `json:"failed_cells"`
	SkippedCells          int       `json:"skipped_cells"`
	FixedRate             int       `json:"fixed_rate,omitempty"`
	FixedDurationSeconds  int       `json:"fixed_duration_seconds,omitempty"`
	FixedPass             string    `json:"fixed_pass,omitempty"`
	SearchDurationSeconds int       `json:"search_duration_seconds,omitempty"`
	Error                 string    `json:"error,omitempty"`
}

type AttemptResource struct {
	MeasuredSeconds       float64 `json:"measured_seconds"`
	CPUUsageUsecDelta     int64   `json:"cpu_usage_usec_delta"`
	CPUUtilizationPercent float64 `json:"cpu_utilization_percent"`
	CPUThrottledUsecDelta int64   `json:"cpu_throttled_usec_delta"`
	IOReadBytesDelta      int64   `json:"io_read_bytes_delta"`
	IOWriteBytesDelta     int64   `json:"io_write_bytes_delta"`
	MemoryPeakBytes       int64   `json:"memory_peak_bytes"`
	MemoryOOMDelta        int64   `json:"memory_oom_delta"`
	MemoryOOMKillDelta    int64   `json:"memory_oom_kill_delta"`
}

type ProbeAttempt struct {
	Phase           string          `json:"phase"`
	Repetition      int             `json:"repetition"`
	RatePerSecond   int             `json:"rate_per_second"`
	DurationSeconds int             `json:"duration_seconds"`
	StartedAt       time.Time       `json:"started_at"`
	FinishedAt      time.Time       `json:"finished_at"`
	StartupSeconds  float64         `json:"startup_seconds"`
	WarmupSeconds   float64         `json:"warmup_seconds"`
	UnitName        string          `json:"unit_name"`
	EffectiveLimits CgroupLimits    `json:"effective_limits"`
	Report          ProbeReport     `json:"report"`
	Resource        AttemptResource `json:"resource"`
	LastResource    CgroupSample    `json:"last_resource"`
	PeakResource    CgroupSample    `json:"peak_resource"`
	FinalUnitState  UnitState       `json:"final_unit_state"`
	Error           string          `json:"error,omitempty"`
}

type CapacityResult struct {
	HardEventsPerSecond        int `json:"hard_events_per_second"`
	InteractiveEventsPerSecond int `json:"interactive_events_per_second"`
	RecommendedEventsPerSecond int `json:"recommended_events_per_second"`
}

type CellResult struct {
	Version              string         `json:"version"`
	RunID                string         `json:"run_id"`
	Cell                 Cell           `json:"cell"`
	Status               string         `json:"status"`
	StartedAt            time.Time      `json:"started_at"`
	FinishedAt           time.Time      `json:"finished_at"`
	StartupSeconds       float64        `json:"startup_seconds"`
	WarmupSeconds        float64        `json:"warmup_seconds"`
	DatasetFingerprint   string         `json:"dataset_fingerprint"`
	ManifestSHA256       string         `json:"manifest_sha256"`
	KeeperBinarySHA256   string         `json:"keeper_binary_sha256"`
	BenchctlBinarySHA256 string         `json:"benchctl_binary_sha256"`
	UnitName             string         `json:"unit_name"`
	AllowedCPUs          string         `json:"allowed_cpus"`
	DriverCPUs           string         `json:"driver_cpus"`
	SharedDriver         bool           `json:"shared_driver"`
	EffectiveLimits      CgroupLimits   `json:"effective_limits"`
	Attempts             []ProbeAttempt `json:"attempts"`
	Capacity             CapacityResult `json:"capacity"`
	LastResource         CgroupSample   `json:"last_resource"`
	PeakResource         CgroupSample   `json:"peak_resource"`
	FinalUnitState       UnitState      `json:"final_unit_state"`
	DatabaseBytesBefore  int64          `json:"database_bytes_before"`
	DatabaseBytesAfter   int64          `json:"database_bytes_after"`
	WALBytesAfter        int64          `json:"wal_bytes_after"`
	WorkDatabaseRetained bool           `json:"work_database_retained"`
	Error                string         `json:"error,omitempty"`
}

type runContext struct {
	options        RunOptions
	manifest       Manifest
	plan           Plan
	metadata       RunMetadata
	runDir         string
	datasetsDir    string
	binaryDigest   string
	benchctlDigest string
	redis          SystemdRuntime
	redisPass      string
	onlineCPUs     int
}

func ExecuteRun(parent context.Context, options RunOptions) (metadata RunMetadata, returnErr error) {
	if options.RedisPort == 0 {
		options.RedisPort = 16379
	}
	if options.AppPort == 0 {
		options.AppPort = 18080
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 8 * time.Hour
	}
	if options.FixedPass == "" {
		options.FixedPass = "interactive"
	}
	if strings.TrimSpace(options.Root) == "" || strings.TrimSpace(options.RunID) == "" || strings.TrimSpace(options.KeeperBinary) == "" {
		return metadata, fmt.Errorf("run root, run ID, and Keeper binary are required")
	}
	cleanRoot := filepath.Clean(options.Root)
	if cleanRoot == "." || cleanRoot == string(os.PathSeparator) {
		return metadata, fmt.Errorf("benchmark root is unsafe: %q", options.Root)
	}
	if !validRunID(options.RunID) {
		return metadata, fmt.Errorf("invalid benchmark run ID %q", options.RunID)
	}
	if options.FixedRate < 0 || (options.FixedDuration > 0 && options.FixedRate == 0) {
		return metadata, fmt.Errorf("fixed duration requires a positive fixed rate")
	}
	if options.SearchDuration < 0 {
		return metadata, fmt.Errorf("search duration cannot be negative")
	}
	if _, err := FixedRateSoakPassed(options.FixedPass, ProbeEvaluation{}); err != nil {
		return metadata, err
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return metadata, fmt.Errorf("capacity benchmark requires linux/amd64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	ctx, cancel := context.WithTimeout(parent, options.MaxDuration)
	defer cancel()
	lock, err := AcquireFileLock(filepath.Join(options.Root, "benchmark.lock"))
	if err != nil {
		return metadata, err
	}
	defer lock.Close()

	manifest, err := LoadManifest(options.ManifestPath)
	if err != nil {
		return metadata, err
	}
	if err := manifest.Validate(); err != nil {
		return metadata, err
	}
	plan, planDigest, err := LoadPlan(options.PlanPath)
	if err != nil {
		return metadata, err
	}
	if plan.ManifestSHA256 != manifest.SourceSHA256 {
		return metadata, fmt.Errorf("plan manifest hash %s does not match %s", plan.ManifestSHA256, manifest.SourceSHA256)
	}
	selected, err := selectCells(plan.Cells, options.CellIDs)
	if err != nil {
		return metadata, err
	}
	binaryDigest, err := FileSHA256(options.KeeperBinary)
	if err != nil {
		return metadata, fmt.Errorf("hash Keeper binary: %w", err)
	}
	benchctlPath, err := os.Executable()
	if err != nil {
		return metadata, fmt.Errorf("resolve benchctl executable: %w", err)
	}
	benchctlDigest, err := FileSHA256(benchctlPath)
	if err != nil {
		return metadata, fmt.Errorf("hash benchctl binary: %w", err)
	}
	runDir := filepath.Join(options.Root, "runs", options.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return metadata, fmt.Errorf("create benchmark run directory: %w", err)
	}
	metadata = RunMetadata{
		Version: manifest.Version, RunID: options.RunID, State: "preparing", StartedAt: time.Now(), UpdatedAt: time.Now(),
		OS: runtime.GOOS, Arch: runtime.GOARCH, ManifestSHA256: manifest.SourceSHA256, PlanSHA256: planDigest,
		KeeperBinarySHA256: binaryDigest, BenchctlBinarySHA256: benchctlDigest, RedisPort: options.RedisPort, AppPort: options.AppPort,
		FixedRate: options.FixedRate, FixedDurationSeconds: int(options.FixedDuration.Seconds()), FixedPass: options.FixedPass,
		SearchDurationSeconds: int(options.SearchDuration.Seconds()),
	}
	for _, cell := range selected {
		metadata.SelectedCells = append(metadata.SelectedCells, cell.ID)
	}
	metadataPath := filepath.Join(runDir, "run.json")
	if err := WriteJSONAtomic(metadataPath, metadata); err != nil {
		return metadata, err
	}
	defer func() {
		if returnErr != nil {
			metadata.State = "failed"
			metadata.Error = returnErr.Error()
		}
		metadata.UpdatedAt = time.Now()
		metadata.FinishedAt = time.Now()
		_ = WriteJSONAtomic(metadataPath, metadata)
	}()

	for _, command := range []string{"systemd-run", "systemctl", "sqlite3", "taskset"} {
		if _, err := exec.LookPath(command); err != nil {
			return metadata, fmt.Errorf("required benchmark command %s is unavailable: %w", command, err)
		}
	}
	onlineCPUs, err := OnlineCPUCount()
	if err != nil {
		return metadata, err
	}
	if onlineCPUs < 4 {
		return metadata, fmt.Errorf("capacity benchmark requires 4 online CPUs, found %d", onlineCPUs)
	}
	context := &runContext{
		options: options, manifest: manifest, plan: plan, metadata: metadata, runDir: runDir,
		datasetsDir: filepath.Join(options.Root, "datasets"), binaryDigest: binaryDigest, benchctlDigest: benchctlDigest,
		redisPass: "benchmark-only", onlineCPUs: onlineCPUs,
	}
	redisUnit := unitName(options.RunID, "redis")
	context.redis, err = StartRedisUnit(ctx, RedisStartOptions{
		UnitName: redisUnit, Root: runDir, Port: options.RedisPort, Password: context.redisPass, BinaryPath: options.RedisBinary,
	})
	if err != nil {
		return metadata, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, stopUnitWithTimeout(context.redis.UnitName, 20*time.Second))
	}()

	metadata.State = "running"
	metadata.UpdatedAt = time.Now()
	if err := WriteJSONAtomic(metadataPath, metadata); err != nil {
		return metadata, err
	}
	for _, cell := range selected {
		if err := ctx.Err(); err != nil {
			return metadata, err
		}
		result, skipped, cellErr := context.executeCell(ctx, cell)
		if skipped {
			metadata.SkippedCells++
		} else if cellErr != nil || result.Status != "completed" {
			metadata.FailedCells++
		} else {
			metadata.CompletedCells++
		}
		metadata.UpdatedAt = time.Now()
		if err := WriteJSONAtomic(metadataPath, metadata); err != nil {
			return metadata, err
		}
		if err := RebuildResultsJSONL(runDir); err != nil {
			return metadata, err
		}
	}
	metadata.State = "completed"
	metadata.UpdatedAt = time.Now()
	if _, err := SummarizeRun(runDir); err != nil {
		return metadata, err
	}
	return metadata, nil
}

func validRunID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func (run *runContext) executeCell(ctx context.Context, cell Cell) (result CellResult, skipped bool, returnErr error) {
	datasetDir := filepath.Join(run.datasetsDir, cell.DatasetID)
	datasetPath, err := resolveDatasetPath(datasetDir)
	if err != nil {
		return result, false, fmt.Errorf("cell %s dataset: %w", cell.ID, err)
	}
	datasetResultPath := filepath.Join(datasetDir, "dataset.json")
	dataset, err := LoadDatasetResult(datasetResultPath)
	if err != nil {
		return result, false, fmt.Errorf("cell %s dataset metadata: %w", cell.ID, err)
	}
	if err := validateCellDataset(cell, dataset, datasetPath); err != nil {
		return result, false, err
	}
	cellDir := filepath.Join(run.runDir, "cells", cell.ID)
	if run.options.Resume {
		matched, matchErr := ResumeCellMatches(filepath.Join(cellDir, "result.json"), run.manifest.SourceSHA256, run.binaryDigest, run.benchctlDigest, dataset.SemanticFingerprint)
		if matchErr != nil {
			return result, false, matchErr
		}
		if matched {
			return result, true, nil
		}
	}
	attemptDir, err := nextAttemptDirectory(cellDir)
	if err != nil {
		return result, false, err
	}
	workDir := filepath.Join(attemptDir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return result, false, err
	}
	result = CellResult{
		Version: run.manifest.Version, RunID: run.options.RunID, Cell: cell, Status: "preparing", StartedAt: time.Now(),
		DatasetFingerprint: dataset.SemanticFingerprint, ManifestSHA256: run.manifest.SourceSHA256,
		KeeperBinarySHA256: run.binaryDigest, BenchctlBinarySHA256: run.benchctlDigest, DatabaseBytesBefore: dataset.DatabaseBytes,
	}
	resultPath := filepath.Join(cellDir, "result.json")
	defer func() {
		result.FinishedAt = time.Now()
		// Keeper 与采样器的 defer 后注册、会先停止；只有完整成功的可重建 clone 才在结果落盘前精确裁剪。
		if returnErr == nil && result.Status == "completed" {
			if pruneErr := PruneCompletedWorkDatabase(workDir); pruneErr != nil {
				returnErr = pruneErr
			} else {
				result.WorkDatabaseRetained = false
			}
		}
		if returnErr != nil {
			result.Error = returnErr.Error()
			if result.Status != "oom" && result.Status != "timeout" {
				result.Status = "failed"
			}
		}
		_ = WriteJSONAtomic(filepath.Join(attemptDir, "result.json"), result)
		_ = WriteJSONAtomic(resultPath, result)
		if result.Status == "completed" {
			_ = WriteJSONAtomic(filepath.Join(cellDir, "done.marker"), map[string]string{
				"manifest_sha256": result.ManifestSHA256, "keeper_binary_sha256": result.KeeperBinarySHA256,
				"benchctl_binary_sha256": result.BenchctlBinarySHA256, "dataset_fingerprint": result.DatasetFingerprint,
			})
		}
	}()

	activeDB := filepath.Join(workDir, "app.db")
	result.WorkDatabaseRetained = true
	envPath := filepath.Join(attemptDir, "keeper.env")
	if err := writeKeeperEnvironment(envPath, workDir, run.options.AppPort, run.options.RedisPort, run.redisPass); err != nil {
		return result, false, err
	}
	cpu := int(cell.Resource.CPU)
	allowedCPUs := cpuSet(0, cpu)
	driverCPUs := cpuSet(cpu, run.onlineCPUs)
	sharedDriver := false
	if driverCPUs == "" {
		driverCPUs = cpuSet(0, run.onlineCPUs)
		sharedDriver = true
	}
	result.AllowedCPUs = allowedCPUs
	result.DriverCPUs = driverCPUs
	result.SharedDriver = sharedDriver
	if err := SetProcessAffinity(driverCPUs); err != nil {
		return result, false, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, SetProcessAffinity(cpuSet(0, run.onlineCPUs)))
	}()
	probeSequence := 0
	runAttempt := func(phase string, repetition, rate int, duration time.Duration) ProbeAttempt {
		probeSequence++
		attempt := run.runIsolatedProbeAttempt(ctx, cell, datasetPath, activeDB, envPath, attemptDir, allowedCPUs, probeSequence, phase, repetition, rate, duration)
		mergeProbeAttemptRuntime(&result, attempt)
		return attempt
	}
	result.Status = "running"
	if run.options.FixedRate > 0 {
		duration := run.options.FixedDuration
		if duration <= 0 {
			duration = time.Duration(run.manifest.Search.SoakSeconds) * time.Second
		}
		attempt := runAttempt("soak", 1, run.options.FixedRate, duration)
		result.Attempts = append(result.Attempts, attempt)
		if attempt.Report.Evaluation.HardPass {
			result.Capacity.HardEventsPerSecond = run.options.FixedRate
		}
		if attempt.Report.Evaluation.InteractivePass {
			result.Capacity.InteractiveEventsPerSecond = run.options.FixedRate
			result.Capacity.RecommendedEventsPerSecond = run.options.FixedRate
		}
		passed, _ := FixedRateSoakPassed(run.options.FixedPass, attempt.Report.Evaluation)
		if !passed {
			if attempt.Report.Metrics.OOM {
				result.Status = "oom"
			}
			return result, false, fmt.Errorf("%s soak failed at %d events/s: %s", run.options.FixedPass, run.options.FixedRate, strings.Join(attempt.Report.Evaluation.Reasons, ","))
		}
		if stat, statErr := os.Stat(activeDB); statErr == nil {
			result.DatabaseBytesAfter = stat.Size()
		}
		if stat, statErr := os.Stat(activeDB + "-wal"); statErr == nil {
			result.WALBytesAfter = stat.Size()
		}
		result.Status = "completed"
		return result, false, nil
	}
	search, err := NewCapacitySearch(cell.RatesPerSecond)
	if err != nil {
		return result, false, err
	}
	searchAttempts := map[int]ProbeAttempt{}
	runSearchAttempt := func(rate int) ProbeAttempt {
		if attempt, ok := searchAttempts[rate]; ok {
			return attempt
		}
		searchDuration := run.options.SearchDuration
		if searchDuration <= 0 {
			searchDuration = time.Duration(run.manifest.Search.ProbeSeconds) * time.Second
		}
		attempt := runAttempt("search", 1, rate, searchDuration)
		result.Attempts = append(result.Attempts, attempt)
		searchAttempts[rate] = attempt
		return attempt
	}
	for {
		rate, ok := search.Next()
		if !ok {
			break
		}
		attempt := runSearchAttempt(rate)
		search.Record(rate, attempt.Report.Evaluation.HardPass)
		if IsCapacityAttemptInfrastructureFailure(attempt) {
			break
		}
	}
	hardCandidate := search.HardCapacity()
	interactiveCandidate := 0
	if run.manifest.Search.SearchDashboardCapacity && hardCandidate > 0 {
		interactiveRates := ratesAtOrBelow(cell.RatesPerSecond, hardCandidate)
		interactiveSearch, searchErr := NewCapacitySearch(interactiveRates)
		if searchErr != nil {
			return result, false, searchErr
		}
		for {
			rate, ok := interactiveSearch.Next()
			if !ok {
				break
			}
			attempt := runSearchAttempt(rate)
			interactiveSearch.Record(rate, attempt.Report.Evaluation.InteractivePass)
			if IsCapacityAttemptInfrastructureFailure(attempt) {
				break
			}
		}
		interactiveCandidate = interactiveSearch.HardCapacity()
	}
	verifiedHard := hardCandidate
	if !run.manifest.Search.SkipBoundary {
		verifiedHard = 0
		for _, candidate := range SelectBoundaryCandidates(result.Attempts, hardCandidate, 2) {
			passed := true
			for repetition := 1; repetition <= run.manifest.Search.BoundaryRepetitions; repetition++ {
				attempt := runAttempt("boundary", repetition, candidate, time.Duration(run.manifest.Search.BoundarySeconds)*time.Second)
				result.Attempts = append(result.Attempts, attempt)
				if !attempt.Report.Evaluation.HardPass {
					passed = false
					break
				}
			}
			if passed {
				verifiedHard = candidate
				break
			}
		}
	}
	interactive := min(interactiveCandidate, verifiedHard)
	if !run.manifest.Search.SearchDashboardCapacity {
		for _, attempt := range result.Attempts {
			if attempt.RatePerSecond <= verifiedHard && attempt.Report.Evaluation.InteractivePass && attempt.RatePerSecond > interactive {
				interactive = attempt.RatePerSecond
			}
		}
	}
	recommended := int(math.Floor(float64(interactive) * run.manifest.Search.RecommendedCapacityRate))
	if interactive > 0 && recommended < 1 {
		recommended = 1
	}
	result.Capacity = CapacityResult{HardEventsPerSecond: verifiedHard, InteractiveEventsPerSecond: interactive, RecommendedEventsPerSecond: recommended}
	for _, attempt := range result.Attempts {
		if IsCapacityAttemptInfrastructureFailure(attempt) {
			return result, false, fmt.Errorf("benchmark probe failed during %s at %d events/s: %s", attempt.Phase, attempt.RatePerSecond, attempt.Error)
		}
	}
	if stat, statErr := os.Stat(activeDB); statErr == nil {
		result.DatabaseBytesAfter = stat.Size()
	}
	if stat, statErr := os.Stat(activeDB + "-wal"); statErr == nil {
		result.WALBytesAfter = stat.Size()
	}
	result.Status = "completed"
	return result, false, nil
}

func FixedRateSoakPassed(mode string, evaluation ProbeEvaluation) (bool, error) {
	switch strings.TrimSpace(mode) {
	case "hard":
		return evaluation.HardPass, nil
	case "interactive":
		return evaluation.InteractivePass, nil
	default:
		return false, fmt.Errorf("fixed pass mode must be hard or interactive")
	}
}

// IsCapacityAttemptInfrastructureFailure separates an expected capacity OOM
// from a broken probe. The OOM rate remains in the result as the upper failure
// boundary; panic, sampler/runtime errors, and other probe errors still fail
// the cell.
func IsCapacityAttemptInfrastructureFailure(attempt ProbeAttempt) bool {
	if attempt.Report.Metrics.OOM {
		return false
	}
	return attempt.Report.Metrics.Panic || attempt.Error != ""
}

// PruneCompletedWorkDatabase 只裁剪成功 cell 的可重建 SQLite clone；日志、结果和其它诊断文件保持不动。
func PruneCompletedWorkDatabase(workDir string) error {
	cleaned := filepath.Clean(strings.TrimSpace(workDir))
	if cleaned == "." || cleaned == string(os.PathSeparator) || cleaned == "" {
		return fmt.Errorf("completed work directory is unsafe: %q", workDir)
	}
	databasePath := filepath.Join(cleaned, "app.db")
	var result error
	for _, path := range []string{databasePath + "-shm", databasePath + "-wal", databasePath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, fmt.Errorf("remove completed benchmark clone %s: %w", filepath.Clean(path), err))
		}
	}
	return result
}

func (run *runContext) runIsolatedProbeAttempt(ctx context.Context, cell Cell, datasetPath, databasePath, envPath, attemptDir, allowedCPUs string, sequence int, phase string, repetition, rate int, duration time.Duration) (attempt ProbeAttempt) {
	attempt = ProbeAttempt{Phase: phase, Repetition: repetition, RatePerSecond: rate, DurationSeconds: int(duration.Seconds()), StartedAt: time.Now()}
	thresholds := ProbeThresholds{MinDurableRatio: 0.99, MaxBacklogGrowth: 0,
		InteractiveP95MS: float64(run.manifest.Search.DashboardCoreP95MS), InteractiveP99MS: float64(run.manifest.Search.DashboardOverallP99MS)}
	if err := ResetDatasetClone(ctx, datasetPath, databasePath); err != nil {
		markProbeAttemptError(&attempt, thresholds, err)
		attempt.FinishedAt = time.Now()
		return attempt
	}
	unit := unitName(run.options.RunID, cell.ID, filepath.Base(attemptDir), fmt.Sprintf("probe-%03d", sequence))
	attempt.UnitName = unit
	runtimeUnit, err := StartKeeperUnit(ctx, KeeperStartOptions{
		UnitName: unit, BinaryPath: run.options.KeeperBinary, Environment: envPath, WorkingDir: attemptDir,
		StdoutPath: filepath.Join(attemptDir, "stdout.log"), StderrPath: filepath.Join(attemptDir, "stderr.log"),
		CPU: int(cell.Resource.CPU), MemoryMiB: cell.Resource.MemoryMiB, AllowedCPUs: allowedCPUs,
	})
	if err != nil {
		markProbeAttemptError(&attempt, thresholds, err)
		attempt.FinishedAt = time.Now()
		return attempt
	}
	attempt.EffectiveLimits = runtimeUnit.Limits
	var sampler *CgroupSampler
	defer func() {
		if attempt.FinalUnitState.ActiveState == "" {
			attempt.FinalUnitState, _ = readUnitStateWithTimeout(runtimeUnit.UnitName, 3*time.Second)
		}
		if sampler != nil {
			last, peak, sampleErr := sampler.Stop()
			attempt.LastResource = last
			attempt.PeakResource = peak
			if sampleErr != nil {
				markProbeAttemptError(&attempt, thresholds, sampleErr)
			}
		}
		if stopErr := stopUnitWithTimeout(runtimeUnit.UnitName, 20*time.Second); stopErr != nil {
			markProbeAttemptError(&attempt, thresholds, stopErr)
		}
		attempt.FinishedAt = time.Now()
	}()

	sampler, err = StartCgroupSampler(ctx, runtimeUnit.CgroupPath, filepath.Join(attemptDir, fmt.Sprintf("cgroup-samples-%03d.jsonl", sequence)), 200*time.Millisecond)
	if err != nil {
		markProbeAttemptError(&attempt, thresholds, err)
		return attempt
	}
	startupDuration, err := WaitHTTPHealth(ctx, fmt.Sprintf("http://127.0.0.1:%d", run.options.AppPort), unit, 90*time.Second)
	attempt.StartupSeconds = startupDuration.Seconds()
	if err != nil {
		last, _ := ReadCgroupSample(runtimeUnit.CgroupPath)
		state, _ := readUnitStateWithTimeout(unit, 3*time.Second)
		attempt.FinalUnitState = state
		if last.MemoryOOMKill > 0 || state.Result == "oom-kill" {
			attempt.Report.Metrics.OOM = true
		} else if state.ActiveState != "active" {
			attempt.Report.Metrics.Panic = true
		}
		markProbeAttemptError(&attempt, thresholds, err)
		return attempt
	}
	warmupDuration, err := WarmDashboard(ctx, fmt.Sprintf("http://127.0.0.1:%d", run.options.AppPort))
	attempt.WarmupSeconds = warmupDuration.Seconds()
	if err != nil {
		last, _ := ReadCgroupSample(runtimeUnit.CgroupPath)
		state, _ := readUnitStateWithTimeout(unit, 3*time.Second)
		attempt.FinalUnitState = state
		if last.MemoryOOMKill > 0 || state.Result == "oom-kill" {
			attempt.Report.Metrics.OOM = true
		} else if state.ActiveState != "active" {
			attempt.Report.Metrics.Panic = true
		}
		markProbeAttemptError(&attempt, thresholds, err)
		return attempt
	}
	select {
	case <-ctx.Done():
		markProbeAttemptError(&attempt, thresholds, ctx.Err())
		return attempt
	case <-time.After(time.Second):
	}

	before, _ := ReadCgroupSample(runtimeUnit.CgroupPath)
	measuredAt := time.Now()
	apiProfiles, err := BuildAPIKeyProfiles(cell.Cardinality.APIKeys, run.manifest.TrafficTiers, run.manifest.Dataset.Seed)
	if err == nil {
		attempt.Report, err = RunProbe(ctx, ProbeOptions{
			RedisAddress: netAddress(run.options.RedisPort), RedisPassword: run.redisPass, RedisChannel: "usage",
			ApplicationURL: fmt.Sprintf("http://127.0.0.1:%d", run.options.AppPort), DatabasePath: databasePath,
			RatePerSecond: rate, Duration: duration, DrainTimeout: 30 * time.Second, HTTPRatePerSecond: run.manifest.Search.DashboardRequestsPerSecond,
			Cardinality: cell.Cardinality, APIKeyProfiles: apiProfiles, Seed: run.manifest.Dataset.Seed,
			Thresholds: thresholds,
		})
	}
	after, _ := ReadCgroupSample(runtimeUnit.CgroupPath)
	attempt.Resource = resourceDelta(before, after, time.Since(measuredAt), int(cell.Resource.CPU))
	attempt.LastResource = after
	if after.MemoryOOMKill > before.MemoryOOMKill {
		attempt.Report.Metrics.OOM = true
		attempt.Report.Evaluation = EvaluateProbe(attempt.Report.Metrics, thresholds)
	}
	if state, stateErr := readUnitStateWithTimeout(runtimeUnit.UnitName, 3*time.Second); stateErr == nil {
		attempt.FinalUnitState = state
		if state.ActiveState == "active" {
			// 正常 probe 在 defer 停止 unit；这里仅记录运行末端状态。
		} else if state.Result == "oom-kill" || after.MemoryOOMKill > before.MemoryOOMKill {
			attempt.Report.Metrics.OOM = true
		} else {
			attempt.Report.Metrics.Panic = true
		}
		attempt.Report.Evaluation = EvaluateProbe(attempt.Report.Metrics, thresholds)
	}
	if err != nil {
		markProbeAttemptError(&attempt, thresholds, err)
	}
	return attempt
}

func markProbeAttemptError(attempt *ProbeAttempt, thresholds ProbeThresholds, err error) {
	if attempt == nil || err == nil {
		return
	}
	if attempt.Error == "" {
		attempt.Error = err.Error()
	} else {
		attempt.Error += "; " + err.Error()
	}
	attempt.Report.Metrics.Errors++
	attempt.Report.Evaluation = EvaluateProbe(attempt.Report.Metrics, thresholds)
	if !slices.Contains(attempt.Report.Evaluation.Reasons, "probe_error") {
		attempt.Report.Evaluation.Reasons = append(attempt.Report.Evaluation.Reasons, "probe_error")
	}
}

func mergeProbeAttemptRuntime(result *CellResult, attempt ProbeAttempt) {
	if result == nil {
		return
	}
	if result.UnitName == "" {
		result.UnitName = attempt.UnitName
		result.EffectiveLimits = attempt.EffectiveLimits
		result.StartupSeconds = attempt.StartupSeconds
		result.WarmupSeconds = attempt.WarmupSeconds
	}
	result.LastResource = attempt.LastResource
	result.FinalUnitState = attempt.FinalUnitState
	if attempt.PeakResource.MemoryPeakBytes > result.PeakResource.MemoryPeakBytes {
		result.PeakResource = attempt.PeakResource
	}
}

func validateCellDataset(cell Cell, dataset DatasetResult, path string) error {
	if dataset.GeneratorVersion != DatasetGeneratorVersion {
		return fmt.Errorf("cell %s dataset generator=%q, want %q", cell.ID, dataset.GeneratorVersion, DatasetGeneratorVersion)
	}
	if dataset.HotEvents != cell.HotEvents || dataset.ArchiveEvents != cell.ArchiveEvents {
		return fmt.Errorf("cell %s dataset rows hot=%d archive=%d, want %d/%d", cell.ID, dataset.HotEvents, dataset.ArchiveEvents, cell.HotEvents, cell.ArchiveEvents)
	}
	if dataset.Identities != int64(cell.Cardinality.Identities) || dataset.Models != int64(cell.Cardinality.Models) || dataset.APIKeys != int64(cell.Cardinality.APIKeys) {
		return fmt.Errorf("cell %s dataset cardinality does not match plan", cell.ID)
	}
	if dataset.UsedIdentities != dataset.Identities || dataset.UsedModels != dataset.Models || dataset.UsedAPIKeys != dataset.APIKeys {
		return fmt.Errorf("cell %s dataset does not exercise every identity/model/API key", cell.ID)
	}
	if dataset.QuickCheck != "ok" || dataset.OrphanIdentities != 0 || dataset.OrphanModels != 0 || dataset.OrphanAPIKeys != 0 || dataset.TokenSemanticViolations != 0 || dataset.SemanticFingerprint == "" {
		return fmt.Errorf("cell %s dataset validation is not reusable", cell.ID)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("cell %s dataset file: %w", cell.ID, err)
	}
	return nil
}

func resolveDatasetPath(datasetDir string) (string, error) {
	for _, name := range []string{"app.db", "app.db.zst"} {
		path := filepath.Join(datasetDir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("neither app.db nor app.db.zst exists in %s", filepath.Clean(datasetDir))
}

func selectCells(cells []Cell, ids []string) ([]Cell, error) {
	if len(ids) == 0 {
		return append([]Cell(nil), cells...), nil
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var selected []Cell
	for _, cell := range cells {
		if wanted[cell.ID] {
			selected = append(selected, cell)
			delete(wanted, cell.ID)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("unknown benchmark cells: %s", strings.Join(missing, ", "))
	}
	return selected, nil
}

func descendingPassingRates(attempts []ProbeAttempt, upper int) []int {
	seen := map[int]bool{}
	var rates []int
	for _, attempt := range attempts {
		if attempt.Phase == "search" && attempt.RatePerSecond <= upper && attempt.Report.Evaluation.HardPass && !seen[attempt.RatePerSecond] {
			seen[attempt.RatePerSecond] = true
			rates = append(rates, attempt.RatePerSecond)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(rates)))
	return rates
}

// SelectBoundaryCandidates limits ordinary matrix validation to the highest
// short-probe capacity and one conservative fallback at roughly half that
// rate. Final deployment recommendations are still verified by dedicated
// soaks, so testing every intermediate passing rate only adds runtime.
func SelectBoundaryCandidates(attempts []ProbeAttempt, upper, maximum int) []int {
	rates := descendingPassingRates(attempts, upper)
	if maximum <= 0 || len(rates) <= maximum {
		return rates
	}
	selected := []int{rates[0]}
	for len(selected) < maximum {
		target := selected[len(selected)-1] / 2
		candidate := 0
		for _, rate := range rates {
			if rate < selected[len(selected)-1] && rate <= target {
				candidate = rate
				break
			}
		}
		if candidate == 0 {
			candidate = rates[len(rates)-1]
		}
		if slices.Contains(selected, candidate) {
			break
		}
		selected = append(selected, candidate)
	}
	return selected
}

func resourceDelta(before, after CgroupSample, measured time.Duration, cpu int) AttemptResource {
	usageDelta := after.CPUUsageUsec - before.CPUUsageUsec
	utilization := 0.0
	if measured > 0 && cpu > 0 {
		utilization = float64(usageDelta) / (measured.Seconds() * 1_000_000 * float64(cpu)) * 100
	}
	return AttemptResource{
		MeasuredSeconds:       measured.Seconds(),
		CPUUsageUsecDelta:     usageDelta,
		CPUUtilizationPercent: utilization,
		CPUThrottledUsecDelta: after.CPUThrottledUsec - before.CPUThrottledUsec,
		IOReadBytesDelta:      after.IOReadBytes - before.IOReadBytes,
		IOWriteBytesDelta:     after.IOWriteBytes - before.IOWriteBytes,
		MemoryPeakBytes:       after.MemoryPeakBytes,
		MemoryOOMDelta:        after.MemoryOOM - before.MemoryOOM,
		MemoryOOMKillDelta:    after.MemoryOOMKill - before.MemoryOOMKill,
	}
}

func ratesAtOrBelow(rates []int, upper int) []int {
	selected := make([]int, 0, len(rates))
	for _, rate := range rates {
		if rate > upper {
			break
		}
		selected = append(selected, rate)
	}
	return selected
}

func ResumeCellMatches(path, manifestDigest, keeperDigest, benchctlDigest, datasetFingerprint string) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var result CellResult
	if err := json.Unmarshal(data, &result); err != nil {
		return false, fmt.Errorf("decode existing cell result: %w", err)
	}
	return result.Status == "completed" && result.ManifestSHA256 == manifestDigest && result.KeeperBinarySHA256 == keeperDigest && result.BenchctlBinarySHA256 == benchctlDigest && result.DatasetFingerprint == datasetFingerprint, nil
}

func nextAttemptDirectory(cellDir string) (string, error) {
	if err := os.MkdirAll(cellDir, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(cellDir)
	if err != nil {
		return "", err
	}
	maximum := 0
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "attempt-") {
			continue
		}
		value, _ := strconv.Atoi(strings.TrimPrefix(entry.Name(), "attempt-"))
		maximum = max(maximum, value)
	}
	path := filepath.Join(cellDir, fmt.Sprintf("attempt-%03d", maximum+1))
	return path, os.MkdirAll(path, 0o755)
}

func writeKeeperEnvironment(path, workDir string, appPort, redisPort int, password string) error {
	content := strings.Join([]string{
		"APP_HOST=127.0.0.1",
		"APP_PORT=" + strconv.Itoa(appPort),
		"WORK_DIR=" + workDir,
		"CPA_BASE_URL=http://127.0.0.1:1",
		"CPA_MANAGEMENT_KEY=" + password,
		"REDIS_QUEUE_ADDR=127.0.0.1:" + strconv.Itoa(redisPort),
		"REDIS_QUEUE_TLS=false",
		"REDIS_QUEUE_BATCH_SIZE=10000",
		"REDIS_QUEUE_IDLE_INTERVAL=100ms",
		"REQUEST_TIMEOUT=2s",
		"AUTH_ENABLED=false",
		"BACKUP_ENABLED=false",
		"LOG_FILE_ENABLED=false",
		"LOG_LEVEL=warn",
		"TZ=Asia/Shanghai",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write Keeper benchmark environment: %w", err)
	}
	return nil
}

func cpuSet(start, end int) string {
	if start >= end {
		return ""
	}
	if end-start == 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end-1)
}

func OnlineCPUCount() (int, error) {
	data, err := os.ReadFile("/sys/devices/system/cpu/online")
	if err != nil {
		return 0, fmt.Errorf("read online CPUs: %w", err)
	}
	value := strings.TrimSpace(string(data))
	parts := strings.Split(value, "-")
	if len(parts) == 1 {
		last, err := strconv.Atoi(parts[0])
		return last + 1, err
	}
	if len(parts) == 2 && parts[0] == "0" {
		last, err := strconv.Atoi(parts[1])
		if err == nil {
			return last + 1, nil
		}
	}
	return 0, fmt.Errorf("benchmark requires contiguous online CPUs, got %q", value)
}

func SetProcessAffinity(cpus string) error {
	if cpus == "" {
		return fmt.Errorf("process affinity CPU set is empty")
	}
	output, err := exec.Command("taskset", "-apc", cpus, strconv.Itoa(os.Getpid())).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set benchmark driver affinity to %s: %w: %s", cpus, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func unitName(parts ...string) string {
	joined := strings.Join(parts, "-")
	var builder strings.Builder
	for _, character := range strings.ToLower(joined) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	name := strings.Trim(builder.String(), "-")
	if len(name) > 180 {
		digest := sha256.Sum256([]byte(name))
		name = name[:160] + "-" + hex.EncodeToString(digest[:6])
	}
	return "cpa-keeper-bench-" + name
}

func netAddress(port int) string {
	return "127.0.0.1:" + strconv.Itoa(port)
}

func stopUnitWithTimeout(unitName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return StopUnit(ctx, unitName)
}

func readUnitStateWithTimeout(unitName string, timeout time.Duration) (UnitState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return ReadUnitState(ctx, unitName)
}

func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := file.WriteTo(hash); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func WriteJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".benchmark-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func LoadDatasetResult(path string) (DatasetResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DatasetResult{}, err
	}
	var result DatasetResult
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return DatasetResult{}, err
	}
	return result, nil
}
