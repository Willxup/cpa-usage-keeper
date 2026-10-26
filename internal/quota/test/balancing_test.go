package test

import (
	"testing"
	"time"

	"cpa-usage-keeper/internal/quota"
)

func TestQuotaBalancer_CalculateBurnRate(t *testing.T) {
	// Consumed 20% over 2 hours -> 10% per hour
	rate := quota.CalculateBurnRate(30.0, 50.0, 2*time.Hour)
	if rate.BurnRatePerHour != 10.0 {
		t.Errorf("expected 10%% per hour, got %v", rate.BurnRatePerHour)
	}
	if rate.UsedDeltaPercent != 20.0 {
		t.Errorf("expected 20%% delta, got %v", rate.UsedDeltaPercent)
	}

	// Negative delta (quota reset) returns 0 delta
	resetRate := quota.CalculateBurnRate(80.0, 10.0, 1*time.Hour)
	if resetRate.BurnRatePerHour != 0.0 {
		t.Errorf("expected 0%% burn rate on reset, got %v", resetRate.BurnRatePerHour)
	}
}

func TestQuotaBalancer_HeadroomEvaluation(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	balancer := quota.NewQuotaBalancer(20.0, 5*time.Minute)

	// 1. Healthy credential (80% remaining)
	usedP80 := 20.0
	obsHealthy := quota.SubscriptionObservation{
		AuthIndex: "auth-healthy",
		Provider:  "codex",
		Windows: []quota.QuotaWindowItem{
			{Key: "primary", Percent: &usedP80},
		},
	}
	recHealthy := balancer.Evaluate(obsHealthy, nil, now, 10)
	if recHealthy.HeadroomStatus != quota.HeadroomHealthy {
		t.Errorf("expected healthy headroom, got %s", recHealthy.HeadroomStatus)
	}
	if recHealthy.CanAdjust {
		t.Errorf("expected CanAdjust=false for healthy credential")
	}

	// 2. Critical credential (10% remaining)
	usedP90 := 90.0
	obsCritical := quota.SubscriptionObservation{
		AuthIndex: "auth-critical",
		Provider:  "codex",
		Windows: []quota.QuotaWindowItem{
			{Key: "primary", Percent: &usedP90},
		},
	}
	recCritical := balancer.Evaluate(obsCritical, nil, now, 10)
	if recCritical.HeadroomStatus != quota.HeadroomCritical {
		t.Errorf("expected critical headroom, got %s", recCritical.HeadroomStatus)
	}
	if !recCritical.CanAdjust {
		t.Errorf("expected CanAdjust=true for critical headroom")
	}
	if recCritical.SuggestedPriority == nil || *recCritical.SuggestedPriority != 5 {
		t.Errorf("expected priority reduced to 5, got %v", recCritical.SuggestedPriority)
	}

	// 3. Exhausted credential (0% remaining)
	usedP100 := 100.0
	obsExhausted := quota.SubscriptionObservation{
		AuthIndex: "auth-exhausted",
		Provider:  "codex",
		Windows: []quota.QuotaWindowItem{
			{Key: "primary", Percent: &usedP100},
		},
	}
	recExhausted := balancer.Evaluate(obsExhausted, nil, now, 10)
	if recExhausted.HeadroomStatus != quota.HeadroomExhausted {
		t.Errorf("expected exhausted headroom, got %s", recExhausted.HeadroomStatus)
	}
	if recExhausted.SuggestedPriority == nil || *recExhausted.SuggestedPriority != 1 {
		t.Errorf("expected priority reduced to 1, got %v", recExhausted.SuggestedPriority)
	}
}

func TestQuotaBalancer_HysteresisCooldown(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	balancer := quota.NewQuotaBalancer(20.0, 5*time.Minute)

	usedP90 := 90.0
	obs := quota.SubscriptionObservation{
		AuthIndex: "auth-1",
		Provider:  "claude",
		Windows: []quota.QuotaWindowItem{
			{Key: "five_hour", Percent: &usedP90},
		},
	}

	// First adjustment allowed
	rec1 := balancer.Evaluate(obs, nil, now, 10)
	if !rec1.CanAdjust {
		t.Fatalf("expected initial adjustment allowed")
	}
	balancer.RecordAdjustment("auth-1", now)

	// Second adjustment 2 minutes later blocked by hysteresis
	twoMinutesLater := now.Add(2 * time.Minute)
	rec2 := balancer.Evaluate(obs, nil, twoMinutesLater, 10)
	if rec2.CanAdjust {
		t.Errorf("expected adjustment blocked by hysteresis within 5m")
	}

	// Third adjustment 6 minutes later allowed
	sixMinutesLater := now.Add(6 * time.Minute)
	rec3 := balancer.Evaluate(obs, nil, sixMinutesLater, 10)
	if !rec3.CanAdjust {
		t.Errorf("expected adjustment allowed after hysteresis window passes")
	}
}
