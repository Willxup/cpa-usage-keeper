package test

import (
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
)

func TestSubscriptionObservation_MultiWindow_Independent(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	output := quota.ProviderOutput{
		Provider: "claude",
		Result: quota.ClaudeResult{
			Usage: &quota.ClaudeUsagePayload{
				FiveHour: &quota.ClaudeUsageWindow{Utilization: 35, ResetsAt: "2026-10-26T17:00:00Z"},
				SevenDay: &quota.ClaudeUsageWindow{Utilization: 70, ResetsAt: "2026-11-02T12:00:00Z"},
			},
			Profile: &quota.ClaudeProfileResponse{Account: &quota.ClaudeProfileAccount{Email: "test@example.com"}},
		},
	}
	identity := entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude"}
	obs := quota.NormalizeSubscriptionObservation(output, "claude-auth", identity, now, nil, nil)

	if obs == nil {
		t.Fatal("expected non-nil observation")
	}
	if obs.Status != quota.ObservationStatusComplete {
		t.Fatalf("expected status complete, got %s", obs.Status)
	}
	if len(obs.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(obs.Windows))
	}
	w1 := obs.Windows[0]
	w2 := obs.Windows[1]
	if w1.Key != "five_hour" || *w1.Percent != 35 {
		t.Errorf("unexpected w1: %#v", w1)
	}
	if w2.Key != "seven_day" || *w2.Percent != 70 {
		t.Errorf("unexpected w2: %#v", w2)
	}
	// Assert values are not summed or averaged
	if w1.Percent == w2.Percent {
		t.Errorf("windows should remain independent")
	}
}

func TestSubscriptionObservation_MissingValues_NotZero(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	// Window without numeric used or limit
	output := quota.ProviderOutput{
		Provider: "codex",
		Result: quota.CodexResult{
			Usage: &quota.CodexUsagePayload{
				RateLimit: &quota.CodexRateLimitInfo{
					PrimaryWindow: &quota.CodexUsageWindow{
						UsedPercent:        0, // explicitly zero percent
						LimitWindowSeconds: 18000,
						ResetAfterSeconds:  600,
					},
				},
			},
		},
	}
	identity := entities.UsageIdentity{Identity: "codex-auth", Provider: "codex", Type: "codex"}
	obs := quota.NormalizeSubscriptionObservation(output, "codex-auth", identity, now, nil, nil)

	if len(obs.Windows) == 0 {
		t.Fatal("expected at least 1 window")
	}
	primary := obs.Windows[0]
	// Used and Limit were not in upstream window, so they must be nil, NOT 0
	if primary.Used != nil {
		t.Errorf("expected Used to be nil, got %v", *primary.Used)
	}
	if primary.Limit != nil {
		t.Errorf("expected Limit to be nil, got %v", *primary.Limit)
	}
	if primary.Percent == nil || *primary.Percent != 0 {
		t.Errorf("expected explicit Percent=0, got %v", primary.Percent)
	}
}

func TestSubscriptionObservation_ResetSemantics_Classification(t *testing.T) {
	sec := int64(3600)
	tests := []struct {
		name              string
		resetAt           string
		resetAfterSeconds *int64
		want              quota.ResetSemantics
	}{
		{"rfc3339 absolute", "2026-10-26T18:00:00Z", nil, quota.ResetSemanticsAbsolute},
		{"rolling continuous", "continuous rolling window", nil, quota.ResetSemanticsContinuous},
		{"relative countdown", "", &sec, quota.ResetSemanticsRelative},
		{"unknown empty", "", nil, quota.ResetSemanticsUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quota.ClassifyResetSemantics(tt.resetAt, tt.resetAfterSeconds)
			if got != tt.want {
				t.Errorf("ClassifyResetSemantics(%q, %v) = %v, want %v", tt.resetAt, tt.resetAfterSeconds, got, tt.want)
			}
		})
	}
}

func TestSubscriptionObservation_Timestamp_Separation(t *testing.T) {
	observedAt := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	lastSuccessAt := time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC)
	output := quota.ProviderOutput{
		Provider: "claude",
		Result: quota.ClaudeResult{
			Usage: &quota.ClaudeUsagePayload{
				FiveHour: &quota.ClaudeUsageWindow{Utilization: 50, ResetsAt: "2026-10-26T17:00:00Z"},
			},
		},
	}
	identity := entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude"}
	obs := quota.NormalizeSubscriptionObservation(output, "claude-auth", identity, observedAt, &lastSuccessAt, nil)

	if !obs.ObservedAt.Equal(observedAt) {
		t.Errorf("expected ObservedAt %v, got %v", observedAt, obs.ObservedAt)
	}
	if obs.LastSuccessAt == nil || !obs.LastSuccessAt.Equal(lastSuccessAt) {
		t.Errorf("expected LastSuccessAt %v, got %v", lastSuccessAt, obs.LastSuccessAt)
	}
	expectedReset := time.Date(2026, 10, 26, 17, 0, 0, 0, time.UTC)
	if obs.ResetAt == nil || !obs.ResetAt.Equal(expectedReset) {
		t.Errorf("expected ResetAt %v, got %v", expectedReset, obs.ResetAt)
	}
}

func TestSubscriptionObservation_EnrichmentDegradation(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	// Claude with usage but Profile=nil
	claudeOutput := quota.ProviderOutput{
		Provider: "claude",
		Result: quota.ClaudeResult{
			Usage: &quota.ClaudeUsagePayload{
				FiveHour: &quota.ClaudeUsageWindow{Utilization: 20, ResetsAt: "2026-10-26T17:00:00Z"},
			},
			Profile: nil,
		},
	}
	identity := entities.UsageIdentity{Identity: "claude-auth", Provider: "claude", Type: "claude"}
	claudeObs := quota.NormalizeSubscriptionObservation(claudeOutput, "claude-auth", identity, now, nil, nil)
	if claudeObs.Status != quota.ObservationStatusPartial {
		t.Errorf("expected partial status on Claude profile degradation, got %s", claudeObs.Status)
	}
	if claudeObs.Confidence != 0.85 {
		t.Errorf("expected 0.85 confidence on partial degradation, got %v", claudeObs.Confidence)
	}
	if len(claudeObs.Windows) != 1 {
		t.Errorf("expected 1 window preserved, got %d", len(claudeObs.Windows))
	}
}

func TestSubscriptionObservation_FailedPreservesStale(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	lastSuccess := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	fetchErr := errors.New("upstream connection timeout")
	identity := entities.UsageIdentity{Identity: "test-auth", Provider: "claude", Type: "claude"}

	obs := quota.NormalizeSubscriptionObservation(quota.ProviderOutput{}, "test-auth", identity, now, &lastSuccess, fetchErr)
	if obs.Status != quota.ObservationStatusFailed {
		t.Errorf("expected status failed, got %s", obs.Status)
	}
	if obs.Freshness != quota.FreshnessStale {
		t.Errorf("expected freshness stale, got %s", obs.Freshness)
	}
	if obs.Confidence != 0.0 {
		t.Errorf("expected 0.0 confidence on failure, got %v", obs.Confidence)
	}
	if obs.Error == nil || obs.Error.Code != "quota_fetch_failed" {
		t.Errorf("expected populated Error, got %#v", obs.Error)
	}
}


