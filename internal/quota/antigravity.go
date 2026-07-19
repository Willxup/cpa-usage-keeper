package quota

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type antigravityProvider struct {
	caller  ManagementAPICaller
	configs []APICallConfig
}

func NewAntigravityProvider(caller ManagementAPICaller, configs ...APICallConfig) ProviderHandler {
	return antigravityProvider{caller: caller, configs: configs}
}

func (p antigravityProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// Antigravity quota 依赖 project_id；缺少时阻断请求并提示用户补齐认证文件元数据。
	if input.Identity.ProjectID == nil || *input.Identity.ProjectID == "" {
		return ProviderOutput{}, fmt.Errorf("%w: missing project_id parameter", ErrProviderInput)
	}
	if len(p.configs) == 0 {
		return ProviderOutput{}, fmt.Errorf("%w: antigravity config is required", ErrProviderInput)
	}
	// 多个候选 endpoint 按配置顺序尝试，直到解析到可用 quota 为止。
	var lastErr error
	var emptyQuota *AntigravityQuotaPayload
	for _, config := range p.configs {
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
		return ProviderOutput{Provider: "antigravity", Result: AntigravityResult{Quota: quota}}, nil
	}
	if emptyQuota != nil {
		return ProviderOutput{Provider: "antigravity", Result: AntigravityResult{Quota: emptyQuota}}, nil
	}
	return ProviderOutput{}, lastErr
}
