//go:build linux

package main

import "syscall"

// pricingBenchmarkDisk 只在 Linux 正式压测主机读取工作目录所在文件系统容量。
func pricingBenchmarkDisk(path string) (free, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	return stat.Bavail * uint64(stat.Bsize), stat.Blocks * uint64(stat.Bsize)
}
