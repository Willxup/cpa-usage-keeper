package quota

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

const antigravitySubscriptionTimeout = 10 * time.Second

type antigravityProvider struct {
	caller              ManagementAPICaller
	quotaConfigs        []APICallConfig
	subscriptionConfigs []APICallConfig
}

func NewAntigravityProvider(caller ManagementAPICaller, quotaConfigs []APICallConfig, subscriptionConfigs []APICallConfig) ProviderHandler {
	return antigravityProvider{caller: caller, quotaConfigs: quotaConfigs, subscriptionConfigs: subscriptionConfigs}
}

func (p antigravityProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// Antigravity quota 依赖 project_id；缺少时阻断请求并提示用户补齐认证文件元数据。
	if input.Identity.ProjectID == nil || *input.Identity.ProjectID == "" {
		return ProviderOutput{}, fmt.Errorf("%w: missing project_id parameter", ErrProviderInput)
	}
	if len(p.quotaConfigs) == 0 {
		return ProviderOutput{}, fmt.Errorf("%w: antigravity config is required", ErrProviderInput)
	}
	quota, err := p.checkQuota(ctx, input)
	if err != nil {
		return ProviderOutput{}, err
	}
	subscription := p.checkSubscription(ctx, input)
	return ProviderOutput{Provider: "antigravity", Result: AntigravityResult{Quota: quota, Subscription: subscription}}, nil
}

func (p antigravityProvider) checkQuota(ctx context.Context, input ProviderInput) (*AntigravityQuotaPayload, error) {
	// 多个候选 endpoint 按配置顺序尝试，直到解析到可用 quota 为止。
	var lastErr error
	var emptyQuota *AntigravityQuotaPayload
	for _, config := range p.quotaConfigs {
		response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
			AuthIndex: input.Identity.Identity,
			Method:    config.Method,
			URL:       config.URL,
			Header:    copyHeaders(config.Headers),
			Data:      map[string]string{"project": *input.Identity.ProjectID},
		})
		if err != nil {
			lastErr = err
			continue
		}
		quota, err := parseAntigravityQuotaPayload(response)
		if err != nil {
			lastErr = err
			continue
		}
		if len(quota.Groups) == 0 {
			// 空 groups 是成功响应，继续尝试 fallback；全部为空时再返回成功空结果。
			emptyQuota = quota
			continue
		}
		return quota, nil
	}
	if emptyQuota != nil {
		return emptyQuota, nil
	}
	return nil, lastErr
}

func (p antigravityProvider) checkSubscription(ctx context.Context, input ProviderInput) *AntigravitySubscriptionPayload {
	// Subscription 是额度成功后的可选补充；daily 与 prod fallback 共用同一个 10 秒 Context。
	subscriptionCtx, cancel := context.WithTimeout(ctx, antigravitySubscriptionTimeout)
	defer cancel()
	for _, config := range p.subscriptionConfigs {
		response, err := p.caller.CallManagementAPI(subscriptionCtx, apicall.Request{
			AuthIndex: input.Identity.Identity,
			Method:    config.Method,
			URL:       config.URL,
			Header:    copyHeaders(config.Headers),
			Data: map[string]any{
				"metadata": map[string]string{"ideType": "ANTIGRAVITY"},
			},
		})
		if err != nil {
			continue
		}
		subscription, err := parseAntigravitySubscriptionPayload(response)
		if err != nil {
			continue
		}
		if effectiveAntigravitySubscriptionTier(subscription) == nil {
			continue
		}
		return subscription
	}
	return nil
}
