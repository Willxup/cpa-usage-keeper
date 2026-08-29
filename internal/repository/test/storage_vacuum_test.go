package test

import (
	"testing"

	"cpa-usage-keeper/internal/repository"
)

func TestStorageVacuumRequiredUsesFreeBytesRatioAndDiskSpace(t *testing.T) {
	const (
		pageSize = int64(4096)
		oneGiB   = uint64(1 << 30)
	)
	tests := []struct {
		name          string
		pageCount     int64
		freelistCount int64
		available     uint64
		want          bool
	}{
		{name: "all conditions", pageCount: 1_500_000, freelistCount: 300_000, available: 13 * oneGiB, want: true},
		{name: "free bytes below one GiB", pageCount: 1_000_000, freelistCount: 250_000, available: 8 * oneGiB},
		{name: "free ratio below twenty percent", pageCount: 1_500_000, freelistCount: 299_999, available: 13 * oneGiB},
		{name: "temporary disk space insufficient", pageCount: 1_500_000, freelistCount: 300_000, available: 12 * oneGiB},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stats := repository.StorageVacuumStats{PageSize: pageSize, PageCount: test.pageCount, FreelistCount: test.freelistCount}
			if got := repository.StorageVacuumRequired(stats, test.available); got != test.want {
				t.Fatalf("StorageVacuumRequired() = %v, want %v for %+v available=%d", got, test.want, stats, test.available)
			}
		})
	}
}

func TestStorageVacuumRequiredHasNoTimeIntervalCondition(t *testing.T) {
	stats := repository.StorageVacuumStats{PageSize: 4096, PageCount: 1_500_000, FreelistCount: 300_000}
	if !repository.StorageVacuumRequired(stats, 13<<30) {
		t.Fatal("expected current page and disk conditions alone to allow vacuum")
	}
}

func TestStorageVacuumRequiredRejectsOverflowingSpaceEstimate(t *testing.T) {
	stats := repository.StorageVacuumStats{PageSize: 1 << 32, PageCount: 1 << 31, FreelistCount: 1 << 30}
	if repository.StorageVacuumRequired(stats, ^uint64(0)) {
		t.Fatal("expected overflowing temporary space estimate to skip vacuum")
	}
}
