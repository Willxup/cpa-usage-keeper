package quota

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
)

type kimiCredentialReader interface {
	FetchKimiCredentialMetadata(context.Context, string) (*authfiles.KimiCredentialMetadata, error)
}

type kimiProvider struct {
	caller           ManagementAPICaller
	config, aiConfig APICallConfig
}

func NewKimiProvider(caller ManagementAPICaller, config, aiConfig APICallConfig) ProviderHandler {
	return kimiProvider{caller: caller, config: config, aiConfig: aiConfig}
}

func (p kimiProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	identity := input.Identity
	// 四种 Kimi type 都可能被显式域名覆盖；runtime-only 无磁盘 path，不能借同名文件猜测站点。
	if identity.FileName == nil || strings.TrimSpace(*identity.FileName) == "" || identity.FilePath == nil || strings.TrimSpace(*identity.FilePath) == "" {
		return ProviderOutput{}, fmt.Errorf("Kimi credential is not downloadable; quota domain is unavailable")
	}
	reader, ok := p.caller.(kimiCredentialReader)
	if !ok {
		return ProviderOutput{}, fmt.Errorf("Kimi credential metadata reader is unavailable")
	}
	metadata, err := reader.FetchKimiCredentialMetadata(ctx, *identity.FileName)
	if err != nil {
		return ProviderOutput{}, err
	}
	if metadata == nil {
		return ProviderOutput{}, fmt.Errorf("Kimi credential metadata is unavailable")
	}
	config := p.config
	if kimiCredentialDomain(*metadata, input) == "ai" {
		config = p.aiConfig
	}
	response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: identity.Identity, Method: config.Method, URL: config.URL, Header: copyHeaders(config.Headers),
	})
	if err != nil {
		return ProviderOutput{}, err
	}
	usage, err := parseKimiUsagePayload(response)
	if err != nil {
		return ProviderOutput{}, err
	}
	// 请求站点只决定 endpoint，不改写 CPA 的原始 provider/type 身份。
	return ProviderOutput{Provider: firstNonEmpty(identity.Provider, identity.Type), Result: KimiResult{Usage: usage}}, nil
}

func kimiCredentialDomain(metadata authfiles.KimiCredentialMetadata, input ProviderInput) string {
	// 对齐 CPA/CPAMC：显式 domain > base_url > 文件 type > provider；URL 只用于识别两个固定站点。
	if metadata.Domain != "" {
		if kimiDomain(metadata.Domain) == "ai" {
			return "ai"
		}
		return "com"
	}
	if parsed, err := url.Parse(metadata.BaseURL); err == nil {
		host := strings.ToLower(parsed.Hostname())
		switch {
		case host == "kimi.ai", strings.HasSuffix(host, ".kimi.ai"):
			return "ai"
		case host == "kimi.com", strings.HasSuffix(host, ".kimi.com"):
			return "com"
		}
	}
	for _, value := range []string{metadata.Type, input.Identity.Provider, input.Identity.Type} {
		if domain := kimiDomain(value); domain != "" {
			return domain
		}
	}
	return "com"
}

func kimiDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "ai", value == "kimi-ai", value == "kimi.ai", strings.HasSuffix(value, ".kimi.ai"):
		return "ai"
	case value == "com", value == "kimi", value == "kimi.com", strings.HasSuffix(value, ".kimi.com"):
		return "com"
	default:
		return ""
	}
}
