package test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository/latencystore"
)

type queryMergeResult struct {
	Count, MaxTTFT, MaxLatency, P95TTFT, P95Latency int64
	Points                                          []latency.SamplePoint
}

var queryMergeBenchmarkResult queryMergeResult

// legacyQueryMerge 复现优化前的查询行为：每行解码后以 Points 排序计数，再全量排序裁剪合并样本。
func legacyQueryMerge(rows []entities.UsageLatencyStat) (queryMergeResult, error) {
	ttft := latency.NewSketch()
	latencySketch := latency.NewSketch()
	retained := make(map[int64]latency.SamplePoint)
	result := queryMergeResult{}
	for _, row := range rows {
		partTTFT, err := latency.UnmarshalSketch(row.TTFTSketch)
		if err != nil {
			return queryMergeResult{}, err
		}
		partLatency, err := latency.UnmarshalSketch(row.LatencySketch)
		if err != nil {
			return queryMergeResult{}, err
		}
		partSamples, err := latency.UnmarshalSampleSet(row.SamplePoints)
		if err != nil {
			return queryMergeResult{}, err
		}
		// 旧 decodeLatencyRow 为计数调用 Points；Merge 随后完整排序裁剪。
		points := partSamples.Points()
		if int64(len(points)) > row.SampleCount {
			return queryMergeResult{}, fmt.Errorf("count mismatch")
		}
		if err := ttft.Merge(partTTFT); err != nil {
			return queryMergeResult{}, err
		}
		if err := latencySketch.Merge(partLatency); err != nil {
			return queryMergeResult{}, err
		}
		for _, point := range points {
			if previous, ok := retained[point.EventID]; ok && previous != point {
				return queryMergeResult{}, fmt.Errorf("conflict")
			}
			retained[point.EventID] = point
		}
		if len(retained) > latency.MaxSamplePoints {
			ordered := make([]latency.SamplePoint, 0, len(retained))
			for _, point := range retained {
				ordered = append(ordered, point)
			}
			sortBenchmarkPoints(ordered)
			for _, point := range ordered[latency.MaxSamplePoints:] {
				delete(retained, point.EventID)
			}
		}
		result.Count += row.SampleCount
		result.MaxTTFT = max(result.MaxTTFT, row.MaxTTFTMS)
		result.MaxLatency = max(result.MaxLatency, row.MaxLatencyMS)
	}
	result.P95TTFT, result.P95Latency = ttft.P95(), latencySketch.P95()
	result.Points = make([]latency.SamplePoint, 0, len(retained))
	for _, point := range retained {
		result.Points = append(result.Points, point)
	}
	sortBenchmarkPoints(result.Points)
	return result, nil
}

func sortBenchmarkPoints(points []latency.SamplePoint) {
	sort.Slice(points, func(i, j int) bool {
		if points[i].Priority != points[j].Priority {
			return points[i].Priority < points[j].Priority
		}
		return points[i].EventID < points[j].EventID
	})
}

func BenchmarkQueryMergeDiagnosticsRows(b *testing.B) {
	for _, size := range []struct {
		name               string
		buckets, perBucket int
	}{
		{"sparse_8x25", 8, 25},
		{"dense_8x450", 8, 450},
		{"month_30x500", 30, 500},
		{"month_1500x500", 1500, 500},
		{"saturated_32x1000", 32, 1000},
	} {
		b.Run(size.name, func(b *testing.B) {
			start := time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC)
			rows := make([]entities.UsageLatencyStat, 0, size.buckets)
			if size.buckets == 1500 {
				// 30 天乘 50 个 key，逐行构造以限制基准准备阶段的内存占用。
				for bucket := 0; bucket < size.buckets; bucket++ {
					events := make([]entities.UsageEvent, 0, size.perBucket)
					day := start.AddDate(0, 0, bucket/50)
					for index := 0; index < size.perBucket; index++ {
						id := int64(bucket*size.perBucket + index + 1)
						event := latencyStoreTestEvent(id, day.Add(time.Duration(index)*time.Second), 10+id%113, 100+id%277)
						event.APIGroupKey = fmt.Sprintf("key-%02d", bucket%50)
						events = append(events, event)
					}
					built, err := latency.BuildRows(events, day.AddDate(0, 0, 1))
					if err != nil {
						b.Fatal(err)
					}
					for _, row := range built {
						if row.BucketType == entities.UsageLatencyBucketDay {
							rows = append(rows, row)
						}
					}
				}
			} else {
				events := make([]entities.UsageEvent, 0, size.buckets*size.perBucket)
				for bucket := 0; bucket < size.buckets; bucket++ {
					for index := 0; index < size.perBucket; index++ {
						id := int64(bucket*size.perBucket + index + 1)
						events = append(events, latencyStoreTestEvent(id, start.Add(time.Duration(bucket)*time.Hour+time.Duration(index)*time.Second), 10+id%113, 100+id%277))
					}
				}
				built, err := latency.BuildRows(events, events[len(events)-1].Timestamp)
				if err != nil {
					b.Fatal(err)
				}
				for _, row := range built {
					if row.BucketType == entities.UsageLatencyBucketHour {
						rows = append(rows, row)
					}
				}
			}
			if len(rows) != size.buckets {
				b.Fatalf("rows=%d", len(rows))
			}
			want, err := legacyQueryMerge(rows)
			if err != nil {
				b.Fatal(err)
			}
			gotAggregate, err := latencystore.MergeDiagnosticsRows(rows)
			if err != nil {
				b.Fatal(err)
			}
			got := queryMergeResult{Count: gotAggregate.SampleCount, MaxTTFT: gotAggregate.MaxTTFTMS, MaxLatency: gotAggregate.MaxLatencyMS, P95TTFT: gotAggregate.TTFTSketch.P95(), P95Latency: gotAggregate.LatencySketch.P95(), Points: gotAggregate.SamplePoints.Points()}
			if !reflect.DeepEqual(got, want) {
				b.Fatal("optimized query merge changed exact output")
			}
			for _, implementation := range []struct {
				name string
				run  func() (queryMergeResult, error)
			}{
				{"legacy_sort", func() (queryMergeResult, error) { return legacyQueryMerge(rows) }},
				{"optimized", func() (queryMergeResult, error) {
					merged, err := latencystore.MergeDiagnosticsRows(rows)
					if err != nil {
						return queryMergeResult{}, err
					}
					return queryMergeResult{Count: merged.SampleCount, MaxTTFT: merged.MaxTTFTMS, MaxLatency: merged.MaxLatencyMS, P95TTFT: merged.TTFTSketch.P95(), P95Latency: merged.LatencySketch.P95(), Points: merged.SamplePoints.Points()}, nil
				}},
			} {
				b.Run(implementation.name, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						result, err := implementation.run()
						if err != nil {
							b.Fatal(err)
						}
						queryMergeBenchmarkResult = result
					}
				})
			}
		})
	}
}
