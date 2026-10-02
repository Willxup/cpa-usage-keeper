//go:build !linux

package main

// pricingBenchmarkDisk 让非 Linux 构建可通过；正式runner只在 Linux 执行。
func pricingBenchmarkDisk(string) (free, total uint64) { return 0, 0 }
