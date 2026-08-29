package test

import (
	"encoding/json"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/quota"
)

func TestQuotaSubscriptionResponseUsesResponseLevelContract(t *testing.T) {
	body, err := json.Marshal(quota.CheckResponse{
		ID:           "codex-auth",
		Quota:        []quota.QuotaRow{{Key: "rate_limit.primary_window", Label: "5h"}},
		Subscription: &quota.SubscriptionInfo{Provider: "codex", Plan: "pro-20x"},
	})
	if err != nil {
		t.Fatalf("marshal quota response: %v", err)
	}

	text := string(body)
	if !strings.Contains(text, `"subscription":{"provider":"codex","plan":"pro-20x"}`) {
		t.Fatalf("expected response-level subscription, got %s", text)
	}
	if strings.Contains(text, "planType") || strings.Contains(text, "plan_type") {
		t.Fatalf("expected legacy plan fields to be absent, got %s", text)
	}
}
