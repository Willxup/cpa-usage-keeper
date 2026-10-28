package repository

import (
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// ListUsageAPIKeyOptions 只按页面与时间判断 Key 是否使用过，不叠加已选 Key 或事件维度。
func ListUsageAPIKeyOptions(db *gorm.DB, scope string, filter dto.UsageQueryFilter, cache *UsageRecentEventCache) ([]entities.CPAAPIKey, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	keys, err := ListActiveCPAAPIKeys(db)
	if err != nil {
		return nil, err
	}
	used := make(map[string]struct{})
	stats := func(start, end time.Time, grain string) error {
		if !start.Before(end) {
			return nil
		}
		table := "usage_overview_hourly_stats"
		if grain == "daily" {
			table = "usage_overview_daily_stats"
		}
		query := overviewBucketRead(start, end, grain).query(db.Table(table)).
			Select("1").Where("api_group_key = cpa_api_keys.api_key AND request_count > 0")
		var matches []entities.CPAAPIKey
		if err := db.Model(&entities.CPAAPIKey{}).Where("is_deleted = ? AND api_key <> ?", false, "").Where("EXISTS (?)", query).Find(&matches).Error; err != nil {
			return err
		}
		for _, key := range matches {
			used[key.APIKey] = struct{}{}
		}
		return nil
	}
	events := func(start, end time.Time, includeEnd bool) error {
		for _, key := range keys {
			if _, found := used[key.APIKey]; found || key.APIKey == "" {
				continue
			}
			found, err := usageAPIKeyHasEvent(db, key.APIKey, start, end, includeEnd)
			if err != nil {
				return err
			}
			if found {
				used[key.APIKey] = struct{}{}
			}
		}
		return nil
	}
	mergeCache := func(start, end time.Time, includeEnd, openEnd bool) bool {
		found, ok := cache.UsedAPIKeys(start, end, includeEnd, openEnd)
		if ok {
			for key := range found {
				used[key] = struct{}{}
			}
		}
		return ok
	}
	if scope == "realtime" {
		window, _ := usageOverviewRealtimeWindow(filter.RealtimeWindow)
		end := usageOverviewQueryNow(filter)
		if filter.RealtimeEndTime != nil {
			end = *filter.RealtimeEndTime
		}
		start := end.Add(-window)
		// 只读取可见实时窗口，不把图表平滑预热区的 Key 当成当前用量。
		if !mergeCache(start, end, false, false) {
			err = events(start, end, false)
		}
	} else {
		if filter.StartTime == nil || filter.EndTime == nil {
			return nil, fmt.Errorf("API key options require start_time and end_time")
		}
		switch scope {
		case "request-events":
			err = events(*filter.StartTime, *filter.EndTime, !filter.EndExclusive)
		case "analysis":
			if analysisUsesDailyStats(filter) {
				start, end := *filter.StartTime, *filter.EndTime
				if timeutil.IsUsageRollingDayRange(filter.Range) {
					start, end = analysisRollingDayWindow(start, end)
				}
				err = stats(start, end, "daily")
			} else {
				start, end := usageOverviewFullHourWindow(*filter.StartTime, *filter.EndTime)
				err = stats(start, analysisHourlyStatsEnd(filter, end), "hourly")
			}
		case "overview":
			if filter.Range == "custom" && (filter.CustomUnit == "day" || filter.CustomUnit == "hour") {
				grain := "hourly"
				if filter.CustomUnit == "day" {
					grain = "daily"
				}
				err = stats(*filter.StartTime, *filter.EndTime, grain)
				break
			}
			now := usageOverviewQueryNow(filter)
			effective := usageOverviewEffectiveFilter(filter, now)
			start, end := usageOverviewFullHourWindow(*effective.StartTime, *effective.EndTime)
			dayStart, dayEnd := usageOverviewFullDayWindow(start, end)
			if shouldBucketUsageOverviewByDay(effective, computeWindowMinutes(effective)) && dayStart.Before(dayEnd) {
				if err = stats(dayStart, dayEnd, "daily"); err != nil {
					break
				}
				if err = stats(start, dayStart, "hourly"); err != nil {
					break
				}
				err = stats(dayEnd, end, "hourly")
			} else {
				err = stats(start, end, "hourly")
			}
			if err != nil {
				break
			}
			// 复用 Overview 窄边界与当前右边界的语义，不扩大为整段事件扫描。
			for _, window := range usageOverviewRawEventWindows(effective, start, end, usageOverviewCurrentRightBoundary(filter, now)) {
				if usageOverviewRecentCacheCoversWindow(cache, window, now) && mergeCache(window.start, window.end, window.includeEnd, window.currentRight) {
					continue
				}
				if err = events(window.start, window.end, window.includeEnd); err != nil {
					break
				}
			}
		default:
			return nil, fmt.Errorf("invalid API key options scope %q", scope)
		}
	}
	if err != nil {
		return nil, err
	}
	result := make([]entities.CPAAPIKey, 0, len(keys))
	for _, key := range keys {
		if _, ok := used[key.APIKey]; ok && strings.TrimSpace(key.APIKey) != "" {
			result = append(result, key)
		}
	}
	return result, nil
}

// 索引候选范围在 UTC 小数秒和 DST 回拨处可能略宽。逐条校验时间，命中即停；
// 不读取事件实体，也不能直接把粗范围 EXISTS 当成真实时间范围内存在。
func usageAPIKeyHasEvent(db *gorm.DB, key string, start, end time.Time, includeEnd bool) (bool, error) {
	for _, read := range usageEventTimeReads(start, end, includeEnd) {
		query := read.query(db.Model(&entities.UsageEvent{})).Where("api_group_key = ?", key).Select("timestamp")
		rows, err := query.Rows()
		if err != nil {
			return false, err
		}
		found := false
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				break
			}
			var at time.Time
			at, err = timeutil.ParseStorageTime(value)
			if err != nil {
				break
			}
			if read.contains(at) {
				found = true
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err != nil {
			return false, err
		}
		if closeErr != nil {
			return false, closeErr
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}
