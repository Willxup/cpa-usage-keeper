package test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPricingRestoreCLIFlagsAndFailureExitCodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := "pricing-restore"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build restore CLI: %v\n%s", err, output)
	}
	outputPath := filepath.Join(t.TempDir(), "must-not-exist.db")
	for _, tc := range []struct {
		name    string
		args    []string
		exit    int
		message string
	}{
		{"help", []string{"--help"}, 0, "incomplete legacy migration"},
		{"missing_paths", nil, 2, "usage: pricing-restore"},
		{"unexpected_arg", []string{"-backup", "backup.db", "-fault", "fault.db", "-out", outputPath, "extra"}, 2, "usage: pricing-restore"},
		{"missing_backup", []string{"-backup", filepath.Join(t.TempDir(), "missing.db"), "-fault", "fault.db", "-out", outputPath}, 1, "restore failed:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := exec.CommandContext(ctx, binary, tc.args...)
			output, err := command.CombinedOutput()
			exitCode := 0
			if err != nil {
				status, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run CLI: %v", err)
				}
				exitCode = status.ExitCode()
			}
			if exitCode != tc.exit || !strings.Contains(string(output), tc.message) {
				t.Fatalf("exit=%d want=%d error=%v output=%s", exitCode, tc.exit, err, output)
			}
			if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
				t.Fatalf("invalid CLI invocation created output: %v", err)
			}
		})
	}
}
