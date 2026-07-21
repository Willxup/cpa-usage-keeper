package dto

import (
	"time"

	"cpa-usage-keeper/internal/entities"
)

// UsageActivityBlockRecord 保存 Activity 热力图一个真实半开时间块的请求与 Token 累计。
type UsageActivityBlockRecord struct {
	// StartTime 直接来自统一 Activity bucket 起点。
	StartTime time.Time
	// EndTime 优先使用数据库行保存的真实 bucket_end，空桶使用同一边界 helper。
	EndTime time.Time
	// SuccessCount 是当前块成功请求数。
	SuccessCount int64
	// FailureCount 是当前块失败请求数。
	FailureCount int64
	// InputTokens 是当前块 canonical input_tokens。
	InputTokens int64
	// OutputTokens 是当前块 canonical output_tokens。
	OutputTokens int64
	// ReasoningTokens 是当前块 canonical reasoning_tokens。
	ReasoningTokens int64
	// CacheReadTokens 是当前块 canonical cache_read_tokens，不包含 cached_tokens。
	CacheReadTokens int64
	// CacheCreationTokens 是当前块 canonical cache_creation_tokens。
	CacheCreationTokens int64
	// TotalTokens 是当前块 canonical total_tokens。
	TotalTokens int64
}

// UsageActivityGridRecord 保存固定 7×52 Activity 窗口和补齐后的连续 blocks。
type UsageActivityGridRecord struct {
	// Grain 表示本次查询使用 short、medium、long 或 daily。
	Grain entities.UsageActivityGrain
	// Rows 固定为 7。
	Rows int
	// Columns 固定为 52。
	Columns int
	// BucketSeconds 保存当前窗口实际块宽的最大秒数，供客户端描述网格粒度。
	BucketSeconds int64
	// WindowStart 是第一个真实 bucket 起点。
	WindowStart time.Time
	// WindowEnd 是最后一个真实 bucket 终点。
	WindowEnd time.Time
	// TotalSuccess 是当前完整 Activity 窗口内所有返回行的成功请求总数。
	TotalSuccess int64
	// TotalFailure 是当前完整 Activity 窗口内所有返回行的失败请求总数。
	TotalFailure int64
	// InputTokens 是当前完整 Activity 窗口内 canonical input_tokens 总和。
	InputTokens int64
	// OutputTokens 是当前完整 Activity 窗口内 canonical output_tokens 总和。
	OutputTokens int64
	// ReasoningTokens 是当前完整 Activity 窗口内 canonical reasoning_tokens 总和。
	ReasoningTokens int64
	// CacheReadTokens 是当前完整 Activity 窗口内 canonical cache_read_tokens 总和。
	CacheReadTokens int64
	// CacheCreationTokens 是当前完整 Activity 窗口内 canonical cache_creation_tokens 总和。
	CacheCreationTokens int64
	// TotalTokens 是当前完整 Activity 窗口内 canonical total_tokens 总和。
	TotalTokens int64
	// Blocks 始终包含按时间升序排列的 364 个连续块。
	Blocks []UsageActivityBlockRecord
}
