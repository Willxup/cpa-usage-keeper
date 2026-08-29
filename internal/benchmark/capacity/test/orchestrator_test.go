package capacity_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/benchmark/capacity"
	_ "github.com/mattn/go-sqlite3"
)

func TestResumeCellMatchesTerminalExactProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	provenance := capacity.ExecutionProvenance{
		ManifestSHA256: "manifest", PlanSHA256: "plan", KeeperBinarySHA256: "keeper", BenchctlBinarySHA256: "benchctl",
		DatasetFingerprint: "dataset", DatasetValidationSHA256: "proof", FixedRate: 100, FixedDurationSeconds: 300, FixedPass: "hard",
	}
	result := capacity.CellResult{
		Status: "completed", ManifestSHA256: "manifest", PlanSHA256: "plan", KeeperBinarySHA256: "keeper", BenchctlBinarySHA256: "benchctl", DatasetFingerprint: "dataset",
		DatasetValidationSHA256: "proof", FixedRate: 100, FixedDurationSeconds: 300, FixedPass: "hard",
	}
	if err := capacity.WriteJSONAtomic(path, result); err != nil {
		t.Fatalf("WriteJSONAtomic returned error: %v", err)
	}
	matched, err := capacity.ResumeCellMatches(path, provenance)
	if err != nil || !matched {
		t.Fatalf("expected exact result to resume: matched=%v err=%v", matched, err)
	}
	result.Status = "failed"
	result.Error = "hard soak failed at 100 events/s: durable_throughput,http_p99"
	result.Attempts = []capacity.ProbeAttempt{{RatePerSecond: 100}}
	if err := capacity.WriteJSONAtomic(path, result); err != nil {
		t.Fatalf("WriteJSONAtomic failed result returned error: %v", err)
	}
	matched, err = capacity.ResumeCellMatches(path, provenance)
	if err != nil || !matched {
		t.Fatalf("expected terminal fixed-rate failure to resume: matched=%v err=%v", matched, err)
	}
	result.Error = "start keeper: unit failed"
	if err := capacity.WriteJSONAtomic(path, result); err != nil {
		t.Fatalf("WriteJSONAtomic infrastructure failure returned error: %v", err)
	}
	matched, err = capacity.ResumeCellMatches(path, provenance)
	if err != nil || matched {
		t.Fatalf("infrastructure failure must rerun: matched=%v err=%v", matched, err)
	}
	result.Status = "completed"
	result.Error = ""
	result.Attempts = nil
	if err := capacity.WriteJSONAtomic(path, result); err != nil {
		t.Fatalf("restore completed result returned error: %v", err)
	}
	provenance.KeeperBinarySHA256 = "different"
	matched, err = capacity.ResumeCellMatches(path, provenance)
	if err != nil || matched {
		t.Fatalf("changed binary must rerun: matched=%v err=%v", matched, err)
	}
}

func TestExecuteRunRejectsUnsafeRootAndRunIDBeforeRuntimeSetup(t *testing.T) {
	for _, options := range []capacity.RunOptions{
		{Root: "/", RunID: "safe-run", KeeperBinary: "keeper"},
		{Root: t.TempDir(), RunID: "../escape", KeeperBinary: "keeper"},
	} {
		if _, err := capacity.ExecuteRun(t.Context(), options); err == nil {
			t.Fatalf("ExecuteRun should reject unsafe options: %+v", options)
		}
	}
}

func TestRedisCPUSetUsesLastOnlineCPU(t *testing.T) {
	for online, want := range map[int]string{0: "", 1: "0", 4: "3", 8: "7"} {
		if got := capacity.RedisCPUSet(online); got != want {
			t.Fatalf("RedisCPUSet(%d)=%q, want %q", online, got, want)
		}
	}
}

