package test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/backup"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	overviewSixDimensionsVersion   = "20261123_usage_overview_six_dimensions"
	overviewHistoryExtraBuckets    = 169
	overviewHistoryRowsBeforeMerge = overviewHistoryExtraBuckets + 8
	overviewHistoryRowsAfterMerge  = overviewHistoryExtraBuckets + 5
)

var overviewTables = [...]string{"usage_overview_hourly_stats", "usage_overview_daily_stats"}
var overviewHistoryBuckets = [...]string{"2000-01-01T11:00:00+05:45", "2000-01-01T00:00:00+05:45"}

func TestUsageOverviewSixDimensionsMergesStoredBucketsAndRestoresBackup(t *testing.T) {
	db := openTenDimensionMigrationFixture(t)
	// 直接 Run 允许费用仍为 NULL。删掉热/冷事件表，明确证明迁移不依赖任何原始明细。
	for _, table := range []string{"usage_events", "usage_events_archive"} {
		mergeFixtureExec(t, db, "DROP TABLE "+table)
	}
	for i, table := range overviewTables {
		seedOverviewHistoryRows(t, db, table, overviewHistoryBuckets[i])
	}
	if err := db.AutoMigrate(&entities.UsageAggregationCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	stamp := "2026-11-22T23:57:01+05:45"
	mergeFixtureExec(t, db, `INSERT INTO usage_aggregation_checkpoints
		(name,last_aggregated_usage_event_id,stats_updated_at,created_at,updated_at)
		VALUES ('overview',7429,?,?,?),('activity',8121,?,?,?)`, stamp, stamp, stamp, stamp, stamp, stamp)
	checkpoints := readOverviewMergeCheckpoints(t, db)

	if err := migration.Run(db); err == nil || !strings.Contains(err.Error(), "destructive migration backup is required") {
		t.Fatalf("migration ran without required backup hook: %v", err)
	}
	backupFailure := errors.New("injected backup failure")
	if err := migration.Run(db, migration.RunOptions{BeforeDestructiveMigration: func(context.Context, string) error {
		return backupFailure
	}}); !errors.Is(err, backupFailure) {
		t.Fatalf("backup failure did not stop migration: %v", err)
	}
	var before [len(overviewTables)]overviewMergeTotals
	for i, table := range overviewTables {
		assertTenDimensionOverviewIndex(t, db, table)
		before[i] = readOverviewMergeTotals(t, db, table)
		if before[i].Rows != overviewHistoryRowsBeforeMerge || db.Migrator().HasTable(table+"__six_dimensions") {
			t.Fatalf("%s changed before successful backup: %+v", table, before[i])
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	backupWriter := backup.NewWriter(t.TempDir())
	var backupPath string
	var backups int
	options := migration.RunOptions{BeforeDestructiveMigration: func(ctx context.Context, version string) error {
		if version != overviewSixDimensionsVersion {
			t.Fatalf("unexpected destructive version %s", version)
		}
		for i, table := range overviewTables {
			assertTenDimensionOverviewIndex(t, db, table)
			assertOverviewMergeTotals(t, readOverviewMergeTotals(t, db, table), before[i], before[i].Rows)
		}
		backups++
		var err error
		backupPath, err = backupWriter.WriteDatabase(ctx, sqlDB, time.Now())
		return err
	}}
	if err := migration.Run(db, options); err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db, options); err != nil || backups != 1 {
		t.Fatalf("normal reentry should skip backup and writes: calls=%d err=%v", backups, err)
	}
	assertOverviewMergeVersionCount(t, db, 1)
	if got := readOverviewMergeCheckpoints(t, db); !reflect.DeepEqual(got, checkpoints) {
		t.Fatalf("checkpoints changed: got %v want %v", got, checkpoints)
	}
	for i, table := range overviewTables {
		assertSixDimensionOverviewSchema(t, db, table)
		after := readOverviewMergeTotals(t, db, table)
		assertOverviewMergeTotals(t, after, before[i], overviewHistoryRowsAfterMerge)
		assertMergedOverviewHistory(t, db, table, overviewHistoryBuckets[i])
		t.Logf("%s: %d -> %d rows", table, before[i].Rows, after.Rows)
	}

	// 从真实备份恢复到独立文件，验证旧索引、数据、水位和升级路径均可用。
	snapshot, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(restorePath, snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := gorm.Open(sqlite.Open(restorePath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	closeMigrationTestDatabase(t, restored)
	var quickCheck string
	if err := restored.Raw("PRAGMA quick_check").Row().Scan(&quickCheck); err != nil || quickCheck != "ok" {
		t.Fatalf("restored backup quick_check = %q, %v", quickCheck, err)
	}
	assertOverviewMergeVersionCount(t, restored, 0)
	if got := readOverviewMergeCheckpoints(t, restored); !reflect.DeepEqual(got, checkpoints) {
		t.Fatalf("restored checkpoints = %v, want %v", got, checkpoints)
	}
	for i, table := range overviewTables {
		assertTenDimensionOverviewIndex(t, restored, table)
		assertOverviewMergeTotals(t, readOverviewMergeTotals(t, restored, table), before[i], before[i].Rows)
	}
	// 已验证的备份用于这次测试恢复后的再次迁移，不需要多复制一份文件。
	if err := migration.Run(restored, migration.RunOptions{BeforeDestructiveMigration: func(context.Context, string) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	for i, table := range overviewTables {
		assertSixDimensionOverviewSchema(t, restored, table)
		assertOverviewMergeTotals(t, readOverviewMergeTotals(t, restored, table), before[i], overviewHistoryRowsAfterMerge)
	}
}

func TestUsageOverviewSixDimensionsRetriesFailedCutoverAndMarker(t *testing.T) {
	db := openTenDimensionMigrationFixture(t)
	for i, table := range overviewTables {
		seedOverviewHistoryRows(t, db, table, overviewHistoryBuckets[i])
	}
	options := migration.RunOptions{BeforeDestructiveMigration: func(context.Context, string) error { return nil }}

	// 让自然日最后一个索引创建失败，证明小时表改名和旧索引删除会随同回滚。
	const conflict = "idx_usage_overview_daily_stats_auth_bucket"
	mergeFixtureExec(t, db, "DROP INDEX "+conflict)
	mergeFixtureExec(t, db, "CREATE TABLE index_blocker (id INTEGER)")
	mergeFixtureExec(t, db, "CREATE INDEX "+conflict+" ON index_blocker (id)")
	if err := migration.Run(db, options); err == nil || !strings.Contains(err.Error(), conflict) {
		t.Fatalf("expected second table index conflict, got %v", err)
	}
	assertOverviewMergeVersionCount(t, db, 0)
	for _, table := range overviewTables {
		assertTenDimensionOverviewIndex(t, db, table)
		if got := readOverviewMergeTotals(t, db, table); got.Rows != overviewHistoryRowsBeforeMerge {
			t.Fatalf("%s lost original rows after failed cutover: %+v", table, got)
		}
	}

	// 保留失败尝试的暂存表，再中断版本登记；成功切换必须不重复累加，也不能留下半套索引。
	mergeFixtureExec(t, db, "DROP TABLE index_blocker")
	mergeFixtureExec(t, db, fmt.Sprintf(`CREATE TRIGGER interrupt_merge_record BEFORE INSERT ON schema_migrations
		WHEN NEW.version = '%s' BEGIN SELECT RAISE(ABORT, 'interrupted merge marker'); END`, overviewSixDimensionsVersion))
	if err := migration.Run(db, options); err == nil || !strings.Contains(err.Error(), "interrupted merge marker") {
		t.Fatalf("expected marker failure after atomic cutover, got %v", err)
	}
	assertOverviewMergeVersionCount(t, db, 0)
	var after [len(overviewTables)]overviewMergeTotals
	for i, table := range overviewTables {
		assertSixDimensionOverviewSchema(t, db, table)
		after[i] = readOverviewMergeTotals(t, db, table)
		if after[i].Rows != overviewHistoryRowsAfterMerge {
			t.Fatalf("%s retained interrupted copy rows: %+v", table, after[i])
		}
		assertMergedOverviewHistory(t, db, table, overviewHistoryBuckets[i])
	}
	mergeFixtureExec(t, db, "DROP TRIGGER interrupt_merge_record")
	if err := migration.Run(db, options); err != nil {
		t.Fatalf("recover missing marker on six dimensions: %v", err)
	}
	assertOverviewMergeVersionCount(t, db, 1)
	for i, table := range overviewTables {
		assertOverviewMergeTotals(t, readOverviewMergeTotals(t, db, table), after[i], overviewHistoryRowsAfterMerge)
	}
}

func openTenDimensionMigrationFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := openUnmigratedTestDatabase(t)
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatal(err)
	}
	mergeFixtureExec(t, db, "DELETE FROM schema_migrations WHERE version IN (?, ?)", pricingStorageStructureVersion, overviewSixDimensionsVersion)
	// 使用真正的 20261009 建表入口，证明新运行时实体没有污染该阶段的缺表分支。
	if err := migration.RunPricingStorageStructure(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func mergeFixtureExec(t *testing.T, db *gorm.DB, stmt string, args ...any) {
	t.Helper()
	if err := db.Exec(stmt, args...).Error; err != nil {
		t.Fatalf("execute %s: %v", stmt, err)
	}
}

func seedOverviewHistoryRows(t *testing.T, db *gorm.DB, table, historicBucket string) {
	t.Helper()
	const earlier, later = "2002-01-01T00:00:00+05:45", "2002-01-03T00:00:00+05:45"
	insert := func(bucket, tier, effort, tag, created, updated string, n int64, cost, missing any) {
		if err := db.Table(table).Create(map[string]any{
			"bucket_start": bucket, "api_group_key": "key", "model": "model", "auth_index": "auth",
			"service_tier": tier, "reasoning_effort": effort,
			"model_alias": "alias-" + tag, "response_service_tier": "response-" + tag, "endpoint": "/" + tag, "executor_type": "type-" + tag,
			"request_count": n * 3, "success_count": n * 2, "failure_count": n,
			"input_tokens": n * 10, "output_tokens": n * 9, "reasoning_tokens": n * 2, "cached_tokens": n * 8,
			"cache_read_tokens": n * 4, "cache_creation_tokens": n * 3, "total_tokens": n * 21,
			"speed_tps_sum": float64(n) * 12.3, "speed_sample_count": n * 3,
			"decode_speed_tps_sum": float64(n) * 13.7, "decode_speed_sample_count": n,
			"cost_usd": cost, "unavailable_cost_count": missing, "created_at": created, "updated_at": updated,
		}).Error; err != nil {
			t.Fatalf("seed %s %s: %v", table, tag, err)
		}
	}
	insert(historicBucket, "fast", "high", "a", later, later, 1, 0.1, 1)
	insert(historicBucket, "fast", "high", "b", earlier, earlier, 2, 0.2, 2)
	insert(historicBucket, "ultrafast", "high", "c", earlier, later, 1, 0.0, 0)
	insert(historicBucket, "fast", "low", "d", earlier, later, 1, 0.0, 3)
	insert(historicBucket, "", "high", "e", earlier, later, 1, nil, nil)
	insert(historicBucket, "", "high", "f", earlier, later, 1, nil, nil)
	insert(historicBucket, "fast", "", "g", earlier, later, 1, 0.0, 0)
	insert(historicBucket, "fast", "", "h", earlier, later, 1, 0.0, 0)
	// 两张表的独立时间桶数均跨过分页上限，并沿用旧时区的存储值。
	start := time.Date(2001, time.January, 1, 0, 0, 0, 0, time.FixedZone("NPT", 5*3600+45*60))
	for i := range overviewHistoryExtraBuckets {
		insert(start.AddDate(0, 0, i).Format(time.RFC3339), "fast", "high", "page", earlier, later, int64(i+1), 1.125, 1)
	}
}

type overviewMergeTotals struct {
	Rows                int64
	Counts              [12]int64
	Missing             sql.NullInt64
	Cost, Speed, Decode sql.NullFloat64
}

const overviewMergeTotalFields = `COUNT(*), SUM(request_count), SUM(success_count), SUM(failure_count),
	SUM(input_tokens), SUM(output_tokens), SUM(reasoning_tokens), SUM(cached_tokens), SUM(cache_read_tokens),
	SUM(cache_creation_tokens), SUM(total_tokens), SUM(speed_sample_count), SUM(decode_speed_sample_count),
	SUM(unavailable_cost_count), SUM(cost_usd), SUM(speed_tps_sum), SUM(decode_speed_tps_sum)`

func readOverviewMergeTotals(t *testing.T, db *gorm.DB, table string, where ...any) overviewMergeTotals {
	t.Helper()
	var totals overviewMergeTotals
	args := []any{&totals.Rows}
	for i := range totals.Counts {
		args = append(args, &totals.Counts[i])
	}
	args = append(args, &totals.Missing, &totals.Cost, &totals.Speed, &totals.Decode)
	query := db.Table(table).Select(overviewMergeTotalFields)
	if len(where) != 0 {
		query = query.Where(where[0], where[1:]...)
	}
	if err := query.Row().Scan(args...); err != nil {
		t.Fatalf("read %s totals: %v", table, err)
	}
	return totals
}

func assertOverviewMergeTotals(t *testing.T, got, want overviewMergeTotals, wantRows int64) {
	t.Helper()
	if got.Rows != wantRows || got.Counts != want.Counts || got.Missing != want.Missing {
		t.Fatalf("rows or integer totals differ: got %+v want %+v with %d rows", got, want, wantRows)
	}
	for _, pair := range [][2]sql.NullFloat64{{got.Cost, want.Cost}, {got.Speed, want.Speed}, {got.Decode, want.Decode}} {
		if pair[0].Valid != pair[1].Valid || pair[0].Valid && !(math.Abs(pair[0].Float64-pair[1].Float64) <= 1e-9) {
			t.Fatalf("REAL totals differ: got %+v want %+v", got, want)
		}
	}
}

func assertMergedOverviewHistory(t *testing.T, db *gorm.DB, table, bucket string) {
	t.Helper()
	got := readOverviewMergeTotals(t, db, table, "bucket_start = ? AND service_tier = 'fast' AND reasoning_effort = 'high'", bucket)
	assertOverviewMergeTotals(t, got, overviewMergeTotals{
		Counts: [12]int64{9, 6, 3, 30, 27, 6, 24, 12, 9, 63, 9, 3}, Missing: sql.NullInt64{Int64: 3, Valid: true},
		Cost: sql.NullFloat64{Float64: 0.3, Valid: true}, Speed: sql.NullFloat64{Float64: 36.9, Valid: true}, Decode: sql.NullFloat64{Float64: 41.1, Valid: true},
	}, 1)
	var stored, created, updated string
	if err := db.Table(table).Select("CAST(bucket_start AS TEXT), CAST(created_at AS TEXT), CAST(updated_at AS TEXT)").
		Where("bucket_start = ? AND service_tier = 'fast' AND reasoning_effort = 'high'", bucket).Row().Scan(&stored, &created, &updated); err != nil ||
		stored != bucket || created != "2002-01-01T00:00:00+05:45" || updated != "2002-01-03T00:00:00+05:45" {
		t.Fatalf("%s did not preserve stored time bounds: %q %q %q %v", table, stored, created, updated, err)
	}
	type group struct {
		ServiceTier, ReasoningEffort string
		RequestCount                 int64
		CostUSD                      sql.NullFloat64
		UnavailableCostCount         sql.NullInt64
	}
	var remaining []group
	if err := db.Table(table).Select("service_tier, reasoning_effort, request_count, cost_usd, unavailable_cost_count").
		Where("bucket_start = ? AND NOT (service_tier = 'fast' AND reasoning_effort = 'high')", bucket).
		Order("service_tier, reasoning_effort").Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	want := []group{
		{ServiceTier: "", ReasoningEffort: "high", RequestCount: 6},
		{ServiceTier: "fast", ReasoningEffort: "", RequestCount: 6, CostUSD: sql.NullFloat64{Valid: true}, UnavailableCostCount: sql.NullInt64{Valid: true}},
		{ServiceTier: "fast", ReasoningEffort: "low", RequestCount: 3, CostUSD: sql.NullFloat64{Valid: true}, UnavailableCostCount: sql.NullInt64{Int64: 3, Valid: true}},
		{ServiceTier: "ultrafast", ReasoningEffort: "high", RequestCount: 3, CostUSD: sql.NullFloat64{Valid: true}, UnavailableCostCount: sql.NullInt64{Valid: true}},
	}
	if len(remaining) != len(want) {
		t.Fatalf("%s kept wrong tier/effort groups: %+v", table, remaining)
	}
	for i, row := range remaining {
		expected := want[i]
		if row.ServiceTier != expected.ServiceTier || row.ReasoningEffort != expected.ReasoningEffort || row.RequestCount != expected.RequestCount ||
			row.UnavailableCostCount != expected.UnavailableCostCount || row.CostUSD.Valid != expected.CostUSD.Valid ||
			row.CostUSD.Valid && !(math.Abs(row.CostUSD.Float64-expected.CostUSD.Float64) <= 1e-12) {
			t.Fatalf("%s tier/effort group %+v, want %+v", table, row, expected)
		}
	}
}

func readOverviewMergeCheckpoints(t *testing.T, db *gorm.DB) []entities.UsageAggregationCheckpoint {
	t.Helper()
	var rows []entities.UsageAggregationCheckpoint
	if err := db.Order("name").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func assertOverviewMergeVersionCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", overviewSixDimensionsVersion).Count(&count).Error; err != nil || count != want {
		t.Fatalf("overview migration markers = %d, want %d: %v", count, want, err)
	}
}

func assertTenDimensionOverviewIndex(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	assertOverviewMergeIndex(t, db, "uniq_"+table+"_dimensions", "bucket_start,api_group_key,model,auth_index,model_alias,service_tier,response_service_tier,reasoning_effort,endpoint,executor_type")
}

func assertSixDimensionOverviewSchema(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	if db.Migrator().HasTable(table+"__six_dimensions") || db.Migrator().HasIndex(table, "idx_"+table+"_model_alias_bucket") {
		t.Fatalf("%s still has temporary table or legacy alias index", table)
	}
	for _, name := range []string{"model_alias", "response_service_tier", "endpoint", "executor_type"} {
		if db.Migrator().HasColumn(table, name) {
			t.Fatalf("%s still has column %s", table, name)
		}
	}
	index := "uniq_" + table + "_dimensions"
	assertOverviewMergeIndex(t, db, index, "bucket_start,api_group_key,model,auth_index,service_tier,reasoning_effort")
	var unique int
	if err := db.Raw(`SELECT "unique" FROM pragma_index_list(?) WHERE name = ?`, table, index).Row().Scan(&unique); err != nil || unique != 1 {
		t.Fatalf("%s is not unique: %d, %v", index, unique, err)
	}
	for suffix, columns := range map[string]string{
		"bucket_start": "bucket_start", "api_bucket": "api_group_key,bucket_start",
		"api_model_bucket": "api_group_key,model,bucket_start", "auth_bucket": "auth_index,bucket_start",
	} {
		assertOverviewMergeIndex(t, db, "idx_"+table+"_"+suffix, columns)
	}
}

func assertOverviewMergeIndex(t *testing.T, db *gorm.DB, index, want string) {
	t.Helper()
	var got string
	if err := db.Raw(`SELECT group_concat(name) FROM (SELECT name FROM pragma_index_info(?) ORDER BY seqno)`, index).Row().Scan(&got); err != nil || got != want {
		t.Fatalf("%s columns %q, want %q: %v", index, got, want, err)
	}
}
