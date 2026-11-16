package quota

import (
	"context"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/credentials"

	"github.com/sirupsen/logrus"
)

// routingRecoveryMinRemaining 高于 CPA 指数退避上限（30 分钟），只处理按上游窗口重置时间设置的冷却。
const routingRecoveryMinRemaining = 30 * time.Minute

// routingRecoveryTimeout 限制一次冷却查询加解除的总时长；两者都是 CPA 本地管理接口。
const routingRecoveryTimeout = 15 * time.Second

// routingRecoveryClient 是解除 CPA 路由冷却所需的最小管理接口；未实现时不做恢复。
type routingRecoveryClient interface {
	FetchCredentialCooldowns(context.Context, string) ([]credentials.CredentialCooldown, bool, error)
	ResetQuota(context.Context, string) error
}

// sharedQuotaAvailable 判断决定整个账号能否路由的共享窗口是否都已恢复：Claude five_hour/seven_day，Codex rate_limit 主窗口。
// 模型或功能级子限额（如 seven_day_opus、Fable 周限额、Codex additional）在 CPA 中只冷却对应模型，不能阻止账号恢复。
// 判断基于原始 payload：上游未给出使用率的窗口视为未知，不能当作 0%。
func sharedQuotaAvailable(output ProviderOutput) bool {
	if usage := codexUsagePayloadFromProviderOutput(output); usage != nil {
		limit := usage.RateLimit
		if limit == nil || (limit.Allowed != nil && !*limit.Allowed) || (limit.LimitReached != nil && *limit.LimitReached) {
			return false
		}
		return windowsBelowLimit(
			codexWindowUsage(limit.PrimaryWindow),
			codexWindowUsage(limit.SecondaryWindow),
		)
	}
	if usage := claudeUsagePayloadFromProviderOutput(output); usage != nil {
		return windowsBelowLimit(claudeWindowUsage(usage.FiveHour), claudeWindowUsage(usage.SevenDay))
	}
	return false
}

// windowUsage 是单个窗口的已用百分比；present=false 表示上游没有返回该窗口。
type windowUsage struct {
	present, known bool
	usedPercent    float64
}

func codexWindowUsage(window *CodexUsageWindow) windowUsage {
	if window == nil {
		return windowUsage{}
	}
	return windowUsage{present: true, known: window.HasUsedPercent, usedPercent: window.UsedPercent}
}

func claudeWindowUsage(window *ClaudeUsageWindow) windowUsage {
	if window == nil {
		return windowUsage{}
	}
	return windowUsage{present: true, known: window.HasUtilization, usedPercent: window.Utilization}
}

// windowsBelowLimit 要求至少一个窗口存在，且每个存在的窗口都给出 [0, 100) 内的使用率；NaN、负数等异常值视为未知。
func windowsBelowLimit(windows ...windowUsage) bool {
	found := false
	for _, window := range windows {
		if !window.present {
			continue
		}
		if !window.known || !(window.usedPercent >= 0 && window.usedPercent < 100) {
			return false
		}
		found = true
	}
	return found
}

func hasQuotaCredentialCooldown(cooldowns []credentials.CredentialCooldown) bool {
	for _, cooldown := range cooldowns {
		if cooldown.Scope == "credential" &&
			(cooldown.Reason == "credential_quota" || cooldown.Reason == "quota") &&
			time.Duration(cooldown.RemainingSeconds)*time.Second > routingRecoveryMinRemaining {
			return true
		}
	}
	return false
}

// recoverCredentialRouting 在官方额度显示账号已恢复、CPA 仍按额度耗尽冷却整个凭证时解除冷却。
// 额度可能在 Keeper 之外恢复（官方页面使用 reset、上游提前恢复），CPA 不会主动发现。
// 误判只会让 CPA 多收到一次 429 并重新冷却，下一轮刷新看到耗尽后不会再次解除。
func (s *Service) recoverCredentialRouting(authIndex string, response CheckResponse) {
	if s.routingRecovery == nil || !response.sharedQuotaAvailable {
		return
	}
	// 刷新任务的 deadline 可能已被可选的订阅查询耗尽；恢复使用独立超时，并随服务关闭取消。
	ctx, cancel := context.WithTimeout(s.refreshContextSnapshot(), routingRecoveryTimeout)
	defer cancel()
	logger := logrus.WithField("auth_index", authIndex)
	cooldowns, known, err := s.routingRecovery.FetchCredentialCooldowns(ctx, authIndex)
	if err != nil {
		logger.WithError(err).Warn("CPA credential cooldown lookup failed after quota refresh")
		return
	}
	if !known || !hasQuotaCredentialCooldown(cooldowns) {
		return
	}
	if err := s.routingRecovery.ResetQuota(ctx, authIndex); err != nil {
		logger.WithError(err).Warn("CPA credential cooldown reset failed after quota recovered")
		return
	}
	logger.Info("Cleared CPA credential cooldown after quota refresh showed available quota")
}
