package relayusage

import "errors"

// SkipReason 标识某条 identity 未进入用量查询的原因，供前端区分展示。
const (
	SkipNotAIProvider    = "not_ai_provider"      // 非 API Key 身份（如 OAuth AuthFile）
	SkipNoAPIKey         = "no_api_key"           // LookupKey 为空
	SkipUnsupported      = "unsupported_platform" // base-url 无法识别为中转商，或平台未内置
	SkipIdentityNotFound = "identity_not_found"
)

// ErrValidation 表示入参校验失败。
var ErrValidation = errors.New("relay usage validation error")
