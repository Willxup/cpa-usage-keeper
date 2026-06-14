package cooldown

import (
	"time"

	"cpa-usage-keeper/internal/entities"
)

// CooldownBuildOptions 描述构建 AuthFileCooldown 所需的全部字段，429 和 inspection 两条路径共用。
type CooldownBuildOptions struct {
	AuthFileName     string
	AuthFilePath     string
	AuthIndex        string
	RecoverAt        time.Time
	Reason           entities.AuthFileCooldownReason
	Owner            entities.AuthFileCooldownOwner
	Source           entities.AuthFileCooldownSource
	PreviousDisabled bool
	DisabledByKeeper bool
	UpstreamCode     int
	UpstreamMessage  string
	SourceEventKey   string
	SourceRequestID  string
	LastError        string
}

// BuildCooldown 是 429 自动 cooldown 和巡检手动 cooldown 共用的构造器，只组装实体不写库。
func BuildCooldown(opts CooldownBuildOptions) *entities.AuthFileCooldown {
	return &entities.AuthFileCooldown{
		Provider:           "codex",
		AuthIndex:          opts.AuthIndex,
		AuthFileName:       opts.AuthFileName,
		AuthFilePath:       opts.AuthFilePath,
		RecoverAt:          opts.RecoverAt,
		Reason:             opts.Reason,
		Owner:              opts.Owner,
		State:              entities.AuthFileCooldownActive,
		DisabledByKeeper:   opts.DisabledByKeeper,
		PreviousDisabled:   opts.PreviousDisabled,
		Source:             opts.Source,
		UpstreamStatusCode: opts.UpstreamCode,
		UpstreamMessage:    opts.UpstreamMessage,
		SourceEventKey:     opts.SourceEventKey,
		SourceRequestID:    opts.SourceRequestID,
		LastError:          opts.LastError,
	}
}
