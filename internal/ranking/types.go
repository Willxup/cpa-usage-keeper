package ranking

import (
	"errors"
	"time"
)

const (
	IdentitySettingKey = "ranking.identity"
	MinAvatarID        = 1
	MaxAvatarID        = 66
)

type Status string

const (
	StatusDisabled Status = "disabled"
	StatusJoining  Status = "joining"
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusDeleted  Status = "deleted"
)

var (
	ErrInvalidState = errors.New("invalid ranking state")
	ErrDeletedState = errors.New("ranking participant is permanently deleted")
)

// State 是 ranking.identity 的完整持久化内容。私钥只能在后端模块内部流转。
type State struct {
	Status                        Status     `json:"status"`
	PublicKey                     string     `json:"public_key,omitempty"`
	PrivateKey                    string     `json:"private_key,omitempty"`
	RegistrationIdempotencyKey    string     `json:"registration_idempotency_key,omitempty"`
	DisplayName                   string     `json:"display_name,omitempty"`
	AvatarID                      uint8      `json:"avatar_id,omitempty"`
	ParticipantID                 string     `json:"participant_id,omitempty"`
	ParticipationStartedAt        *time.Time `json:"participation_started_at,omitempty"`
	LastAllocatedSequence         int64      `json:"last_allocated_sequence,omitempty"`
	LastSuccessfulCompleteDay     string     `json:"last_successful_complete_day,omitempty"`
	LastSuccessfulCompleteEventID int64      `json:"last_successful_complete_event_id,omitempty"`
	LastSuccessfulSyncAt          *time.Time `json:"last_successful_sync_at,omitempty"`
	LastAttemptAt                 *time.Time `json:"last_attempt_at,omitempty"`
	LastError                     string     `json:"last_error,omitempty"`
}

type Metrics struct {
	RequestCount        int64 `json:"request_count"`
	SuccessCount        int64 `json:"success_count"`
	FailureCount        int64 `json:"failure_count"`
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	TTFTSumMS           int64 `json:"ttft_sum_ms"`
	TTFTSampleCount     int64 `json:"ttft_sample_count"`
	LatencySumMS        int64 `json:"latency_sum_ms"`
	LatencySampleCount  int64 `json:"latency_sample_count"`
	Peak5MRequestCount  int64 `json:"peak_5m_request_count" gorm:"column:peak_5m_request_count"`
	Peak5MTotalTokens   int64 `json:"peak_5m_total_tokens" gorm:"column:peak_5m_total_tokens"`
}
