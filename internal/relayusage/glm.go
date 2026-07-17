package relayusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"cpa-usage-keeper/internal/quota"
)

// glmAdapter 查询智谱 GLM Coding Plan 用量。
// 文档接口：https://open.bigmodel.cn/api/monitor/usage/quota/limit
// 注意：GLM 返回字段名与含义相反——usage 是总限额，currentValue 是已用量。
type glmAdapter struct {
	client HTTPDoer
	url    string
}

func newGLMAdapter(client HTTPDoer) *glmAdapter {
	return &glmAdapter{client: client, url: glmUsageURL}
}

func (a *glmAdapter) Platform() string { return "glm" }

const glmUsageURL = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"

type glmLimitItem struct {
	Type          string  `json:"type"`
	Percentage    float64 `json:"percentage"`
	Usage         float64 `json:"usage"`
	CurrentValue  float64 `json:"currentValue"`
	Remaining     float64 `json:"remaining"`
	NextResetTime any     `json:"nextResetTime"`
	Unit          int     `json:"unit"`
	Number        int     `json:"number"`
}

type glmResponse struct {
	Code int `json:"code"`
	Data struct {
		Level  string         `json:"level"`
		Limits []glmLimitItem `json:"limits"`
	} `json:"data"`
}

func (a *glmAdapter) Fetch(ctx context.Context, apiKey, baseURL string) (RelayUsageResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("build glm request: %w", err)
	}
	// GLM 的 Authorization 直接放 token，不带 Bearer 前缀。
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("glm request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("read glm response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return RelayUsageResult{}, fmt.Errorf("glm api error: status %d, body %s", resp.StatusCode, truncateBody(body))
	}

	var payload glmResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return RelayUsageResult{}, fmt.Errorf("decode glm response: %w", err)
	}
	if payload.Code != 200 {
		return RelayUsageResult{}, fmt.Errorf("glm api error: code %d", payload.Code)
	}

	rows := make([]quota.QuotaRow, 0, len(payload.Data.Limits))
	for _, item := range payload.Data.Limits {
		used := item.CurrentValue
		limit := item.Usage
		if limit == 0 && item.CurrentValue != 0 && item.Remaining != 0 {
			limit = item.CurrentValue + item.Remaining
		}
		remaining := item.Remaining
		id, label := glmLimitLabel(item)
		// GLM 返回的 percentage 是已用百分比，比 currentValue/usage 推导更权威（部分窗口两者不一致）。
		usedPercent := percent(used, limit)
		if item.Percentage > 0 {
			clamped := clampPercent(item.Percentage)
			usedPercent = &clamped
		}
		rows = append(rows, quota.QuotaRow{
			Key:               id,
			Label:             label,
			Used:              floatPtr(used),
			Limit:             floatPtr(limit),
			Remaining:         floatPtr(remaining),
			UsedPercent:       usedPercent,
			RemainingFraction: remainingFraction(remaining, limit),
			ResetAt:           resetTimeFromAny(item.NextResetTime),
		})
	}
	return RelayUsageResult{Platform: "glm", Rows: rows}, nil
}

func glmLimitLabel(item glmLimitItem) (id, label string) {
	if item.Type == "TIME_LIMIT" {
		return "monthly_mcp", "Monthly MCP"
	}
	if item.Type == "TOKENS_LIMIT" {
		switch item.Unit {
		case 3:
			return "5hour_tokens", "5h Tokens"
		case 6:
			return "weekly_tokens", "Weekly Tokens"
		}
		return fmt.Sprintf("tokens_%dx%d", item.Unit, item.Number), fmt.Sprintf("%dh Tokens", item.Number)
	}
	return fmt.Sprintf("limit_%dx%d", item.Unit, item.Number), fmt.Sprintf("Limit (%dx%d)", item.Unit, item.Number)
}
