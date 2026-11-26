package api

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
)

type usageIdentityResolver struct {
	credentialsByIdentity map[string]entities.UsageIdentity
	providersByIdentity   map[string]entities.UsageIdentity
	maskAccountNames      bool
}

func newUsageIdentityResolver(identities []entities.UsageIdentity) usageIdentityResolver {
	return buildUsageIdentityResolver(identities, false)
}

func newMaskedUsageIdentityResolver(identities []entities.UsageIdentity) usageIdentityResolver {
	return buildUsageIdentityResolver(identities, true)
}

func buildUsageIdentityResolver(identities []entities.UsageIdentity, maskAccountNames bool) usageIdentityResolver {
	credentialsByIdentity := make(map[string]entities.UsageIdentity, len(identities))
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
		case entities.UsageIdentityAuthTypeCredential:
			credentialsByIdentity[key] = identity
		case entities.UsageIdentityAuthTypeAIProvider:
			providersByIdentity[key] = identity
		}
	}

	return usageIdentityResolver{
		credentialsByIdentity: credentialsByIdentity,
		providersByIdentity:   providersByIdentity,
		maskAccountNames:      maskAccountNames,
	}
}

type resolvedUsageIdentity struct {
	DisplayName string
	Type        string
}

func resolvedUsageIdentityFromEntity(item entities.UsageIdentity, maskAccountNames bool) resolvedUsageIdentity {
	displayName := helper.UsageIdentityDisplayName(item)
	if maskAccountNames {
		displayName = helper.UsageIdentityMaskedDisplayName(item)
	}
	return resolvedUsageIdentity{
		DisplayName: displayName,
		Type:        strings.TrimSpace(item.Type),
	}
}

func (r usageIdentityResolver) resolveByAuthIndex(authIndex string) (resolvedUsageIdentity, bool) {
	key := strings.TrimSpace(authIndex)
	if key == "" {
		return resolvedUsageIdentity{}, false
	}
	if identity, ok := r.providersByIdentity[key]; ok {
		return resolvedUsageIdentityFromEntity(identity, r.maskAccountNames), true
	}
	if identity, ok := r.credentialsByIdentity[key]; ok {
		return resolvedUsageIdentityFromEntity(identity, r.maskAccountNames), true
	}
	return resolvedUsageIdentity{}, false
}
