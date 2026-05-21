package entities

import "time"

// UsageEvent 是落库后的单条 usage 请求事件实体。
type UsageEvent struct {
	ID                  int64     `gorm:"primaryKey;index:idx_usage_events_timestamp_id,sort:desc,priority:2;index:idx_usage_events_auth_type_auth_index_id,priority:3"`
	EventKey            string    `gorm:"index:idx_usage_events_event_key"`
	APIGroupKey         string    `gorm:"index:idx_usage_events_api_group_key"`
	Provider            string    `gorm:"column:provider"`
	Endpoint            string    `gorm:"column:endpoint"`
	AuthType            string    `gorm:"column:auth_type;index:idx_usage_events_auth_type_auth_index_id,priority:1"`
	RequestID           string    `gorm:"column:request_id"`
	Model               string    `gorm:"index:idx_usage_events_model"`
	ModelAlias          *string   `gorm:"column:model_alias"`
	ReasoningEffort     string    `gorm:"column:reasoning_effort;not null;default:''"`
	Timestamp           time.Time `gorm:"serializer:storageTime;index:idx_usage_events_timestamp_id,sort:desc,priority:1"`
	Source              string
	AuthIndex           string `gorm:"index:idx_usage_events_auth_index;index:idx_usage_events_auth_type_auth_index_id,priority:2"`
	Failed              bool   `gorm:"index:idx_usage_events_failed"`
	LatencyMS           int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64 `gorm:"not null;default:0"`
	CacheCreationTokens int64 `gorm:"not null;default:0"`
	TotalTokens         int64
	CreatedAt           time.Time `gorm:"serializer:storageTime"`
}
