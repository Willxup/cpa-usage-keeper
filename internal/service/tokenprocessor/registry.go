package tokenprocessor

import (
	"fmt"
	"strings"
)

// executorDefinition 把一个 CPA Go executor alias 显式绑定到已有协议 handler。
type executorDefinition struct {
	alias     string
	handlerID HandlerID
}

// identityAliasDefinition 只定义旧事件 fallback hint，绝不代表 CPA parser contract。
type identityAliasDefinition struct {
	alias     string
	handlerID HandlerID
}

// identityPrefixDefinition 仅用于文档明确允许的 OpenAI Compatibility 动态 identity 前缀。
type identityPrefixDefinition struct {
	prefix    string
	handlerID HandlerID
}

// registryIndexes 是只读静态索引；所有 resolver 调用共享同一份校验后的 registry。
type registryIndexes struct {
	executors        map[string]executorDefinition
	identities       map[string]identityAliasDefinition
	identityPrefixes []identityPrefixDefinition
}

// handlerDefinitions 明确列出唯一协议 handler，registry 校验用它阻止 executor 指向悬空实现。
var handlerDefinitions = []HandlerID{
	HandlerClaude,
	HandlerGemini,
	HandlerResponsesInclusive,
	HandlerStrictPassThrough,
	HandlerOpenAICompatibility,
}

// tokenHandlers 是协议实现的唯一 registry；executor 与 identity registry 只能引用这里的 HandlerID。
var tokenHandlers = map[HandlerID]tokenHandler{
	HandlerClaude:              claudeHandler{},
	HandlerGemini:              geminiHandler{},
	HandlerResponsesInclusive:  responsesInclusiveHandler{},
	HandlerStrictPassThrough:   strictPassThroughHandler{},
	HandlerOpenAICompatibility: openAICompatibilityHandler{},
}

// executorDefinitions 是中央静态引用表；新增 executor 必须同时拥有独立文件、此处一条引用和黑盒测试。
var executorDefinitions = []executorDefinition{
	// 来源 claude_executor.go / CPA ClaudeExecutor；Claude handler 合并 Input/cache，并在不变量成立时授予 canonical Total 权限。
	claudeExecutorDefinition,
	// 来源 gemini_executor.go / CPA GeminiExecutor；Gemini handler 合并 Output/reasoning，并在映射明确时授予 canonical Total 权限。
	geminiExecutorDefinition,
	// 来源 gemini_vertex_executor.go / CPA GeminiVertexExecutor；字段合同与 Gemini 相同，复用 handler 与 canonical Total 权限。
	geminiVertexExecutorDefinition,
	// 来源 gemini_cli_executor.go / 历史 CPA GeminiCLIExecutor；只为旧 payload 保留 Gemini handler 与既有 canonical 权限。
	geminiCLIExecutorDefinition,
	// 来源 ai_studio_executor.go / CPA AIStudioExecutor；Gemini usage 语义允许复用 handler 与 canonical Total 权限。
	aiStudioExecutorDefinition,
	// 来源 antigravity_executor.go / CPA AntigravityExecutor；prompt/candidates/thoughts 映射允许复用 Gemini handler 与 canonical 权限。
	antigravityExecutorDefinition,
	// 来源 codex_executor.go / CPA CodexExecutor；Responses handler 可校验 canonical Total，cached-only 旧形态则明确降为 preserve。
	codexExecutorDefinition,
	// 来源 codex_websockets_executor.go / CPA CodexWebsocketsExecutor；传输不改字段合同，复用 Responses canonical 权限。
	codexWebsocketsExecutorDefinition,
	// 来源 codex_auto_executor.go / CPA CodexAutoExecutor；只兼容委托 alias，复用 Responses handler 与相同权限边界。
	codexAutoExecutorDefinition,
	// 来源 xai_executor.go / CPA XAIExecutor；/v1/responses 的 inclusive 字段合同允许复用 Responses canonical 权限。
	xaiExecutorDefinition,
	// 来源 xai_websockets_executor.go / CPA XAIWebsocketsExecutor；WebSocket 不改字段合同，复用 Responses canonical 权限。
	xaiWebsocketsExecutorDefinition,
	// 来源 xai_auto_executor.go / CPA XAIAutoExecutor；只兼容委托 alias，复用 Responses handler 与相同权限边界。
	xaiAutoExecutorDefinition,
	// 来源 kimi_executor.go / CPA KimiExecutor；通用 parser 不能证明 reasoning inclusion，因此 strict handler 只保留 zero-only 旧规则。
	kimiExecutorDefinition,
	// 来源 openai_compat_executor.go / CPA OpenAICompatExecutor；私有规则链只执行已证兼容，Total 权限保持 zero-only。
	openAICompatExecutorDefinition,
}

