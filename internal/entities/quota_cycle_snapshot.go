package entities

import "time"

// QuotaCycleSnapshot 记录每次 quota refresh 时 CPA 返回的窗口快照, 主要用于
// (a) 持久化 reset_at 时间序列以便事后查询账号在某次 7d cycle 内的实际消耗;
// (b) 推断 cycle 边界 (通过 reset_at 跳变检测).
//
// 每个 (provider, auth_index, window_seconds) 在一个 cycle 内通常对应多条
// snapshot (每次刷新写一条); 不同 cycle 之间 reset_at 会向后跳一个 window.
type QuotaCycleSnapshot struct {
	ID             int64     `gorm:"primaryKey"`
	Provider       string    `gorm:"column:provider;not null;index:idx_qcs_provider_window,priority:1"`
	AuthIndex      string    `gorm:"column:auth_index;not null;index:idx_qcs_provider_window,priority:2"`
	WindowSeconds  int64     `gorm:"column:window_seconds;not null;index:idx_qcs_provider_window,priority:3"`
	WindowLabel    string    `gorm:"column:window_label;not null;default:''"`
	ResetAt        time.Time `gorm:"column:reset_at;serializer:storageTime;not null;index:idx_qcs_provider_window,priority:4"`
	UsedPercent    float64   `gorm:"column:used_percent;not null;default:0"`
	CreditsBalance *float64  `gorm:"column:credits_balance"`
	CapturedAt     time.Time `gorm:"column:captured_at;serializer:storageTime;not null;index:idx_qcs_captured_at"`
	RawPayload     string    `gorm:"column:raw_payload;not null;default:''"`
	CreatedAt      time.Time `gorm:"serializer:storageTime"`
}

func (QuotaCycleSnapshot) TableName() string {
	return "quota_cycle_snapshots"
}
