package dto

import (
	"time"
)

// UsageActivityWindow 是 Recent Activity 接口允许的固定展示窗口。
type UsageActivityWindow string

const (
	UsageActivityWindow24H UsageActivityWindow = "24h"
	UsageActivityWindow7D  UsageActivityWindow = "7d"
	UsageActivityWindow30D UsageActivityWindow = "30d"
	UsageActivityWindow1Y  UsageActivityWindow = "1y"
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
