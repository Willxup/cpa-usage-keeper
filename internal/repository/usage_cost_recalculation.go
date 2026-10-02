package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

const (
	usageCostRecalculationPageSize = 1000
	usageCostRecalculationIDChunk  = 900 // 留在 SQLite 旧版 999 参数限制之内。
	usageCostRecalculationKey      = "bucket_start = ? AND api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ? AND service_tier = ? AND response_service_tier = ? AND reasoning_effort = ? AND endpoint = ? AND executor_type = ?"
	// 与 overview 的 strings.TrimSpace 所用 Unicode White_Space 集合一致，不能退化成 SQLite 默认只去 ASCII 空格的 TRIM。
	usageCostRecalculationSpace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
	// SQLite 直接解析 .999999999 会舍入到下秒；去掉 fraction 后得到精确整秒，再按绝对小时分组。
	usageCostRecalculationHourSQL = "CAST(strftime('%s', substr(timestamp,1,19) || CASE WHEN substr(timestamp,-1) = 'Z' THEN 'Z' ELSE substr(timestamp,-6) END) AS INTEGER) / 3600"
	usageCostRecalculationDaySQL  = "substr(timestamp,1,10)"
)

// UsageCostRecalculationScope 固定本次 [Start,End) 的 CPA 实际时刻与停稳后热表 ID 上限。
// 整点、30 天、配置修订与 Overview 已追平由任务协调器先行保证。
type UsageCostRecalculationScope struct {
	Start      time.Time
	End        time.Time
	MaxEventID int64
}

// validate 只防止无效内部边界；整点、近 30 天及修订仍由受理层判定。
func (scope UsageCostRecalculationScope) validate() error {
	if scope.Start.IsZero() || !scope.Start.Before(scope.End) || scope.MaxEventID < 0 {
		return fmt.Errorf("invalid usage cost recalculation scope")
	}
	return nil
}

// LoadUsageCostRecalculationPage 从 Reader 按 ID 扫最多 1000 条热事件，并以 Go instant 筛选固定 S/T。
// cursor 是已扫描 ID 而非已选事件数；无目标事件的页仍推进，避免 ID 空洞或乱序时间漏读。
func LoadUsageCostRecalculationPage(ctx context.Context, reader *gorm.DB, scope UsageCostRecalculationScope, afterID int64) ([]entities.UsageEvent, int64, bool, error) {
	if reader == nil {
		return nil, 0, false, fmt.Errorf("usage cost recalculation reader is nil")
	}
	if err := scope.validate(); err != nil {
		return nil, 0, false, err
	}
	if afterID < 0 || afterID > scope.MaxEventID {
		return nil, 0, false, fmt.Errorf("invalid usage cost recalculation cursor %d", afterID)
	}
	if afterID == scope.MaxEventID {
		return []entities.UsageEvent{}, afterID, true, nil
	}
	var scanned []entities.UsageEvent
	if err := reader.Clauses(dbresolver.Read).WithContext(ctx).Model(&entities.UsageEvent{}).
		Select(entities.UsageAggregationEventProjectionColumns).
		Where("id > ? AND id <= ?", afterID, scope.MaxEventID).
		Order("id ASC").Limit(usageCostRecalculationPageSize).Find(&scanned).Error; err != nil {
		return nil, 0, false, fmt.Errorf("load usage cost recalculation page: %w", err)
	}
	next := scope.MaxEventID
	if len(scanned) > 0 {
		next = scanned[len(scanned)-1].ID
	}
	selected := make([]entities.UsageEvent, 0, len(scanned))
	for _, event := range scanned {
		if !event.Timestamp.Before(scope.Start) && event.Timestamp.Before(scope.End) {
			selected = append(selected, event)
		}
	}
	return selected, next, len(scanned) < usageCostRecalculationPageSize, nil
}

