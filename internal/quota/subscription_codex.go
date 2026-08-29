package quota

import "strings"

func resolveCodexSubscription(result any) *SubscriptionInfo {
	switch value := result.(type) {
	case CodexResult:
		return codexUsageSubscription(value.Usage)
	case *CodexResult:
		if value == nil {
			return nil
		}
		return codexUsageSubscription(value.Usage)
	default:
		return nil
	}
}

func codexUsageSubscription(usage *CodexUsagePayload) *SubscriptionInfo {
	if usage == nil {
		return nil
	}
	return newCodexSubscription(usage.PlanType)
}

func newCodexSubscription(rawPlan string) *SubscriptionInfo {
	displayPlan := strings.TrimSpace(rawPlan)
	if displayPlan == "" {
		return nil
	}
	canonicalPlan := strings.ToLower(displayPlan)
	switch canonicalPlan {
	case "pro":
		canonicalPlan = "pro-20x"
	case "prolite", "pro-lite", "pro_lite":
		canonicalPlan = "pro-5x"
	case "free", "plus", "team", "pro-5x", "pro-20x", "enterprise":
		// 已知套餐返回稳定机器值；未知套餐保留上游原始显示值。
	default:
		canonicalPlan = displayPlan
	}
	return &SubscriptionInfo{Provider: "codex", Plan: canonicalPlan}
}
