package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type xaiProvider struct {
	caller        ManagementAPICaller
	weeklyConfig  APICallConfig
	monthlyConfig APICallConfig
}

func NewXAIProvider(caller ManagementAPICaller, weeklyConfig APICallConfig, monthlyConfig APICallConfig) ProviderHandler {
	return xaiProvider{caller: caller, weeklyConfig: weeklyConfig, monthlyConfig: monthlyConfig}
}

func (p xaiProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// weekly 与 monthly 是同级限额来源；任一请求失败都不能阻止另一请求继续尝试。
	weeklyCh := make(chan xaiBillingAttempt, 1)
	monthlyCh := make(chan xaiBillingAttempt, 1)
	go func() {
		billing, err := p.requestBilling(ctx, input, p.weeklyConfig, "weekly")
		weeklyCh <- xaiBillingAttempt{billing: billing, err: err}
	}()
	go func() {
		billing, err := p.requestBilling(ctx, input, p.monthlyConfig, "monthly")
		monthlyCh <- xaiBillingAttempt{billing: billing, err: err}
	}()
	weeklyAttempt := <-weeklyCh
	monthlyAttempt := <-monthlyCh
	weekly, weeklyErr := weeklyAttempt.billing, weeklyAttempt.err
	monthly, monthlyErr := monthlyAttempt.billing, monthlyAttempt.err
	if weekly != nil || monthly != nil {
		return ProviderOutput{Provider: "xai", Result: XAIResult{
			Weekly:  weekly,
			Monthly: monthly,
		}}, nil
	}
	return ProviderOutput{}, selectXAIBillingError(weeklyErr, monthlyErr)
}

type xaiBillingAttempt struct {
	billing *XAIBillingPayload
	err     error
}

func selectXAIBillingError(billingErrors ...error) error {
	// 只返回一个主错误，确保提示、HTTP 状态和失败缓存 TTL 来自同一分类。
	for _, err := range billingErrors {
		var httpErr ProviderHTTPError
		if err == nil || !errors.As(err, &httpErr) || !isRefreshCacheableHTTPStatus(httpErr.StatusCode) {
			continue
		}
		return err
	}
	for _, err := range billingErrors {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return err
		}
	}
	for _, err := range billingErrors {
		if err != nil {
			return err
		}
	}
	return errors.New("xAI billing requests failed")
}

func (p xaiProvider) requestBilling(ctx context.Context, input ProviderInput, config APICallConfig, period string) (*XAIBillingPayload, error) {
	headers := copyHeaders(config.Headers)
	if input.Identity.XAIUserID != nil {
		if xaiUserID := strings.TrimSpace(*input.Identity.XAIUserID); xaiUserID != "" {
			headers = mergeHeaders(headers, map[string]string{"x-userid": xaiUserID})
		}
	}
	response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    config.Method,
		URL:       config.URL,
		Header:    headers,
	})
	if err != nil {
		return nil, err
	}
	billing, err := parseXAIBillingPayload(response)
	if err != nil {
		return nil, err
	}
	if !hasXAIBillingQuotaRows(billing, period) {
		return nil, fmt.Errorf("empty xAI %s billing response", period)
	}
	return billing, nil
}

func hasXAIBillingQuotaRows(billing *XAIBillingPayload, period string) bool {
	result := XAIResult{}
	switch period {
	case "weekly":
		result.Weekly = billing
	case "monthly":
		result.Monthly = billing
	default:
		return false
	}
	return len(normalizeXAIQuotaRows(result)) > 0
}
