package providermetadata

import "fmt"

// providerSources 按用户指定优先级显式构造七个来源，不使用 init 或动态注册。
func providerSources() []source {
	// 每次返回新 slice，避免调用方或测试意外修改全局 registry 状态。
	return []source{
		// Codex 是 registry 第一优先来源。
		codexSource(),
		// xAI API Key 是 registry 第二优先来源。
		xaiSource(),
		// 普通 Gemini 是 registry 第三优先来源。
		geminiSource(),
		// Gemini Interactions 保留独立 provider 类型并排在普通 Gemini 后。
		geminiInteractionsSource(),
		// Claude 排在 Gemini 家族后。
		claudeSource(),
		// Vertex 使用 CPA 正确拼写并排在 OpenAI 前。
		vertexSource(),
		// OpenAI Compatibility 最后归并 provider 层与多 key entry。
		openAICompatibilitySource(),
	}
}

// validateSources 拒绝空标识和重复 source/type，防止 registry 维护错误改变 stale 范围。
func validateSources(sources []source) error {
	// seenIDs 记录 registry 内部 source ID。
	seenIDs := make(map[string]struct{}, len(sources))
	// seenTypes 记录最终写入的 provider type。
	seenTypes := make(map[string]struct{}, len(sources))
	// 按声明顺序检查每个固定来源。
	for _, item := range sources {
		// 空 source ID 无法生成稳定 warning。
		if item.id == "" {
			return fmt.Errorf("provider metadata source id is required")
		}
		// 空 provider type 无法形成正确 stale scope。
		if item.providerType == "" {
			return fmt.Errorf("provider metadata source %q type is required", item.id)
		}
		// 空默认名会让缺 name 的正常 CPA entry 被错误过滤。
		if item.defaultDisplayName == "" {
			return fmt.Errorf("provider metadata source %q default display name is required", item.id)
		}
		// 空 warning 名无法保持来源错误文本稳定。
		if item.warningName == "" {
			return fmt.Errorf("provider metadata source %q warning name is required", item.id)
		}
		// 每个 registry source 必须绑定唯一 endpoint 执行函数。
		if item.fetch == nil {
			return fmt.Errorf("provider metadata source %q fetch is required", item.id)
		}
		// 重复 source ID 表示 registry 声明冲突。
		if _, ok := seenIDs[item.id]; ok {
			return fmt.Errorf("provider metadata source id %q is duplicated", item.id)
		}
		// 记录已通过校验的 source ID。
		seenIDs[item.id] = struct{}{}
		// 重复 provider type 会让两个 endpoint 互相 stale，必须拒绝。
		if _, ok := seenTypes[item.providerType]; ok {
			return fmt.Errorf("provider metadata type %q is duplicated", item.providerType)
		}
		// 记录已通过校验的 provider type。
		seenTypes[item.providerType] = struct{}{}
	}
	// 全部固定来源通过校验。
	return nil
}
