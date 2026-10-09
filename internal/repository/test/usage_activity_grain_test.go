package test

import (
	"slices"
	"sort"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

func TestOpenDatabaseCreatesUsageActivitySchema(t *testing.T) {
	db := openTestDatabase(t)

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
	if !slices.Equal(columns, wantColumns) {
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
	if !slices.Equal(gotColumns, wantColumns) {
		t.Fatalf("index %s columns=%v, want %v", name, gotColumns, wantColumns)
	}
}
