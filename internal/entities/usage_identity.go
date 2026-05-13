package entities

import "time"

// UsageIdentityAuthType 表示 usage identity 的来源类型。
type UsageIdentityAuthType int

const (
	UsageIdentityAuthTypeAuthFile   UsageIdentityAuthType = 1
	UsageIdentityAuthTypeAIProvider UsageIdentityAuthType = 2
)

// UsageIdentity 是从 CPA auth_files 和 provider config 同步出的 usage source 身份实体。
type UsageIdentity struct {
	ID           uint                  `gorm:"primaryKey;index:idx_usage_identities_auth_type_name_id,priority:3"`
	Name         string                `gorm:"index:idx_usage_identities_auth_type_name_id,priority:2"`
	AuthType     UsageIdentityAuthType `gorm:"uniqueIndex:uniq_usage_identities_type_identity;index:idx_usage_identities_auth_type_name_id,priority:1;index:idx_usage_identities_auth_type_type,priority:1"`
	AuthTypeName string
	Identity     string `gorm:"uniqueIndex:uniq_usage_identities_type_identity"`
	Type         string `gorm:"column:type;index:idx_usage_identities_auth_type_type,priority:2"`
	Provider     string
	LookupKey    string
	Prefix       string
	BaseURL      string
	AccountID    *string
	ProjectID    *string

	ActiveStart *time.Time `gorm:"serializer:storageTime"`
	ActiveUntil *time.Time `gorm:"serializer:storageTime"`
	PlanType    *string

	TotalRequests   int64
	SuccessCount    int64
	FailureCount    int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	TotalTokens     int64

	LastAggregatedUsageEventID uint
	FirstUsedAt                *time.Time `gorm:"serializer:storageTime"`
	LastUsedAt                 *time.Time `gorm:"serializer:storageTime"`
	StatsUpdatedAt             *time.Time `gorm:"serializer:storageTime"`

	IsDeleted bool
	CreatedAt time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt time.Time  `gorm:"serializer:storageTime"`
	DeletedAt *time.Time `gorm:"serializer:storageTime"`
}
