package quota

import (
	"context"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type codexProvider struct {
	caller ManagementAPICaller
	config APICallConfig
}

func NewCodexProvider(caller ManagementAPICaller, config APICallConfig) ProviderHandler {
	return codexProvider{caller: caller, config: config}
}

func (p codexProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// 官方接口已允许不带账号 ID；同步到账号时追加 header，否则只使用通用认证头刷新限额。
	headers := copyHeaders(p.config.Headers)
	if accountID := optionalAccountID(input.Identity.AccountID); accountID != "" {
		headers = mergeHeaders(headers, map[string]string{"Chatgpt-Account-Id": accountID})
	}
	// 统一调用 CPA api-call，由后端补齐固定 URL/header 和当前账号的动态 header。
	request := apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.config.Method,
		URL:       p.config.URL,
		Header:    headers,
	}
	response, err := p.caller.CallManagementAPI(ctx, request)
	if err != nil {
		return ProviderOutput{}, err
	}
	usage, err := parseCodexUsagePayload(response)
	if err != nil {
		return ProviderOutput{}, err
	}
	return ProviderOutput{Provider: "codex", Result: CodexResult{Usage: usage}}, nil
}

func optionalAccountID(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (p codexProvider) Reset(ctx context.Context, input ProviderInput) (ProviderResetOutput, error) {
	headers := copyHeaders(p.config.Headers)
	if accountID := optionalAccountID(input.Identity.AccountID); accountID != "" {
		headers = mergeHeaders(headers, map[string]string{"Chatgpt-Account-Id": accountID})
	}
	// reset 与普通限额刷新共用同一份 auth header，但调用官方 consume 端点消费一次 reset credit。
	redeemRequestID, err := newRedeemRequestID()
	if err != nil {
		return ProviderResetOutput{}, err
	}
	request := apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    "POST",
		URL:       CodexRateLimitResetCreditsConsumeURL,
		Header:    headers,
		Data:      map[string]string{"redeem_request_id": redeemRequestID},
	}
	response, err := p.caller.CallManagementAPI(ctx, request)
	if err != nil {
		return ProviderResetOutput{}, err
	}
	return parseCodexResetCreditResponse(response)
}
