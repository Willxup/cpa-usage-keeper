package repository

import (
	"fmt"
	"path/filepath"

	"cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	storageVacuumMinFreeBytes = uint64(1 << 30)
	storageVacuumMinFreeRatio = 0.20
)

// StorageVacuumStats 是 SQLite 主库当前页使用情况。
type StorageVacuumStats struct {
	PageSize      int64
	PageCount     int64
	FreelistCount int64
}

func (s StorageVacuumStats) freeBytes() uint64 {
	if s.PageSize <= 0 || s.FreelistCount <= 0 {
		return 0
	}
	return uint64(s.PageSize) * uint64(s.FreelistCount)
}

func (s StorageVacuumStats) databaseBytes() uint64 {
	if s.PageSize <= 0 || s.PageCount <= 0 {
		return 0
	}
	return uint64(s.PageSize) * uint64(s.PageCount)
}

func (s StorageVacuumStats) freeRatio() float64 {
	if s.PageCount <= 0 || s.FreelistCount <= 0 {
		return 0
	}
	return float64(s.FreelistCount) / float64(s.PageCount)
}

// StorageVacuumRequired 只根据当前空闲页和临时磁盘空间决定，不保存或检查上次整理时间。
func StorageVacuumRequired(stats StorageVacuumStats, availableDiskBytes uint64) bool {
	if stats.freeBytes() < storageVacuumMinFreeBytes || stats.freeRatio() < storageVacuumMinFreeRatio {
		return false
	}
	// 完整 VACUUM 可能同时保留临时库与 journal/WAL，按两份原库再加 1 GiB 余量保守判断。
	databaseBytes := stats.databaseBytes()
	if databaseBytes > (^uint64(0)-storageVacuumMinFreeBytes)/2 {
		return false
	}
	requiredDiskBytes := databaseBytes*2 + storageVacuumMinFreeBytes
	return availableDiskBytes >= requiredDiskBytes
}

func maybeVacuumStorage(db *gorm.DB) (dto.StorageVacuumResult, error) {
	stats, databasePath, err := loadStorageVacuumStats(db)
	if err != nil {
		return dto.StorageVacuumResult{}, err
	}
	result := dto.StorageVacuumResult{
		PageSize:      stats.PageSize,
		PageCount:     stats.PageCount,
		FreelistCount: stats.FreelistCount,
		FreeBytes:     stats.freeBytes(),
		FreeRatio:     stats.freeRatio(),
	}
	if stats.freeBytes() < storageVacuumMinFreeBytes || stats.freeRatio() < storageVacuumMinFreeRatio {
		result.SkippedReason = "threshold_not_met"
		return result, nil
	}
	if databasePath == "" {
		result.SkippedReason = "database_path_unavailable"
		return result, nil
	}
	availableDiskBytes, err := storageAvailableDiskBytes(filepath.Dir(databasePath))
	if err != nil {
		result.SkippedReason = "disk_space_unavailable"
		return result, nil
	}
	result.AvailableDiskBytes = availableDiskBytes
	if !StorageVacuumRequired(stats, availableDiskBytes) {
		result.SkippedReason = "disk_space_insufficient"
		return result, nil
	}
	if err := Vacuum(db.Clauses(dbresolver.Write)); err != nil {
		return result, err
	}
	result.Performed = true
	return result, nil
}

func loadStorageVacuumStats(db *gorm.DB) (StorageVacuumStats, string, error) {
	if db == nil {
		return StorageVacuumStats{}, "", fmt.Errorf("database is nil")
	}
	writeDB := db.Clauses(dbresolver.Write)
	stats := StorageVacuumStats{}
	for _, query := range []struct {
		sql  string
		dest *int64
		name string
	}{
		{sql: "PRAGMA page_size", dest: &stats.PageSize, name: "page_size"},
		{sql: "PRAGMA page_count", dest: &stats.PageCount, name: "page_count"},
		{sql: "PRAGMA freelist_count", dest: &stats.FreelistCount, name: "freelist_count"},
	} {
		if err := writeDB.Raw(query.sql).Scan(query.dest).Error; err != nil {
			return StorageVacuumStats{}, "", fmt.Errorf("load sqlite %s: %w", query.name, err)
		}
	}
	var databases []struct {
		Name string `gorm:"column:name"`
		File string `gorm:"column:file"`
	}
	if err := writeDB.Raw("PRAGMA database_list").Scan(&databases).Error; err != nil {
		return StorageVacuumStats{}, "", fmt.Errorf("load sqlite database path: %w", err)
	}
	for _, database := range databases {
		if database.Name == "main" {
			return stats, database.File, nil
		}
	}
	return stats, "", nil
}
