package api

import (
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/redact"
)

type usageSourceResolver struct {
	authIdentities     map[string]models.UsageIdentity
	providerIdentities map[string]models.UsageIdentity
	providerRawByKey   map[string]string
}

// newUsageSourceResolver 把活跃 usage identity 建成内存索引，供 Credentials 和事件展示快速解析 source。
func newUsageSourceResolver(identities []models.UsageIdentity) usageSourceResolver {
	authIdentities := make(map[string]models.UsageIdentity, len(identities))
	providerIdentities := make(map[string]models.UsageIdentity, len(identities))
	providerRawByKey := make(map[string]string, len(identities))
	for _, identity := range identities {
		// resolver 索引只收录活跃身份，避免 deleted identity 影响 Credentials 和事件展示解析。
		if identity.IsDeleted {
			continue
		}
		key := strings.TrimSpace(identity.Identity)
		if key == "" {
			continue
		}
		switch identity.AuthType {
		case models.UsageIdentityAuthTypeAuthFile:
			authIdentities[key] = identity
		case models.UsageIdentityAuthTypeAIProvider:
			providerIdentities[key] = identity
			resolved := usageSourceResolutionFromIdentity(identity, key)
			if resolved.SourceKey != "" {
				providerRawByKey[resolved.SourceKey] = key
			}
		}
	}

	return usageSourceResolver{
		authIdentities:     authIdentities,
		providerIdentities: providerIdentities,
		providerRawByKey:   providerRawByKey,
	}
}

type usageSourceResolution struct {
	DisplayName string
	SourceType  string
	SourceKey   string
}

// usageSourceResolutionFromIdentity 从 provider identity 生成前端展示名、类型和稳定 source_key。
func usageSourceResolutionFromIdentity(item models.UsageIdentity, fallbackIdentity string) usageSourceResolution {
	identityType := safeAIProviderDisplayValue(item.Type, fallbackIdentity, "")
	displayName := firstNonEmptyString(
		safeAIProviderDisplayValue(item.Name, fallbackIdentity, ""),
		safeAIProviderDisplayValue(item.Provider, fallbackIdentity, ""),
		identityType,
		redact.APIKeyDisplayName(fallbackIdentity),
	)
	sourceKey := "provider:" + uintToString(item.ID)
	if item.ID == 0 {
		sourceKey = "provider:" + redact.APIKeyDisplayName(fallbackIdentity)
	}
	return usageSourceResolution{
		DisplayName: displayName,
		SourceType:  identityType,
		SourceKey:   sourceKey,
	}
}

// rawSourceForPublicValue 把前端传回的 provider source_key 还原成真实 source，用于后端过滤 usage_events。
func (r usageSourceResolver) rawSourceForPublicValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if raw, ok := r.providerRawByKey[trimmed]; ok {
		return raw
	}
	return trimmed
}

// resolve 按 provider source、auth_index、fallback 推断的顺序解析一条 usage 记录的前端展示来源。
func (r usageSourceResolver) resolve(rawSource string, authIndex string) usageSourceResolution {
	// 优先用 API key source 匹配 AI provider identity，确保 provider 展示名和 source_key 稳定。
	normalizedSource := strings.TrimSpace(rawSource)
	if normalizedSource != "" {
		if item, ok := r.providerIdentities[normalizedSource]; ok {
			return usageSourceResolutionFromIdentity(item, normalizedSource)
		}
	}

	// provider source 没有命中时，再用 oauth/auth file 的 auth_index 解析账号身份。
	normalizedAuthIndex := strings.TrimSpace(authIndex)
	if normalizedAuthIndex != "" {
		if identity, ok := r.authIdentities[normalizedAuthIndex]; ok {
			displayName := firstNonEmptyString(identity.Name, normalizedAuthIndex)
			return usageSourceResolution{
				DisplayName: displayName,
				SourceType:  firstNonEmptyString(identity.Type, identity.Provider),
				SourceKey:   "auth:" + normalizedAuthIndex,
			}
		}
	}

	// 没有 identity 命中时走安全 fallback，避免把原始 API key 暴露给前端。
	if normalizedSource == "" {
		return usageSourceResolution{DisplayName: "-", SourceKey: "raw:-"}
	}
	if looksLikeEmail(normalizedSource) {
		return usageSourceResolution{
			DisplayName: normalizedSource,
			SourceKey:   "email:" + normalizedSource,
		}
	}
	if inferredProvider := inferUsageProviderType(normalizedSource); inferredProvider != "" {
		return usageSourceResolution{
			DisplayName: inferredProvider,
			SourceType:  inferredProvider,
			SourceKey:   "provider:fallback:" + inferredProvider,
		}
	}
	masked := redact.APIKeyDisplayName(normalizedSource)
	return usageSourceResolution{
		DisplayName: masked,
		SourceKey:   "raw:" + masked,
	}
}

// uintToString 统一把数据库 ID 转成 source_key 使用的字符串片段。
func uintToString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}

// firstNonEmptyString 按优先级返回第一个非空展示字段。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// looksLikeEmail 识别邮箱类 source，邮箱本身可作为安全展示值。
func looksLikeEmail(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	atIndex := strings.Index(trimmed, "@")
	return atIndex > 0 && atIndex < len(trimmed)-1 && strings.Contains(trimmed[atIndex+1:], ".")
}

// inferUsageProviderType 在没有 metadata 命中时，根据 source 特征推断 provider 类型作为兜底展示。
func inferUsageProviderType(source string) string {
	value := strings.ToLower(strings.TrimSpace(source))
	switch {
	case value == "":
		return ""
	case strings.Contains(value, "ampcode"):
		return "ampcode"
	case strings.HasPrefix(value, "sk-ant-") || strings.Contains(value, "anthropic") || strings.Contains(value, "claude"):
		return "claude"
	case strings.HasPrefix(value, "sk-proj-") || strings.HasPrefix(value, "sk-") || strings.Contains(value, "openai") || strings.Contains(value, "gpt"):
		return "openai"
	case strings.HasPrefix(value, "aiza") || strings.Contains(value, "gemini"):
		return "gemini"
	case strings.Contains(value, "vertex"):
		return "vertex"
	case strings.Contains(value, "codex"):
		return "codex"
	default:
		return ""
	}
}
