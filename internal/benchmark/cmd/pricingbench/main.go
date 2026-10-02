// pricingbench 在独立合成库上运行首次价格升级和历史费用重算的容量实验。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	flags := flag.NewFlagSet("pricingbench", flag.ExitOnError)
	root := flags.String("root", "", "new benchmark output directory")
	events := flags.Int64("events", 0, "synthetic event count")
	scenario := flags.String("scenario", pricingBenchLatest, "latest or five-dim")
	seed := flags.Uint64("seed", 20260923, "deterministic dataset seed")
	anchorText := flags.String("anchor", "", "UTC hourly dataset anchor (RFC3339; default current UTC hour)")
	receiveInterval := flags.Duration("receive-interval", 20*time.Millisecond, "synthetic durable inbox arrival interval")
	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if flags.NArg() != 0 || strings.TrimSpace(*root) == "" || *events == 0 {
		fmt.Fprintln(os.Stderr, "usage: pricingbench --root DIR --events COUNT [--scenario latest|five-dim] [--anchor UTC-RFC3339]")
		os.Exit(2)
	}
	anchor := time.Now().UTC().Truncate(time.Hour)
	if *anchorText != "" {
		parsed, err := time.Parse(time.RFC3339, *anchorText)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid anchor: %v\n", err)
			os.Exit(2)
		}
		anchor = parsed.UTC()
	}
	if time.Local != time.UTC && time.Local.String() != "UTC" {
		fmt.Fprintln(os.Stderr, "pricing benchmark requires TZ=UTC")
		os.Exit(2)
	}
	if *receiveInterval <= 0 {
		fmt.Fprintln(os.Stderr, "receive-interval must be positive")
		os.Exit(2)
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "formal pricing benchmark runs require Linux")
		os.Exit(2)
	}
	absoluteRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, name := range []string{"pricing-report.json", "pricing-old.db", "pricing-old.db-wal", "pricing-old.db-shm", "pricing-old.db-journal"} {
		path := filepath.Join(absoluteRoot, name)
		if _, err := os.Lstat(path); err == nil {
			fmt.Fprintf(os.Stderr, "benchmark output already exists: %s\n", path)
			os.Exit(2)
		} else if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "inspect benchmark output %s: %v\n", path, err)
			os.Exit(2)
		}
	}
	if err := os.MkdirAll(absoluteRoot, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	report, runErr := runPricingBenchmark(context.Background(), pricingBenchOptions{
		Root: absoluteRoot, Events: *events, Scenario: *scenario, Seed: *seed,
		Anchor: anchor, ReceiveInterval: *receiveInterval,
	})
	if runErr != nil {
		report.Error = runErr.Error()
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode benchmark report: %v\n", err)
		os.Exit(1)
	}
	reportPath := filepath.Join(absoluteRoot, "pricing-report.json")
	reportFile, err := os.OpenFile(reportPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create benchmark report: %v\n", err)
		os.Exit(1)
	}
	if _, err := reportFile.Write(append(encoded, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "write benchmark report: %v\n", err)
		os.Exit(1)
	}
	if err := reportFile.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close benchmark report: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("report: %s\n", reportPath)
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "pricing benchmark failed: %v\n", runErr)
		os.Exit(1)
	}
}
