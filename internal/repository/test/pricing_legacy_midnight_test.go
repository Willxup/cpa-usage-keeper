package test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"
)

// 首次升级沿真实 M1–M6 路径重建。午夜不存在时，既有写桶公式可能生成前一日 23 点的 instant；
// M6 必须比较这个已存 instant，不能再次按它的日历日期归日。
func TestLegacyPricingMidnightTransitionFirstUpgradeAndResume(t *testing.T) {
	for _, tc := range []struct {
		zone  string
		year  int
		month time.Month
		day   int
	}{
		{"America/Santiago", 2026, time.September, 6},
		{"America/Sao_Paulo", 2018, time.November, 4},
		{"UTC", 2026, time.September, 6},
		{"Asia/Shanghai", 2026, time.September, 6},
	} {
		for _, resume := range []bool{false, true} {
			name := tc.zone + "/first"
			if resume {
				name = tc.zone + "/resume"
			}
			t.Run(name, func(t *testing.T) {
				location, err := time.LoadLocation(tc.zone)
				if err != nil {
					t.Fatal(err)
				}
				previous := time.Local
				time.Local = location
				t.Cleanup(func() { time.Local = previous })
				instant := time.Date(tc.year, tc.month, tc.day, 12, 0, 0, 0, location)
				fixture := openPublishedPricingOverviewFixture(t)
				if err := fixture.writer.Exec("UPDATE usage_events SET timestamp = ? WHERE id = 1", timeutil.FormatStorageTime(instant)).Error; err != nil {
					t.Fatal(err)
				}
				if err := fixture.writer.Exec("INSERT INTO usage_events_archive (id,event_key,model,auth_index,timestamp,input_tokens,output_tokens,cache_read_tokens,cache_creation_tokens,total_tokens,failed) VALUES (2,'cold-midnight','model-a','hot-a',?,50,10,0,0,60,0)", timeutil.FormatStorageTime(instant)).Error; err != nil {
					t.Fatal(err)
				}
				if err := fixture.writer.Exec("UPDATE usage_aggregation_checkpoints SET last_aggregated_usage_event_id = 2 WHERE name = 'overview'").Error; err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				baseline, err := repository.MigrateLegacyPricingEvents(ctx, fixture.writer, fixture.reader, fixture.backupDir, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if resume {
					if err := fixture.writer.Exec(`CREATE TRIGGER interrupt_midnight_m6 BEFORE UPDATE OF data_complete ON pricing_migration_state WHEN NEW.data_complete = 1 BEGIN SELECT RAISE(ABORT, 'interrupted after M6'); END`).Error; err != nil {
						t.Fatal(err)
					}
					if err := repository.CompleteLegacyPricingData(ctx, fixture.writer, fixture.reader, baseline); err == nil || !strings.Contains(err.Error(), "interrupted after M6") {
						t.Fatalf("expected interruption after successful M6 validation, got %v", err)
					}
					var failed entities.PricingMigrationState
					if err := fixture.reader.Take(&failed).Error; err != nil || failed.DataComplete || failed.Phase != "overview_rebuilding" {
						t.Fatalf("interrupted state: %+v %v", failed, err)
					}
					if err := fixture.writer.Exec("DROP TRIGGER interrupt_midnight_m6").Error; err != nil {
						t.Fatal(err)
					}
					closePublishedPricingPools(fixture.reader, fixture.writer)
					writer, reader, err := repository.OpenUnmigratedDatabasePools(config.Config{SQLitePath: fixture.path})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { closePublishedPricingPools(reader, writer) })
					fixture.writer, fixture.reader = writer, reader
					baseline, err = repository.MigrateLegacyPricingEvents(ctx, writer, reader, filepath.Join(t.TempDir(), "unused-backups"), time.Now())
					if err != nil {
						t.Fatalf("resume M1–M4: %v", err)
					}
				}
				if err := repository.CompleteLegacyPricingData(ctx, fixture.writer, fixture.reader, baseline); err != nil {
					t.Fatalf("first legacy upgrade must accept stored bucket instant: %v", err)
				}
				for _, table := range []string{"usage_overview_hourly_stats", "usage_overview_daily_stats"} {
					assertLegacyOverviewFee(t, fixture, table, "model-a", 0.0012, 0, 2, 180)
					var raw string
					if err := fixture.reader.Table(table).Select("bucket_start").Where("model = ?", "model-a").Scan(&raw).Error; err != nil {
						t.Fatal(err)
					}
					stored, err := timeutil.ParseStorageTime(raw)
					if err != nil {
						t.Fatal(err)
					}
					want := instant.Truncate(time.Hour)
					if table == "usage_overview_daily_stats" {
						want = time.Date(tc.year, tc.month, tc.day, 0, 0, 0, 0, location)
					}
					if !stored.Equal(want) {
						t.Fatalf("%s changed existing write semantics: got %s want %s", table, stored, want)
					}
				}
				var completed entities.PricingMigrationState
				if err := fixture.reader.Take(&completed).Error; err != nil || !completed.DataComplete {
					t.Fatalf("completion state: %+v %v", completed, err)
				}
			})
		}
	}
}
