package quota

import (
	"strings"

	"cpa-usage-keeper/internal/entities"
)

type subscriptionResolver func(result any) *SubscriptionInfo

var subscriptionResolvers = map[string]subscriptionResolver{
	"antigravity": resolveAntigravitySubscription,
	"claude":      resolveClaudeSubscription,
	"codex":       resolveCodexSubscription,
}

// NormalizeSubscription 将 provider 已取得的原始结果转换为响应级订阅，不执行任何外部请求。
func NormalizeSubscription(output ProviderOutput) *SubscriptionInfo {
	provider := normalizeIdentityType(output.Provider)
	resolver, ok := subscriptionResolvers[provider]
	if !ok {
		return nil
	}
	subscription := resolver(output.Result)
	if subscription == nil {
		return nil
	}
	subscription.Provider = normalizeIdentityType(subscription.Provider)
	subscription.Plan = strings.TrimSpace(subscription.Plan)
	subscription.TierID = strings.TrimSpace(subscription.TierID)
	subscription.TierName = strings.TrimSpace(subscription.TierName)
	if subscription.Provider == "" || subscription.Provider != provider || subscription.Plan == "" {
		return nil
	}
	return subscription
}

// ResolveIdentitySubscription 只发布明确属于 Codex Auth File 的内部套餐 metadata。
func ResolveIdentitySubscription(identity entities.UsageIdentity) *SubscriptionInfo {
	if identity.AuthType != entities.UsageIdentityAuthTypeAuthFile {
		return nil
	}
	if normalizeIdentityType(identity.Type) != "codex" && normalizeIdentityType(identity.Provider) != "codex" {
		return nil
	}
	if identity.PlanType == nil {
		return nil
	}
	return newCodexSubscription(*identity.PlanType)
}
