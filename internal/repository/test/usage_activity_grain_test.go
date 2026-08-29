package test

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestUsageActivityFixedGrainsCoverExactWindows(t *testing.T) {
	// 准备：列出三种固定窗口 grain、完整窗口长度与唯一允许的整数秒跨度。
	testCases := []struct {
		name        string
		grain       entities.UsageActivityGrain
		window      time.Duration
		allowedSpan map[time.Duration]bool
	}{
		{
			name:   "short",
			grain:  entities.UsageActivityGrainShort,
			window: 24 * time.Hour,
			allowedSpan: map[time.Duration]bool{
				237 * time.Second: true,
				238 * time.Second: true,
			},
		},
		{
			name:   "medium",
			grain:  entities.UsageActivityGrainMedium,
			window: 7 * 24 * time.Hour,
			allowedSpan: map[time.Duration]bool{
				1661 * time.Second: true,
				1662 * time.Second: true,
			},
		},
		{
			name:   "long",
			grain:  entities.UsageActivityGrainLong,
			window: 30 * 24 * time.Hour,
			allowedSpan: map[time.Duration]bool{
				7120 * time.Second: true,
				7121 * time.Second: true,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// 执行：以非边界时间查询当前 grain 对齐后的固定 364 个 buckets。
			referenceEnd := time.Date(2026, 7, 20, 12, 34, 56, 789000000, time.UTC)
			buckets, err := repository.UsageActivityWindowEndingAt(testCase.grain, referenceEnd)
			if err != nil {
				t.Fatalf("UsageActivityWindowEndingAt returned error: %v", err)
			}
			// 断言：格子数、完整窗口、覆盖范围、桶宽和连续性都必须满足固定契约。
			if len(buckets) != repository.UsageActivityHeatmapBlocks {
				t.Fatalf("expected %d buckets, got %d", repository.UsageActivityHeatmapBlocks, len(buckets))
			}
			if got := buckets[len(buckets)-1].End.Sub(buckets[0].Start); got != testCase.window {
				t.Fatalf("expected exact window %s, got %s", testCase.window, got)
			}
			if buckets[len(buckets)-1].End.Before(referenceEnd) {
				t.Fatalf("expected aligned window end to cover reference end: end=%s reference=%s", buckets[len(buckets)-1].End, referenceEnd)
			}

			seenSpans := map[time.Duration]bool{}
			for index, bucket := range buckets {
				if !bucket.Start.Before(bucket.End) {
					t.Fatalf("bucket %d has invalid range: %+v", index, bucket)
				}
				span := bucket.End.Sub(bucket.Start)
				if !testCase.allowedSpan[span] {
					t.Fatalf("bucket %d has unexpected span %s", index, span)
				}
				seenSpans[span] = true
				if index > 0 && !buckets[index-1].End.Equal(bucket.Start) {
					t.Fatalf("buckets %d and %d are not contiguous: %s != %s", index-1, index, buckets[index-1].End, bucket.Start)
				}
			}
			if len(seenSpans) != len(testCase.allowedSpan) {
				t.Fatalf("expected both allowed spans, got %+v", seenSpans)
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
		// 执行：对同一 timestamp 查询两次，并用当前 bucket_end 再查询下一桶。
		bucket, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainShort, timestamp)
		if err != nil {
			t.Fatalf("UsageActivityBucketForTimestamp(%s) returned error: %v", timestamp, err)
		}
		// 断言：timestamp 必须位于半开区间内，相同输入稳定，end 精确进入下一桶。
		if timestamp.Before(bucket.Start) || !timestamp.Before(bucket.End) {
			t.Fatalf("timestamp %s is outside bucket %+v", timestamp, bucket)
		}

		repeated, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainShort, timestamp)
		if err != nil {
			t.Fatalf("second bucket lookup returned error: %v", err)
		}
		if repeated != bucket {
			t.Fatalf("same timestamp produced unstable buckets: first=%+v second=%+v", bucket, repeated)
		}

		next, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainShort, bucket.End)
		if err != nil {
			t.Fatalf("boundary bucket lookup returned error: %v", err)
		}
		if !next.Start.Equal(bucket.End) {
			t.Fatalf("expected end boundary to enter next bucket: current=%+v next=%+v", bucket, next)
		}
	}
}

func TestUsageActivityDailyGrainUsesNextLocalMidnight(t *testing.T) {
	// 准备：切到纽约 DST 跳时日，确保自然日不是固定 24 小时。
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	originalLocation := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = originalLocation })

	// 执行：查询 DST 当日本地中午所属的 daily bucket。
	timestamp := time.Date(2026, 3, 8, 12, 0, 0, 0, location)
	bucket, err := repository.UsageActivityBucketForTimestamp(entities.UsageActivityGrainDaily, timestamp)
	if err != nil {
		t.Fatalf("UsageActivityBucketForTimestamp returned error: %v", err)
	}
	// 断言：边界必须是相邻本地零点，实际跨度为 23 小时。
	wantStart := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
	wantEnd := time.Date(2026, 3, 9, 0, 0, 0, 0, location)
	if !bucket.Start.Equal(wantStart) || !bucket.End.Equal(wantEnd) {
		t.Fatalf("unexpected daily bucket: got=%+v want=%s..%s", bucket, wantStart, wantEnd)
	}
	if got := bucket.End.Sub(bucket.Start); got != 23*time.Hour {
		t.Fatalf("expected DST day to span 23h, got %s", got)
	}
}

