package repository

import (
	"sort"
	"time"

	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

const analysisModelSpeedMaxBuckets = 8

type analysisModelSpeedAccumulator struct {
	record dto.AnalysisModelSpeedRecord
	models map[string]*dto.AnalysisModelSpeedSeriesRecord
}

// newAnalysisModelSpeed 按源桶索引连续分组；日边界由独立历法日期生成，避免午夜跳时后的钟点持续漂移。
func newAnalysisModelSpeed(start, end time.Time, daily bool) *analysisModelSpeedAccumulator {
	start, end = timeutil.NormalizeStorageTime(start), timeutil.NormalizeStorageTime(end)
	boundaries := []time.Time{start}
	if daily {
		date := timeutil.DateOf(start)
		for {
			bounds, exists := timeutil.DayBounds(date, start.Location())
			if exists && !bounds.Start.Before(start) {
				start = bounds.Start
				boundaries[0] = start
				break
			}
			date = date.AddDays(1)
		}
		for boundaries[len(boundaries)-1].Before(end) {
			date = date.AddDays(1)
			bounds, exists := timeutil.DayBounds(date, start.Location())
			if !exists {
				continue
			}
			next := bounds.Start
			if next.After(end) {
				next = end
			}
			boundaries = append(boundaries, next)
		}
	} else {
		for at := start; at.Before(end); {
			at = at.Add(time.Hour)
			boundaries = append(boundaries, at)
		}
	}
	count := len(boundaries) - 1
	bucketCount := min(count, analysisModelSpeedMaxBuckets)
	record := dto.AnalysisModelSpeedRecord{RangeStart: &start, RangeEnd: &end, Buckets: make([]dto.AnalysisModelSpeedBucketRecord, 0, bucketCount), Series: []dto.AnalysisModelSpeedSeriesRecord{}}
	for i := 0; i < bucketCount; i++ {
		// 余数均匀分到前面的组，避免最后只剩一个源桶。
		first := i*(count/bucketCount) + min(i, count%bucketCount)
		last := first + count/bucketCount
		if i < count%bucketCount {
			last++
		}
		record.Buckets = append(record.Buckets, dto.AnalysisModelSpeedBucketRecord{Start: boundaries[first], End: boundaries[last]})
	}
	return &analysisModelSpeedAccumulator{record: record, models: map[string]*dto.AnalysisModelSpeedSeriesRecord{}}
}

// add 与原 Analysis 行遍历同步，速度有效条件已由增量存储计算，不再用 Token/费用筛选。
func (a *analysisModelSpeedAccumulator) add(row analysisOverviewStatProjection, bucket time.Time) {
	index := sort.Search(len(a.record.Buckets), func(i int) bool { return a.record.Buckets[i].End.After(bucket) })
	if index == len(a.record.Buckets) || bucket.Before(a.record.Buckets[index].Start) {
		return
	}
	model := normalizeUsageOverviewDimension(row.Model)
	series := a.models[model]
	if series == nil {
		series = &dto.AnalysisModelSpeedSeriesRecord{Model: model, Samples: make([]dto.AnalysisModelSpeedSampleRecord, len(a.record.Buckets))}
		a.models[model] = series
	}
	series.TotalTokens += row.TotalTokens
	series.SpeedSampleCount += row.SpeedSampleCount
	sample := &series.Samples[index]
	sample.SpeedTPSSum += row.SpeedTPSSum
	sample.SpeedSampleCount += row.SpeedSampleCount
}

func (a *analysisModelSpeedAccumulator) finalize() dto.AnalysisModelSpeedRecord {
	for _, series := range a.models {
		for i := range series.Samples {
			sample := &series.Samples[i]
			if sample.SpeedSampleCount > 0 {
				mean := sample.SpeedTPSSum / float64(sample.SpeedSampleCount)
				sample.MeanTPS = &mean
			}
		}
		a.record.Series = append(a.record.Series, *series)
	}
	sort.Slice(a.record.Series, func(i, j int) bool {
		left, right := a.record.Series[i], a.record.Series[j]
		if left.TotalTokens == right.TotalTokens {
			return left.Model < right.Model
		}
		return left.TotalTokens > right.TotalTokens
	})
	return a.record
}
