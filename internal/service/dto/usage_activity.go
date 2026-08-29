package dto

import (
	"time"
)

// UsageActivityWindow 同时承载标准展示档位和归一化为 Day 视图的自然日请求模式。
type UsageActivityWindow string

const (
	UsageActivityWindowDay   UsageActivityWindow = "day"
	UsageActivityWindowWeek  UsageActivityWindow = "week"
	UsageActivityWindowMonth UsageActivityWindow = "month"
	UsageActivityWindowYear  UsageActivityWindow = "year"
	// Today 与 Yesterday 只用于请求，响应统一返回 Day 档位。
	UsageActivityWindowToday     UsageActivityWindow = "today"
	UsageActivityWindowYesterday UsageActivityWindow = "yesterday"
)

// UsageActivityBlock 是 Request Health 与 Token Activity 共用的真实半开时间块。
type UsageActivityBlock struct {
	StartTime           time.Time
	EndTime             time.Time
	Success             int64
	Failure             int64
	Rate                float64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

// UsageActivitySnapshot 是 Recent Activity 接口的服务层结果。
type UsageActivitySnapshot struct {
	Window              UsageActivityWindow
	Grain               string
	Rows                int
	Columns             int
	BucketSeconds       int64
	WindowStart         time.Time
	WindowEnd           time.Time
	TotalSuccess        int64
	TotalFailure        int64
	SuccessRate         float64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	Blocks              []UsageActivityBlock
}
