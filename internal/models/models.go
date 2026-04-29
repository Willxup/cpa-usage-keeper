package models

import (
	"time"

	"gorm.io/gorm"
)

type SnapshotRun struct {
	ID             uint      `gorm:"primaryKey"`
	FetchedAt      time.Time `gorm:"index:idx_snapshot_runs_fetched_at"`
	CPABaseURL     string
	ExportedAt     *time.Time
	Version        string
	Status         string `gorm:"index:idx_snapshot_runs_status"`
	HTTPStatus     int
	PayloadHash    string
	RawPayload     []byte
	BackupFilePath string
	ErrorMessage   string
	InsertedEvents int
	DedupedEvents  int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type UsageEvent struct {
	ID              uint   `gorm:"primaryKey"`
	EventKey        string `gorm:"uniqueIndex:uniq_usage_events_event_key"`
	SnapshotRunID   uint
	APIGroupKey     string    `gorm:"index:idx_usage_events_api_group_key"`
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
	SnapshotRunID *uint     `gorm:"index"`
	UsageEventKey string    `gorm:"index"`
	PoppedAt      time.Time `gorm:"not null;index"`
	ProcessedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AuthFile struct {
	ID          uint   `gorm:"primaryKey"`
	AuthIndex   string `gorm:"uniqueIndex:uniq_auth_files_auth_index"`
	Name        string
	Email       string
	Type        string
	Provider    string
	Label       string
	Status      string
	Source      string
	Disabled    bool
	Unavailable bool
	RuntimeOnly bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

type ProviderMetadata struct {
	ID           uint   `gorm:"primaryKey"`
	LookupKey    string `gorm:"uniqueIndex:uniq_provider_metadata_lookup_key"`
	ProviderType string `gorm:"index:idx_provider_metadata_provider_type"`
	DisplayName  string
	ProviderKey  string `gorm:"index:idx_provider_metadata_provider_key"`
	MatchKind    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
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

func All() []any {
	return []any{
		&SnapshotRun{},
		&UsageEvent{},
		&RedisUsageInbox{},
		&AuthFile{},
		&ProviderMetadata{},
		&ModelPriceSetting{},
	}
}
