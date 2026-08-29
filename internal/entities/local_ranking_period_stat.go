package entities

import "time"

// LocalRankingPeriodKind 区分本地排行按日和按月维护的固定周期累计。
type LocalRankingPeriodKind string

const (
	LocalRankingPeriodDay   LocalRankingPeriodKind = "day"
	LocalRankingPeriodMonth LocalRankingPeriodKind = "month"
)

// LocalRankingPeriodStat 每个 API Key 只保留今日、昨日、本月和上月四行累计。
type LocalRankingPeriodStat struct {
	PeriodKind LocalRankingPeriodKind `gorm:"type:text;primaryKey;check:chk_local_ranking_period_stats_kind,period_kind IN ('day','month')"`
	PeriodKey  string                 `gorm:"type:text;primaryKey"`
	APIKeyID   int64                  `gorm:"primaryKey;index:idx_local_ranking_period_stats_api_key"`

	RequestCount       int64 `gorm:"not null;default:0"`
	SuccessCount       int64 `gorm:"not null;default:0"`
	FailureCount       int64 `gorm:"not null;default:0"`
	InputTokens        int64 `gorm:"not null;default:0"`
	CacheReadTokens    int64 `gorm:"not null;default:0"`
	TotalTokens        int64 `gorm:"not null;default:0"`
	TTFTSumMS          int64 `gorm:"column:ttft_sum_ms;not null;default:0"`
	TTFTSampleCount    int64 `gorm:"column:ttft_sample_count;not null;default:0"`
	LatencySumMS       int64 `gorm:"not null;default:0"`
	LatencySampleCount int64 `gorm:"not null;default:0"`
	Peak5MRequestCount int64 `gorm:"column:peak_5m_request_count;not null;default:0"`
	Peak5MTotalTokens  int64 `gorm:"column:peak_5m_total_tokens;not null;default:0"`

	UpdatedAt time.Time `gorm:"serializer:storageTime;not null"`
}
