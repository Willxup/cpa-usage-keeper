package test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/latency"
	"cpa-usage-keeper/internal/repository/latencystore"
	"cpa-usage-keeper/internal/repository/migration"

	"gorm.io/gorm"
)

const latencySampleCapMigrationVersion = "20260925_limit_latency_sample_points"

// The legacy fixture is encoded independently: the production Add method must cap at 1000.
func capLegacyPoints(base int64, count int) []latency.SamplePoint {
	points := make([]latency.SamplePoint, count)
	for index := range points {
		id := base + int64(index)
		points[index] = latency.SamplePoint{EventID: id, Priority: capLegacyPriority(uint64(id)), TTFTMS: 100 + id%53, LatencyMS: 1000 + id%211}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Priority != points[j].Priority {
			return points[i].Priority < points[j].Priority
		}
		return points[i].EventID < points[j].EventID
	})
	return points
}

func capLegacyPriority(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}

func capEncodePoints(points []latency.SamplePoint, corruptLast bool) []byte {
	encoded := binary.AppendUvarint([]byte{latency.FormatVersion}, uint64(len(points)))
	for index, point := range points {
		encoded = binary.AppendUvarint(encoded, uint64(point.EventID))
		encoded = binary.AppendUvarint(encoded, point.Priority)
		encoded = binary.AppendUvarint(encoded, uint64(point.TTFTMS))
		value := point.LatencyMS
		if corruptLast && index == len(points)-1 {
			value = 0
		}
		encoded = binary.AppendUvarint(encoded, uint64(value))
	}
	return encoded
}

