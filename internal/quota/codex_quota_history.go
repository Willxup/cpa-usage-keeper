package quota

import (
	"math"
	"strings"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"github.com/sirupsen/logrus"
)

// BuildCodexMainQuotaObservations 从主动查询原始 ProviderOutput 只提取 Primary/Secondary 主额度历史。
func BuildCodexMainQuotaObservations(authIndex string, output ProviderOutput, observedAt time.Time) []repositorydto.CodexMainQuotaObservation {
	// auth_index 是未来回溯 UsageEvent 的稳定键；缺失时整份输出没有可持久化归属。
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" || observedAt.IsZero() {
		return nil
	}
	// 只接受真实 CodexResult；Provider 字符串提示不能替代结果类型证明。
	usage := codexUsagePayloadFromProviderOutput(output)
	if usage == nil || usage.RateLimit == nil {
		return nil
	}

	// 结果最多两条并预分配容量；CodeReviewRateLimit 与 AdditionalRateLimits 不参与遍历。
	observations := make([]repositorydto.CodexMainQuotaObservation, 0, 2)
	if observation, ok := buildCodexMainQuotaObservation(authIndex, "primary", usage.RateLimit.PrimaryWindow, observedAt); ok {
		observations = append(observations, observation)
	}
	if observation, ok := buildCodexMainQuotaObservation(authIndex, "secondary", usage.RateLimit.SecondaryWindow, observedAt); ok {
		observations = append(observations, observation)
	}
	return observations
}

func codexUsagePayloadFromProviderOutput(output ProviderOutput) *CodexUsagePayload {
	// Provider handler 当前返回值可能是值或指针，两种形式共享完全相同的历史语义。
	switch result := output.Result.(type) {
	case CodexResult:
		return result.Usage
	case *CodexResult:
		if result != nil {
			return result.Usage
		}
	}
	return nil
}

func buildCodexMainQuotaObservation(authIndex string, role string, window *CodexUsageWindow, observedAt time.Time) (repositorydto.CodexMainQuotaObservation, bool) {
	// 零值结果保持不可写；调用方只能在 ok=true 时消费字段。
	observation := repositorydto.CodexMainQuotaObservation{}
	// used、window seconds 和至少一种 reset 边界都必须由上游明确提供。
	if window == nil || !window.HasUsedPercent || !window.HasLimitWindowSeconds || (!window.HasResetAt && !window.HasResetAfterSeconds) {
		return observation, false
	}
	// raw 百分比允许有限超界值用于诊断，但 NaN/Inf 不能稳定转换或持久化。
	if math.IsNaN(window.UsedPercent) || math.IsInf(window.UsedPercent, 0) {
		return observation, false
	}
	// 秒数必须为正且能安全转换为 time.Duration，避免周期起点计算整数溢出。
	if window.LimitWindowSeconds <= 0 || window.LimitWindowSeconds > math.MaxInt64/int64(time.Second) {
		return observation, false
	}

	// 优先采用上游直接返回的重置时刻；只有倒计时时，才用截断到秒的观察时间推算。
	resetAt := time.Time{}
	resetSource := ""
	if window.HasResetAt && window.ResetAt > 0 {
		resetAt = time.Unix(window.ResetAt, 0)
		resetSource = "absolute"
	} else if window.HasResetAfterSeconds && window.ResetAfterSeconds >= 0 && window.ResetAfterSeconds <= math.MaxInt64/int64(time.Second) {
		resetAt = observedAt.Truncate(time.Second).Add(time.Duration(window.ResetAfterSeconds) * time.Second)
		resetSource = "relative"
	}
	if resetAt.IsZero() {
		return observation, false
	}

	// 统一剩余百分比采用页面同口径：先钳制已用值，再四舍五入成 0–100 整数。
	clampedUsed := math.Max(0, math.Min(100, window.UsedPercent))
	remainingPercent := int(math.Round(100 - clampedUsed))
	// 有限超界值只用 debug 暴露上游异常，不进入统一历史 DTO。
	if window.UsedPercent < 0 || window.UsedPercent > 100 {
		logrus.WithFields(logrus.Fields{
			"auth_index":       authIndex,
			"window_role":      role,
			"raw_used_percent": window.UsedPercent,
		}).Debug("codex quota raw used percent clamped for history")
	}

	observation = repositorydto.CodexMainQuotaObservation{
		AuthIndex:        authIndex,
		WindowRole:       role,
		WindowSeconds:    window.LimitWindowSeconds,
		ResetAtSource:    resetSource,
		ResetAt:          resetAt,
		RemainingPercent: remainingPercent,
		FirstObservedAt:  observedAt,
		LastObservedAt:   observedAt,
		ObservationCount: 1,
	}
	return observation, true
}
