package entities

import "time"

// UsageEvent 是落库后的单条 usage 请求事件实体。
type UsageEvent struct {
	ID              uint      `gorm:"primaryKey;index:idx_usage_events_timestamp_id,sort:desc,priority:2;index:idx_usage_events_auth_type_auth_index_id,priority:3;index:idx_usage_events_auth_type_source_id,priority:3"`
	EventKey        string    `gorm:"uniqueIndex:uniq_usage_events_event_key"`
	APIGroupKey     string    `gorm:"index:idx_usage_events_trim_api_group_key,expression:TRIM(api_group_key)"`
	Provider        string    `gorm:"column:provider;index:idx_usage_events_trim_provider,expression:TRIM(provider)"`
	Endpoint        string    `gorm:"column:endpoint"`
	AuthType        string    `gorm:"column:auth_type;index:idx_usage_events_trim_auth_type,expression:TRIM(auth_type);index:idx_usage_events_auth_type_auth_index_id,priority:1;index:idx_usage_events_auth_type_source_id,priority:1"`
	RequestID       string    `gorm:"column:request_id"`
	Model           string    `gorm:"index:idx_usage_events_model;index:idx_usage_events_trim_model,expression:TRIM(model)"`
	ModelAlias      *string   `gorm:"column:model_alias"`
	Timestamp       time.Time `gorm:"serializer:storageTime;index:idx_usage_events_timestamp_id,sort:desc,priority:1"`
	Source          string    `gorm:"index:idx_usage_events_trim_source,expression:TRIM(source);index:idx_usage_events_auth_type_source_id,priority:2"`
	AuthIndex       string    `gorm:"index:idx_usage_events_trim_auth_index,expression:TRIM(auth_index);index:idx_usage_events_auth_type_auth_index_id,priority:2"`
	Failed          bool      `gorm:"index:idx_usage_events_failed"`
	LatencyMS       int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
	CreatedAt       time.Time `gorm:"serializer:storageTime"`
}
