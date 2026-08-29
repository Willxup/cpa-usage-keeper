package entities

import "time"

const (
	AppSettingValueTypeBool = "bool"
	AppSettingValueTypeJSON = "json"
)

// AppSetting 保存应用级持久配置。业务层负责按 key 转成强类型配置。
type AppSetting struct {
	SettingKey string    `gorm:"primaryKey;not null;size:191;column:setting_key"`
	Value      *string   `gorm:"column:value"`
	ValueType  string    `gorm:"not null;size:32;column:value_type"`
	CreatedAt  time.Time `gorm:"serializer:storageTime;not null;column:created_at"`
	UpdatedAt  time.Time `gorm:"serializer:storageTime;not null;column:updated_at"`
}

func (AppSetting) TableName() string {
	return "app_settings"
}
