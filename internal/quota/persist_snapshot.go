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

// persistQuotaSnapshots 把一次 quota refresh 的结果里 7d (604800s) 主窗口持久化到
// quota_cycle_snapshots, 用于后续按 cycle 聚合账号 token / 成本. 失败仅日志告警,
// 不影响 refresh task 状态 (best-effort).
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
	for _, row := range response.Quota {
		windowSeconds := quotaRowWindowSeconds(row)
		if windowSeconds != 604800 {
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
		}
	}
}

func quotaRowWindowSeconds(row QuotaRow) int64 {
	if row.Window != nil && row.Window.Seconds != nil {
		return *row.Window.Seconds
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
