package test

import (
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
)

func TestBuildRowsCreatesHourAndDayRowsWithoutMutatingEvents(t *testing.T) {
	// 固定项目时区，验证小时桶和本地自然日桶使用同一边界语义。
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	generate := true
	doNotGenerate := false
	ttft100 := int64(100)
	ttft200 := int64(200)
	invalidTTFT := int64(0)
	events := []entities.UsageEvent{
		{ID: 1, APIGroupKey: " key-a ", Timestamp: time.Date(2026, 7, 26, 10, 15, 0, 0, location), Generate: &generate, LatencyMS: 900, TTFTMS: &ttft100},
		{ID: 2, APIGroupKey: "key-a", Timestamp: time.Date(2026, 7, 26, 10, 45, 0, 0, location), Generate: &generate, LatencyMS: 1200, TTFTMS: &ttft200},
		{ID: 3, APIGroupKey: "key-b", Timestamp: time.Date(2026, 7, 26, 11, 0, 0, 0, location), Failed: true, Generate: &generate, LatencyMS: 500, TTFTMS: &ttft100},
		{ID: 4, APIGroupKey: "key-b", Timestamp: time.Date(2026, 7, 26, 11, 0, 0, 0, location), Generate: &doNotGenerate, LatencyMS: 500, TTFTMS: &ttft100},
		{ID: 5, APIGroupKey: "key-b", Timestamp: time.Date(2026, 7, 26, 11, 0, 0, 0, location), Generate: &generate, LatencyMS: 500, TTFTMS: &invalidTTFT},
	}
	// 深拷贝指针字段，确保 BuildRows 不排序或改写输入元素。
	before := append([]entities.UsageEvent(nil), events...)

	rows, err := latency.BuildRows(events, time.Date(2026, 7, 26, 12, 0, 0, 0, location))
	if err != nil {
		t.Fatalf("BuildRows returned error: %v", err)
	}
	if !reflect.DeepEqual(events, before) {
		t.Fatalf("BuildRows mutated input events:\n before=%+v\n after=%+v", before, events)
	}
	if len(rows) != 2 {
		t.Fatalf("expected one hour and one day row, got %+v", rows)
	}

	// 两条有效事件落在同一个 key/hour/day，因此两行都累计精确 count/max。
	for _, row := range rows {
		if row.APIGroupKey != "key-a" || row.SampleCount != 2 || row.MaxTTFTMS != 200 || row.MaxLatencyMS != 1200 || row.FormatVersion != latency.FormatVersion {
			t.Fatalf("unexpected latency aggregate row: %+v", row)
		}
		ttftSketch, err := latency.UnmarshalSketch(row.TTFTSketch)
		if err != nil {
			t.Fatalf("decode TTFT sketch: %v", err)
		}
		latencySketch, err := latency.UnmarshalSketch(row.LatencySketch)
		if err != nil {
			t.Fatalf("decode latency sketch: %v", err)
		}
		points, err := latency.UnmarshalSampleSet(row.SamplePoints)
		if err != nil {
			t.Fatalf("decode sample points: %v", err)
		}
		if ttftSketch.Count() != 2 || latencySketch.Count() != 2 || len(points.Points()) != 2 {
			t.Fatalf("row encodings lost samples: ttft=%d latency=%d points=%d", ttftSketch.Count(), latencySketch.Count(), len(points.Points()))
		}
	}

	// 小时行从本地整点开始，天行从本地自然日零点开始。
	hourStart := time.Date(2026, 7, 26, 10, 0, 0, 0, location)
	dayStart := time.Date(2026, 7, 26, 0, 0, 0, 0, location)
	starts := map[entities.UsageLatencyBucketType]time.Time{
		rows[0].BucketType: rows[0].BucketStart,
		rows[1].BucketType: rows[1].BucketStart,
	}
	if !starts[entities.UsageLatencyBucketHour].Equal(hourStart) || !starts[entities.UsageLatencyBucketDay].Equal(dayStart) {
		t.Fatalf("unexpected hour/day bucket starts: %+v", starts)
	}
}

func TestBuildRowsAlignsHourBucketsToLocalClockInNonWholeHourOffsetTimezone(t *testing.T) {
	// time.Truncate 按绝对时间截断，在非整小时偏移时会生成 09:45；存储桶必须仍从本地 10:00 开始。
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Kathmandu")
	if err != nil {
		t.Fatalf("load non-whole-hour offset location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	generate := true
	ttft := int64(100)
	eventTime := time.Date(2026, 7, 26, 10, 15, 0, 0, location)
	rows, err := latency.BuildRows([]entities.UsageEvent{{
		ID: 1, APIGroupKey: "half-hour-key", Timestamp: eventTime, Generate: &generate, TTFTMS: &ttft, LatencyMS: 900,
	}}, eventTime.Add(time.Hour))
	if err != nil {
		t.Fatalf("BuildRows returned error: %v", err)
	}
	for _, row := range rows {
		if row.BucketType == entities.UsageLatencyBucketHour {
			want := time.Date(2026, 7, 26, 10, 0, 0, 0, location)
			if !row.BucketStart.Equal(want) {
				t.Fatalf("expected local hour bucket %s, got %s", want, row.BucketStart)
			}
			return
		}
	}
	t.Fatal("expected one retained hour row")
}
