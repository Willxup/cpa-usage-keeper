package migration

import (
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	overviewSixDimensions = "bucket_start, api_group_key, model, auth_index, service_tier, reasoning_effort"
	// 固定本版本的物理结构；暂存时不能创建与两张旧表同名的 SQLite 索引。
	overviewSixDimensionsTableSQL = ` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bucket_start DATETIME NOT NULL,
		api_group_key TEXT NOT NULL,
		model TEXT NOT NULL,
		auth_index TEXT NOT NULL DEFAULT '',
		service_tier TEXT NOT NULL DEFAULT '',
		reasoning_effort TEXT NOT NULL DEFAULT '',
		request_count INTEGER NOT NULL DEFAULT 0,
		success_count INTEGER NOT NULL DEFAULT 0,
		failure_count INTEGER NOT NULL DEFAULT 0,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		cache_read_tokens INTEGER NOT NULL DEFAULT 0,
		cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		speed_tps_sum REAL NOT NULL DEFAULT 0,
		speed_sample_count INTEGER NOT NULL DEFAULT 0,
		decode_speed_tps_sum REAL NOT NULL DEFAULT 0,
		decode_speed_sample_count INTEGER NOT NULL DEFAULT 0,
		cost_usd REAL,
		unavailable_cost_count INTEGER,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`
)

var overviewSixDimensionsIntegerMetrics = [...]string{
	"request_count", "success_count", "failure_count",
	"input_tokens", "output_tokens", "reasoning_tokens", "cached_tokens", "cache_read_tokens", "cache_creation_tokens", "total_tokens",
	"unavailable_cost_count", "speed_sample_count", "decode_speed_sample_count",
}

var overviewSixDimensionsRealMetrics = [...]string{"cost_usd", "speed_tps_sum", "decode_speed_tps_sum"}

type overviewSixDimensionsTable struct {
	name            string
	bucketsPerBatch int
}

func (table overviewSixDimensionsTable) stagingName() string { return table.name + "__six_dimensions" }

