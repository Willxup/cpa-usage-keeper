package quota

import "strings"

func resolveClaudeSubscription(result any) *SubscriptionInfo {
	var profile *ClaudeProfileResponse
	switch value := result.(type) {
	case ClaudeResult:
		profile = value.Profile
	case *ClaudeResult:
		if value != nil {
			profile = value.Profile
		}
	}
	if profile == nil {
		return nil
	}

	// 套餐优先级保持 Max → Pro → active Team；Free 还必须不存在其它明确的组织套餐。
	if profile.Account != nil && profile.Account.HasClaudeMax != nil && *profile.Account.HasClaudeMax {
		return newClaudeSubscription("max")
	}
	if profile.Account != nil && profile.Account.HasClaudePro != nil && *profile.Account.HasClaudePro {
		return newClaudeSubscription("pro")
	}
	organizationType := ""
	subscriptionStatus := ""
	if profile.Organization != nil {
		organizationType = strings.ToLower(strings.TrimSpace(profile.Organization.OrganizationType))
		subscriptionStatus = strings.ToLower(strings.TrimSpace(profile.Organization.SubscriptionStatus))
	}
	if organizationType == "claude_team" && subscriptionStatus == "active" {
		return newClaudeSubscription("team")
	}
	if organizationType != "" && organizationType != "claude_free" {
		return nil
	}
	if profile.Account != nil &&
		profile.Account.HasClaudeMax != nil && !*profile.Account.HasClaudeMax &&
		profile.Account.HasClaudePro != nil && !*profile.Account.HasClaudePro {
		return newClaudeSubscription("free")
	}
	return nil
}

func newClaudeSubscription(plan string) *SubscriptionInfo {
	return &SubscriptionInfo{Provider: "claude", Plan: plan}
}
