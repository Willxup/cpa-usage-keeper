package cooldown

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/timeutil"
)

// ResolveRecoverAt 解析并校验 recover_at 时间。
// 支持绝对时间（resetsAt）和相对秒数（resetsInSeconds），结果必须晚于 now。
func ResolveRecoverAt(now time.Time, resetsAt *time.Time, resetsInSeconds *int64) (time.Time, error) {
	var raw time.Time
	if resetsAt != nil && !resetsAt.IsZero() {
		raw = *resetsAt
	} else if resetsInSeconds != nil && *resetsInSeconds > 0 {
		raw = now.Add(time.Duration(*resetsInSeconds) * time.Second)
	} else {
		return time.Time{}, fmt.Errorf("no valid resets_at or resets_in_seconds")
	}
	normalized := timeutil.NormalizeStorageTime(raw)
	if !normalized.After(now) {
		return time.Time{}, fmt.Errorf("recover_at %s is not after now %s", normalized.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	return normalized, nil
}

// ParseRecoverAtFromFields 从一组字段名-值映射中解析 recover_at，支持多种字段名。
// 按优先级：resets_at > recover_at > reset_at > reset_time > resets_in_seconds > reset_after_seconds > retry_after。
func ParseRecoverAtFromFields(now time.Time, raw map[string]any) (time.Time, error) {
	for _, field := range []string{"resets_at", "recover_at", "reset_at", "reset_time"} {
		if v, ok := raw[field]; ok {
			switch tv := v.(type) {
			case string:
				t, err := time.Parse(time.RFC3339, tv)
				if err == nil && !t.IsZero() {
					return ResolveRecoverAt(now, &t, nil)
				}
			case float64:
				t := time.Unix(int64(tv), 0)
				if !t.IsZero() {
					return ResolveRecoverAt(now, &t, nil)
				}
			}
		}
	}
	for _, field := range []string{"resets_in_seconds", "reset_after_seconds", "retry_after"} {
		if v, ok := raw[field]; ok {
			var sec int64
			switch sv := v.(type) {
			case float64:
				sec = int64(sv)
			case int64:
				sec = sv
			default:
				continue
			}
			if sec > 0 {
				return ResolveRecoverAt(now, nil, &sec)
			}
		}
	}
	return time.Time{}, fmt.Errorf("no valid recover_at field in payload")
}

// ParseRecoverAtFromRequest 从 disable-limited 请求项中解析 recover_at。
// 支持 resets_at / recover_at / reset_at / reset_time (RFC3339) 和 resets_in_seconds / reset_after_seconds / retry_after (秒数)。
func ParseRecoverAtFromRequest(now time.Time, resetsAt, recoverAt, resetAt, resetTime string, resetsInSeconds, resetAfterSeconds, retryAfter *int64) (time.Time, error) {
	for _, s := range []string{resetsAt, recoverAt, resetAt, resetTime} {
		if s == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, s)
		if err == nil && !t.IsZero() {
			return ResolveRecoverAt(now, &t, nil)
		}
	}
	for _, sec := range []*int64{resetsInSeconds, resetAfterSeconds, retryAfter} {
		if sec != nil && *sec > 0 {
			return ResolveRecoverAt(now, nil, sec)
		}
	}
	return time.Time{}, fmt.Errorf("no valid recover_at in request item")
}
