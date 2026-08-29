package quota

import "strings"

func resolveAntigravitySubscription(result any) *SubscriptionInfo {
	var subscription *AntigravitySubscriptionPayload
	switch value := result.(type) {
	case AntigravityResult:
		subscription = value.Subscription
	case *AntigravityResult:
		if value != nil {
			subscription = value.Subscription
		}
	}
	if subscription == nil {
		return nil
	}

	tier := effectiveAntigravitySubscriptionTier(subscription)
	if tier == nil {
		return nil
	}
	tierID := strings.TrimSpace(tier.ID)
	tierName := strings.TrimSpace(tier.Name)

	plan := "unknown"
	switch tierID {
	case "free-tier":
		plan = "free"
	case "g1-pro-tier":
		plan = "pro"
	case "g1-ultra-lite-tier":
		plan = "ultra-lite"
	case "g1-ultra-tier":
		plan = "ultra"
	}
	return &SubscriptionInfo{Provider: "antigravity", Plan: plan, TierID: tierID, TierName: tierName}
}

func effectiveAntigravitySubscriptionTier(subscription *AntigravitySubscriptionPayload) *GeminiCliUserTier {
	if subscription == nil {
		return nil
	}
	// paidTier 只有在 ID 明确存在时才覆盖 currentTier，与上游管理中心保持一致。
	tier := subscription.CurrentTier
	if subscription.PaidTier != nil && strings.TrimSpace(subscription.PaidTier.ID) != "" {
		tier = subscription.PaidTier
	}
	if tier == nil || (strings.TrimSpace(tier.ID) == "" && strings.TrimSpace(tier.Name) == "") {
		return nil
	}
	return tier
}
