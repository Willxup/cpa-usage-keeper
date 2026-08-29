package helper

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
)

func UsageIdentityDisplayName(item entities.UsageIdentity) string {
	if item.Alias != nil {
		if alias := strings.TrimSpace(*item.Alias); alias != "" {
			return alias
		}
	}
	name := strings.TrimSpace(item.Name)
	provider := strings.TrimSpace(item.Provider)
	if item.AuthType != entities.UsageIdentityAuthTypeAIProvider {
		if name != "" {
			return name
		}
		return firstNonEmptyString(provider, item.Identity)
	}

	isOpenAICompatible := strings.TrimSpace(item.Type) == "openai"
	if isOpenAICompatible && name != "" && name != "openai" && provider == name {
		if lookupKey := maskedUsageIdentityLookupKey(item.LookupKey); lookupKey != "" {
			return strings.Join(displayQualifiers(name, lookupKey), " @ ")
		}
		return name
	}

	prefix := strings.TrimSpace(item.Prefix)
	baseURL := formatBaseURLDisplay(item.BaseURL)
	qualifiers := displayQualifiers(prefix, baseURL)
	if len(qualifiers) > 0 {
		return strings.Join(qualifiers, " @ ")
	}
	return name
}

func maskedUsageIdentityLookupKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	masked := RedactSensitiveValue(trimmed)
	if masked == "unknown" {
		return ""
	}
	return masked
}

func displayQualifiers(values ...string) []string {
	qualifiers := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		qualifiers = append(qualifiers, value)
	}
	return qualifiers
}

func formatBaseURLDisplay(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(lower, prefix) {
			trimmed = trimmed[len(prefix):]
			break
		}
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if strings.HasSuffix(strings.ToLower(trimmed), "/v1") {
		trimmed = trimmed[:len(trimmed)-len("/v1")]
	}
	return trimmed
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
