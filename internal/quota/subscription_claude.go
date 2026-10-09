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
		return newClaudeSubscription(claudeMaxPlan(profile.Organization))
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

// claudeMaxPlan 从组织的 rate_limit_tier 区分 Max 5x / 20x；
// 缺失或无法识别时退回不带倍数的 max。
func claudeMaxPlan(organization *ClaudeProfileOrganization) string {
	if organization == nil {
		return "max"
	}
	switch organization.RateLimitTier {
	case "default_claude_max_5x":
		return "max-5x"
	case "default_claude_max_20x":
		return "max-20x"
	default:
		return "max"
	}
}

func newClaudeSubscription(plan string) *SubscriptionInfo {
	return &SubscriptionInfo{Provider: "claude", Plan: plan}
}
