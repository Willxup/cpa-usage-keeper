package repository

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/repository/latencystore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const analysisLatencyHourRangeMax = 24 * time.Hour

// BuildAnalysisLatencyDiagnosticsWithFilter 只读取现有 Latency 聚合行并在内存合并诊断结果。
func BuildAnalysisLatencyDiagnosticsWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) (dto.AnalysisLatencyDiagnosticsRecord, error) {
	empty := emptyAnalysisLatencyDiagnosticsRecord()
	if db == nil {
		return empty, fmt.Errorf("database is nil")
	}
	if filter.StartTime == nil || filter.EndTime == nil {
		return empty, fmt.Errorf("analysis latency requires start_time and end_time")
	}
	start := timeutil.NormalizeStorageTime(*filter.StartTime)
	end := timeutil.NormalizeStorageTime(*filter.EndTime)
	if !end.After(start) {
		return empty, fmt.Errorf("analysis latency end_time must be after start_time")
	}

	bucketType, err := analysisLatencyBucketType(filter, start, end)
	if err != nil {
		return empty, err
	}
	alignedStart := start
	alignedEnd := end
	if bucketType == entities.UsageLatencyBucketDay && !(filter.Range == "custom" && strings.TrimSpace(filter.CustomUnit) == "day") {
		// 超过 24 小时的滚动范围与 Analysis 主数据共用完整自然日边界。
		alignedStart, alignedEnd = analysisRollingDayWindow(start, end)
	} else {
		// 小时范围两侧向上取整；Custom + Day 已经由解析层提供完整自然日边界。
		alignedStart = alignAnalysisLatencyBucketEnd(start, bucketType)
		alignedEnd = alignAnalysisLatencyBucketEnd(end, bucketType)
	}
	if !alignedEnd.After(alignedStart) {
		return empty, nil
	}

	var rows []entities.UsageLatencyStat
	query := db.Clauses(dbresolver.Read).
		Where("bucket_type = ? AND bucket_start >= ? AND bucket_start < ?", bucketType, timeutil.FormatStorageTime(alignedStart), timeutil.FormatStorageTime(alignedEnd))
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	if err := query.Order("bucket_start ASC").Find(&rows).Error; err != nil {
		return empty, fmt.Errorf("load analysis latency rollups: %w", err)
	}

	aggregate, err := latencystore.MergeDiagnosticsRows(rows)
	if err != nil {
		return empty, fmt.Errorf("merge analysis latency rollups: %w", err)
	}
	points := aggregate.SamplePoints.Points()
	result := empty
	result.Points = make([]dto.AnalysisLatencyPointRecord, 0, len(points))
	for _, point := range points {
		result.Points = append(result.Points, dto.AnalysisLatencyPointRecord{TTFTMS: point.TTFTMS, LatencyMS: point.LatencyMS})
	}
	result.TotalPoints = aggregate.SampleCount
	result.Sampled = aggregate.SampleCount > int64(len(points))
	result.P95TTFTMS = aggregate.TTFTSketch.P95()
	result.P95LatencyMS = aggregate.LatencySketch.P95()
	result.MaxTTFTMS = aggregate.MaxTTFTMS
	result.MaxLatencyMS = aggregate.MaxLatencyMS
	return result, nil
}

func analysisLatencyBucketType(filter dto.UsageQueryFilter, start, end time.Time) (entities.UsageLatencyBucketType, error) {
	if filter.Range == "custom" && strings.TrimSpace(filter.CustomUnit) == "day" {
		if _, err := analysisLatencyCalendarDays(start, end); err != nil {
			return "", err
		}
		// Custom 天已经是完整自然日区间，始终读取长期保留的 day 桶。
		return entities.UsageLatencyBucketDay, nil
	}
	if end.Sub(start) <= analysisLatencyHourRangeMax {
		return entities.UsageLatencyBucketHour, nil
	}
	return entities.UsageLatencyBucketDay, nil
}

func analysisLatencyCalendarDays(start, end time.Time) (int, error) {
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())
	if !start.Equal(startDay) || !end.Equal(endDay) {
		return 0, fmt.Errorf("custom analysis latency day range must align to local day boundaries")
	}
	calendarDays := 0
	for day := startDay; day.Before(endDay); day = day.AddDate(0, 0, 1) {
		calendarDays++
		if calendarDays > timeutil.LongCustomDayRangeMaxDays {
			return 0, fmt.Errorf("custom analysis latency day range cannot exceed %d calendar days", timeutil.LongCustomDayRangeMaxDays)
		}
	}
	if calendarDays == 0 {
		return 0, fmt.Errorf("custom analysis latency day range is empty")
	}
	return calendarDays, nil
}

func alignAnalysisLatencyBucketEnd(value time.Time, bucketType entities.UsageLatencyBucketType) time.Time {
	localValue := timeutil.NormalizeStorageTime(value)
	switch bucketType {
	case entities.UsageLatencyBucketDay:
		boundary := time.Date(localValue.Year(), localValue.Month(), localValue.Day(), 0, 0, 0, 0, localValue.Location())
		if localValue.Equal(boundary) {
			return boundary
		}
		return boundary.AddDate(0, 0, 1)
	default:
		boundary := localValue.Add(-time.Duration(localValue.Minute())*time.Minute - time.Duration(localValue.Second())*time.Second - time.Duration(localValue.Nanosecond()))
		if localValue.Equal(boundary) {
			return boundary
		}
		return boundary.Add(time.Hour)
	}
}
