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
	return ProviderOutput{}, joinXAIBillingErrors(weeklyErr, monthlyErr)
}

type xaiBillingAttempt struct {
	billing *XAIBillingPayload
	err     error
}

func joinXAIBillingErrors(billingErrors ...error) error {
	// 两个来源都失败时，优先保留 401/402，让现有失败缓存继续使用正确的 HTTP TTL。
	for preferredIndex, err := range billingErrors {
		var httpErr ProviderHTTPError
		if err == nil || !errors.As(err, &httpErr) || !isRefreshCacheableHTTPStatus(httpErr.StatusCode) {
			continue
		}
		ordered := make([]error, 0, len(billingErrors))
		ordered = append(ordered, err)
		for index, candidate := range billingErrors {
			if index != preferredIndex && candidate != nil {
				ordered = append(ordered, candidate)
			}
		}
		return errors.Join(ordered...)
	}
	return errors.Join(billingErrors...)
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
	if billing == nil || !hasXAIBillingData(billing.Config) {
		return nil, fmt.Errorf("empty xAI %s billing response", period)
	}
	return billing, nil
}

func hasXAIBillingData(config *XAIBillingConfig) bool {
	if config == nil {
		return false
	}
	if config.CreditUsagePercent != nil || config.MonthlyLimit.Val != nil || config.Used.Val != nil || config.OnDemandCap.Val != nil || config.OnDemandUsed.Val != nil {
		return true
	}
	if strings.TrimSpace(config.BillingPeriodStart) != "" || strings.TrimSpace(config.BillingPeriodEnd) != "" {
		return true
	}
	if period := config.CurrentPeriod; period != nil && (strings.TrimSpace(period.Type) != "" || strings.TrimSpace(period.Start) != "" || strings.TrimSpace(period.End) != "") {
		return true
	}
	for _, product := range config.ProductUsage {
		if strings.TrimSpace(product.Product) != "" && product.UsagePercent != nil {
			return true
		}
	}
	return false
}
