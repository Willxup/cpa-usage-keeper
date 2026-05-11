package quota

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type claudeProvider struct {
	caller        ManagementAPICaller
	usageConfig   APICallConfig
	profileConfig APICallConfig
}

func NewClaudeProvider(caller ManagementAPICaller, usageConfig APICallConfig, profileConfig APICallConfig) ProviderHandler {
	return claudeProvider{caller: caller, usageConfig: usageConfig, profileConfig: profileConfig}
}

func (p claudeProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// Claude 需要先取 usage，再取 profile；profile 中包含前端标签展示需要的信息。
	usageResponse, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.usageConfig.Method,
		URL:       p.usageConfig.URL,
		Header:    p.usageConfig.Headers,
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	usage, err := parseClaudeUsagePayload(usageResponse)
	if err != nil {
		return ProviderOutput{}, err
	}
	// usage 解析成功后再查询 profile，避免 profile 请求掩盖主限额接口错误。
	profileResponse, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.profileConfig.Method,
		URL:       p.profileConfig.URL,
		Header:    p.profileConfig.Headers,
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	profile, err := parseClaudeProfilePayload(profileResponse)
	if err != nil {
		return ProviderOutput{}, err
	}
	return ProviderOutput{Provider: "claude", Result: ClaudeResult{Usage: usage, Profile: profile}}, nil
}
