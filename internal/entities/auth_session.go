package entities

import "time"

// AuthSession persists browser login sessions across service restarts.
type AuthSession struct {
	TokenHash   string `gorm:"primaryKey;column:token_hash"`
	Role        string `gorm:"not null;index:idx_auth_sessions_role"`
	Source      string `gorm:"not null;default:standard;index:idx_auth_sessions_source"`
	Alias       string `gorm:"not null;default:''"`
	CPAAPIKeyID int64  `gorm:"index:idx_auth_sessions_cpa_api_key_id"`
	LoginIP     string
	LastSeenIP  string
	UserAgent   string
	LastSeenAt  *time.Time `gorm:"serializer:storageTime"`
	ExpiresAt   time.Time  `gorm:"serializer:storageTime;not null;index:idx_auth_sessions_expires_at"`
	CreatedAt   time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt   time.Time  `gorm:"serializer:storageTime"`
}
