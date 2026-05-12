package api

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
)

type usageIdentityResolver struct {
	authFilesByIdentity map[string]entities.UsageIdentity
	providersByIdentity map[string]entities.UsageIdentity
}

func newUsageIdentityResolver(identities []entities.UsageIdentity) usageIdentityResolver {
	authFilesByIdentity := make(map[string]entities.UsageIdentity, len(identities))
	providersByIdentity := make(map[string]entities.UsageIdentity, len(identities))
	for _, identity := range identities {
		if identity.IsDeleted {
			continue
		}
		key := strings.TrimSpace(identity.Identity)
		if key == "" {
			continue
		}
		switch identity.AuthType {
		case entities.UsageIdentityAuthTypeAuthFile:
			authFilesByIdentity[key] = identity
		case entities.UsageIdentityAuthTypeAIProvider:
			providersByIdentity[key] = identity
		}
	}

	return usageIdentityResolver{
		authFilesByIdentity: authFilesByIdentity,
		providersByIdentity: providersByIdentity,
	}
}

type resolvedUsageIdentity struct {
	DisplayName string
	Type        string
}

func usageIdentityDisplayName(item entities.UsageIdentity) string {
	name := strings.TrimSpace(item.Name)
	provider := strings.TrimSpace(item.Provider)
	if item.AuthType != entities.UsageIdentityAuthTypeAIProvider {
		if name != "" {
			return name
		}
		return provider
	}

	if strings.TrimSpace(item.Type) == "openai" && name != "" && name != "openai" && provider == name {
		return name
	}

	prefix := strings.TrimSpace(item.Prefix)
	baseURL := formatUsageIdentityBaseURLDisplay(item.BaseURL)
	qualifiers := usageIdentityDisplayQualifiers(prefix, baseURL)
	switch {
	case name != "" && len(qualifiers) > 0:
		return name + "(" + strings.Join(qualifiers, " @ ") + ")"
	case name != "":
		return name
	case prefix != "" && baseURL != "":
		return prefix + "(" + baseURL + ")"
	case prefix != "":
		return prefix
	case provider != "" && baseURL != "":
		return provider + "(" + baseURL + ")"
	case baseURL != "":
		return baseURL
	default:
		return provider
	}
}

func usageIdentityDisplayQualifiers(values ...string) []string {
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

func formatUsageIdentityBaseURLDisplay(raw string) string {
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
	return strings.TrimRight(trimmed, "/")
}

func resolvedUsageIdentityFromEntity(item entities.UsageIdentity) resolvedUsageIdentity {
	return resolvedUsageIdentity{
		DisplayName: usageIdentityDisplayName(item),
		Type:        strings.TrimSpace(item.Type),
	}
}

func (r usageIdentityResolver) resolveByAuthIndex(authIndex string) (resolvedUsageIdentity, bool) {
	key := strings.TrimSpace(authIndex)
	if key == "" {
		return resolvedUsageIdentity{}, false
	}
	if identity, ok := r.providersByIdentity[key]; ok {
		return resolvedUsageIdentityFromEntity(identity), true
	}
	if identity, ok := r.authFilesByIdentity[key]; ok {
		return resolvedUsageIdentityFromEntity(identity), true
	}
	return resolvedUsageIdentity{}, false
}
