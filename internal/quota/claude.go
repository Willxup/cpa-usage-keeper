package quota

import (
	"context"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

const claudeProfileTimeout = 10 * time.Second

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
		Header:    copyHeaders(p.usageConfig.Headers),
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	usage, err := parseClaudeUsagePayload(usageResponse)
	if err != nil {
		return ProviderOutput{}, err
	}
	profile := p.checkProfile(ctx, input)
	return ProviderOutput{Provider: "claude", Result: ClaudeResult{Usage: usage, Profile: profile}}, nil
}

func (p claudeProvider) checkProfile(ctx context.Context, input ProviderInput) *ClaudeProfileResponse {
	// Profile 只是套餐补充信息，继承父 Context 并限制在 10 秒内，任何失败都不影响 Usage 主结果。
	profileCtx, cancel := context.WithTimeout(ctx, claudeProfileTimeout)
	defer cancel()
	profileResponse, err := p.caller.CallManagementAPI(profileCtx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.profileConfig.Method,
		URL:       p.profileConfig.URL,
		Header:    copyHeaders(p.profileConfig.Headers),
	})
	if err != nil {
		return nil
	}
	profile, err := parseClaudeProfilePayload(profileResponse)
	if err != nil {
		return nil
	}
	return profile
}