// CountUsageCostRecalculationEvents 用相同有界页和实际时刻筛选统计任务真实目标数，不假设 ID 连续。
func CountUsageCostRecalculationEvents(ctx context.Context, reader *gorm.DB, scope UsageCostRecalculationScope) (int64, error) {
	var count int64
	for after := int64(0); ; {
		events, next, done, err := LoadUsageCostRecalculationPage(ctx, reader, scope, after)
		if err != nil {
			return 0, err
		}
		count += int64(len(events))
		if done {
			return count, nil
		}
		after = next
	}
}

type usageCostRecalculationDelta struct {
	cost        float64
	unavailable int64
}

type usageCostRecalculationFee struct {
	cost      float64
	available bool
}

// ApplyUsageCostRecalculationBatch 由已停稳处理/聚合/维护且 Overview 覆盖 H 的协调器调用。
// 固定 Resolver 只计算本页新总费用；Writer 事务重读实际旧值并同事务更新事件、小时和日的费用差额。
// 任一写入失败整页回滚，已提交的早期页保留；不重放请求数、Token 或 checkpoint。
func ApplyUsageCostRecalculationBatch(ctx context.Context, writer *gorm.DB, scope UsageCostRecalculationScope, events []entities.UsageEvent, resolver pricing.Resolver) error {
	if writer == nil {
		return fmt.Errorf("usage cost recalculation writer is nil")
	}
	if err := scope.validate(); err != nil {
		return err
	}
	if len(events) > usageCostRecalculationPageSize {
		return fmt.Errorf("usage cost recalculation batch exceeds %d events", usageCostRecalculationPageSize)
	}
	if len(events) == 0 {
		return nil
	}
	fees := make(map[int64]usageCostRecalculationFee, len(events))
	for _, event := range events {
		if event.ID <= 0 || event.ID > scope.MaxEventID || event.Timestamp.Before(scope.Start) || !event.Timestamp.Before(scope.End) {
			return fmt.Errorf("usage cost recalculation event %d is outside fixed scope", event.ID)
		}
		if _, exists := fees[event.ID]; exists {
			return fmt.Errorf("usage cost recalculation event %d repeats in one batch", event.ID)
		}
		fee := resolver.CalculateFee(UsageEventCostSubject(event))
		if !finiteUsageRecalculationCost(fee.TotalCostUSD) {
			return fmt.Errorf("usage event %d recalculated cost is not finite", event.ID)
		}
		fees[event.ID] = usageCostRecalculationFee{cost: fee.TotalCostUSD, available: fee.Available}
	}
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		oldFees, err := loadUsageRecalculationOldFees(tx, events)
		if err != nil {
			return err
		}
		hourly := make(map[overview.BucketKey]usageCostRecalculationDelta)
		daily := make(map[overview.BucketKey]usageCostRecalculationDelta)
		for _, event := range events {
			old, exists := oldFees[event.ID]
			if !exists || old.CostUSD == nil || old.CostAvailable == nil || !finiteUsageRecalculationCost(*old.CostUSD) {
				return fmt.Errorf("usage event %d old cost is missing or not finite", event.ID)
			}
			newFee := fees[event.ID]
			delta := newFee.cost - *old.CostUSD
			if !finiteUsageRecalculationCost(delta) {
				return fmt.Errorf("usage event %d fee delta is not finite", event.ID)
			}
			unavailableDelta := int64(0)
			if !newFee.available {
				unavailableDelta++
			}
			if !*old.CostAvailable {
				unavailableDelta--
			}
			hourKey, dayKey := overview.BucketKeysForEvent(event)
			if err := addUsageRecalculationDelta(hourly, hourKey, delta, unavailableDelta); err != nil {
				return err
			}
			if err := addUsageRecalculationDelta(daily, dayKey, delta, unavailableDelta); err != nil {
				return err
			}
			result := tx.Model(&entities.UsageEvent{}).Where("id = ?", event.ID).
				UpdateColumns(map[string]any{"cost_usd": newFee.cost, "cost_available": newFee.available})
			if result.Error != nil {
				return fmt.Errorf("write usage event %d recalculated fee: %w", event.ID, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("usage event %d disappeared during recalculation", event.ID)
			}
		}
		for _, grain := range []struct {
			table string
			rows  map[overview.BucketKey]usageCostRecalculationDelta
		}{
			{"usage_overview_hourly_stats", hourly},
			{"usage_overview_daily_stats", daily},
		} {
			for key, delta := range grain.rows {
				if err := addUsageRecalculationBucketFee(tx, grain.table, key, delta); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type usageCostRecalculationOldFee struct {
	ID            int64
	CostUSD       *float64
	CostAvailable *bool
}

// loadUsageRecalculationOldFees 在 writer 事务中重读实际旧费用，IN 列表分片避开 999 参数上限。
func loadUsageRecalculationOldFees(tx *gorm.DB, events []entities.UsageEvent) (map[int64]usageCostRecalculationOldFee, error) {
	old := make(map[int64]usageCostRecalculationOldFee, len(events))
	for first := 0; first < len(events); first += usageCostRecalculationIDChunk {
		last := min(first+usageCostRecalculationIDChunk, len(events))
		ids := make([]int64, 0, last-first)
		for _, event := range events[first:last] {
			ids = append(ids, event.ID)
		}
		var rows []usageCostRecalculationOldFee
		if err := tx.Model(&entities.UsageEvent{}).Select("id, cost_usd, cost_available").Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, fmt.Errorf("read old usage cost: %w", err)
		}
		for _, row := range rows {
			old[row.ID] = row
		}
	}
	return old, nil
}

// addUsageRecalculationDelta 把本页同桶费用差额合并，溢出在进入事务提交前返回错误。
func addUsageRecalculationDelta(group map[overview.BucketKey]usageCostRecalculationDelta, key overview.BucketKey, cost float64, unavailable int64) error {
	previous := group[key]
	previous.cost += cost
	if !finiteUsageRecalculationCost(previous.cost) {
		return fmt.Errorf("usage overview fee delta is not finite")
	}
	previous.unavailable += unavailable
	group[key] = previous
	return nil
}

// addUsageRecalculationBucketFee 只写现有桶的费用列，缺桶或累计成非有限值使整页回滚。
func addUsageRecalculationBucketFee(tx *gorm.DB, table string, key overview.BucketKey, delta usageCostRecalculationDelta) error {
	result := tx.Table(table).Where(usageCostRecalculationKey, usageCostRecalculationKeyArgs(key)...).
		Where("cost_usd IS NOT NULL AND unavailable_cost_count IS NOT NULL AND cost_usd + ? BETWEEN ? AND ? AND unavailable_cost_count + ? >= 0", delta.cost, -math.MaxFloat64, math.MaxFloat64, delta.unavailable).
		Updates(map[string]any{
			"cost_usd":               gorm.Expr("cost_usd + ?", delta.cost),
			"unavailable_cost_count": gorm.Expr("unavailable_cost_count + ?", delta.unavailable),
		})
	if result.Error != nil {
		return fmt.Errorf("update %s recalculation delta: %w", table, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%s expected fee bucket is missing, unbackfilled or not finite", table)
	}
	return nil
}

// usageCostRecalculationKeyArgs 按 Overview 唯一索引全部维度定位小时或日的同一行。
func usageCostRecalculationKeyArgs(key overview.BucketKey) []any {
	return []any{timeutil.FormatStorageTime(key.BucketStart), key.APIGroupKey, key.Model, key.AuthIndex, key.ModelAlias,
		key.ServiceTier, key.ResponseServiceTier, key.ReasoningEffort, key.Endpoint, key.ExecutorType}
}

func finiteUsageRecalculationCost(cost float64) bool {
	return !math.IsNaN(cost) && !math.IsInf(cost, 0)
}

type usageCostRecalculationTotal struct {
	key         overview.BucketKey
	cost        float64
	unavailable int64
}

// FinalizeUsageCostRecalculationStats 只重算本次命中过事件的小时/自然日时间桶。
// Reader 各 grain 一次按完整维度 SUM 全桶已存事件费用（含 S/T 外贡献），Rows 流式每 1000 组交 Writer 短事务覆盖。
// 调用方保证事件批次已完成且处理/聚合/维护仍停稳；错误不撤销已提交批次，也不写持久任务游标。
func FinalizeUsageCostRecalculationStats(ctx context.Context, reader, writer *gorm.DB, scope UsageCostRecalculationScope) error {
	if reader == nil || writer == nil {
		return fmt.Errorf("usage cost recalculation reader/writer is nil")
	}
	if err := scope.validate(); err != nil {
		return err
	}
	hours := make(map[int64]struct{})
	days := make(map[string]struct{})
	for after := int64(0); ; {
		events, next, done, err := LoadUsageCostRecalculationPage(ctx, reader, scope, after)
		if err != nil {
			return err
		}
		for _, event := range events {
			hour, _ := overview.BucketKeysForEvent(event)
			hours[hour.BucketStart.Unix()/3600] = struct{}{}
			// 选择 SQL 本地日期使用事件本身；有些时区在零点跳时，time.Date 算出的桶起点可落在前一日。
			days[timeutil.NormalizeStorageTime(event.Timestamp).Format("2006-01-02")] = struct{}{}
		}
		if done {
			break
		}
		after = next
	}
	if len(hours) == 0 {
		return nil
	}
	// Rows 持续占用 Reader 连接；比较 Read/Write 实际物理池，允许同一业务 GORM 句柄经 dbresolver 分流。
	readSQL, readErr := reader.Clauses(dbresolver.Read).DB()
	writeSQL, writeErr := writer.Clauses(dbresolver.Write).DB()
	if readErr != nil || writeErr != nil {
		return fmt.Errorf("inspect usage cost recalculation pools: read=%v write=%v", readErr, writeErr)
	}
	if readSQL == writeSQL && readSQL.Stats().MaxOpenConnections == 1 {
		return fmt.Errorf("usage cost recalculation finalization requires an independent reader pool")
	}
	hourValues := make([]int64, 0, len(hours))
	for hour := range hours {
		hourValues = append(hourValues, hour)
	}
	sort.Slice(hourValues, func(i, j int) bool { return hourValues[i] < hourValues[j] })
	if err := overwriteUsageRecalculationGroups(ctx, reader, writer, "usage_overview_hourly_stats", usageCostRecalculationHourSQL, scope.MaxEventID, hourValues); err != nil {
		return err
	}
	dayValues := make([]string, 0, len(days))
	for day := range days {
		dayValues = append(dayValues, day)
	}
	sort.Strings(dayValues)
	return overwriteUsageRecalculationGroups(ctx, reader, writer, "usage_overview_daily_stats", usageCostRecalculationDaySQL, scope.MaxEventID, dayValues)
}

// SQL 仅使用已发布迁移归一化后的项目时区 RFC3339 时间；分组字符集与 Go strings.TrimSpace 一致。
// timestamp 的本地日期与 overview 自然日相同，绝对小时使用去小数后的 epoch 秒，DST 回拨两小时不合并。
func usageCostRecalculationGroupSelect(bucketSQL string) (string, []any) {
	columns := []string{"api_group_key", "model", "auth_index", "model_alias", "service_tier", "response_service_tier", "reasoning_effort", "endpoint", "executor_type"}
	parts := []string{bucketSQL + " AS bucket_key"}
	args := make([]any, 0, len(columns))
	for index, column := range columns {
		trimmed := "TRIM(COALESCE(" + column + ", ''), ?)"
		if index < 2 {
			trimmed = "COALESCE(NULLIF(" + trimmed + ", ''), 'unknown')"
		}
		parts = append(parts, trimmed)
		args = append(args, usageCostRecalculationSpace)
	}
	parts = append(parts,
		"SUM(cost_usd) AS total_cost",
		"SUM(CASE WHEN cost_available = 0 THEN 1 ELSE 0 END) AS unavailable_count",
		"SUM(CASE WHEN cost_usd IS NULL OR cost_available IS NULL THEN 1 ELSE 0 END) AS missing_count")
	return strings.Join(parts, ", "), args
}

// overwriteUsageRecalculationGroups 持续读取 SQL 分组结果，不把一个月的高维键装进 Go map。
// reader 可在 WAL 快照中保持 Rows，writer 每 1000 组独立提交并归还唯一写连接。
func overwriteUsageRecalculationGroups[T int64 | string](ctx context.Context, reader, writer *gorm.DB, table, bucketSQL string, maxEventID int64, buckets []T) error {
	selectSQL, selectArgs := usageCostRecalculationGroupSelect(bucketSQL)
	rows, err := reader.Clauses(dbresolver.Read).WithContext(ctx).Table("usage_events").
		Select(selectSQL, selectArgs...).Where("id <= ?", maxEventID).Where(bucketSQL+" IN ?", buckets).
		Group("1,2,3,4,5,6,7,8,9,10").Order("1,2,3,4,5,6,7,8,9,10").Rows()
	if err != nil {
		return fmt.Errorf("read %s recalculation groups: %w", table, err)
	}
	defer rows.Close()
	batch := make([]usageCostRecalculationTotal, 0, usageCostRecalculationPageSize)
	for rows.Next() {
		var bucket T
		var key overview.BucketKey
		var cost sql.NullFloat64
		var unavailable sql.NullInt64
		var missing int64
		if err := rows.Scan(&bucket, &key.APIGroupKey, &key.Model, &key.AuthIndex, &key.ModelAlias,
			&key.ServiceTier, &key.ResponseServiceTier, &key.ReasoningEffort, &key.Endpoint, &key.ExecutorType,
			&cost, &unavailable, &missing); err != nil {
			return fmt.Errorf("scan %s recalculation group: %w", table, err)
		}
		if missing != 0 || !cost.Valid || !unavailable.Valid || !finiteUsageRecalculationCost(cost.Float64) {
			return fmt.Errorf("%s recalculation group has missing or non-finite event fee", table)
		}
		switch value := any(bucket).(type) {
		case int64:
			key.BucketStart = time.Unix(value*3600, 0).In(time.Local)
		case string:
			// 先按纯历法解析日期，避免午夜跳时令 ParseInLocation 把该日期归到前一日。
			day, err := time.Parse("2006-01-02", value)
			if err != nil {
				return fmt.Errorf("parse recalculation day %q: %w", value, err)
			}
			key.BucketStart = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
		}
		batch = append(batch, usageCostRecalculationTotal{key: key, cost: cost.Float64, unavailable: unavailable.Int64})
		if len(batch) == usageCostRecalculationPageSize {
			if err := overwriteUsageRecalculationGroupBatch(ctx, writer, table, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("stream %s recalculation groups: %w", table, err)
	}
	return overwriteUsageRecalculationGroupBatch(ctx, writer, table, batch)
}

// overwriteUsageRecalculationGroupBatch 只覆盖费用两列；预期桶缺失时本组短事务回滚。
func overwriteUsageRecalculationGroupBatch(ctx context.Context, writer *gorm.DB, table string, batch []usageCostRecalculationTotal) error {
	if len(batch) == 0 {
		return nil
	}
	return writer.Clauses(dbresolver.Write).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, group := range batch {
			result := tx.Table(table).Where(usageCostRecalculationKey, usageCostRecalculationKeyArgs(group.key)...).
				Where("cost_usd IS NOT NULL AND unavailable_cost_count IS NOT NULL").
				Updates(map[string]any{"cost_usd": group.cost, "unavailable_cost_count": group.unavailable})
			if result.Error != nil {
				return fmt.Errorf("overwrite %s recalculation fee: %w", table, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%s expected fee bucket is missing or unbackfilled", table)
			}
		}
		return nil
	})
}
