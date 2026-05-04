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

func newUsageSourceResolver(identities []models.UsageIdentity) usageSourceResolver {
	authIdentities := make(map[string]models.UsageIdentity, len(identities))
	providerIdentities := make(map[string]models.UsageIdentity, len(identities))
	providerRawByKey := make(map[string]string, len(identities))
	for _, identity := range identities {
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

func (r usageSourceResolver) resolve(rawSource string, authIndex string) usageSourceResolution {
	normalizedSource := strings.TrimSpace(rawSource)
	if normalizedSource != "" {
		if item, ok := r.providerIdentities[normalizedSource]; ok {
			return usageSourceResolutionFromIdentity(item, normalizedSource)
		}
	}

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

func uintToString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func looksLikeEmail(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	atIndex := strings.Index(trimmed, "@")
	return atIndex > 0 && atIndex < len(trimmed)-1 && strings.Contains(trimmed[atIndex+1:], ".")
}

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