func TestPreflightDatasetDependenciesRequiresZstdOnlyForCompressedCanonical(t *testing.T) {
	root := t.TempDir()
	datasetDir := filepath.Join(root, "datasets", "custom-dataset")
	if err := os.MkdirAll(datasetDir, 0o755); err != nil {
		t.Fatalf("create dataset directory: %v", err)
	}
	compressed := filepath.Join(datasetDir, "app.db.zst")
	if err := os.WriteFile(compressed, []byte("compressed"), 0o600); err != nil {
		t.Fatalf("write compressed canonical: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	cells := []capacity.Cell{{DatasetID: "custom-dataset"}}
	if err := capacity.PreflightDatasetDependencies(root, cells); err == nil || !strings.Contains(err.Error(), "zstd") {
		t.Fatalf("compressed canonical without zstd error=%v", err)
	}
	if err := os.WriteFile(filepath.Join(datasetDir, "app.db"), []byte("raw"), 0o600); err != nil {
		t.Fatalf("write raw canonical: %v", err)
	}
	if err := capacity.PreflightDatasetDependencies(root, cells); err != nil {
		t.Fatalf("raw canonical should not require zstd: %v", err)
	}
}

func TestReadCgroupSampleAcceptsEmptyIOStat(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"cpu.stat":            "usage_usec 10\nuser_usec 6\nsystem_usec 4\nthrottled_usec 0\nnr_throttled 0\n",
		"memory.current":      "1024\n",
		"memory.peak":         "2048\n",
		"memory.swap.current": "0\n",
		"memory.events":       "oom 0\noom_kill 0\n",
		"pids.current":        "3\n",
		"io.stat":             "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	sample, err := capacity.ReadCgroupSample(root)
	if err != nil {
		t.Fatalf("ReadCgroupSample returned error: %v", err)
	}
	if sample.IOReadBytes != 0 || sample.IOWriteBytes != 0 || sample.MemoryPeakBytes != 2048 {
		t.Fatalf("unexpected sample: %+v", sample)
	}
}

func TestReadAndValidateCgroupLimits(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"cpu.max":               "200000 100000\n",
		"cpuset.cpus.effective": "0-1\n",
		"memory.max":            "536870912\n",
		"memory.swap.max":       "0\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	limits, err := capacity.ReadCgroupLimits(root)
	if err != nil {
		t.Fatalf("ReadCgroupLimits returned error: %v", err)
	}
	if err := capacity.ValidateCgroupLimits(limits, 2, 512, "0-1"); err != nil {
		t.Fatalf("ValidateCgroupLimits returned error: %v", err)
	}
	if err := capacity.ValidateCgroupLimits(limits, 1, 512, "0-1"); err == nil {
		t.Fatal("expected CPU mismatch to fail")
	}
}

func TestReadAndValidateUnlimitedMemoryCgroupLimits(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"cpu.max":               "100000 100000\n",
		"cpuset.cpus.effective": "0\n",
		"memory.max":            "max\n",
		"memory.swap.max":       "0\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	limits, err := capacity.ReadCgroupLimits(root)
	if err != nil {
		t.Fatalf("ReadCgroupLimits returned error: %v", err)
	}
	if !limits.MemoryMaxUnlimited || limits.MemoryMaxBytes != 0 {
		t.Fatalf("unlimited memory was not preserved: %+v", limits)
	}
	if err := capacity.ValidateCgroupLimits(limits, 1, 0, "0"); err != nil {
		t.Fatalf("ValidateCgroupLimits returned error: %v", err)
	}
	if err := capacity.ValidateCgroupLimits(limits, 1, 512, "0"); err == nil {
		t.Fatal("finite memory request must reject an unlimited cgroup")
	}
}

func TestResumeCellMissingResultNeedsRun(t *testing.T) {
	matched, err := capacity.ResumeCellMatches(filepath.Join(t.TempDir(), "missing.json"), capacity.ExecutionProvenance{})
	if err != nil || matched {
		t.Fatalf("missing result should not resume: matched=%v err=%v", matched, err)
	}
}

func TestDropFilePageCacheAcceptsSyncedClone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	if err := os.WriteFile(path, []byte("sqlite pages"), 0o600); err != nil {
		t.Fatalf("write clone: %v", err)
	}
	if err := capacity.DropFilePageCache(path); err != nil {
		t.Fatalf("DropFilePageCache returned error: %v", err)
	}
}

func TestPruneCompletedWorkDatabaseRemovesOnlySQLiteClone(t *testing.T) {
	workDir := t.TempDir()
	for _, name := range []string{"app.db", "app.db-wal", "app.db-shm", "keeper.env"} {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := capacity.PruneCompletedWorkDatabase(workDir); err != nil {
		t.Fatalf("PruneCompletedWorkDatabase returned error: %v", err)
	}
	for _, name := range []string{"app.db", "app.db-wal", "app.db-shm"} {
		if _, err := os.Stat(filepath.Join(workDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, "keeper.env")); err != nil {
		t.Fatalf("non-database artifact must remain: %v", err)
	}
}

func TestResetDatasetCloneReplacesPreviousProbeState(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	destination := filepath.Join(root, "work", "app.db")
	sourceDB, err := sql.Open("sqlite3", source)
	if err != nil {
		t.Fatalf("open source database: %v", err)
	}
	if _, err := sourceDB.Exec("CREATE TABLE marker(value TEXT); INSERT INTO marker VALUES ('canonical');"); err != nil {
		sourceDB.Close()
		t.Fatalf("create source database: %v", err)
	}
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source database: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create work directory: %v", err)
	}
	for _, path := range []string{destination, destination + "-wal", destination + "-shm"} {
		if err := os.WriteFile(path, []byte("previous probe state"), 0o600); err != nil {
			t.Fatalf("write previous probe artifact %s: %v", path, err)
		}
	}
	if err := capacity.ResetDatasetClone(t.Context(), source, destination); err != nil {
		t.Fatalf("ResetDatasetClone returned error: %v", err)
	}
	destinationDB, err := sql.Open("sqlite3", destination+"?mode=ro")
	if err != nil {
		t.Fatalf("open restored clone: %v", err)
	}
	defer destinationDB.Close()
	var value string
	if err := destinationDB.QueryRow("SELECT value FROM marker").Scan(&value); err != nil {
		t.Fatalf("query restored clone: %v", err)
	}
	if value != "canonical" {
		t.Fatalf("unexpected restored content: %q", value)
	}
	for _, path := range []string{destination + "-wal", destination + "-shm"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale sidecar must be removed: %s err=%v", path, err)
		}
	}
}

func TestResetDatasetCloneCopiesStaticCanonicalWithoutSQLiteCLI(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	destination := filepath.Join(root, "work", "app.db")
	want := []byte("static canonical database bytes")
	if err := os.WriteFile(source, want, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	t.Setenv("PATH", t.TempDir())

	if err := capacity.ResetDatasetClone(t.Context(), source, destination); err != nil {
		t.Fatalf("ResetDatasetClone returned error: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("destination bytes=%q, want %q", got, want)
	}
}

func TestFixedRateSoakPassedSupportsHardOnlyMode(t *testing.T) {
	evaluation := capacity.ProbeEvaluation{HardPass: true, InteractivePass: false}
	passed, err := capacity.FixedRateSoakPassed("hard", evaluation)
	if err != nil || !passed {
		t.Fatalf("hard-only soak should pass: passed=%v err=%v", passed, err)
	}
	passed, err = capacity.FixedRateSoakPassed("interactive", evaluation)
	if err != nil || passed {
		t.Fatalf("interactive soak should fail: passed=%v err=%v", passed, err)
	}
	if _, err := capacity.FixedRateSoakPassed("unknown", evaluation); err == nil {
		t.Fatal("unknown fixed pass mode must fail")
	}
}
