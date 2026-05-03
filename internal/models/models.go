package models

import "time"

type UsageEvent struct {
	ID              uint      `gorm:"primaryKey"`
	EventKey        string    `gorm:"uniqueIndex:uniq_usage_events_event_key"`
	APIGroupKey     string    `gorm:"index:idx_usage_events_api_group_key"`
	Provider        string    `gorm:"column:provider"`
	Endpoint        string    `gorm:"column:endpoint"`
	AuthType        string    `gorm:"column:auth_type"`
	RequestID       string    `gorm:"column:request_id"`
	Model           string    `gorm:"index:idx_usage_events_model"`
	Timestamp       time.Time `gorm:"index:idx_usage_events_timestamp"`
	Source          string    `gorm:"index:idx_usage_events_source"`
	AuthIndex       string    `gorm:"index:idx_usage_events_auth_index"`
	Failed          bool      `gorm:"index:idx_usage_events_failed"`
	LatencyMS       int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64
	CreatedAt       time.Time
}

type RedisUsageInbox struct {
	ID            uint   `gorm:"primaryKey"`
	QueueKey      string `gorm:"not null;index"`
	MessageHash   string `gorm:"not null;index"`
	RawMessage    string `gorm:"not null"`
	Status        string `gorm:"not null;index"`
	AttemptCount  int    `gorm:"not null;default:0"`
	LastError     string
	UsageEventKey string    `gorm:"index"`
	PoppedAt      time.Time `gorm:"not null;index"`
	ProcessedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ModelPriceSetting struct {
	ID                   uint   `gorm:"primaryKey"`
	Model                string `gorm:"uniqueIndex:uniq_model_price_settings_model"`
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CachePricePer1M      float64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type UsageIdentityAuthType int

const (
	UsageIdentityAuthTypeAuthFile   UsageIdentityAuthType = 1
	UsageIdentityAuthTypeAIProvider UsageIdentityAuthType = 2
)

type UsageIdentity struct {
	ID           uint `gorm:"primaryKey"`
	Name         string
	AuthType     UsageIdentityAuthType `gorm:"uniqueIndex:uniq_usage_identities_type_identity;index:idx_usage_identities_auth_type"`
	AuthTypeName string                `gorm:"index:idx_usage_identities_auth_type_name"`
	Identity     string                `gorm:"uniqueIndex:uniq_usage_identities_type_identity;index:idx_usage_identities_identity"`
	Type         string                `gorm:"column:type"`
	Provider     string

	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64

	LastAggregatedUsageEventID uint `gorm:"index:idx_usage_identities_last_aggregated_usage_event_id"`
	FirstUsedAt                *time.Time
	LastUsedAt                 *time.Time
	StatsUpdatedAt             *time.Time

	IsDeleted bool `gorm:"index:idx_usage_identities_is_deleted"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time `gorm:"index:idx_usage_identities_deleted_at"`
}

func All() []any {
	return []any{
		&UsageEvent{},
		&RedisUsageInbox{},
		&ModelPriceSetting{},
		&UsageIdentity{},
	}
}
