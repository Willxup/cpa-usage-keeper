package authfiles

// KimiCredentialMetadata 只保留解析 Kimi 配额站点所需的非敏感字段。
type KimiCredentialMetadata struct {
	Domain  string
	BaseURL string
	Type    string
}
