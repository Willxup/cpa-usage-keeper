package entities

import "time"

// RedisUsageInbox 是从 CPA Redis queue 拉取后等待解码/入库的原始消息实体。
type RedisUsageInbox struct {
	ID            uint   `gorm:"primaryKey;index:idx_redis_usage_inboxes_status_id,priority:2"`
	QueueKey      string `gorm:"not null"`
	MessageHash   string `gorm:"not null"`
	RawMessage    string `gorm:"not null"`
	Status        string `gorm:"not null;index:idx_redis_usage_inboxes_status_id,priority:1;index:idx_redis_usage_inboxes_status_processed_at,priority:1;index:idx_redis_usage_inboxes_status_updated_at,priority:1;index:idx_redis_usage_inboxes_status_usage_event_key,priority:1"`
	AttemptCount  int    `gorm:"not null;default:0"`
	LastError     string
	UsageEventKey string     `gorm:"index:idx_redis_usage_inboxes_status_usage_event_key,priority:2"`
	PoppedAt      time.Time  `gorm:"serializer:storageTime;not null"`
	ProcessedAt   *time.Time `gorm:"serializer:storageTime;index:idx_redis_usage_inboxes_status_processed_at,priority:2"`
	CreatedAt     time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt     time.Time  `gorm:"serializer:storageTime;index:idx_redis_usage_inboxes_status_updated_at,priority:2"`
}
