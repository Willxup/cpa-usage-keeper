package repository

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// UsageEventsLiveSnapshot 在同一 reader 快照中固定全局水位、总数和首批历史。
func UsageEventsLiveSnapshot(db *gorm.DB, filter dto.UsageQueryFilter, snapshot *pricing.Snapshot) (*dto.UsageEventsPageRecord, int64, error) {
	var page *dto.UsageEventsPageRecord
	var high int64
	err := db.Clauses(dbresolver.Read).Transaction(func(tx *gorm.DB) error {
		var err error
		high, err = UsageEventsLiveHighID(tx)
		if err != nil {
			return err
		}
		filter.MaxID = &high
		page, err = ListUsageEventsWithFilter(tx, filter, snapshot)
		return err
	})
	return page, high, err
}

// UsageEventsLiveCount 使用与历史首屏相同的筛选统计当前范围。
func UsageEventsLiveCount(db *gorm.DB, filter dto.UsageQueryFilter) (int64, error) {
	var total int64
	err := applyUsageEventListQuery(queryUsageEvents(db), filter).Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("count live usage events: %w", err)
	}
	return total, nil
}

// UsageEventsLiveHighID 使用主键索引读取当前提交上界，空表以零表示。
func UsageEventsLiveHighID(db *gorm.DB) (int64, error) {
	var high int64
	if err := db.Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&high).Error; err != nil {
		return 0, fmt.Errorf("load usage events high id: %w", err)
	}
	return high, nil
}

// UsageEventsLiveIDBatch 先限制原始 ID 数量，再使用历史列表相同的筛选与轻量投影。
// ID 允许不连续，因此进度取实际扫描到的最后一个 ID，而不是做数值加法。
func UsageEventsLiveIDBatch(db *gorm.DB, filter dto.UsageQueryFilter, after int64, limit int, snapshot *pricing.Snapshot) ([]dto.UsageEventRecord, int64, int, error) {
	var ids []int64
	if err := db.Model(&entities.UsageEvent{}).Where("id > ?", after).Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, after, 0, fmt.Errorf("scan usage event ids: %w", err)
	}
	if len(ids) == 0 {
		return nil, after, 0, nil
	}
	high := ids[len(ids)-1]
	filter.MaxID = &high
	query := applyUsageEventListQuery(db.Model(&entities.UsageEvent{}), filter).
		Where("id > ?", after).Select(usageEventProjectionColumns).Order("id ASC")
	rows, err := loadUsageEventRecordsForQuery(db, query, snapshot)
	return rows, high, len(ids), err
}

// UsageEventsLiveTimeBatch 先按时间索引限制原始行，再对这一小段应用列表筛选；不匹配的行也计入重放上限。
func UsageEventsLiveTimeBatch(db *gorm.DB, filter dto.UsageQueryFilter, high int64, from, until time.Time, lastTime *time.Time, lastID int64, limit int, snapshot *pricing.Snapshot) ([]dto.UsageEventRecord, *time.Time, int64, int, error) {
	start, end := timeutil.FormatStorageTime(from), timeutil.FormatStorageTime(until)
	window := func(query *gorm.DB) *gorm.DB {
		query = query.Where("id <= ? AND timestamp > ? AND timestamp <= ?", high, start, end)
		if lastTime != nil {
			cursor := timeutil.FormatStorageTime(*lastTime)
			query = query.Where("(timestamp > ? OR (timestamp = ? AND id > ?))", cursor, cursor, lastID)
		}
		return query
	}
	var scanned []entities.UsageEvent
	if err := window(db.Model(&entities.UsageEvent{})).Select("id, timestamp").Order("timestamp ASC, id ASC").Limit(limit).Find(&scanned).Error; err != nil {
		return nil, lastTime, lastID, 0, err
	}
	if len(scanned) == 0 {
		return nil, lastTime, lastID, 0, nil
	}
	last := scanned[len(scanned)-1]
	upper := timeutil.FormatStorageTime(last.Timestamp)
	filter.MaxID = &high
	query := window(applyUsageEventListQuery(db.Model(&entities.UsageEvent{}), filter)).
		Where("(timestamp < ? OR (timestamp = ? AND id <= ?))", upper, upper, last.ID).
		Select(usageEventProjectionColumns).Order("timestamp ASC, id ASC")
	rows, err := loadUsageEventRecordsForQuery(db, query, snapshot)
	return rows, &last.Timestamp, last.ID, len(scanned), err
}
