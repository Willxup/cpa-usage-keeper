package quota

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type openCodeGoProvider struct {
	caller ManagementAPICaller
	config APICallConfig
}

func NewOpenCodeGoProvider(caller ManagementAPICaller, config APICallConfig) ProviderHandler {
	return openCodeGoProvider{caller: caller, config: config}
}

func (p openCodeGoProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// OpenCode Go 凭证是 opencode-go-cliproxyapi 插件写入的 api_key auth record；
	// api-call 从 auth 的 api_key 属性解析 $TOKEN$，因此上游调用自带认证。
	response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.config.Method,
		URL:       p.config.URL,
		Header:    copyHeaders(p.config.Headers),
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	usage, err := parseOpenCodeGoUsagePayload(response)
	if err != nil {
		return ProviderOutput{}, err
	}
	return ProviderOutput{Provider: "opencode-go", Result: OpenCodeGoResult{Usage: usage}}, nil
}
