package quota

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

type geminiCLIProvider struct {
	caller           ManagementAPICaller
	config           APICallConfig
	codeAssistConfig APICallConfig
}

func NewGeminiCLIProvider(caller ManagementAPICaller, config APICallConfig, codeAssistConfig APICallConfig) ProviderHandler {
	return geminiCLIProvider{caller: caller, config: config, codeAssistConfig: codeAssistConfig}
}

func (p geminiCLIProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	// Gemini CLI 主 quota 和 Code Assist 都依赖 project_id，缺少时不发起上游请求。
	if input.Identity.ProjectID == nil || *input.Identity.ProjectID == "" {
		return ProviderOutput{}, fmt.Errorf("%w: missing project_id parameter", ErrProviderInput)
	}
	quotaResponse, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.config.Method,
		URL:       p.config.URL,
		Header:    p.config.Headers,
		Data:      map[string]string{"project": *input.Identity.ProjectID},
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	quota, err := parseGeminiCliQuotaPayload(quotaResponse)
	if err != nil {
		return ProviderOutput{}, err
	}
	// Code Assist 是补充数据，失败时不影响主 quota 的展示。
	codeAssist := p.checkCodeAssist(ctx, input)
	return ProviderOutput{Provider: "gemini-cli", Result: GeminiCLIResult{Quota: quota, CodeAssist: codeAssist}}, nil
}

func (p geminiCLIProvider) checkCodeAssist(ctx context.Context, input ProviderInput) *GeminiCLICodeAssistPayload {
	// 补充查询采用静默降级，避免 Code Assist 不可用时整行限额失败。
	response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: input.Identity.Identity,
		Method:    p.codeAssistConfig.Method,
		URL:       p.codeAssistConfig.URL,
		Header:    p.codeAssistConfig.Headers,
		Data: map[string]any{
			"cloudaicompanionProject": *input.Identity.ProjectID,
			"metadata": map[string]string{
				"ideType":     "IDE_UNSPECIFIED",
				"platform":    "PLATFORM_UNSPECIFIED",
				"pluginType":  "GEMINI",
				"duetProject": *input.Identity.ProjectID,
			},
		},
	})
	if err != nil {
		return nil
	}
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	codeAssist, err := parseGeminiCliCodeAssistPayload(response)
	if err != nil {
		return nil
	}
	return codeAssist
}
