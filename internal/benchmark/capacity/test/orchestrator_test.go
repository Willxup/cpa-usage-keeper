package capacity_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"cpa-usage-keeper/internal/benchmark/capacity"
)

func TestResumeCellMatchesOnlyCompleteExactProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	result := capacity.CellResult{
		Status: "completed", ManifestSHA256: "manifest", KeeperBinarySHA256: "keeper", BenchctlBinarySHA256: "benchctl", DatasetFingerprint: "dataset",
	}
	if err := capacity.WriteJSONAtomic(path, result); err != nil {
		t.Fatalf("WriteJSONAtomic returned error: %v", err)
	}
	matched, err := capacity.ResumeCellMatches(path, "manifest", "keeper", "benchctl", "dataset")
	if err != nil || !matched {
		t.Fatalf("expected exact result to resume: matched=%v err=%v", matched, err)
	}
	matched, err = capacity.ResumeCellMatches(path, "manifest", "different", "benchctl", "dataset")
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
	matched, err := capacity.ResumeCellMatches(filepath.Join(t.TempDir(), "missing.json"), "m", "k", "b", "d")
	if err != nil || matched {
		t.Fatalf("missing result should not resume: matched=%v err=%v", matched, err)
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
	if output, err := exec.Command("sqlite3", source, "CREATE TABLE marker(value TEXT); INSERT INTO marker VALUES ('canonical');").CombinedOutput(); err != nil {
		t.Fatalf("create source database: %v: %s", err, output)
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
	output, err := exec.Command("sqlite3", destination, "SELECT value FROM marker;").CombinedOutput()
	if err != nil {
		t.Fatalf("query restored clone: %v: %s", err, output)
	}
	if string(output) != "canonical\n" {
		t.Fatalf("unexpected restored content: %q", output)
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
