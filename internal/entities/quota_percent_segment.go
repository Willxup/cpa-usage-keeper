package entities

import "time"

// QuotaPercentSegment 保存一个额度周期内观察到的整数剩余百分比连续状态段。
type QuotaPercentSegment struct {
	ID               int64      `gorm:"primaryKey"`
	CycleID          int64      `gorm:"not null;uniqueIndex:uniq_quota_percent_segments_cycle_percent,priority:1"`
	RemainingPercent int        `gorm:"not null;check:chk_quota_percent_segments_remaining,remaining_percent BETWEEN 0 AND 100;uniqueIndex:uniq_quota_percent_segments_cycle_percent,priority:2"`
	FirstObservedAt  time.Time  `gorm:"serializer:sortableTime;not null;check:chk_quota_percent_segments_observed_bounds,first_observed_at <= last_observed_at"`
	LastObservedAt   time.Time  `gorm:"serializer:sortableTime;not null"`
	ObservationCount int64      `gorm:"not null;default:1;check:chk_quota_percent_segments_count,observation_count >= 1"`
	CreatedAt        time.Time  `gorm:"serializer:storageTime;not null"`
	UpdatedAt        time.Time  `gorm:"serializer:storageTime;not null"`
	Cycle            QuotaCycle `gorm:"foreignKey:CycleID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}

func (QuotaPercentSegment) TableName() string {
	return "quota_percent_segments"
}
