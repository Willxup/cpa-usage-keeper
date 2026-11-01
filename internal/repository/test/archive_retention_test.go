package test

import (
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestArchiveRetentionCleanup(t *testing.T) {
	for _, days := range []int{0, -1, 30, 89, 90, 180, int(^uint(0) >> 1)} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 3, 9, 4, 30, 0, 0, time.Local)
			cutoff := time.Date(2026, 3, 9, 0, 0, 0, 0, time.Local).AddDate(0, 0, -180)
			rows := make([]entities.UsageEventArchive, 1200)
			for i := range rows {
				// 交错时间顺序，验证分批扫描不能假设 ID 与请求时间同序。
				at := cutoff.Add(-time.Second)
				if i%2 == 0 {
					at = cutoff
				}
				rows[i] = entities.UsageEventArchive{ID: int64(i + 1), EventKey: fmt.Sprint(i), Timestamp: at}
			}
			if err := db.CreateInBatches(rows, 10).Error; err != nil {
				t.Fatal(err)
			}
			hot := entities.UsageEvent{EventKey: "recent", Timestamp: now.AddDate(0, 0, -60)}
			if err := db.Create(&hot).Error; err != nil {
				t.Fatal(err)
			}
			result, err := repository.CleanupStorage(db, now, days)
			if err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if days == 90 {
				want = 1200
			}
			if days == 180 {
				want = 600
			}
			if result.UsageEventsArchiveDeleted != want {
				t.Fatalf("deleted %d, want %d", result.UsageEventsArchiveDeleted, want)
			}
			var count int64
			if err := db.Model(&entities.UsageEventArchive{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1200-want {
				t.Fatalf("remaining %d", count)
			}
			if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatal("hot event deleted")
			}
			again, err := repository.CleanupStorage(db, now, days)
			if err != nil || again.UsageEventsArchiveDeleted != 0 {
				t.Fatalf("repeat: %+v %v", again, err)
			}
		})
	}
}

func TestArchiveRetentionKeepsCutoffDayInHotTable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		now    time.Time
		cutoff time.Time
	}{
		{
			name:   "spring-dst",
			now:    time.Date(2026, 5, 15, 4, 30, 0, 0, time.Local),
			cutoff: time.Date(2026, 2, 14, 0, 0, 0, 0, time.Local),
		},
		{
			name:   "fall-dst",
			now:    time.Date(2026, 11, 15, 4, 30, 0, 0, time.Local),
			cutoff: time.Date(2026, 8, 17, 0, 0, 0, 0, time.Local),
		},
	} {
		for _, retentionDays := range []int{0, 90, 180} {
			t.Run(fmt.Sprintf("%s/retention_%d", tc.name, retentionDays), func(t *testing.T) {
				db := openTestDatabase(t)
				// 真实序列化覆盖 UTC 零点后的小数秒，确认归档与删除使用同一自然日边界。
				events := []entities.UsageEvent{
					{EventKey: "before", Timestamp: tc.cutoff.Add(-time.Nanosecond)},
					{EventKey: "exact", Timestamp: tc.cutoff},
					{EventKey: "plus-1ns", Timestamp: tc.cutoff.Add(time.Nanosecond)},
					{EventKey: "plus-500ms", Timestamp: tc.cutoff.Add(500 * time.Millisecond)},
					{EventKey: "plus-1s", Timestamp: tc.cutoff.Add(time.Second)},
				}
				if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&entities.UsageEventArchive{
					ID: 1000, EventKey: "previous-archive", Timestamp: tc.cutoff.AddDate(0, 0, -1),
				}).Error; err != nil {
					t.Fatal(err)
				}
				seedArchiveCaughtUpCheckpoints(t, db, eventsMaxID(t, db), tc.now)

				result, err := repository.CleanupStorage(db, tc.now, retentionDays)
				if err != nil {
					t.Fatal(err)
				}
				if result.UsageEventsArchived != 1 {
					t.Errorf("archived %d, want only the event before cutoff", result.UsageEventsArchived)
				}
				var hotKeys, archiveKeys []string
				if err := db.Model(&entities.UsageEvent{}).Order("id").Pluck("event_key", &hotKeys).Error; err != nil {
					t.Fatal(err)
				}
				if want := []string{"exact", "plus-1ns", "plus-500ms", "plus-1s"}; !slices.Equal(hotKeys, want) {
					t.Errorf("hot events %v, want %v", hotKeys, want)
				}
				if err := db.Model(&entities.UsageEventArchive{}).Order("id").Pluck("event_key", &archiveKeys).Error; err != nil {
					t.Fatal(err)
				}
				wantArchive := []string{"before", "previous-archive"}
				var wantDeleted int64
				if retentionDays == 90 {
					wantArchive = nil
					wantDeleted = 2
				}
				if !slices.Equal(archiveKeys, wantArchive) || result.UsageEventsArchiveDeleted != wantDeleted {
					t.Errorf("archive events %v, deleted %d; want %v, deleted %d", archiveKeys, result.UsageEventsArchiveDeleted, wantArchive, wantDeleted)
				}
			})
		}
	}
}

// 用真实存储序列化覆盖 UTC 小数秒及夏令时切换后的自然日边界。
func TestArchiveRetentionPreservesCutoffDaySubseconds(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			location, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			previous := time.Local
			time.Local = location
			t.Cleanup(func() { time.Local = previous })
			db := openTestDatabase(t)
			// 2026-03-08 是纽约夏令时起始日；固定自然日而非 180*24h。
			now := time.Date(2026, 9, 4, 4, 30, 0, 0, location)
			cutoff := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
			offsets := []time.Duration{-time.Nanosecond, 0, time.Nanosecond, 500 * time.Millisecond, 23 * time.Hour}
			for i, offset := range offsets {
				row := entities.UsageEventArchive{ID: int64(i + 1), EventKey: fmt.Sprint(i), Timestamp: cutoff.Add(offset)}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			result, err := repository.CleanupStorage(db, now, 180)
			if err != nil {
				t.Fatal(err)
			}
			if result.UsageEventsArchiveDeleted != 1 {
				t.Fatalf("deleted %d, want only the record before cutoff", result.UsageEventsArchiveDeleted)
			}
			var ids []int64
			if err := db.Model(&entities.UsageEventArchive{}).Order("id").Pluck("id", &ids).Error; err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids, []int64{2, 3, 4, 5}) {
				t.Fatalf("retained IDs %v, want [2 3 4 5]", ids)
			}
		})
	}
}
