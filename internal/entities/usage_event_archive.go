package entities

import "time"

// UsageEventStorageColumns 是 hot/archive 原始事件复制使用的完整持久化列契约。
const UsageEventStorageColumns = "id, event_key, api_group_key, provider, endpoint, auth_type, request_id, client_ip, x_forwarded_for, user_agent, model, model_alias, reasoning_effort, service_tier, response_service_tier, executor_type, timestamp, source, auth_index, failed, generate, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, created_at"

// UsageEventArchive 永久保存已经离开 hot usage_events 的原始事件。
// 字段必须与 UsageEvent 的持久化列保持一致，但 archive 不承担在线查询，因此不复制二级索引。
type UsageEventArchive struct {
	ID                  int64 `gorm:"primaryKey;autoIncrement:false"`
	EventKey            string
	APIGroupKey         string
	Provider            string  `gorm:"column:provider"`
	Endpoint            string  `gorm:"column:endpoint"`
	AuthType            string  `gorm:"column:auth_type"`
	RequestID           string  `gorm:"column:request_id"`
	ClientIP            *string `gorm:"column:client_ip"`
	XForwardedFor       *string `gorm:"column:x_forwarded_for"`
	UserAgent           *string `gorm:"column:user_agent"`
	Model               string
	ModelAlias          *string   `gorm:"column:model_alias"`
	ReasoningEffort     string    `gorm:"column:reasoning_effort;not null;default:''"`
	ServiceTier         string    `gorm:"column:service_tier;not null;default:''"`
	ResponseServiceTier string    `gorm:"column:response_service_tier;not null;default:''"`
	ExecutorType        string    `gorm:"column:executor_type;not null;default:''"`
	Timestamp           time.Time `gorm:"serializer:storageTime"`
	Source              string
	AuthIndex           string
	Failed              bool
	Generate            *bool `gorm:"column:generate;not null;default:true"`
	LatencyMS           int64
	TTFTMS              *int64 `gorm:"column:ttft_ms"`
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64 `gorm:"not null;default:0"`
	CacheCreationTokens int64 `gorm:"not null;default:0"`
	TotalTokens         int64
	CreatedAt           time.Time `gorm:"serializer:storageTime"`
}

func (UsageEventArchive) TableName() string {
	return "usage_events_archive"
}