// identityAliasDefinitions 原样保留 Keeper 已验证的 identity 路由，但每一项都只有 identity_hint 强度。
var identityAliasDefinitions = []identityAliasDefinition{
	// claude 是旧 Keeper Claude identity 名称，只提示使用 Claude 既有规则。
	{alias: "claude", handlerID: HandlerClaude},
	// anthropic 是 Claude 厂商别名，只提示使用 Claude 既有规则。
	{alias: "anthropic", handlerID: HandlerClaude},
	// gemini 是标准 Gemini identity 名称，只提示使用 Gemini 既有四分支规则。
	{alias: "gemini", handlerID: HandlerGemini},
	// vertex 与 Gemini 使用相同历史 Token 规则，但 identity 不能授予 parser contract。
	{alias: "vertex", handlerID: HandlerGemini},
	// gemini-cli 保留旧 CPA identity 兼容，不表示当前 CPA 仍注册该 executor。
	{alias: "gemini-cli", handlerID: HandlerGemini},
	// gemini-cli-code-assist 保留旧 code assist identity 的 Gemini 规则。
	{alias: "gemini-cli-code-assist", handlerID: HandlerGemini},
	// gemini-interactions 的历史映射仍使用 Gemini reasoning fold 规则。
	{alias: "gemini-interactions", handlerID: HandlerGemini},
	// aistudio 是 AI Studio 紧凑别名，继续提示 Gemini handler。
	{alias: "aistudio", handlerID: HandlerGemini},
	// ai-studio 是 AI Studio 连字符别名，继续提示 Gemini handler。
	{alias: "ai-studio", handlerID: HandlerGemini},
	// antigravity 的历史 Token 规则与 Gemini 一致，但 identity 只是一条 hint。
	{alias: "antigravity", handlerID: HandlerGemini},
	// codex identity 保留 Responses 格式的旧规则，但非零 Total 强纠错仍需 executor 合同。
	{alias: "codex", handlerID: HandlerResponsesInclusive},
	// xai identity 保留 Responses 格式的旧规则，但非零 Total 强纠错仍需 executor 合同。
	{alias: "xai", handlerID: HandlerResponsesInclusive},
	// kimi identity 保持 strict，不因名称假设 reasoning 已包含在 Output。
	{alias: "kimi", handlerID: HandlerStrictPassThrough},
	// moonshot 是 Kimi 厂商别名，同样保持 strict。
	{alias: "moonshot", handlerID: HandlerStrictPassThrough},
	// openai 在 Keeper 现有 metadata 中代表 OpenAI Compatibility，必须保留 #272 路径。
	{alias: "openai", handlerID: HandlerOpenAICompatibility},
	// openai-compatible 是现有兼容 identity 的标准别名。
	{alias: "openai-compatible", handlerID: HandlerOpenAICompatibility},
	// openai_compatibility 是现有下划线别名。
	{alias: "openai_compatibility", handlerID: HandlerOpenAICompatibility},
	// openai-compatibility 是现有连字符别名。
	{alias: "openai-compatibility", handlerID: HandlerOpenAICompatibility},
}

// identityPrefixDefinitions 只允许设计文档确认的动态 OpenAI-compatible identity 前缀。
var identityPrefixDefinitions = []identityPrefixDefinition{
	// 该前缀保留现有 openai-compatible-* 扩展方式，其他 provider 名称不做前缀猜测。
	{prefix: "openai-compatible-", handlerID: HandlerOpenAICompatibility},
}

// defaultRegistry 在包加载时一次性构建并校验静态表；这里没有 init 自注册或运行时插件顺序问题。
var defaultRegistry, defaultRegistryErr = buildRegistryIndexes()

