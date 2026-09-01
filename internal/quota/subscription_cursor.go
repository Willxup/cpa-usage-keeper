package quota

import "strings"

func resolveCursorSubscription(result any) *SubscriptionInfo {
	var plan *CursorPlanPayload
	switch value := result.(type) {
	case CursorResult:
		plan = value.Plan
	case *CursorResult:
		if value != nil {
			plan = value.Plan
		}
	}
	if plan == nil || plan.PlanInfo == nil {
		return nil
	}
	return newCursorSubscription(plan.PlanInfo.PlanName)
}

func newCursorSubscription(rawPlan string) *SubscriptionInfo {
	displayPlan := strings.TrimSpace(rawPlan)
	if displayPlan == "" {
		return nil
	}
	return &SubscriptionInfo{Provider: "cursor", Plan: strings.ToLower(displayPlan)}
}
