package dto

import "strings"

// QuotaWindowKey 只映射两个已支持 provider 的账号主窗口；API 角色与持久化键分开。
func QuotaWindowKey(provider, role string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) + "/" + strings.ToLower(strings.TrimSpace(role)) {
	case "codex/primary":
		return "rate_limit.primary_window", true
	case "codex/secondary":
		return "rate_limit.secondary_window", true
	case "claude/primary":
		return "five_hour", true
	case "claude/secondary":
		return "seven_day", true
	default:
		return "", false
	}
}

func QuotaWindowRole(provider, key string) (string, bool) {
	for _, role := range []string{"primary", "secondary"} {
		if mapped, ok := QuotaWindowKey(provider, role); ok && mapped == key {
			return role, true
		}
	}
	return "", false
}