func buildRegistryIndexes() (registryIndexes, error) {
	// 先校验 handler ID 唯一，避免 alias 指向含糊的协议实现。
	handlers := make(map[HandlerID]struct{}, len(handlerDefinitions))
	for _, handlerID := range handlerDefinitions {
		if _, exists := handlers[handlerID]; exists {
			return registryIndexes{}, fmt.Errorf("duplicate token handler id %q", handlerID)
		}
		handlers[handlerID] = struct{}{}
	}
	// 每个声明的 handler 都必须有且只有一个纯计算实现，防止 resolver 通过但 Process 才发现悬空实现。
	if len(tokenHandlers) != len(handlers) {
		return registryIndexes{}, fmt.Errorf("token handler registry size %d does not match definitions %d", len(tokenHandlers), len(handlers))
	}
	for handlerID, handler := range tokenHandlers {
		if _, exists := handlers[handlerID]; !exists {
			return registryIndexes{}, fmt.Errorf("token handler implementation %q is not declared", handlerID)
		}
		if handler == nil || handler.ID() != handlerID {
			return registryIndexes{}, fmt.Errorf("token handler implementation %q reports mismatched id", handlerID)
		}
	}
	// OpenAI Compatibility 规则名和顺序也是静态合同，必须与 registry 一起校验。
	if err := validateOpenAICompatibilityRules(); err != nil {
		return registryIndexes{}, err
	}

	// executor alias 经过 trim/lower 后必须唯一，保证 exact match 的结果稳定。
	executors := make(map[string]executorDefinition, len(executorDefinitions))
	for _, definition := range executorDefinitions {
		alias := normalizeRegistryAlias(definition.alias)
		if alias == "" {
			return registryIndexes{}, fmt.Errorf("empty token executor alias")
		}
		if _, exists := handlers[definition.handlerID]; !exists {
			return registryIndexes{}, fmt.Errorf("executor alias %q references unknown handler %q", definition.alias, definition.handlerID)
		}
		if _, exists := executors[alias]; exists {
			return registryIndexes{}, fmt.Errorf("duplicate token executor alias %q", definition.alias)
		}
		executors[alias] = definition
	}

	// identity alias 也必须唯一，但它们只生成 identity_hint，不与 executor registry 合并权限。
	identities := make(map[string]identityAliasDefinition, len(identityAliasDefinitions))
	for _, definition := range identityAliasDefinitions {
		alias := normalizeRegistryAlias(definition.alias)
		if alias == "" {
			return registryIndexes{}, fmt.Errorf("empty token identity alias")
		}
		if _, exists := handlers[definition.handlerID]; !exists {
			return registryIndexes{}, fmt.Errorf("identity alias %q references unknown handler %q", definition.alias, definition.handlerID)
		}
		if _, exists := identities[alias]; exists {
			return registryIndexes{}, fmt.Errorf("duplicate token identity alias %q", definition.alias)
		}
		identities[alias] = definition
	}

	// prefix matcher 按声明顺序保留，未来新增规则必须先证明互斥且不能抢占 exact alias。
	prefixes := make([]identityPrefixDefinition, 0, len(identityPrefixDefinitions))
	seenPrefixes := make(map[string]struct{}, len(identityPrefixDefinitions))
	for _, definition := range identityPrefixDefinitions {
		prefix := normalizeRegistryAlias(definition.prefix)
		if prefix == "" {
			return registryIndexes{}, fmt.Errorf("empty token identity prefix")
		}
		if _, exists := handlers[definition.handlerID]; !exists {
			return registryIndexes{}, fmt.Errorf("identity prefix %q references unknown handler %q", definition.prefix, definition.handlerID)
		}
		if _, exists := seenPrefixes[prefix]; exists {
			return registryIndexes{}, fmt.Errorf("duplicate token identity prefix %q", definition.prefix)
		}
		seenPrefixes[prefix] = struct{}{}
		definition.prefix = prefix
		prefixes = append(prefixes, definition)
	}

	return registryIndexes{executors: executors, identities: identities, identityPrefixes: prefixes}, nil
}

func normalizeRegistryAlias(value string) string {
	// alias 只做大小写和首尾空白归一化，不改写内部字符，防止相似名称被误识别成已知协议。
	return strings.ToLower(strings.TrimSpace(value))
}
