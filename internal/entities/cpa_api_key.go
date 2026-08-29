package entities

import "time"

// CPAAPIKey 保存 CPA 管理接口同步到本地的 API-Key，完整 key 仅供后端内部查询使用。
type CPAAPIKey struct {
	ID                   int64  `gorm:"primaryKey"`
	APIKey               string `gorm:"uniqueIndex:uniq_cpa_api_keys_api_key"`
	DisplayKey           string
	KeyAlias             string
	LocalRankingAvatarID *uint8
	IsDeleted            bool       `gorm:"index:idx_cpa_api_keys_is_deleted"`
	LastSyncedAt         *time.Time `gorm:"serializer:storageTime"`
	CreatedAt            time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt            time.Time  `gorm:"serializer:storageTime"`
}
