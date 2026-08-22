package helper

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
)

const sensitiveValueMask = "*********"

// RedactSensitiveValue 使用统一格式隐藏前端展示中的敏感值：长值保留前 3 位和后 6 位，短值全隐藏。
func RedactSensitiveValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "unknown" {
		return "unknown"
	}
	runes := []rune(trimmed)
	if len(runes) <= 9 {
		return sensitiveValueMask
	}
	return string(runes[:3]) + sensitiveValueMask + string(runes[len(runes)-6:])
}

// CPAAPIKeyMaskedDisplayKey 返回 CPA API Key 的安全展示 key；优先基于原始 key 重新脱敏，避免历史 DisplayKey 格式不一致。
func CPAAPIKeyMaskedDisplayKey(row entities.CPAAPIKey) string {
	// 插件 key 的 api_key 列存的是可猜测的短 id 而非真实密钥，展示时直接使用同步来的 key_preview。
	if row.Source == entities.CPAAPIKeySourcePlugin {
		return strings.TrimSpace(row.DisplayKey)
	}
	if strings.TrimSpace(row.APIKey) != "" {
		return RedactSensitiveValue(row.APIKey)
	}
	return strings.TrimSpace(row.DisplayKey)
}

// CPAAPIKeyDisplayName 返回 CPA API Key 的前端展示名：优先别名，其次使用统一脱敏 key。
func CPAAPIKeyDisplayName(row entities.CPAAPIKey) string {
	if strings.TrimSpace(row.KeyAlias) != "" {
		return strings.TrimSpace(row.KeyAlias)
	}
	return CPAAPIKeyMaskedDisplayKey(row)
}
