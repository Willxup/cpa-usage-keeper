package relayusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/quota"
)

// kimiAdapter 查询月之暗面 Kimi Coding Plan 用量。
// 用量接口：https://api.kimi.com/coding/v1/usages（仅 Coding API endpoint 接受此 key 类型）。
type kimiAdapter struct {
	client HTTPDoer
	url    string
}

func newKimiAdapter(client HTTPDoer) *kimiAdapter {
	return &kimiAdapter{client: client, url: kimiUsageURL}
}

func (a *kimiAdapter) Platform() string { return "kimi" }

const kimiUsageURL = "https://api.kimi.com/coding/v1/usages"

type kimiUsageDetail struct {
	Limit     string `json:"limit"`
	Used      string `json:"used"`
	Remaining string `json:"remaining"`
	ResetTime string `json:"resetTime"`
}

type kimiWindowLimit struct {
	Window struct {
		Duration int    `json:"duration"`
		TimeUnit string `json:"timeUnit"`
	} `json:"window"`
	Detail kimiUsageDetail `json:"detail"`
}

type kimiUsageResponse struct {
	Usage    *kimiUsageDetail  `json:"usage"`
	Limits   []kimiWindowLimit `json:"limits"`
	Parallel *struct {
		Limit string `json:"limit"`
	} `json:"parallel"`
}

func (a *kimiAdapter) Fetch(ctx context.Context, apiKey, baseURL string) (RelayUsageResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("build kimi request: %w", err)
	}
	req.Header.Set("Authorization", bearerToken(apiKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("kimi request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("read kimi response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return RelayUsageResult{}, fmt.Errorf("kimi api error: status %d, body %s", resp.StatusCode, truncateBody(body))
	}

	var payload kimiUsageResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return RelayUsageResult{}, fmt.Errorf("decode kimi response: %w", err)
	}

	var rows []quota.QuotaRow
	if payload.Usage != nil {
		rows = append(rows, kimiDetailRow("weekly_tokens", "Weekly Tokens", *payload.Usage))
	}
	for _, item := range payload.Limits {
		id := fmt.Sprintf("window_%d_%s", item.Window.Duration, item.Window.TimeUnit)
		label := kimiTimeUnitLabel(item.Window.TimeUnit)
		rows = append(rows, kimiDetailRow(id, label, item.Detail))
	}
	if payload.Parallel != nil && strings.TrimSpace(payload.Parallel.Limit) != "" {
		limit := parseFloatString(payload.Parallel.Limit)
		rows = append(rows, quota.QuotaRow{
			Key:       "parallel_requests",
			Label:     "Parallel Requests",
			Used:      floatPtr(0),
			Limit:     floatPtr(limit),
			Remaining: floatPtr(limit),
			ResetAt:   "",
		})
	}
	return RelayUsageResult{Platform: "kimi", Rows: rows}, nil
}

func kimiDetailRow(id, label string, detail kimiUsageDetail) quota.QuotaRow {
	used := parseFloatString(detail.Used)
	limit := parseFloatString(detail.Limit)
	remaining := parseFloatString(detail.Remaining)
	return quota.QuotaRow{
		Key:               id,
		Label:             label,
		Used:              floatPtr(used),
		Limit:             floatPtr(limit),
		Remaining:         floatPtr(remaining),
		UsedPercent:       percent(used, limit),
		RemainingFraction: remainingFraction(remaining, limit),
		ResetAt:           parseResetTime(detail.ResetTime),
	}
}

func kimiTimeUnitLabel(timeUnit string) string {
	switch timeUnit {
	case "TIME_UNIT_MINUTE":
		return "5h Tokens"
	case "TIME_UNIT_HOUR":
		return "Hourly"
	case "TIME_UNIT_DAY":
		return "Daily"
	default:
		return strings.ToLower(strings.TrimPrefix(timeUnit, "TIME_UNIT_"))
	}
}
