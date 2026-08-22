package keypolicy

// PluginKeysResponse 是 CPA key-policy 插件 /v0/management/plugins/cpa-key-policy/keys 响应 DTO。
type PluginKeysResponse struct {
	Keys []PluginKey `json:"keys"`
}

// PluginKey 只保留 keeper 需要的插件 key 字段；aliases、models 等其余插件字段全部忽略。
type PluginKey struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	KeyPreview string `json:"key_preview"`
}
