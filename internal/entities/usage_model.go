package entities

import "time"

// UsageModel records request model names seen in usage_events without scanning raw events on pricing reads.
type UsageModel struct {
	ID               int64      `gorm:"primaryKey"`
	Model            string     `gorm:"not null;uniqueIndex:uniq_usage_models_model_auth_type_auth_index,priority:1;index:idx_usage_models_model"`
	AuthType         string     `gorm:"not null;default:'';uniqueIndex:uniq_usage_models_model_auth_type_auth_index,priority:2;index:idx_usage_models_auth_type_auth_index,priority:1"`
	AuthIndex        string     `gorm:"not null;default:'';uniqueIndex:uniq_usage_models_model_auth_type_auth_index,priority:3;index:idx_usage_models_auth_type_auth_index,priority:2"`
	RequestCount     int64      `gorm:"not null;default:0"`
	FirstUsedAt      *time.Time `gorm:"serializer:storageTime"`
	LastUsedAt       *time.Time `gorm:"serializer:storageTime"`
	LastUsageEventID int64      `gorm:"not null;default:0"`
	CreatedAt        time.Time  `gorm:"serializer:storageTime;not null"`
	UpdatedAt        time.Time  `gorm:"serializer:storageTime;not null"`
}
