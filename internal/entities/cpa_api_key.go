package entities

import "time"

const (
	// CPAAPIKeySourceCPA 表示由 CPA 核心 /v0/management/api-keys 清单同步的 key。
	CPAAPIKeySourceCPA = "cpa"
	// CPAAPIKeySourcePlugin 表示由 key-policy 插件清单同步的 key；插件 key 的 id 是短字符串，绝不能用于登录。
	CPAAPIKeySourcePlugin = "plugin"
)

// CPAAPIKey 保存 CPA 管理接口同步到本地的 API-Key，完整 key 仅供后端内部查询使用。
type CPAAPIKey struct {
	ID                   int64  `gorm:"primaryKey"`
	APIKey               string `gorm:"uniqueIndex:uniq_cpa_api_keys_api_key"`
	DisplayKey           string
	KeyAlias             string
	LocalRankingAvatarID *uint8
	// Source 区分 CPA 核心 key 与插件 key；两类来源各自全量替换，互不软删。
	Source       string     `gorm:"not null;default:cpa;index:idx_cpa_api_keys_source"`
	IsDeleted    bool       `gorm:"index:idx_cpa_api_keys_is_deleted"`
	LastSyncedAt *time.Time `gorm:"serializer:storageTime"`
	CreatedAt    time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt    time.Time  `gorm:"serializer:storageTime"`
}
