package helper

import (
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"
)

const sensitiveValueMask = "*********"

// RedactSensitiveValue 使用统一格式隐藏前端展示中的敏感值：长值保留前 3 位和后 6 位，短值全隐藏。
func RedactSensitiveValue(value string) string {
	if IsPluginAPIKeyGroupKey(value) {
		return "Third-party Identity"
	}
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
	if row.SourceType == entities.CPAAPIKeySourcePlugin {
		return fmt.Sprintf("Third-party Identity #%d", row.ID)
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

// MaskAccountName 对面向 API Key 只读视图的账号名称/邮箱进行脱敏，保留少量首尾字符以便区分不同凭证。
func MaskAccountName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if at := strings.LastIndex(trimmed, "@"); at > 0 && at < len(trimmed)-1 {
		local := trimmed[:at]
		domain := trimmed[at+1:]
		return maskEmailLocal(local) + "@" + maskEmailDomain(domain)
	}
	return maskGenericLabel(trimmed)
}

func maskEmailLocal(local string) string {
	runes := []rune(strings.TrimSpace(local))
	switch n := len(runes); {
	case n == 0:
		return "***"
	case n == 1:
		return "*"
	case n == 2:
		return string(runes[:1]) + "***"
	case n <= 4:
		return string(runes[:1]) + "***" + string(runes[n-1:])
	default:
		return string(runes[:2]) + "***" + string(runes[n-2:])
	}
}

func maskEmailDomain(domain string) string {
	cleaned := strings.TrimSpace(domain)
	if strings.HasSuffix(strings.ToLower(cleaned), ".json") && len(cleaned) > 5 {
		cleaned = cleaned[:len(cleaned)-5]
	}
	parts := strings.Split(cleaned, ".")
	if len(parts) < 2 {
		return maskDomainLabel(cleaned)
	}
	tld := parts[len(parts)-1]
	head := parts[0]
	if head == "" || tld == "" {
		return maskDomainLabel(cleaned)
	}
	return maskDomainLabel(head) + "." + tld
}

func maskDomainLabel(label string) string {
	runes := []rune(strings.TrimSpace(label))
	if len(runes) == 0 {
		return "***"
	}
	return string(runes[:1]) + "***"
}

func maskGenericLabel(value string) string {
	runes := []rune(value)
	switch n := len(runes); {
	case n <= 2:
		return "***"
	case n <= 6:
		return string(runes[:1]) + "***" + string(runes[n-1:])
	default:
		return string(runes[:2]) + "***" + string(runes[n-2:])
	}
}

// UsageIdentityMaskedDisplayName 返回面向 API Key 只读视图的凭证脱敏展示名；管理员设置的非邮箱别名保持原样。
func UsageIdentityMaskedDisplayName(item entities.UsageIdentity) string {
	if item.Alias != nil {
		if alias := strings.TrimSpace(*item.Alias); alias != "" {
			if !strings.Contains(alias, "@") {
				return alias
			}
			return MaskAccountName(alias)
		}
	}
	return MaskAccountName(UsageIdentityDisplayName(item))
}
