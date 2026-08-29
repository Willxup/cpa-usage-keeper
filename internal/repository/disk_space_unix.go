//go:build !windows

package repository

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func storageAvailableDiskBytes(path string) (uint64, error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, fmt.Errorf("load available disk space for %s: %w", path, err)
	}
	if stats.Bsize <= 0 {
		return 0, fmt.Errorf("load available disk space for %s: invalid block size %d", path, stats.Bsize)
	}
	return uint64(stats.Bavail) * uint64(stats.Bsize), nil
}