func TestOpenDatabaseCreatesUsageActivitySchema(t *testing.T) {
	// 准备：为 fresh database 分配独立 SQLite 文件。
	databasePath := filepath.Join(t.TempDir(), "activity-schema.db")
	// 执行：通过生产 OpenDatabase 路径创建最终 schema 和索引。
	db, err := repository.OpenDatabase(config.Config{SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	closeUsageActivityTestDatabase(t, db)

	// 断言：Activity stats 与通用 checkpoint 表、精确字段集合和三个索引顺序都符合数据契约。
	if !db.Migrator().HasTable(&entities.UsageActivityStat{}) {
		t.Fatal("expected usage_activity_stats table")
	}
	if !db.Migrator().HasTable(&entities.UsageAggregationCheckpoint{}) {
		t.Fatal("expected usage_aggregation_checkpoints table")
	}
	if db.Migrator().HasTable(&entities.UsageActivityAggregationCheckpoint{}) {
		t.Fatal("did not expect legacy usage_activity_aggregation_checkpoints table")
	}

	columnTypes, err := db.Migrator().ColumnTypes(&entities.UsageActivityStat{})
	if err != nil {
		t.Fatalf("load activity column types: %v", err)
	}
	columns := make([]string, 0, len(columnTypes))
	for _, columnType := range columnTypes {
		columns = append(columns, columnType.Name())
	}
	sort.Strings(columns)
	wantColumns := []string{
		"api_group_key", "bucket_end", "bucket_start", "cache_creation_tokens", "cache_read_tokens",
		"created_at", "failure_count", "grain", "id", "input_tokens", "output_tokens",
		"reasoning_tokens", "success_count", "total_tokens", "updated_at",
	}
	sort.Strings(wantColumns)
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("unexpected activity columns:\n got: %v\nwant: %v", columns, wantColumns)
	}

	assertUsageActivityIndexColumns(t, db, "uniq_usage_activity_stats_grain_start_api", []string{"grain", "bucket_start", "api_group_key"}, true)
	assertUsageActivityIndexColumns(t, db, "idx_usage_activity_stats_api_grain_start", []string{"api_group_key", "grain", "bucket_start"}, false)
	assertUsageActivityIndexColumns(t, db, "idx_usage_activity_stats_grain_end", []string{"grain", "bucket_end"}, false)

	// 执行：分别尝试写入非法 grain 和反向半开区间。
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	invalidGrainErr := db.Create(&entities.UsageActivityStat{Grain: "invalid", BucketStart: now, BucketEnd: now.Add(time.Minute), APIGroupKey: "invalid-grain"}).Error
	invalidBoundsErr := db.Create(&entities.UsageActivityStat{Grain: entities.UsageActivityGrainShort, BucketStart: now, BucketEnd: now, APIGroupKey: "invalid-bounds"}).Error

	// 断言：最终数据库必须自己拒绝绕过 BuildRows 的非法 Activity 行。
	if invalidGrainErr == nil {
		t.Fatal("expected usage_activity_stats to reject invalid grain")
	}
	if invalidBoundsErr == nil {
		t.Fatal("expected usage_activity_stats to reject bucket_start >= bucket_end")
	}
}

func assertUsageActivityIndexColumns(t *testing.T, db *gorm.DB, name string, wantColumns []string, wantUnique bool) {
	t.Helper()
	type indexListRow struct {
		Name   string `gorm:"column:name"`
		Unique int    `gorm:"column:unique"`
	}
	var indexes []indexListRow
	if err := db.Raw("PRAGMA index_list(usage_activity_stats)").Scan(&indexes).Error; err != nil {
		t.Fatalf("list activity indexes: %v", err)
	}
	found := false
	for _, index := range indexes {
		if index.Name != name {
			continue
		}
		found = true
		if (index.Unique == 1) != wantUnique {
			t.Fatalf("index %s unique=%v, want %v", name, index.Unique == 1, wantUnique)
		}
	}
	if !found {
		t.Fatalf("expected index %s", name)
	}

	type indexInfoRow struct {
		SeqNo int    `gorm:"column:seqno"`
		Name  string `gorm:"column:name"`
	}
	var rows []indexInfoRow
	if err := db.Raw("PRAGMA index_info(" + name + ")").Scan(&rows).Error; err != nil {
		t.Fatalf("load index %s columns: %v", name, err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SeqNo < rows[j].SeqNo })
	gotColumns := make([]string, 0, len(rows))
	for _, row := range rows {
		gotColumns = append(gotColumns, row.Name)
	}
	if !reflect.DeepEqual(gotColumns, wantColumns) {
		t.Fatalf("index %s columns=%v, want %v", name, gotColumns, wantColumns)
	}
}

func closeUsageActivityTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err != nil {
			t.Errorf("load sql db: %v", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sql db: %v", err)
		}
	})
}
