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

// minimaxAdapter 查询 MiniMax Token Plan 用量。
// CN 与国际账号互相隔离：首选端点报鉴权错误（1004/2049）时回退另一端点。
type minimaxAdapter struct {
	client    HTTPDoer
	cnURL     string
	globalURL string
}

func newMiniMaxAdapter(client HTTPDoer) *minimaxAdapter {
	return &minimaxAdapter{client: client, cnURL: minimaxEndpoints.CN, globalURL: minimaxEndpoints.Global}
}

func (a *minimaxAdapter) Platform() string { return "minimax" }

var minimaxEndpoints = struct {
	CN, Global string
}{
	CN:     "https://api.minimaxi.com/v1/token_plan/remains",
	Global: "https://www.minimax.io/v1/token_plan/remains",
}

// minimaxAuthFailureCodes 表示端点不认这个 key（CN/国际账号隔离）。
var minimaxAuthFailureCodes = map[int]struct{}{1004: {}, 2049: {}}

type minimaxModelRemains struct {
	ModelName                       string `json:"model_name"`
	StartTime                       int64  `json:"start_time"`
	EndTime                         int64  `json:"end_time"`
	RemainsTime                     int64  `json:"remains_time"`
	WeeklyStartTime                 int64  `json:"weekly_start_time"`
	WeeklyEndTime                   int64  `json:"weekly_end_time"`
	WeeklyRemainsTime               int64  `json:"weekly_remains_time"`
	CurrentIntervalRemainingPercent int    `json:"current_interval_remaining_percent"`
	CurrentWeeklyRemainingPercent   int    `json:"current_weekly_remaining_percent"`
	CurrentIntervalStatus           int    `json:"current_interval_status"`
	CurrentWeeklyStatus             int    `json:"current_weekly_status"`
}

type minimaxRemainsResponse struct {
	BaseResp *struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp,omitempty"`
	ModelRemains []minimaxModelRemains `json:"model_remains"`
}

func (a *minimaxAdapter) Fetch(ctx context.Context, apiKey, baseURL string) (RelayUsageResult, error) {
	auth := bearerToken(apiKey)
	primary, fallback := a.cnURL, a.globalURL
	// 若 base-url 明确指向国际站，则优先国际端点。
	if strings.Contains(strings.ToLower(baseURL), "minimax.io") {
		primary, fallback = minimaxEndpoints.Global, minimaxEndpoints.CN
	}

	data, err := a.fetchRemains(ctx, primary, auth)
	if err != nil {
		return RelayUsageResult{}, err
	}
	if data.BaseResp != nil {
		if _, authFailed := minimaxAuthFailureCodes[data.BaseResp.StatusCode]; authFailed {
			fallbackData, ferr := a.fetchRemains(ctx, fallback, auth)
			if ferr != nil {
				return RelayUsageResult{}, ferr
			}
			data = fallbackData
		}
	}
	if data.BaseResp != nil && data.BaseResp.StatusCode != 0 {
		return RelayUsageResult{}, fmt.Errorf("minimax api error: %s", data.BaseResp.StatusMsg)
	}

	rows := make([]quota.QuotaRow, 0, len(data.ModelRemains)*2)
	for _, m := range data.ModelRemains {
		// status=3 表示该模态不限量，跳过避免显示 100% 剩余的误导进度条。
		if m.CurrentIntervalStatus != 3 && m.EndTime > m.StartTime {
			intervalTotalMs := float64(m.EndTime - m.StartTime)
			intervalUsedMs := intervalTotalMs - float64(m.RemainsTime)
			if intervalUsedMs < 0 {
				intervalUsedMs = 0
			}
			rows = append(rows, quota.QuotaRow{
				Key:         fmt.Sprintf("interval_%s", m.ModelName),
				Label:       fmt.Sprintf("%s 5h", m.ModelName),
				Used:        floatPtr(intervalUsedMs / 1000),
				Limit:       floatPtr(intervalTotalMs / 1000),
				Remaining:   floatPtr(float64(m.RemainsTime) / 1000),
				UsedPercent: floatPtr(clampPercent(float64(100 - m.CurrentIntervalRemainingPercent))),
				ResetAt:     millisToRFC3339(m.EndTime),
			})
		}
		if m.CurrentWeeklyStatus != 3 && m.WeeklyEndTime > m.WeeklyStartTime {
			weeklyTotalMs := float64(m.WeeklyEndTime - m.WeeklyStartTime)
			weeklyUsedMs := weeklyTotalMs - float64(m.WeeklyRemainsTime)
			if weeklyUsedMs < 0 {
				weeklyUsedMs = 0
			}
			rows = append(rows, quota.QuotaRow{
				Key:         fmt.Sprintf("weekly_%s", m.ModelName),
				Label:       fmt.Sprintf("%s Weekly", m.ModelName),
				Used:        floatPtr(weeklyUsedMs / 1000),
				Limit:       floatPtr(weeklyTotalMs / 1000),
				Remaining:   floatPtr(float64(m.WeeklyRemainsTime) / 1000),
				UsedPercent: floatPtr(clampPercent(float64(100 - m.CurrentWeeklyRemainingPercent))),
				ResetAt:     millisToRFC3339(m.WeeklyEndTime),
			})
		}
	}
	return RelayUsageResult{Platform: "minimax", Rows: rows}, nil
}

func (a *minimaxAdapter) fetchRemains(ctx context.Context, endpoint, auth string) (minimaxRemainsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return minimaxRemainsResponse{}, fmt.Errorf("build minimax request: %w", err)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return minimaxRemainsResponse{}, fmt.Errorf("minimax request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return minimaxRemainsResponse{}, fmt.Errorf("read minimax response: %w", err)
	}
	// MiniMax 的 remains 端点永远返回 HTTP 200，真实状态在 body 的 base_resp.status_code 里。
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return minimaxRemainsResponse{}, fmt.Errorf("minimax api error: status %d, body %s", resp.StatusCode, truncateBody(body))
	}
	var payload minimaxRemainsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return minimaxRemainsResponse{}, fmt.Errorf("decode minimax response: %w", err)
	}
	return payload, nil
}
