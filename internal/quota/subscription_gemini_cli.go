package quota

import "strings"

func resolveGeminiCLISubscription(result any) *SubscriptionInfo {
	var codeAssist *GeminiCLICodeAssistPayload
	switch value := result.(type) {
	case GeminiCLIResult:
		codeAssist = value.CodeAssist
	case *GeminiCLIResult:
		if value != nil {
			codeAssist = value.CodeAssist
		}
	}
	if codeAssist == nil {
		return nil
	}

	tier := effectiveGeminiCLISubscriptionTier(codeAssist)
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
	return &SubscriptionInfo{Provider: "gemini-cli", Plan: plan, TierID: tierID, TierName: tierName}
}

func effectiveGeminiCLISubscriptionTier(codeAssist *GeminiCLICodeAssistPayload) *GeminiCliUserTier {
	if codeAssist == nil {
		return nil
	}
	tier := codeAssist.CurrentTier
	if codeAssist.PaidTier != nil && strings.TrimSpace(codeAssist.PaidTier.ID) != "" {
		tier = codeAssist.PaidTier
	}
	if tier == nil || (strings.TrimSpace(tier.ID) == "" && strings.TrimSpace(tier.Name) == "") {
		return nil
	}
	return tier
}
