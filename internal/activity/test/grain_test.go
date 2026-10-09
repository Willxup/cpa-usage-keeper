package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/activity"
	"cpa-usage-keeper/internal/entities"
)

func TestFixedActivityGrainsCoverExactWindowsWithStableIntegerWidths(t *testing.T) {
	// 准备：固定非边界参考时间，并列出三种 grain 的窗口总长与允许秒宽。
	referenceEnd := time.Date(2026, 7, 20, 12, 34, 56, 789000000, time.UTC)
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
			if buckets[len(buckets)-1].End.Before(referenceEnd) {
				t.Fatalf("aligned window does not cover reference end: %v", buckets[len(buckets)-1].End)
			}
			seenWidths := map[int64]bool{}
			for index, bucket := range buckets {
				width := int64(bucket.End.Sub(bucket.Start) / time.Second)
				if bucket.End.Sub(bucket.Start)%time.Second != 0 || !testCase.allowedWidth[width] {
					t.Fatalf("unexpected bucket %d width %d", index, width)
				}
				seenWidths[width] = true
				if index > 0 && !buckets[index-1].End.Equal(bucket.Start) {
					t.Fatalf("bucket %d is not adjacent to previous bucket", index)
				}
			}
			if len(seenWidths) != len(testCase.allowedWidth) {
				t.Fatalf("expected both allowed widths, got %v", seenWidths)
			}
		})
	}
}

func TestUsageActivityBucketForTimestampUsesHalfOpenStableBoundaries(t *testing.T) {
	// 准备：同时覆盖 epoch 之前、epoch 边界和当前正时间的 timestamp。
	timestamps := []time.Time{
		time.Date(1969, 12, 31, 23, 50, 0, 0, time.UTC),
		time.Unix(0, 0).UTC(),
		time.Date(2026, 7, 20, 12, 34, 56, 789000000, time.UTC),
	}

	for _, timestamp := range timestamps {
		// end 是半开边界，必须进入下一桶。
		bucket, err := activity.BucketForTimestamp(entities.UsageActivityGrainShort, timestamp)
		if err != nil {
			t.Fatalf("UsageActivityBucketForTimestamp(%s) returned error: %v", timestamp, err)
		}
		// 负时间、epoch 和正时间都必须包含在对应半开区间内。
		if timestamp.Before(bucket.Start) || !timestamp.Before(bucket.End) {
			t.Fatalf("timestamp %s is outside bucket %+v", timestamp, bucket)
		}

		next, err := activity.BucketForTimestamp(entities.UsageActivityGrainShort, bucket.End)
		if err != nil {
			t.Fatalf("boundary bucket lookup returned error: %v", err)
		}
		if !next.Start.Equal(bucket.End) {
			t.Fatalf("expected end boundary to enter next bucket: current=%+v next=%+v", bucket, next)
		}
	}
}

func TestDailyActivityBucketUsesAdjacentLocalMidnights(t *testing.T) {
	// 准备：使用 DST 春季跳时地区，并固定跳时当天中午。
	location := useTimezone(t, "America/New_York")
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
	location := useTimezone(t, "America/New_York")

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
	location := useTimezone(t, "Asia/Shanghai")

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
}

func useTimezone(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	return location
}