// usageOverviewSixDimensionsMigration 直接合并现有汇总。启动期间仅 inbox 可并发写入，普通聚合尚未启动。
// 旧表一直保留到对账成功，复制每批归还 writer，最终同时切换两张表。不访问事件表或水位。
func usageOverviewSixDimensionsMigration(db *gorm.DB) error {
	tables := []overviewSixDimensionsTable{
		{name: "usage_overview_hourly_stats", bucketsPerBatch: 168},
		{name: "usage_overview_daily_stats", bucketsPerBatch: 30},
	}
	migrator := db.Migrator()
	hourlyLegacy := migrator.HasColumn(tables[0].name, "model_alias")
	dailyLegacy := migrator.HasColumn(tables[1].name, "model_alias")
	if hourlyLegacy != dailyLegacy {
		return fmt.Errorf("hourly and daily usage overview dimensions do not match")
	}
	if !hourlyLegacy {
		// 两表已原子切换，但进程可能尚未提交 schema_migrations 记录。
		return nil
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, table := range tables {
			// 任何未完成复制只留下暂存表；重试从不变的源表重新生成，避免重复累加。
			if err := tx.Exec("DROP TABLE IF EXISTS " + table.stagingName()).Error; err != nil {
				return fmt.Errorf("discard interrupted overview staging table %s: %w", table.stagingName(), err)
			}
			if err := tx.Exec("CREATE TABLE " + table.stagingName() + overviewSixDimensionsTableSQL).Error; err != nil {
				return fmt.Errorf("create overview staging table %s: %w", table.stagingName(), err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	sums, metrics := overviewSixDimensionsSums()
	type rowCounts struct{ before, after int64 }
	counts := make([]rowCounts, len(tables))
	for index, table := range tables {
		if err := copyOverviewSixDimensions(db, table, sums, metrics); err != nil {
			return err
		}
		before, after, err := verifyOverviewSixDimensionsTotals(db, table, sums)
		if err != nil {
			return err
		}
		counts[index] = rowCounts{before, after}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, table := range tables {
			if err := switchOverviewSixDimensionsTable(tx, table); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for index, table := range tables {
		logrus.WithFields(logrus.Fields{
			"table": table.name, "before_rows": counts[index].before, "after_rows": counts[index].after,
		}).Info("usage overview dimensions reduced")
	}
	return nil
}

func overviewSixDimensionsSums() (sums, columns string) {
	metrics := append(append([]string{}, overviewSixDimensionsIntegerMetrics[:]...), overviewSixDimensionsRealMetrics[:]...)
	// 不用 COALESCE：整组未完成费用回填时 SUM(NULL) 仍为 NULL，显式零费用则保留为零。
	return "SUM(" + strings.Join(metrics, "), SUM(") + ")", strings.Join(metrics, ", ")
}

func copyOverviewSixDimensions(db *gorm.DB, table overviewSixDimensionsTable, sums, metrics string) error {
	insertSQL := "INSERT INTO " + table.stagingName() + " (" + overviewSixDimensions + ", " + metrics + ", created_at, updated_at) " +
		"SELECT " + overviewSixDimensions + ", " + sums + ", MIN(created_at), MAX(updated_at) FROM " + table.name
	var lower string
	for first := true; ; first = false {
		// 用原始存储字符串翻页，避开 SQLite 驱动对 DATETIME 的解析；查询和分组都沿用原桶值。
		page := db.Table(table.name).Select("CAST(bucket_start AS TEXT)").Group("bucket_start").Order("bucket_start").Limit(table.bucketsPerBatch)
		if !first {
			page = page.Where("bucket_start > ?", lower)
		}
		var buckets []string
		if err := page.Scan(&buckets).Error; err != nil {
			return fmt.Errorf("read %s bucket ranges: %w", table.name, err)
		}
		if len(buckets) == 0 {
			return nil
		}
		upper := buckets[len(buckets)-1]
		where := " WHERE bucket_start <= ?"
		args := []any{upper}
		if !first {
			where += " AND bucket_start > ?"
			args = append(args, lower)
		}
		// 单条 INSERT SELECT 是一个短写事务；完整时间桶不会被拆到不同批次。
		if err := db.Exec(insertSQL+where+" GROUP BY "+overviewSixDimensions, args...).Error; err != nil {
			return fmt.Errorf("merge %s overview buckets through %s: %w", table.name, upper, err)
		}
		lower = upper
	}
}

type overviewSixDimensionsTotals struct {
	rows     int64
	integers [len(overviewSixDimensionsIntegerMetrics)]sql.NullInt64
	reals    [len(overviewSixDimensionsRealMetrics)]sql.NullFloat64
}

func readOverviewSixDimensionsTotals(db *gorm.DB, table, sums string) (overviewSixDimensionsTotals, error) {
	var totals overviewSixDimensionsTotals
	dest := []any{&totals.rows}
	for index := range totals.integers {
		dest = append(dest, &totals.integers[index])
	}
	for index := range totals.reals {
		dest = append(dest, &totals.reals[index])
	}
	if err := db.Table(table).Select("COUNT(*), " + sums).Row().Scan(dest...); err != nil {
		return totals, fmt.Errorf("reconcile overview table %s: %w", table, err)
	}
	return totals, nil
}

// verifyOverviewSixDimensionsTotals 在切换前对账，生产环境的全表只读扫描可走独立 reader。
func verifyOverviewSixDimensionsTotals(db *gorm.DB, table overviewSixDimensionsTable, sums string) (int64, int64, error) {
	before, err := readOverviewSixDimensionsTotals(db, table.name, sums)
	if err != nil {
		return 0, 0, err
	}
	after, err := readOverviewSixDimensionsTotals(db, table.stagingName(), sums)
	if err != nil {
		return 0, 0, err
	}
	if after.rows > before.rows || (before.rows > 0 && after.rows == 0) {
		return 0, 0, fmt.Errorf("%s overview row count invalid after merge: %d -> %d", table.name, before.rows, after.rows)
	}
	for index, name := range overviewSixDimensionsIntegerMetrics {
		if before.integers[index] != after.integers[index] {
			return 0, 0, fmt.Errorf("%s overview %s totals differ: %+v -> %+v", table.name, name, before.integers[index], after.integers[index])
		}
	}
	for index, name := range overviewSixDimensionsRealMetrics {
		a, b := before.reals[index], after.reals[index]
		if a.Valid != b.Valid || a.Valid && !overviewSixDimensionsClose(a.Float64, b.Float64) {
			return 0, 0, fmt.Errorf("%s overview %s totals differ: %+v -> %+v", table.name, name, a, b)
		}
	}
	return before.rows, after.rows, nil
}

func overviewSixDimensionsClose(a, b float64) bool {
	const absoluteTolerance, relativeTolerance = 1e-9, 1e-12
	return !math.IsInf(a, 0) && !math.IsInf(b, 0) && math.Abs(a-b) <= absoluteTolerance+relativeTolerance*math.Max(math.Abs(a), math.Abs(b))
}

func switchOverviewSixDimensionsTable(tx *gorm.DB, table overviewSixDimensionsTable) error {
	// SQLite 索引名在库内全局唯一；删旧表释放原名，再在同一事务内建回目标索引。
	for _, statement := range []string{
		"DROP TABLE " + table.name,
		"ALTER TABLE " + table.stagingName() + " RENAME TO " + table.name,
		"CREATE UNIQUE INDEX uniq_" + table.name + "_dimensions ON " + table.name + " (" + overviewSixDimensions + ")",
		"CREATE INDEX idx_" + table.name + "_bucket_start ON " + table.name + " (bucket_start)",
		"CREATE INDEX idx_" + table.name + "_api_bucket ON " + table.name + " (api_group_key, bucket_start)",
		"CREATE INDEX idx_" + table.name + "_api_model_bucket ON " + table.name + " (api_group_key, model, bucket_start)",
		"CREATE INDEX idx_" + table.name + "_auth_bucket ON " + table.name + " (auth_index, bucket_start)",
	} {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("switch %s to six overview dimensions: %w", table.name, err)
		}
	}
	return nil
}
