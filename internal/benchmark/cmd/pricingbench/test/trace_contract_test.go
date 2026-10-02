//go:build sqlite_trace

package test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type writerTraceReport struct {
	Enabled                bool  `json:"enabled"`
	WriteTransactionCount  int64 `json:"write_transaction_count"`
	IncompleteCount        int64 `json:"incomplete_count"`
	MaxWriteUpperNS        int64 `json:"max_write_upper_ns"`
	MaxWriteLowerNS        int64 `json:"max_write_lower_ns"`
	TotalWriteUpperNS      int64 `json:"total_write_upper_ns"`
	TotalWriteLowerNS      int64 `json:"total_write_lower_ns"`
	AutocommitWriteCount   int64 `json:"autocommit_write_count"`
	TotalAutocommitWriteNS int64 `json:"total_autocommit_write_ns"`
}

// TestSQLiteWriterTraceMeasuresRealUpgradeAndRecalculation 验证 benchmark 标签在真实旧库升级与重算中读取原 writer 事务事件。
func TestSQLiteWriterTraceMeasuresRealUpgradeAndRecalculation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	projectRoot := filepath.Join("..", "..", "..", "..", "..")
	outputRoot := filepath.Join(t.TempDir(), "pricingbench")
	command := exec.CommandContext(ctx, "go", "run", "-tags", "sqlite_trace", "./internal/benchmark/cmd/pricingbench",
		"--root", outputRoot, "--events", "120", "--scenario", "latest", "--anchor", "2026-09-23T00:00:00Z")
	command.Dir = projectRoot
	command.Env = append(os.Environ(), "TZ=UTC", "GIN_MODE=release")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pricingbench sqlite_trace failed: %v\n%s", err, output)
	}
	if ctx.Err() != nil {
		t.Fatalf("pricingbench sqlite_trace timed out: %v", ctx.Err())
	}
	encoded, err := os.ReadFile(filepath.Join(outputRoot, "pricing-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Error              string            `json:"error"`
		WriterTrace        writerTraceReport `json:"writer_trace"`
		RecalculationTrace writerTraceReport `json:"recalculation_trace"`
	}
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatalf("decode benchmark report: %v", err)
	}
	if strings.TrimSpace(report.Error) != "" {
		t.Fatalf("pricing benchmark reported failure: %s", report.Error)
	}
	for name, trace := range map[string]writerTraceReport{
		"upgrade": report.WriterTrace, "recalculation": report.RecalculationTrace,
	} {
		if !trace.Enabled || trace.WriteTransactionCount == 0 || trace.IncompleteCount != 0 {
			t.Errorf("%s real writer transactions not observed completely: %+v", name, trace)
		}
		if trace.MaxWriteUpperNS <= 0 || trace.TotalWriteUpperNS <= 0 ||
			trace.MaxWriteLowerNS <= 0 || trace.TotalWriteLowerNS <= 0 ||
			trace.MaxWriteLowerNS > trace.MaxWriteUpperNS || trace.TotalWriteLowerNS > trace.TotalWriteUpperNS {
			t.Errorf("%s invalid write transaction bounds: %+v", name, trace)
		}
	}
	if report.WriterTrace.AutocommitWriteCount == 0 || report.WriterTrace.TotalAutocommitWriteNS <= 0 {
		t.Errorf("upgrade autocommit writes were not traced: %+v", report.WriterTrace)
	}
}
