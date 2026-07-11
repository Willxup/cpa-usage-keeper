package dto

import "time"

// UsageQueryFilter 是仓储层的 usage 查询条件。
type UsageQueryFilter struct {
	Range     string
	StartTime *time.Time
	EndTime   *time.Time
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
	// AuthIndexScopeEnforced 只由服务端的 API Key Viewer 认证文件访问范围写入。
	// Viewer 只允许读取 OAuth Auth File 事件；为 true 时即使 AllowedAuthIndexes 为空也必须拒绝全部数据，避免默认放行。
	AuthIndexScopeEnforced bool
	AllowedAuthIndexes     []string
}

const DefaultUsageEventsLimit = 100
