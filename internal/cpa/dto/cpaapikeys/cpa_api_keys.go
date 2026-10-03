package cpaapikeys

// ManagementAPIKeysResponse 是 CPA /v0/management/api-keys 响应 DTO。
type ManagementAPIKeysResponse struct {
	APIKeys []string          `json:"api-keys"`
	Names   map[string]string `json:"api-key-names"`
}
