package tokenprocessor

// HandlerID 是 Token 字段协议语义的稳定标识；executor 与 identity 最终都只解析到这一组 handler。
type HandlerID string

const (
	// HandlerClaude 表示 CPA Claude parser 的 Input 不含 cache read/creation，需要由 Keeper 合并。
	HandlerClaude HandlerID = "claude"
	// HandlerGemini 表示 CPA Gemini parser 的 Output 不含独立 reasoning，需要由 Keeper 合并。
	HandlerGemini HandlerID = "gemini"
	// HandlerResponsesInclusive 表示 Input 已含 cache 且 Output 已含 reasoning 的 Responses 口径。
	HandlerResponsesInclusive HandlerID = "responses-inclusive"
	// HandlerStrictPassThrough 表示没有足够协议证据，只能保留既有 zero-only 兼容行为。
	HandlerStrictPassThrough HandlerID = "strict-pass-through"
	// HandlerOpenAICompatibility 表示 OpenAI-compatible 方言必须由有序兼容规则判断，不能假设标准 OpenAI 语义。
	HandlerOpenAICompatibility HandlerID = "openai-compatibility"
)

// EvidenceSource 说明本次 handler 选择来自实际 CPA executor、历史 identity hint 或安全默认值。
type EvidenceSource string

const (
	// EvidenceExecutor 表示事件携带已注册 CPA Go executor 类型，能够使用对应 parser 合同。
	EvidenceExecutor EvidenceSource = "executor"
	// EvidenceIdentity 表示 executor 不可用，只能沿用 Keeper 已有 identity 类型规则。
	EvidenceIdentity EvidenceSource = "identity"
	// EvidenceDefault 表示 executor 与 identity 都无法证明协议，只能走 strict default。
	EvidenceDefault EvidenceSource = "default"
)

// EvidenceStrength 区分“parser 已证明字段包含关系”和“identity 只提示历史处理类型”。
type EvidenceStrength string

const (
	// EvidenceParserContract 允许 handler 在基础不变量成立时纠正错误非零 Total。
	EvidenceParserContract EvidenceStrength = "parser_contract"
	// EvidenceIdentityHint 只保留已验证历史规则，不自动获得 executor 的强纠错权限。
	EvidenceIdentityHint EvidenceStrength = "identity_hint"
	// EvidenceUnknown 表示没有可授予强纠错权限的协议证据。
	EvidenceUnknown EvidenceStrength = "unknown"
)

// TokenValues 是 tokenprocessor 唯一接受的业务数据，刻意不携带 UsageEvent 的非 Token 字段。
type TokenValues struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheReadPresent    bool
	CacheCreationTokens int64
	TotalTokens         int64
}

// TokenOutcome 是 Token 处理结果摘要，与 Redis inbox status 完全独立。
type TokenOutcome string

const (
	TokenOutcomeValid         TokenOutcome = "valid"
	TokenOutcomeNormalized    TokenOutcome = "normalized"
	TokenOutcomeCompatibility TokenOutcome = "compatibility"
	TokenOutcomeCorrected     TokenOutcome = "corrected"
	TokenOutcomeAmbiguous     TokenOutcome = "ambiguous"
	TokenOutcomeOverflow      TokenOutcome = "overflow"
)

// Action 记录 Keeper 确实对某个 Token 字段做过的确定性修改。
type Action struct {
	Code           string
	Field          string
	Before         int64
	After          int64
	EvidenceSource EvidenceSource
	Rule           string
	// outcome 只供包内统一汇总严重程度，避免公共 processor 按某个 executor 专属 action code 分支。
	outcome TokenOutcome
}

// Violation 记录已经确认、但当前合同无法安全修复的 Token 不变量问题。
type Violation struct {
	Code   string
	Fields []string
	Reason string
}

// ProcessResult 同时返回最终 Token、全部动作、全部未解决问题和最严重 outcome。
type ProcessResult struct {
	Tokens     TokenValues
	Actions    []Action
	Violations []Violation
	Outcome    TokenOutcome
}

const (
	ActionClampNegativeToken         = "clamp_negative_token"
	ActionBackfillCacheReadAlias     = "backfill_cache_read_alias"
	ActionNormalizeClaudeInput       = "normalize_claude_input"
	ActionNormalizeClaudeCachedAlias = "normalize_claude_cached_alias"
	ActionNormalizeGeminiOutput      = "normalize_gemini_output"
	ActionBackfillZeroTotal          = "backfill_zero_total"
	ActionCorrectNonzeroTotal        = "correct_nonzero_total"
)

const (
	ViolationTokenArithmeticOverflow = "token_arithmetic_overflow"
	ViolationAmbiguousTotalMismatch  = "ambiguous_total_mismatch"
	ViolationCacheReadAliasConflict  = "cache_read_alias_conflict"
	ViolationInputBelowCache         = "input_below_cache_components"
	ViolationOutputBelowReasoning    = "output_below_reasoning"
)

// HandlerResolution 是 resolver 唯一能生成的路由证明；私有标记方法阻止包外伪造 parser_contract。
type HandlerResolution interface {
	HandlerID() HandlerID
	EvidenceSource() EvidenceSource
	EvidenceStrength() EvidenceStrength
	ExecutorType() string
	IdentityType() string
	NeedsIdentity() bool
	UnknownExecutor() bool
	tokenProcessorResolution()
}

// handlerResolution 保存不可由包外结构体字面量构造的实际路由状态。
type handlerResolution struct {
	handlerID        HandlerID
	evidenceSource   EvidenceSource
	evidenceStrength EvidenceStrength
	executorType     string
	identityType     string
	needsIdentity    bool
	unknownExecutor  bool
}

func (r handlerResolution) HandlerID() HandlerID               { return r.handlerID }
func (r handlerResolution) EvidenceSource() EvidenceSource     { return r.evidenceSource }
func (r handlerResolution) EvidenceStrength() EvidenceStrength { return r.evidenceStrength }
func (r handlerResolution) ExecutorType() string               { return r.executorType }
func (r handlerResolution) IdentityType() string               { return r.identityType }
func (r handlerResolution) NeedsIdentity() bool                { return r.needsIdentity }
func (r handlerResolution) UnknownExecutor() bool              { return r.unknownExecutor }
func (handlerResolution) tokenProcessorResolution()            {}
