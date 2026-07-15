package entities

import "time"

// UsageEvent 是落库后的单条 usage 请求事件实体。
type UsageEvent struct {
	ID                  int64 `gorm:"primaryKey;index:idx_usage_events_timestamp_id,sort:desc,priority:2;index:idx_usage_events_auth_type_auth_index_id,priority:3;index:idx_usage_events_auth_index_timestamp_id,priority:3"`
	EventKey            string
	APIGroupKey         string    `gorm:"index:idx_usage_events_api_group_key"`
	Provider            string    `gorm:"column:provider"`
	Endpoint            string    `gorm:"column:endpoint"`
	AuthType            string    `gorm:"column:auth_type;index:idx_usage_events_auth_type_auth_index_id,priority:1"`
	RequestID           string    `gorm:"column:request_id"`
	Model               string    `gorm:"index:idx_usage_events_model"`
	ModelAlias          *string   `gorm:"column:model_alias"`
	ReasoningEffort     string    `gorm:"column:reasoning_effort;not null;default:''"`
	ServiceTier         string    `gorm:"column:service_tier;not null;default:''"`
	ExecutorType        string    `gorm:"column:executor_type;not null;default:''"`
	Timestamp           time.Time `gorm:"serializer:storageTime;index:idx_usage_events_timestamp_id,sort:desc,priority:1;index:idx_usage_events_auth_index_timestamp_id,priority:2"`
	Source              string
	AuthIndex           string `gorm:"index:idx_usage_events_auth_index;index:idx_usage_events_auth_type_auth_index_id,priority:2;index:idx_usage_events_auth_index_timestamp_id,priority:1"`
	Failed              bool
	LatencyMS           int64
	TTFTMS              *int64 `gorm:"column:ttft_ms"`
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64 `gorm:"not null;default:0"`
	CacheReadPresent    bool  `gorm:"-" json:"-"` // 仅在 Redis 入库归一化期间区分 CPA canonical zero 与旧 payload。
	CacheCreationTokens int64 `gorm:"not null;default:0"`
	TotalTokens         int64
	CreatedAt           time.Time `gorm:"serializer:storageTime"`
}