func capRow(t *testing.T, id int64, count int, now time.Time) (entities.UsageLatencyStat, []latency.SamplePoint) {
	t.Helper()
	points := capLegacyPoints(id*10000, count)
	ttft, total := latency.NewSketch(), latency.NewSketch()
	var maxTTFT, maxLatency int64
	for _, point := range points {
		if err := ttft.Add(point.TTFTMS); err != nil {
			t.Fatal(err)
		}
		if err := total.Add(point.LatencyMS); err != nil {
			t.Fatal(err)
		}
		maxTTFT = max(maxTTFT, point.TTFTMS)
		maxLatency = max(maxLatency, point.LatencyMS)
	}
	ttftBlob, err := ttft.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	latencyBlob, err := total.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	row := entities.UsageLatencyStat{
		ID: id, BucketType: entities.UsageLatencyBucketDay, BucketStart: now.AddDate(0, 0, -1),
		APIGroupKey: fmt.Sprintf("test-%03d", id), SampleCount: int64(count),
		MaxTTFTMS: maxTTFT, MaxLatencyMS: maxLatency, FormatVersion: latency.FormatVersion,
		TTFTSketch: ttftBlob, LatencySketch: latencyBlob, SamplePoints: capEncodePoints(points, false),
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	return row, points
}

func capMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openUnmigratedTestDatabase(t)
	if err := db.AutoMigrate(&entities.UsageLatencyStat{}, &entities.UsageAggregationCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", latencySampleCapMigrationVersion).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func capStoredRow(t *testing.T, db *gorm.DB, id int64) entities.UsageLatencyStat {
	t.Helper()
	var row entities.UsageLatencyStat
	if err := db.First(&row, id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func capMigrationCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", latencySampleCapMigrationVersion).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestLatencySampleCapMigrationTrimsOnlyPointBlobAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	db := capMigrationDB(t)
	rows := make([]entities.UsageLatencyStat, 0, 3)
	var expectedFirst []latency.SamplePoint
	for _, size := range []int{2500, 1000, 3} {
		row, points := capRow(t, int64(len(rows)+1), size, now)
		if size == 3 {
			// 旧解码器允许非最短 count varint；未超限桶必须保留其原始字节。
			row.SamplePoints = append([]byte{latency.FormatVersion, 0x83, 0x00}, row.SamplePoints[2:]...)
			if _, err := latency.UnmarshalSampleSet(row.SamplePoints); err != nil {
				t.Fatalf("nonminimal legacy count should remain valid: %v", err)
			}
		}
		rows = append(rows, row)
		if len(rows) == 1 {
			expectedFirst = points[:1000]
		}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-3 * time.Hour)
	checkpoint := entities.UsageAggregationCheckpoint{
		Name: entities.UsageAggregationCheckpointLatency, LastAggregatedUsageEventID: 12345,
		StatsUpdatedAt: &stamp, CreatedAt: stamp, UpdatedAt: stamp,
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	var beforeCheckpoint entities.UsageAggregationCheckpoint
	if err := db.First(&beforeCheckpoint, "name = ?", checkpoint.Name).Error; err != nil {
		t.Fatal(err)
	}
	before := make([]entities.UsageLatencyStat, len(rows))
	for index, row := range rows {
		before[index] = capStoredRow(t, db, row.ID)
	}
	backups := 0
	options := migration.RunOptions{BeforeDestructiveMigration: func(_ context.Context, version string) error {
		if version != latencySampleCapMigrationVersion {
			t.Fatalf("unexpected backup version %q", version)
		}
		backups++
		return nil
	}}
	if err := migration.Run(db, options); err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		got := capStoredRow(t, db, row.ID)
		want := before[index]
		if index == 0 {
			want.SamplePoints = capEncodePoints(expectedFirst, false)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("row %d changed fields outside the expected point prefix", row.ID)
		}
	}
	var afterCheckpoint entities.UsageAggregationCheckpoint
	if err := db.First(&afterCheckpoint, "name = ?", checkpoint.Name).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterCheckpoint, beforeCheckpoint) {
		t.Fatal("latency checkpoint changed during sample cap migration")
	}
	if backups != 1 || capMigrationCount(t, db) != 1 {
		t.Fatalf("backup calls=%d version count=%d", backups, capMigrationCount(t, db))
	}
	if err := migration.Run(db, options); err != nil {
		t.Fatal(err)
	}
	if backups != 1 || capMigrationCount(t, db) != 1 {
		t.Fatal("idempotent rerun generated another backup or version row")
	}
}

func TestLatencySampleCapMigrationRejectsLateCorruptionAndRollsBackFirstPage(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	db := capMigrationDB(t)
	rows := make([]entities.UsageLatencyStat, 0, 201)
	for id := int64(1); id <= 201; id++ {
		size := 1
		if id == 1 {
			size = 2500
		}
		row, points := capRow(t, id, size, now)
		if id == 201 {
			row.SamplePoints = capEncodePoints(points, true)
		}
		rows = append(rows, row)
	}
	if err := db.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	first := capStoredRow(t, db, 1)
	if err := migration.Run(db, migration.RunOptions{BeforeDestructiveMigration: func(context.Context, string) error { return nil }}); err == nil {
		t.Fatal("expected corrupt row after the first page to abort migration")
	}
	if got := capStoredRow(t, db, 1); !bytes.Equal(got.SamplePoints, first.SamplePoints) {
		t.Fatal("first-page point update survived failed migration")
	}
	if capMigrationCount(t, db) != 0 {
		t.Fatal("failed migration recorded its version")
	}
}

func TestLatencySampleCapMigrationBackupAndUpdateFailuresLeaveRowsUntouched(t *testing.T) {
	for _, failure := range []string{"backup", "update"} {
		t.Run(failure, func(t *testing.T) {
			now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
			db := capMigrationDB(t)
			row, _ := capRow(t, 1, 2500, now)
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			before := capStoredRow(t, db, 1)
			backupFailure := errors.New("backup failed")
			if failure == "update" {
				if err := db.Exec(`CREATE TRIGGER fail_sample_cap_update BEFORE UPDATE OF sample_points ON usage_latency_stats BEGIN SELECT RAISE(ABORT, 'forced update failure'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			err := migration.Run(db, migration.RunOptions{BeforeDestructiveMigration: func(context.Context, string) error {
				if failure == "backup" {
					return backupFailure
				}
				return nil
			}})
			if err == nil || (failure == "backup" && !errors.Is(err, backupFailure)) {
				t.Fatalf("expected %s failure, got %v", failure, err)
			}
			if got := capStoredRow(t, db, 1); !reflect.DeepEqual(got, before) {
				t.Fatal("failed migration changed persisted row")
			}
			if capMigrationCount(t, db) != 0 {
				t.Fatal("failed migration recorded its version")
			}
		})
	}
}

func TestLatencyStorageRejectsNew2500PointWriteAndDetectsLegacyTailConflict(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	db := capMigrationDB(t)
	old, points := capRow(t, 1, 2500, now)
	toWrite := old
	toWrite.ID = 0
	if err := latencystore.WritePreparedRows(db, []entities.UsageLatencyStat{toWrite}); err == nil {
		t.Fatal("new storage write accepted a 2500-point row")
	}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	conflict := points[1500]
	conflict.TTFTMS++
	incoming := old
	incoming.ID = 0
	incoming.SampleCount = 1
	incoming.SamplePoints = capEncodePoints([]latency.SamplePoint{conflict}, false)
	ttft, total := latency.NewSketch(), latency.NewSketch()
	if err := ttft.Add(conflict.TTFTMS); err != nil {
		t.Fatal(err)
	}
	if err := total.Add(conflict.LatencyMS); err != nil {
		t.Fatal(err)
	}
	incoming.TTFTSketch, _ = ttft.MarshalBinary()
	incoming.LatencySketch, _ = total.MarshalBinary()
	if _, err := latencystore.PrepareRows(db, []entities.UsageLatencyStat{incoming}, now); err == nil {
		t.Fatal("legacy point beyond the new cap lost conflict detection")
	}
	valid, _ := capRow(t, 2, 1, now)
	valid.ID = 0
	valid.APIGroupKey = old.APIGroupKey
	valid.BucketStart = old.BucketStart
	prepared, err := latencystore.PrepareRows(db, []entities.UsageLatencyStat{valid}, now)
	if err != nil {
		t.Fatalf("merge new point into old 2500-point row: %v", err)
	}
	if len(prepared) != 1 || prepared[0].SampleCount != 2501 {
		t.Fatalf("merged legacy row has wrong count: %+v", prepared)
	}
	if err := latencystore.WritePreparedRows(db, prepared); err != nil {
		t.Fatalf("write merged legacy row: %v", err)
	}
	stored := capStoredRow(t, db, 1)
	decoded, err := latency.UnmarshalSampleSet(stored.SamplePoints)
	if err != nil || decoded.Count() != 1000 || stored.SampleCount != 2501 {
		t.Fatalf("stored merge has points=%d count=%d err=%v", decoded.Count(), stored.SampleCount, err)
	}
}
