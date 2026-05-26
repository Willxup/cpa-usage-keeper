package quota

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Cycle-cost feature 关心的窗口长度白名单 (秒). 5h (18000) 太短不计入,
// 因为 cost tracking 在每 5 小时翻一次没有意义.
const (
	CycleWindowSecondsDaily   int64 = 86400
	CycleWindowSecondsWeekly  int64 = 604800
	CycleWindowSecondsMonthly int64 = 2592000
)

// cycleWindowWhitelist 决定哪些窗口长度会被 persist 到 quota_cycle_snapshots.
// 顺序: 长 → 短 (帮助前端按"长周期优先"展示, 但实际选择由 row 自身决定).
var cycleWindowWhitelist = []int64{
	CycleWindowSecondsMonthly,
	CycleWindowSecondsWeekly,
	CycleWindowSecondsDaily,
}

// isCycleWindowAllowed 返回 windowSeconds 是否在 cost-tracking 白名单内.
func isCycleWindowAllowed(seconds int64) bool {
	for _, allowed := range cycleWindowWhitelist {
		if seconds == allowed {
			return true
		}
	}
	return false
}

// persistQuotaSnapshots 把一次 quota refresh 的结果里有意义的 cycle 窗口
// (daily / weekly / monthly) 持久化到 quota_cycle_snapshots, 供后续按 cycle
// 聚合账号 token / 成本. 失败仅日志告警, 不影响 refresh task 状态 (best-effort).
//
// 支持的 provider 风格 (跟 keeper 已有 normalize.go 对齐):
//   - Codex: 使用 Window.Seconds (604800 / 18000)
//   - Claude: 没有 Window.Seconds, 用 key 前缀 (seven_day, five_hour) 推断
//   - Gemini CLI / Antigravity: 桶状窗口, 多数有 ResetTime, 用 key/scope + Window
//     的 Duration+Unit 推断
//   - Kimi: 多窗口 limit, 使用 Window.Duration + Window.Unit 转秒
//   - iFlow / 其它未知 provider: 同样走 Window.Duration+Unit / Window.Seconds 路径
func (s *Service) persistQuotaSnapshots(ctx context.Context, authIndex string, response CheckResponse) {
	if s == nil || s.db == nil {
		return
	}
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logrus.WithError(err).WithField("auth_index", authIndex).Warn("persist quota snapshot: lookup identity")
		}
		return
	}
	provider := strings.TrimSpace(identity.Provider)
	if provider == "" {
		provider = strings.TrimSpace(identity.Type)
	}
	if provider == "" {
		return
	}
	now := timeutil.NormalizeStorageTime(time.Now())
	seen := make(map[int64]bool, 3) // 避免一次 refresh 同 provider 同窗口重复 insert
	for _, row := range response.Quota {
		windowSeconds, ok := cycleRowWindowSeconds(provider, row)
		if !ok {
			continue
		}
		if seen[windowSeconds] {
			continue
		}
		resetAt, ok := parseQuotaResetAt(row.ResetAt)
		if !ok {
			continue
		}
		usedPercent := 0.0
		if row.UsedPercent != nil {
			usedPercent = *row.UsedPercent
		}
		rawPayload, _ := json.Marshal(row)
		snapshot := entities.QuotaCycleSnapshot{
			Provider:      provider,
			AuthIndex:     authIndex,
			WindowSeconds: windowSeconds,
			WindowLabel:   row.Label,
			ResetAt:       resetAt,
			UsedPercent:   usedPercent,
			CapturedAt:    now,
			RawPayload:    string(rawPayload),
		}
		if err := repository.InsertQuotaCycleSnapshot(ctx, s.db, snapshot); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"provider":       provider,
				"auth_index":     authIndex,
				"window_seconds": windowSeconds,
			}).Warn("persist quota snapshot")
		} else {
			seen[windowSeconds] = true
		}
	}
}

// cycleRowWindowSeconds 多策略推断 row 对应的 cycle 窗口秒数, 只接受 cost-tracking
// 白名单内的窗口. 第二个返回值 false 表示该 row 不是我们关心的 cycle 窗口.
func cycleRowWindowSeconds(provider string, row QuotaRow) (int64, bool) {
	// 1) 显式 Window.Seconds
	if row.Window != nil && row.Window.Seconds != nil {
		seconds := *row.Window.Seconds
		if isCycleWindowAllowed(seconds) {
			return seconds, true
		}
	}
	// 2) Window.Duration + Window.Unit (Kimi / Gemini 偶尔)
	if row.Window != nil && row.Window.Duration != nil && row.Window.Unit != "" {
		if seconds := durationUnitToSeconds(*row.Window.Duration, row.Window.Unit); seconds > 0 && isCycleWindowAllowed(seconds) {
			return seconds, true
		}
	}
	// 3) key / scope 关键词推断 (Claude / 其它 provider 没显式 window 时兜底)
	if seconds := inferWindowFromKey(provider, row.Key, row.Scope); seconds > 0 && isCycleWindowAllowed(seconds) {
		return seconds, true
	}
	return 0, false
}

// durationUnitToSeconds 把 (duration, unit) 转秒. unit 不识别返回 0.
func durationUnitToSeconds(duration float64, unit string) int64 {
	if duration <= 0 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "second", "seconds", "sec", "secs", "s":
		return int64(duration)
	case "minute", "minutes", "min", "mins", "m":
		return int64(duration * 60)
	case "hour", "hours", "hr", "hrs", "h":
		return int64(duration * 3600)
	case "day", "days", "d":
		return int64(duration * 86400)
	case "week", "weeks", "w":
		return int64(duration * 604800)
	case "month", "months":
		return int64(duration * 2592000)
	}
	return 0
}

// inferWindowFromKey 用 row.Key / row.Scope 关键词推断窗口长度. 仅返回白名单内的秒数.
func inferWindowFromKey(provider, key, scope string) int64 {
	k := strings.ToLower(key + " " + scope)
	if strings.Contains(k, "monthly") || strings.Contains(k, "month_") || strings.Contains(k, "30d") {
		return CycleWindowSecondsMonthly
	}
	if strings.Contains(k, "seven_day") || strings.Contains(k, "sevenday") || strings.Contains(k, "weekly") || strings.Contains(k, "7d") || strings.Contains(k, "week_") {
		return CycleWindowSecondsWeekly
	}
	if strings.Contains(k, "daily") || strings.Contains(k, "_day") || strings.Contains(k, "24h") || strings.Contains(k, "one_day") {
		return CycleWindowSecondsDaily
	}
	// provider 特有
	switch strings.ToLower(provider) {
	case "codex":
		// secondary_window 在上游一定是 weekly (primary 是 5h, secondary 是 weekly)
		if strings.Contains(strings.ToLower(key), "secondary_window") {
			return CycleWindowSecondsWeekly
		}
	}
	return 0
}

func parseQuotaResetAt(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := timeutil.ParseStorageTime(raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
