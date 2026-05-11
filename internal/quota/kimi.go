package quota

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type kimiProvider struct {
	caller ManagementAPICaller
	config APICallConfig
}

func NewKimiProvider(caller ManagementAPICaller, config APICallConfig) ProviderHandler {
	return kimiProvider{caller: caller, config: config}
}

func (p kimiProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// Kimi 只需要当前 auth_index 调用单个 usage endpoint，解析后交给统一出口转换。
	response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.config.Method,
		URL:       p.config.URL,
		Header:    p.config.Headers,
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	usage, err := parseKimiUsagePayload(response)
	if err != nil {
		return ProviderOutput{}, err
	}
	return ProviderOutput{Provider: "kimi", Result: KimiResult{Usage: usage}}, nil
}
