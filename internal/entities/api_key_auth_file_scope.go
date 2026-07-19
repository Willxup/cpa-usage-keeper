package entities

import "time"

// APIKeyAuthFileScope 定义 CPA API Key 可查看的认证文件名称。
// 配置保存认证文件名而非 auth_index，避免 CPA 重载认证文件后索引变化导致授权漂移。
type APIKeyAuthFileScope struct {
	ID           int64     `gorm:"primaryKey"`
	CPAAPIKeyID  int64     `gorm:"not null;uniqueIndex:uniq_api_key_auth_file_scopes_key_name,priority:1;index:idx_api_key_auth_file_scopes_key_id"`
	AuthFileName string    `gorm:"not null;uniqueIndex:uniq_api_key_auth_file_scopes_key_name,priority:2"`
	CreatedAt    time.Time `gorm:"serializer:storageTime"`
	UpdatedAt    time.Time `gorm:"serializer:storageTime"`
}
