package dto

import "time"

// UsageQueryFilter 是仓储层的 usage 查询条件。
type UsageQueryFilter struct {
	Range        string
	CustomUnit   string
	StartTime    *time.Time
	EndTime      *time.Time
	EndExclusive bool
	// QueryNow 固定仓储层一次查询里的当前时刻，避免边界补偿在同一请求内发生时间漂移。
	QueryNow        *time.Time
	RealtimeWindow  string
	RealtimeEndTime *time.Time
	Limit           int
	Page            int
	PageSize        int
	Offset          int
	Model           string
	AuthIndex       string
	APIGroupKey     string
	Result          string
}

const DefaultUsageEventsLimit = 100
