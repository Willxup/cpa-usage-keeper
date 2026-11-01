package quota

import (
	"time"
)

type CredentialHeadroomStatus string

const (
	HeadroomHealthy   CredentialHeadroomStatus = "healthy"
	HeadroomWarning   CredentialHeadroomStatus = "warning"
	HeadroomCritical  CredentialHeadroomStatus = "critical"
	HeadroomExhausted CredentialHeadroomStatus = "exhausted"
)

type QuotaBurnRate struct {
	UsedDeltaPercent float64       `json:"used_delta_percent"`
	TimeDelta        time.Duration `json:"time_delta"`
	BurnRatePerHour  float64       `json:"burn_rate_per_hour"`
}

type BalancingRecommendation struct {
	AuthIndex          string                   `json:"auth_index"`
	Provider           string                   `json:"provider"`
	HeadroomStatus     CredentialHeadroomStatus `json:"headroom_status"`
	CurrentRemaining   *float64                 `json:"current_remaining,omitempty"`
	EstimatedExhaustAt *time.Time               `json:"estimated_exhaust_at,omitempty"`
	SuggestedPriority  *int                     `json:"suggested_priority,omitempty"`
	Reason             string                   `json:"reason"`
	CanAdjust          bool                     `json:"can_adjust"`
}

type QuotaBalancer struct {
	headroomThreshold float64
	hysteresisWindow  time.Duration
	lastAdjustments   map[string]time.Time
}

func NewQuotaBalancer(threshold float64, hysteresis time.Duration) *QuotaBalancer {
	if threshold <= 0 {
		threshold = 20.0
	}
	if hysteresis <= 0 {
		hysteresis = 5 * time.Minute
	}
	return &QuotaBalancer{
		headroomThreshold: threshold,
		hysteresisWindow:  hysteresis,
		lastAdjustments:   make(map[string]time.Time),
	}
}

// CalculateBurnRate calculates consumption velocity in % per hour.
func CalculateBurnRate(initialUsedPercent, finalUsedPercent float64, duration time.Duration) QuotaBurnRate {
	if duration <= 0 {
		return QuotaBurnRate{}
	}
	delta := finalUsedPercent - initialUsedPercent
	if delta < 0 {
		delta = 0
	}
	hours := duration.Hours()
	rate := delta / hours
	return QuotaBurnRate{
		UsedDeltaPercent: delta,
		TimeDelta:        duration,
		BurnRatePerHour:  rate,
	}
}

// Evaluate analyzes an observation and produces routing recommendations.
func (b *QuotaBalancer) Evaluate(obs SubscriptionObservation, burnRate *QuotaBurnRate, now time.Time, currentPriority int) BalancingRecommendation {
	rec := BalancingRecommendation{
		AuthIndex:      obs.AuthIndex,
		Provider:       obs.Provider,
		HeadroomStatus: HeadroomHealthy,
		CanAdjust:      false,
	}

	var minRemaining *float64
	for _, w := range obs.Windows {
		if w.Percent != nil {
			rem := 100.0 - *w.Percent
			if minRemaining == nil || rem < *minRemaining {
				minRemaining = &rem
			}
		} else if w.Remaining != nil && w.Limit != nil && *w.Limit > 0 {
			rem := (*w.Remaining / *w.Limit) * 100.0
			if minRemaining == nil || rem < *minRemaining {
				minRemaining = &rem
			}
		}
	}

	rec.CurrentRemaining = minRemaining
	if minRemaining == nil {
		rec.Reason = "no window metrics available"
		return rec
	}

	if *minRemaining <= 0 {
		rec.HeadroomStatus = HeadroomExhausted
		rec.Reason = "quota exhausted"
		newPrio := 1
		rec.SuggestedPriority = &newPrio
	} else if *minRemaining < b.headroomThreshold {
		rec.HeadroomStatus = HeadroomCritical
		rec.Reason = "quota headroom below threshold"
		if currentPriority > 1 {
			newPrio := currentPriority / 2
			if newPrio < 1 {
				newPrio = 1
			}
			rec.SuggestedPriority = &newPrio
		}
	} else if *minRemaining < b.headroomThreshold*1.5 {
		rec.HeadroomStatus = HeadroomWarning
		rec.Reason = "quota headroom in warning zone"
	}

	if burnRate != nil && burnRate.BurnRatePerHour > 0 && *minRemaining > 0 {
		hoursLeft := *minRemaining / burnRate.BurnRatePerHour
		exhaustTime := now.Add(time.Duration(hoursLeft * float64(time.Hour)))
		rec.EstimatedExhaustAt = &exhaustTime

		if obs.ResetAt != nil && exhaustTime.Before(*obs.ResetAt) {
			rec.HeadroomStatus = HeadroomCritical
			rec.Reason = "burn rate projects exhaustion before window reset"
			if currentPriority > 1 {
				newPrio := currentPriority / 2
				if newPrio < 1 {
					newPrio = 1
				}
				rec.SuggestedPriority = &newPrio
			}
		}
	}

	if last, ok := b.lastAdjustments[obs.AuthIndex]; ok {
		if now.Sub(last) < b.hysteresisWindow {
			rec.CanAdjust = false
			rec.Reason += " (in hysteresis cooldown)"
			return rec
		}
	}

	if rec.SuggestedPriority != nil && *rec.SuggestedPriority != currentPriority {
		rec.CanAdjust = true
	}

	return rec
}

func (b *QuotaBalancer) RecordAdjustment(authIndex string, adjustedAt time.Time) {
	if b.lastAdjustments == nil {
		b.lastAdjustments = make(map[string]time.Time)
	}
	b.lastAdjustments[authIndex] = adjustedAt
}
