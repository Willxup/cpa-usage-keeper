package entities

import "time"

// QuotaResetAtSource 标识周期重置时刻来自上游明确时间，还是根据倒计时推算。
type QuotaResetAtSource string

const (
	QuotaResetAtSourceAbsolute QuotaResetAtSource = "absolute"
	QuotaResetAtSourceRelative QuotaResetAtSource = "relative"
)

// QuotaCycle 保存任意 provider 的一个额度周期边界，不包含 Token、Cost 或展示分类。
type QuotaCycle struct {
	ID int64 `gorm:"primaryKey"`
	// Provider 与 QuotaKey 共同表达额度来源和项目；QuotaKey 不限制为 Codex Primary/Secondary。
	Provider  string `gorm:"type:text;not null;check:chk_quota_cycles_provider,provider <> '';uniqueIndex:uniq_quota_cycles_identity,priority:1"`
	AuthIndex string `gorm:"type:text;not null;check:chk_quota_cycles_auth_index,auth_index <> '';uniqueIndex:uniq_quota_cycles_identity,priority:2"`
	QuotaKey  string `gorm:"type:text;not null;check:chk_quota_cycles_quota_key,quota_key <> '';uniqueIndex:uniq_quota_cycles_identity,priority:3"`
	// WindowSeconds 保留上游真实窗口长度，避免把 Weekly 等展示标题固化在表结构中。
	WindowSeconds   int64              `gorm:"not null;check:chk_quota_cycles_window_seconds,window_seconds > 0;uniqueIndex:uniq_quota_cycles_identity,priority:4"`
	ResetAtSource   QuotaResetAtSource `gorm:"type:text;not null;check:chk_quota_cycles_reset_source,reset_at_source IN ('absolute','relative')"`
	WindowStartedAt time.Time          `gorm:"serializer:sortableTime;not null;check:chk_quota_cycles_window_bounds,window_started_at < reset_at"`
	// ResetAt 是同周期防抖后的稳定边界，而不是每次 observation 的瞬时推算值。
	ResetAt         time.Time `gorm:"serializer:sortableTime;not null;uniqueIndex:uniq_quota_cycles_identity,priority:5"`
	FirstObservedAt time.Time `gorm:"serializer:sortableTime;not null;check:chk_quota_cycles_observed_bounds,first_observed_at <= last_observed_at"`
	LastObservedAt  time.Time `gorm:"serializer:sortableTime;not null"`
	CreatedAt       time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt       time.Time `gorm:"serializer:storageTime;not null"`
}

func (QuotaCycle) TableName() string {
	return "quota_cycles"
}
