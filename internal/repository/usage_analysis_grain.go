package repository

import (
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"strings"
	"time"
)

// 页面与候选列表共用粒度判断，尤其保留 DST 长自然日及显式 Custom 粒度。
func analysisUsesDailyStats(filter dto.UsageQueryFilter) bool {
	daily := computeWindowMinutes(filter) > 24*60
	if daily && (filter.Range == "today" || filter.Range == "yesterday") {
		start := timeutil.NormalizeStorageTime(*filter.StartTime)
		day, exists := timeutil.DayBounds(timeutil.DateOf(start), start.Location())
		// 非整点自然日不能由绝对整小时桶准确表示。
		if exists && day.Start.Equal(day.Start.Truncate(time.Hour)) && day.End.Equal(day.End.Truncate(time.Hour)) {
			daily = false
		}
	}
	if strings.TrimSpace(filter.Range) == "custom" {
		switch strings.TrimSpace(filter.CustomUnit) {
		case "day":
			daily = true
		case "hour":
			daily = false
		}
	}
	return daily
}
