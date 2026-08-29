package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
)

func TestFixedActivityGrainsCoverExactWindowsWithStableIntegerWidths(t *testing.T) {
	// 准备：固定非边界参考时间，并列出三种 grain 的窗口总长与允许秒宽。
	referenceEnd := time.Date(2026, 7, 20, 12, 34, 56, 0, time.UTC)
	testCases := []struct {
		name         string
		grain        entities.UsageActivityGrain
		window       time.Duration
		allowedWidth map[int64]bool
	}{
		{name: "short", grain: entities.UsageActivityGrainShort, window: 24 * time.Hour, allowedWidth: map[int64]bool{237: true, 238: true}},
		{name: "medium", grain: entities.UsageActivityGrainMedium, window: 7 * 24 * time.Hour, allowedWidth: map[int64]bool{1661: true, 1662: true}},
		{name: "long", grain: entities.UsageActivityGrainLong, window: 30 * 24 * time.Hour, allowedWidth: map[int64]bool{7120: true, 7121: true}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// 执行：直接调用 migration、runtime 与 query 共用的 activity 边界实现。
			buckets, err := activity.WindowEndingAt(testCase.grain, referenceEnd)
			if err != nil {
				t.Fatalf("WindowEndingAt returned error: %v", err)
			}

			// 断言：固定返回 364 格，首尾精确覆盖完整窗口且相邻边界无缝。
			if len(buckets) != activity.HeatmapBlocks {
				t.Fatalf("expected %d buckets, got %d", activity.HeatmapBlocks, len(buckets))
			}
			if got := buckets[len(buckets)-1].End.Sub(buckets[0].Start); got != testCase.window {
				t.Fatalf("expected exact window %s, got %s", testCase.window, got)
			}
			for index, bucket := range buckets {
				width := int64(bucket.End.Sub(bucket.Start) / time.Second)
				if !testCase.allowedWidth[width] {
					t.Fatalf("unexpected bucket %d width %d", index, width)
				}
				if index > 0 && !buckets[index-1].End.Equal(bucket.Start) {
					t.Fatalf("bucket %d is not adjacent to previous bucket", index)
				}
			}
		})
	}
}

func TestActivityBucketEndBelongsToNextBucket(t *testing.T) {
	// 准备：先取得任意 short bucket 的真实结束边界。
	first, err := activity.BucketForTimestamp(entities.UsageActivityGrainShort, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolve first bucket: %v", err)
	}

	// 执行：用前一 bucket 的半开终点再次归桶。
	next, err := activity.BucketForTimestamp(entities.UsageActivityGrainShort, first.End)
	if err != nil {
		t.Fatalf("resolve next bucket: %v", err)
	}

	// 断言：timestamp == bucket_end 必须稳定落入下一格。
	if !next.Start.Equal(first.End) {
		t.Fatalf("expected next bucket to start at previous end: first=%+v next=%+v", first, next)
	}
}

func TestDailyActivityBucketUsesAdjacentLocalMidnights(t *testing.T) {
	// 准备：使用 DST 春季跳时地区，并固定跳时当天中午。
	previousLocal := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load DST location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	timestamp := time.Date(2026, 3, 8, 12, 0, 0, 0, location)

	// 执行：直接生成 daily Activity 边界。
	bucket, err := activity.BucketForTimestamp(entities.UsageActivityGrainDaily, timestamp)
	if err != nil {
		t.Fatalf("resolve daily bucket: %v", err)
	}

	// 断言：daily 使用相邻本地零点，因此跳时日真实跨度为 23 小时。
	wantStart := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
	wantEnd := time.Date(2026, 3, 9, 0, 0, 0, 0, location)
	if !bucket.Start.Equal(wantStart) || !bucket.End.Equal(wantEnd) || bucket.End.Sub(bucket.Start) != 23*time.Hour {
		t.Fatalf("unexpected DST daily bucket: got=%+v want=%s..%s", bucket, wantStart, wantEnd)
	}
}

func TestCalendarDayActivityWindowUsesExactLocalMidnightsAcrossDST(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load DST location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	dayStart := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
	buckets, err := activity.WindowEndingAt(entities.UsageActivityGrainShort, dayStart.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("WindowEndingAt returned error: %v", err)
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	if len(buckets) != activity.HeatmapBlocks {
		t.Fatalf("expected %d calendar buckets, got %d", activity.HeatmapBlocks, len(buckets))
	}
	if !buckets[0].Start.Equal(dayStart) || !buckets[len(buckets)-1].End.Equal(dayEnd) {
		t.Fatalf("unexpected calendar window: %s..%s", buckets[0].Start, buckets[len(buckets)-1].End)
	}
	if got := buckets[len(buckets)-1].End.Sub(buckets[0].Start); got != 23*time.Hour {
		t.Fatalf("DST calendar window duration=%s, want 23h", got)
	}
	for index, bucket := range buckets {
		if index > 0 && !buckets[index-1].End.Equal(bucket.Start) {
			t.Fatalf("calendar buckets %d and %d are not contiguous", index-1, index)
		}
	}
}

func TestShortActivityStorageBucketsMatchCalendarDayWindow(t *testing.T) {
	previousLocal := time.Local
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })

	dayStart := time.Date(2026, 7, 20, 0, 0, 0, 0, location)
	calendarBuckets, err := activity.WindowEndingAt(entities.UsageActivityGrainShort, dayStart.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("WindowEndingAt returned error: %v", err)
	}
	for _, index := range []int{0, 1, 100, activity.HeatmapBlocks - 1} {
		calendarBucket := calendarBuckets[index]
		timestamp := calendarBucket.Start.Add(calendarBucket.End.Sub(calendarBucket.Start) / 2)
		storedBucket, err := activity.BucketForTimestamp(entities.UsageActivityGrainShort, timestamp)
		if err != nil {
			t.Fatalf("resolve short bucket %d: %v", index, err)
		}
		if !storedBucket.Start.Equal(calendarBucket.Start) || !storedBucket.End.Equal(calendarBucket.End) {
			t.Fatalf("short bucket %d does not match calendar grid: stored=%+v calendar=%+v", index, storedBucket, calendarBucket)
		}
	}

	window, err := activity.WindowEndingAt(entities.UsageActivityGrainShort, dayStart.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("WindowEndingAt returned error: %v", err)
	}
	for index := range window {
		if !window[index].Start.Equal(calendarBuckets[index].Start) || !window[index].End.Equal(calendarBuckets[index].End) {
			t.Fatalf("calendar query bucket %d does not match short storage: query=%+v storage=%+v", index, window[index], calendarBuckets[index])
		}
	}
}
