package dto

import "time"

// UsageQueryFilter 是仓储层的 usage 查询条件。
type UsageQueryFilter struct {
	Range      string
	CustomUnit string
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
	CursorMode      bool
	CursorTimestamp *time.Time
	CursorID        int64
	SkipTotalCount  bool
	Model           string
	AuthIndex       string
	AuthType        string
	APIGroupKey     string
	Result          string
	// ModelDimension 控制模型维度聚合口径；model 表示按真实模型名，alias 表示按模型别名。
	ModelDimension string
}

const (
	// ModelDimensionModel 保持既有按真实模型名聚合的行为。
	ModelDimensionModel = "model"
	// ModelDimensionAlias 改按模型别名聚合。
	ModelDimensionAlias = "alias"
)

const DefaultUsageEventsLimit = 100
