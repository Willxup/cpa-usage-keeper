package quota

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

var claudeGrantIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,40}$`)
var claudeUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type ClaudeResetGrant struct {
	ID               string   `json:"id"`
	Label            string   `json:"label,omitempty"`
	ResetsTotal      int      `json:"resetsTotal"`
	ResetsLeft       int      `json:"resetsLeft"`
	StartsAt         string   `json:"startsAt,omitempty"`
	EndsAt           string   `json:"endsAt,omitempty"`
	Clears           []string `json:"clears"`
	Paused           bool     `json:"paused"`
	UsableNow        bool     `json:"usableNow"`
	UseRequiresLimit bool     `json:"useRequiresLimit"`
}

type ClaudeResetGrantStatus struct {
	Eligible         bool               `json:"eligible"`
	IneligibleReason string             `json:"ineligibleReason,omitempty"`
	AtLimit          bool               `json:"atLimit"`
	Grants           []ClaudeResetGrant `json:"grants"`
	NextGrantID      string             `json:"nextGrantId,omitempty"`
	WeeklyResetsAt   string             `json:"weeklyResetsAt,omitempty"`
	CooldownUntil    string             `json:"cooldownUntil,omitempty"`
	AvailableCount   int                `json:"availableCount"`
}

// grants 是可选权益信息，任何缺失或畸形只丢弃该区块，不影响普通额度。
func parseClaudeResetGrants(raw json.RawMessage) *ClaudeResetGrantStatus {
	var block struct {
		Eligible         *bool   `json:"eligible"`
		IneligibleReason *string `json:"ineligible_reason"`
		AtLimit          *bool   `json:"at_limit"`
		Grants           []struct {
			ID            string   `json:"id"`
			Label         string   `json:"label"`
			Total         *int     `json:"resets_total"`
			Left          *int     `json:"resets_left"`
			Starts        *string  `json:"starts_at"`
			Ends          *string  `json:"ends_at"`
			Clears        []string `json:"clears"`
			Paused        *bool    `json:"paused"`
			Usable        *bool    `json:"usable_now"`
			RequiresLimit *bool    `json:"use_requires_limit"`
		} `json:"grants"`
		Next     *string `json:"next_grant_id"`
		Weekly   *string `json:"weekly_resets_at"`
		Cooldown *string `json:"cooldown_until"`
	}
	if json.Unmarshal(raw, &block) != nil || block.Eligible == nil || len(block.Grants) > 64 || !claudeOptionalTimeValid(block.Weekly) || !claudeOptionalTimeValid(block.Cooldown) {
		return nil
	}
	status := &ClaudeResetGrantStatus{Eligible: *block.Eligible, AtLimit: block.AtLimit != nil && *block.AtLimit, Grants: []ClaudeResetGrant{}, WeeklyResetsAt: optionalClaudeString(block.Weekly), CooldownUntil: optionalClaudeString(block.Cooldown)}
	if block.IneligibleReason != nil {
		switch *block.IneligibleReason {
		case "config_off", "tier", "seat", "mobile", "surface", "cli_version", "no_grant", "tenure", "other_experiment", "unavailable", "unknown":
			status.IneligibleReason = *block.IneligibleReason
		default:
			status.IneligibleReason = "unknown"
		}
	}
	seen := map[string]bool{}
	for _, g := range block.Grants {
		if !claudeGrantIDPattern.MatchString(g.ID) || seen[g.ID] || g.Total == nil || g.Left == nil || *g.Total < 0 || *g.Left < 0 || *g.Left > *g.Total || !claudeOptionalTimeValid(g.Starts) || !claudeOptionalTimeValid(g.Ends) {
			return nil
		}
		seen[g.ID] = true
		// 上游范围是展示数据，保留未知值、顺序与重复值，不猜测或归并。
		clears := append([]string{}, g.Clears...)
		label := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, g.Label)), " ")
		if len([]rune(label)) > 120 {
			label = string([]rune(label)[:120])
		}
		status.Grants = append(status.Grants, ClaudeResetGrant{ID: g.ID, Label: label, ResetsTotal: *g.Total, ResetsLeft: *g.Left, StartsAt: optionalClaudeString(g.Starts), EndsAt: optionalClaudeString(g.Ends), Clears: clears, Paused: g.Paused != nil && *g.Paused, UsableNow: g.Usable != nil && *g.Usable, UseRequiresLimit: g.RequiresLimit == nil || *g.RequiresLimit})
		// 避免畸形巨大整数的加总溢出被误显示为可用权益。
		if *g.Left > int(^uint(0)>>1)-status.AvailableCount {
			return nil
		}
		status.AvailableCount += *g.Left
	}
	if block.Next != nil && seen[*block.Next] {
		status.NextGrantID = *block.Next
	}
	return status
}

func optionalClaudeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func claudeOptionalTimeValid(value *string) bool {
	if value == nil {
		return true
	}
	_, err := time.Parse(time.RFC3339Nano, *value)
	return err == nil
}

func claudeGrantUsable(status *ClaudeResetGrantStatus, grant ClaudeResetGrant, now time.Time) bool {
	if status == nil || !status.Eligible || grant.Paused || !grant.UsableNow || grant.ResetsLeft <= 0 || (grant.UseRequiresLimit && !status.AtLimit) {
		return false
	}
	for _, bound := range []struct {
		value string
		start bool
	}{{grant.StartsAt, true}, {grant.EndsAt, false}, {status.CooldownUntil, true}} {
		if bound.value == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, bound.value)
		if err != nil || (bound.start && now.Before(at)) || (!bound.start && !now.Before(at)) {
			return false
		}
	}
	return true
}

func selectClaudeResetGrant(status *ClaudeResetGrantStatus, now time.Time) *ClaudeResetGrant {
	if status == nil {
		return nil
	}
	usable := []ClaudeResetGrant{}
	for _, grant := range status.Grants {
		if claudeGrantUsable(status, grant, now) {
			if grant.ID == status.NextGrantID {
				return &grant
			}
			usable = append(usable, grant)
		}
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].ID < usable[j].ID })
	if len(usable) == 0 {
		return nil
	}
	return &usable[0]
}
