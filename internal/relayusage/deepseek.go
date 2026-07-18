package relayusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// deepSeekAdapter 查询 DeepSeek 开放平台余额。
// DeepSeek 只有余额接口，没有用量/额度窗口，因此结果只填 Balance。
// 文档：https://api-docs.deepseek.com/zh-cn/api/get-user-balance
type deepSeekAdapter struct {
	client HTTPDoer
	url    string
}

func newDeepSeekAdapter(client HTTPDoer) *deepSeekAdapter {
	return &deepSeekAdapter{client: client, url: deepSeekBalanceURL}
}

func (a *deepSeekAdapter) Platform() string { return "deepseek" }

const deepSeekBalanceURL = "https://api.deepseek.com/user/balance"

type deepSeekBalanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

type deepSeekBalanceResponse struct {
	IsAvailable  bool                  `json:"is_available"`
	BalanceInfos []deepSeekBalanceInfo `json:"balance_infos"`
}

func (a *deepSeekAdapter) Fetch(ctx context.Context, apiKey, baseURL string) (RelayUsageResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, nil)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("build deepseek request: %w", err)
	}
	req.Header.Set("Authorization", bearerToken(apiKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("deepseek request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return RelayUsageResult{}, fmt.Errorf("read deepseek response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return RelayUsageResult{}, fmt.Errorf("deepseek api error: status %d, body %s", resp.StatusCode, truncateBody(body))
	}

	var payload deepSeekBalanceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return RelayUsageResult{}, fmt.Errorf("decode deepseek response: %w", err)
	}
	if len(payload.BalanceInfos) == 0 {
		return RelayUsageResult{}, fmt.Errorf("deepseek api error: balance_infos is empty")
	}
	info := payload.BalanceInfos[0]
	balance := &RelayBalance{
		Available: parseFloatString(info.TotalBalance),
		Granted:   parseFloatString(info.GrantedBalance),
		ToppedUp:  parseFloatString(info.ToppedUpBalance),
		Currency:  info.Currency,
	}
	if balance.Currency == "" {
		balance.Currency = "CNY"
	}
	return RelayUsageResult{Platform: "deepseek", Balance: balance}, nil
}

func truncateBody(body []byte) string {
	if len(body) > 300 {
		return string(body[:300])
	}
	return string(body)
}
