package test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

// TestProviderMetadataSyncPreservesExistingFiveSourceFields 锁定五类既有 provider 的字段来源与 OpenAI 多 key 语义。
func TestProviderMetadataSyncPreservesExistingFiveSourceFields(t *testing.T) {
	// db 保存公开 SyncMetadata 的最终 AI Provider 行。
	db := openMetadataTestDatabase(t, "existing-provider-fields.db")
	// now 固定本轮新建身份时间。
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	// priority 验证 provider 可选数值字段原样传递。
	priority := 7
	// disabled 验证 provider 可选布尔字段原样传递。
	disabled := true
	// note 验证 provider 可选备注字段原样传递。
	note := "provider note"
	// fetcher 默认成功空列表，本测试为五类既有来源填入互不相等的字段值。
	fetcher := newMetadataTestFetcher()
	// Codex 的 api-key/auth-index/prefix/base-url/name 分别使用唯一值，拒绝字段混用。
	fetcher.standardResults["codex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-codex", AuthIndex: "auth-codex", Prefix: "prefix-codex", BaseURL: "https://codex.example/v1", Name: "Codex Team", Priority: &priority, Disabled: &disabled, Note: &note}}}
	// xAI API Key 使用独立 provider type 与默认展示名。
	fetcher.standardResults["xai"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-xai", AuthIndex: "auth-xai", Prefix: "prefix-xai", BaseURL: "https://xai.example/v1"}}}
	// Gemini 缺 name 时必须回退到原 provider type。
	fetcher.standardResults["gemini"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-gemini", AuthIndex: "auth-gemini", Prefix: "prefix-gemini", BaseURL: "https://gemini.example/v1"}}}
	// Gemini Interactions 使用独立原始类型与产品展示名。
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-interactions", AuthIndex: "auth-interactions", Prefix: "prefix-interactions", BaseURL: "https://interactions.example/v1"}}}
	// Claude 保存独立展示名与字段值。
	fetcher.standardResults["claude"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-claude", AuthIndex: "auth-claude", Prefix: "prefix-claude", BaseURL: "https://claude.example/v1", Name: "Claude Team"}}}
	// Vertex 保留 CPA 正确 provider type。
	fetcher.standardResults["vertex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-vertex", AuthIndex: "auth-vertex", Prefix: "prefix-vertex", BaseURL: "https://vertex.example/v1", Name: "Vertex Team"}}}
	// OpenAI Compatibility 在 provider 层保存共享 metadata，在 entry 层保存多个 key/auth-index。
	fetcher.openAIResult = &response.OpenAICompatibilityResult{StatusCode: 200, Payload: []providerconfig.OpenAICompatibilityConfig{{Name: "OpenRouter", Prefix: "prefix-openai", BaseURL: "https://openrouter.example/v1", Priority: &priority, Disabled: &disabled, Note: &note, APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "secret-openai-a", AuthIndex: "auth-openai-a"}, {APIKey: "secret-openai-b", AuthIndex: "auth-openai-b"}}}}}
	// syncer 注入固定时钟和完整 fetcher。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return now }})
	// 执行一轮五来源成功同步。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 全部来源成功时不应返回 warning。
		t.Fatalf("SyncMetadata returned error: %v", err)
	}
	// identities 按 auth_type + auth-index 读取最终行。
	identities := loadMetadataIdentityMap(t, db)
	// codexRow 验证全部单向字段映射。
	codexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-codex")]
	// Identity 只来自 auth-index，LookupKey 只来自 api-key，其它字段互不推导。
	if codexRow.Name != "Codex Team" || codexRow.Provider != "Codex Team" || codexRow.Identity != "auth-codex" || codexRow.LookupKey != "secret-codex" || codexRow.Prefix != "prefix-codex" || codexRow.BaseURL != "https://codex.example/v1" || codexRow.Type != "codex" || codexRow.AuthTypeName != "apikey" || codexRow.IsDeleted {
		// 输出完整行定位字段来源漂移。
		t.Fatalf("codex provider identity = %+v", codexRow)
	}
	// 可选 provider metadata 必须原样持久化。
	if codexRow.Priority == nil || *codexRow.Priority != priority || codexRow.Disabled == nil || *codexRow.Disabled != disabled || codexRow.Note == nil || *codexRow.Note != note {
		// 输出完整行定位 nullable 字段丢失。
		t.Fatalf("codex provider optional metadata = %+v", codexRow)
	}
	// xAIRow 验证新 API Key 来源不会与 OAuth Auth File 混合。
	xAIRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-xai")]
	// 缺 name 时 xAI 使用固定展示名，Identity 与 LookupKey 仍来自独立字段。
	if xAIRow.Name != "xAI" || xAIRow.Provider != "xAI" || xAIRow.Type != "xai" || xAIRow.Identity != "auth-xai" || xAIRow.LookupKey != "secret-xai" || xAIRow.Prefix != "prefix-xai" || xAIRow.BaseURL != "https://xai.example/v1" {
		// 输出完整行定位新来源未接入或字段漂移。
		t.Fatalf("xAI provider identity = %+v", xAIRow)
	}
	// Gemini 缺 name 时使用 provider type 作为 name/provider。
	geminiRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-gemini")]
	// 默认展示名不能来自 prefix、base URL 或 API Key。
	if geminiRow.Name != "gemini" || geminiRow.Provider != "gemini" || geminiRow.LookupKey != "secret-gemini" || geminiRow.Prefix != "prefix-gemini" || geminiRow.BaseURL != "https://gemini.example/v1" || geminiRow.Type != "gemini" {
		// 输出完整行定位默认名或字段混用。
		t.Fatalf("gemini provider identity = %+v", geminiRow)
	}
	// interactionsRow 验证新来源保留独立原始 provider type。
	interactionsRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-interactions")]
	// 缺 name 时使用产品默认展示名，但数据库 Type 不能折叠成 gemini。
	if interactionsRow.Name != "Gemini Interactions" || interactionsRow.Provider != "Gemini Interactions" || interactionsRow.Type != "gemini-interactions" || interactionsRow.Identity != "auth-interactions" || interactionsRow.LookupKey != "secret-interactions" || interactionsRow.Prefix != "prefix-interactions" || interactionsRow.BaseURL != "https://interactions.example/v1" {
		// 输出完整行定位新来源未接入或类型折叠。
		t.Fatalf("Gemini Interactions provider identity = %+v", interactionsRow)
	}
	// Claude 必须保持独立类型和展示名。
	claudeRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-claude")]
	// Claude 行不得被其它 source 覆盖。
	if claudeRow.Type != "claude" || claudeRow.Name != "Claude Team" || claudeRow.LookupKey != "secret-claude" {
		// 输出完整行定位 source 映射错误。
		t.Fatalf("claude provider identity = %+v", claudeRow)
	}
	// Vertex 必须使用正确拼写的 provider type。
	vertexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-vertex")]
	// 数据库中必须使用 CPA 的正确 vertex 拼写。
	if vertexRow.Type != "vertex" || vertexRow.Name != "Vertex Team" || vertexRow.LookupKey != "secret-vertex" {
		// 输出完整行定位 provider type 错误。
		t.Fatalf("vertex provider identity = %+v", vertexRow)
	}
	// OpenAI provider 的第一条 entry 接收共享 provider metadata。
	openAIFirst := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-openai-a")]
	// 多 key 展平后每条 identity 都必须保留 provider 层 name/prefix/base URL。
	if openAIFirst.Type != "openai" || openAIFirst.Name != "OpenRouter" || openAIFirst.Provider != "OpenRouter" || openAIFirst.LookupKey != "secret-openai-a" || openAIFirst.Prefix != "prefix-openai" || openAIFirst.BaseURL != "https://openrouter.example/v1" {
		// 输出完整行定位组合错误。
		t.Fatalf("first OpenAI compatibility identity = %+v", openAIFirst)
	}
	// OpenAI provider 的第二条 entry 必须形成独立 identity。
	openAISecond := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "auth-openai-b")]
	// 第二条 entry 复用 provider metadata，但拥有自己的 key/auth-index。
	if openAISecond.LookupKey != "secret-openai-b" || openAISecond.Identity != "auth-openai-b" || openAISecond.Prefix != "prefix-openai" || openAISecond.BaseURL != "https://openrouter.example/v1" {
		// 输出完整行定位多 entry 串行。
		t.Fatalf("second OpenAI compatibility identity = %+v", openAISecond)
	}
	// prefix 不允许额外生成 identity 行。
	if _, ok := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "prefix-codex")]; ok {
		// 存在前缀 identity 表示旧 prefix-derived 逻辑回归。
		t.Fatalf("provider prefix created an identity: %+v", identities)
	}
	// 七个 provider endpoint 每轮都必须恰好调用一次。
	for _, source := range []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "openai"} {
		// 单来源调用次数必须保持一，不能遗漏或重复读取。
		if fetcher.callCount(source) != 1 {
			// 输出来源与真实次数定位 service 编排错误。
			t.Fatalf("%s calls = %d", source, fetcher.callCount(source))
		}
	}
}

// TestProviderMetadataSyncKeepsFailedSourcesAndStalesOnlySuccessfulTypes 验证部分失败、nil 与成功无效列表的 stale 边界。
func TestProviderMetadataSyncKeepsFailedSourcesAndStalesOnlySuccessfulTypes(t *testing.T) {
	// db 预置四类既有 provider 状态。
	db := openMetadataTestDatabase(t, "provider-stale-boundaries.db")
	// oldTime 是本轮前的 metadata 更新时间。
	oldTime := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	// now 是本轮成功来源 stale 的统一时间。
	now := oldTime.Add(24 * time.Hour)
	// seed 分别代表 fetch error、成功空列表、nil result 和成功但全无效条目。
	seed := []entities.UsageIdentity{
		// Gemini fetch error 后必须保持 active。
		{Name: "Old Gemini", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-gemini", Type: "gemini", Provider: "Gemini", CreatedAt: oldTime, UpdatedAt: oldTime},
		// Claude 成功空列表后必须 stale。
		{Name: "Old Claude", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-claude", Type: "claude", Provider: "Claude", CreatedAt: oldTime, UpdatedAt: oldTime},
		// Codex nil result 后必须保持 active。
		{Name: "Old Codex", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-codex", Type: "codex", Provider: "Codex", CreatedAt: oldTime, UpdatedAt: oldTime},
		// Vertex 成功但所有条目无效仍属于 fetched，因此必须 stale。
		{Name: "Old Vertex", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-vertex", Type: "vertex", Provider: "Vertex", CreatedAt: oldTime, UpdatedAt: oldTime},
	}
	// 写入既有 provider 行。
	if err := db.Create(&seed).Error; err != nil {
		// seed 失败不属于待测逻辑。
		t.Fatalf("seed provider identities: %v", err)
	}
	// fetcher 配置四种不同来源状态。
	fetcher := newMetadataTestFetcher()
	// Gemini error 不返回可用 result。
	fetcher.standardResults["gemini"] = nil
	// 注入 Gemini fetch failure。
	fetcher.standardErrors["gemini"] = errors.New("gemini unavailable")
	// Claude 保持默认 200 空列表。
	fetcher.standardResults["claude"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	// Codex nil result 且 nil error 是独立 warning 状态。
	fetcher.standardResults["codex"] = nil
	// Vertex 返回一条缺 auth-index 的无效 entry，但 endpoint 本身成功。
	fetcher.standardResults["vertex"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "invalid-without-auth-index"}}}
	// syncer 注入固定 stale 时间。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return now }})
	// 执行一轮部分成功同步。
	err := syncer.SyncMetadata(context.Background())
	// 错误必须同时保留 Gemini failure 与 Codex nil warning。
	if err == nil || !strings.Contains(err.Error(), "fetch gemini api keys: gemini unavailable") || !strings.Contains(err.Error(), "codex api keys response is nil") {
		// 输出真实错误定位来源分类丢失。
		t.Fatalf("provider boundary warning = %v", err)
	}
	// identities 读取本轮后四条状态。
	identities := loadMetadataIdentityMap(t, db)
	// Gemini 失败来源必须保持 active 和旧 updated_at。
	geminiRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-gemini")]
	// failure 不允许进入 fetched type stale scope。
	if geminiRow.IsDeleted || geminiRow.DeletedAt != nil || !geminiRow.UpdatedAt.Equal(oldTime) {
		// 输出完整行定位错误 stale。
		t.Fatalf("failed Gemini identity = %+v", geminiRow)
	}
	// Codex nil result 同样必须保持 active 和旧时间。
	codexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-codex")]
	// nil response 不得误判为成功空列表。
	if codexRow.IsDeleted || codexRow.DeletedAt != nil || !codexRow.UpdatedAt.Equal(oldTime) {
		// 输出完整行定位 nil 分类错误。
		t.Fatalf("nil Codex identity = %+v", codexRow)
	}
	// Claude 200 空列表必须 stale。
	claudeRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-claude")]
	// stale 的 deleted_at 与 updated_at 必须共用 now。
	if !claudeRow.IsDeleted || claudeRow.DeletedAt == nil || !claudeRow.DeletedAt.Equal(now) || !claudeRow.UpdatedAt.Equal(now) {
		// 输出完整行定位成功空列表语义错误。
		t.Fatalf("empty Claude identity = %+v", claudeRow)
	}
	// Vertex 成功但条目全无效仍必须 stale。
	vertexRow := identities[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-vertex")]
	// fetched 状态不能由有效 credential 数量决定。
	if !vertexRow.IsDeleted || vertexRow.DeletedAt == nil || !vertexRow.DeletedAt.Equal(now) || !vertexRow.UpdatedAt.Equal(now) {
		// 输出完整行定位全无效 entry 分类错误。
		t.Fatalf("invalid Vertex identity = %+v", vertexRow)
	}
}

// TestProviderMetadataSyncNewSourcesTreatOnlyTyped404AsOptional 验证新 endpoint 的 404 保留与成功空列表 stale 语义。
func TestProviderMetadataSyncNewSourcesTreatOnlyTyped404AsOptional(t *testing.T) {
	// db 同时保存 xAI OAuth 与两个新 AI Provider，验证 auth_type 隔离。
	db := openMetadataTestDatabase(t, "new-provider-optional-404.db")
	// oldTime 是既有行时间。
	oldTime := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	// firstNow 是旧 CPA typed 404 兼容轮时间。
	firstNow := oldTime.Add(24 * time.Hour)
	// secondNow 是 endpoint 已支持且成功空列表的 stale 时间。
	secondNow := firstNow.Add(time.Hour)
	// seed 同时包含相同 auth-index 的 xAI OAuth 与 xAI API Key，关联只能依赖 auth_type + identity。
	seed := []entities.UsageIdentity{
		// OAuth xAI 由 Auth Files 管理，provider stale 不能触碰。
		{Name: "xAI OAuth", AuthType: entities.UsageIdentityAuthTypeAuthFile, AuthTypeName: "oauth", Identity: "shared-xai-auth", Type: "xai", Provider: "xAI", CreatedAt: oldTime, UpdatedAt: oldTime},
		// xAI API Key 在 typed 404 时必须保持 active。
		{Name: "xAI API Key", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "shared-xai-auth", Type: "xai", Provider: "xAI", LookupKey: "old-xai-secret", CreatedAt: oldTime, UpdatedAt: oldTime},
		// Gemini Interactions 在 typed 404 时必须保持 active。
		{Name: "Interactions", AuthType: entities.UsageIdentityAuthTypeAIProvider, AuthTypeName: "apikey", Identity: "old-interactions", Type: "gemini-interactions", Provider: "Gemini Interactions", LookupKey: "old-interactions-secret", CreatedAt: oldTime, UpdatedAt: oldTime},
	}
	// 写入三条既有身份。
	if err := db.Create(&seed).Error; err != nil {
		// seed 失败不属于 optional 404 合同。
		t.Fatalf("seed new provider identities: %v", err)
	}
	// fetcher 默认其它来源成功空列表。
	fetcher := newMetadataTestFetcher()
	// Auth Files 成功返回同一 xAI OAuth，避免全 Auth File stale 干扰 provider 边界。
	fetcher.setAuthFiles([]authfiles.AuthFile{{AuthIndex: "shared-xai-auth", Type: "xai", Provider: "xAI", Name: "xai.json"}})
	// typed xAI 404 必须同时带非 nil result/status 与 error。
	fetcher.standardResults["xai"] = &response.ProviderKeyConfigResult{StatusCode: 404}
	// error 文本本身不参与 optional 判断。
	fetcher.standardErrors["xai"] = errors.New("xai endpoint missing")
	// typed Interactions 404 使用相同兼容分类。
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 404}
	// 独立 error 证明两个来源都被静默跳过。
	fetcher.standardErrors["gemini-interactions"] = errors.New("interactions endpoint missing")
	// currentNow 支持同一 syncer 执行兼容轮和成功空列表轮。
	currentNow := firstNow
	// syncer 注入固定当前轮时间。
	syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return currentNow }})
	// typed 404 轮必须整体成功，不返回 warning。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// optional endpoint 被错误分类时输出真实错误。
		t.Fatalf("typed 404 SyncMetadata returned error: %v", err)
	}
	// firstRows 读取兼容轮后的三条状态。
	firstRows := loadMetadataIdentityMap(t, db)
	// xAI API Key 必须保持 active 和旧时间，因为来源未加入 fetched types。
	xAIProvider := firstRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "shared-xai-auth")]
	// typed 404 不能 stale 既有 provider 行。
	if xAIProvider.IsDeleted || xAIProvider.DeletedAt != nil || !xAIProvider.UpdatedAt.Equal(oldTime) {
		// 输出完整行定位 optional 分类错误。
		t.Fatalf("xAI provider after typed 404 = %+v", xAIProvider)
	}
	// Interactions 同样必须保持 active 和旧时间。
	interactions := firstRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-interactions")]
	// typed 404 不能 stale 既有 Interactions 行。
	if interactions.IsDeleted || interactions.DeletedAt != nil || !interactions.UpdatedAt.Equal(oldTime) {
		// 输出完整行定位 optional 分类错误。
		t.Fatalf("Interactions after typed 404 = %+v", interactions)
	}
	// 第二轮把两个新 endpoint 改为真正的 200 成功空列表。
	fetcher.standardResults["xai"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	// 清除 xAI fetch error。
	fetcher.standardErrors["xai"] = nil
	// Interactions 同样成功返回空列表。
	fetcher.standardResults["gemini-interactions"] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	// 清除 Interactions fetch error。
	fetcher.standardErrors["gemini-interactions"] = nil
	// 切换到第二轮统一时间。
	currentNow = secondNow
	// 成功空列表轮必须完成 stale 而无 warning。
	if err := syncer.SyncMetadata(context.Background()); err != nil {
		// 输出真实错误定位成功空列表分类。
		t.Fatalf("empty-list SyncMetadata returned error: %v", err)
	}
	// secondRows 读取成功空列表后的最终状态。
	secondRows := loadMetadataIdentityMap(t, db)
	// xAI API Key 必须按成功 provider type stale。
	xAIProvider = secondRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "shared-xai-auth")]
	// deleted_at 与 updated_at 共用第二轮时间。
	if !xAIProvider.IsDeleted || xAIProvider.DeletedAt == nil || !xAIProvider.DeletedAt.Equal(secondNow) || !xAIProvider.UpdatedAt.Equal(secondNow) {
		// 输出完整行定位 xAI stale 错误。
		t.Fatalf("xAI provider after empty list = %+v", xAIProvider)
	}
	// Interactions 同样必须 stale。
	interactions = secondRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAIProvider, "old-interactions")]
	// deleted_at 与 updated_at 共用第二轮时间。
	if !interactions.IsDeleted || interactions.DeletedAt == nil || !interactions.DeletedAt.Equal(secondNow) || !interactions.UpdatedAt.Equal(secondNow) {
		// 输出完整行定位 Interactions stale 错误。
		t.Fatalf("Interactions after empty list = %+v", interactions)
	}
	// 相同 auth-index 的 OAuth xAI 仍由 Auth Files 成功刷新并保持 active。
	xAIOAuth := secondRows[metadataIdentityKey(entities.UsageIdentityAuthTypeAuthFile, "shared-xai-auth")]
	// provider stale 不能跨 auth_type 删除 OAuth 行。
	if xAIOAuth.IsDeleted || xAIOAuth.DeletedAt != nil || !xAIOAuth.UpdatedAt.Equal(secondNow) {
		// 输出完整行定位 auth_type 隔离错误。
		t.Fatalf("xAI OAuth after provider stale = %+v", xAIOAuth)
	}
}

// providerPersistenceProjection 只比较业务字段，不把数据库自增 ID 当作 metadata 等价条件。
type providerPersistenceProjection struct {
	// Name 保存最终展示名。
	Name string
	// ProviderType 保存原始 provider type。
	ProviderType string
	// Provider 保存最终 provider 展示字段。
	Provider string
	// LookupKey 保存内部 API Key 映射结果。
	LookupKey string
	// Prefix 保存独立 prefix metadata。
	Prefix string
	// BaseURL 保存独立 endpoint metadata。
	BaseURL string
	// IsDeleted 保存最终 stale 状态。
	IsDeleted bool
}

// TestProviderMetadataSyncCompletionOrderDoesNotChangeDatabase 验证 provider 完成顺序不改变最终持久化业务数据。
func TestProviderMetadataSyncCompletionOrderDoesNotChangeDatabase(t *testing.T) {
	// registryOrder 是用户指定的稳定归并顺序。
	registryOrder := []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "openai"}
	// orders 覆盖正序、逆序和混合 endpoint 完成时序。
	orders := []struct {
		// name 用于标识当前完成时序。
		name string
		// completionOrder 决定 gate 释放顺序。
		completionOrder []string
	}{
		// 正序与 registry 完全一致。
		{name: "forward", completionOrder: []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "openai"}},
		// 逆序证明 fold 不依赖最快返回来源。
		{name: "reverse", completionOrder: []string{"openai", "vertex", "claude", "gemini-interactions", "gemini", "xai", "codex"}},
		// 混合顺序覆盖相邻来源交错完成。
		{name: "mixed", completionOrder: []string{"gemini", "openai", "codex", "claude", "xai", "vertex", "gemini-interactions"}},
	}
	// baseline 保存第一种时序得到的业务字段集合。
	var baseline map[string]providerPersistenceProjection
	// 每种完成时序使用独立数据库和 fetcher。
	for _, order := range orders {
		// 固定当前 order 供子测试闭包读取。
		order := order
		// 子测试按时序名称运行。
		t.Run(order.name, func(t *testing.T) {
			// db 保存当前完成时序的最终 provider rows。
			db := openMetadataTestDatabase(t, "completion-"+order.name+".db")
			// fetcher 默认非 provider metadata 成功空列表。
			fetcher := newMetadataTestFetcher()
			// entered 记录七个 endpoint 已并发开始。
			entered := make(chan string, len(registryOrder))
			// done 记录每个 endpoint 已真正返回。
			done := make(chan string, len(registryOrder))
			// gates 让测试精确控制每个 endpoint 的完成时序。
			gates := make(map[string]chan struct{}, len(registryOrder))
			// gateOnce 让正常释放与 Cleanup 可以幂等关闭通道。
			gateOnce := make(map[string]*sync.Once, len(registryOrder))
			// 为七个来源创建独立 gate。
			for _, source := range registryOrder {
				// 当前 source 使用独立无缓冲关闭信号。
				gates[source] = make(chan struct{})
				// 当前 source 使用独立 Once。
				gateOnce[source] = &sync.Once{}
			}
			// release 幂等释放目标 endpoint。
			release := func(source string) {
				// Once 防止测试清理重复 close。
				gateOnce[source].Do(func() {
					// 关闭 gate 允许 endpoint 返回。
					close(gates[source])
				})
			}
			// 失败路径也释放全部 endpoint，避免 goroutine 泄漏。
			t.Cleanup(func() {
				// 按 registry 遍历所有 gate。
				for _, source := range registryOrder {
					// 幂等释放尚未完成的 endpoint。
					release(source)
				}
			})
			// 六个标准 provider 使用相同 gate hook，但返回各自唯一业务字段。
			for _, source := range registryOrder[:len(registryOrder)-1] {
				// 固定当前 source 供 hook 闭包读取。
				source := source
				// hook 在返回结果前等待测试释放。
				fetcher.standardHooks[source] = func(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
					// 通知测试 endpoint 已开始。
					entered <- source
					// gate 与 caller context 竞争。
					select {
					case <-gates[source]:
						// gate 释放后继续构造成功结果。
					case <-ctx.Done():
						// context 取消时返回真实错误。
						return nil, ctx.Err()
					}
					// 通知测试当前 endpoint 已完成等待。
					done <- source
					// 每个来源返回唯一字段，便于最终数据库全字段比对。
					return &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-" + source, AuthIndex: "auth-" + source, Name: "name-" + source, Prefix: "prefix-" + source, BaseURL: "https://" + source + ".example/v1"}}}, nil
				}
			}
			// OpenAI Compatibility 使用专属 hook 和响应类型。
			fetcher.openAIHook = func(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
				// 通知测试 OpenAI endpoint 已开始。
				entered <- "openai"
				// gate 与 caller context 竞争。
				select {
				case <-gates["openai"]:
					// gate 释放后继续构造成功结果。
				case <-ctx.Done():
					// context 取消时返回真实错误。
					return nil, ctx.Err()
				}
				// 通知测试 OpenAI endpoint 已完成等待。
				done <- "openai"
				// OpenAI provider 层与 entry 层都使用唯一字段。
				return &response.OpenAICompatibilityResult{StatusCode: 200, Payload: []providerconfig.OpenAICompatibilityConfig{{Name: "name-openai", Prefix: "prefix-openai", BaseURL: "https://openai.example/v1", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "secret-openai", AuthIndex: "auth-openai"}}}}}, nil
			}
			// now 在三种完成时序中保持完全一致。
			now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
			// syncer 使用当前 gate fetcher。
			syncer := service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: func() time.Time { return now }})
			// resultCh 接收后台 SyncMetadata 返回值。
			resultCh := make(chan error, 1)
			// 后台执行同步，让测试线程控制 endpoint gate。
			go func() {
				// 缓冲通道保证测试清理后发送不会阻塞。
				resultCh <- syncer.SyncMetadata(context.Background())
			}()
			// 七个 endpoint 必须全部开始后才允许任一返回。
			waitForMetadataSourceSet(t, entered, registryOrder)
			// 按当前 case 指定顺序逐个完成 endpoint。
			for _, source := range order.completionOrder {
				// 释放当前来源。
				release(source)
				// 等待当前来源确认完成后再释放下一项。
				waitForMetadataSourceSet(t, done, []string{source})
			}
			// 等待完整 SyncMetadata 返回。
			select {
			case err := <-resultCh:
				// 全成功来源不允许产生 warning。
				if err != nil {
					// 输出时序与真实错误。
					t.Fatalf("%s SyncMetadata returned error: %v", order.name, err)
				}
			case <-time.After(time.Second):
				// 超时表示并发 endpoint 或 SQLite 写入未退出。
				t.Fatal("timed out waiting for completion-order SyncMetadata")
			}
			// projection 读取当前数据库的业务字段集合。
			projection := loadProviderPersistenceProjection(t, db)
			// 第一种时序建立基线。
			if baseline == nil {
				// 保存独立 map 作为后续比较目标。
				baseline = projection
				// 当前 case 已完成，无需比较自己。
				return
			}
			// 逆序和混合完成必须得到完全相同的业务字段集合。
			if !reflect.DeepEqual(projection, baseline) {
				// 输出基线与实际集合定位完成顺序污染。
				t.Fatalf("%s projection = %#v, want %#v", order.name, projection, baseline)
			}
		})
	}
}

// waitForMetadataSourceSet 在 deadline 内确认期望来源集合全部发送事件。
func waitForMetadataSourceSet(t *testing.T, events <-chan string, want []string) {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// seen 记录实际出现的来源集合。
	seen := make(map[string]struct{}, len(want))
	// timer 防止串行回归或 goroutine 泄漏永久阻塞测试。
	timer := time.NewTimer(time.Second)
	// helper 返回前释放 timer。
	defer timer.Stop()
	// 持续读取直到收齐期望数量。
	for len(seen) < len(want) {
		// 事件与 deadline 竞争。
		select {
		case source := <-events:
			// 记录当前来源。
			seen[source] = struct{}{}
		case <-timer.C:
			// 输出实际集合定位遗漏来源。
			t.Fatalf("timed out waiting for metadata sources: got=%v want=%v", seen, want)
		}
	}
	// 逐个确认期望来源真实出现，避免重复事件伪装成完整集合。
	for _, source := range want {
		// 当前来源必须存在。
		if _, ok := seen[source]; !ok {
			// 输出来源与集合定位重复事件。
			t.Fatalf("metadata source %q did not run: %v", source, seen)
		}
	}
}

// loadProviderPersistenceProjection 读取 AI Provider 业务字段并忽略数据库自增 ID。
func loadProviderPersistenceProjection(t *testing.T, db *gorm.DB) map[string]providerPersistenceProjection {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// rows 接收当前数据库全部 AI Provider identity。
	var rows []entities.UsageIdentity
	// 只按 auth type 过滤，不使用 ID 参与结果等价。
	if err := db.Where("auth_type = ?", entities.UsageIdentityAuthTypeAIProvider).Find(&rows).Error; err != nil {
		// 查询失败表示持久化测试环境错误。
		t.Fatalf("load provider persistence projection: %v", err)
	}
	// projection 按稳定业务 identity 建立集合。
	projection := make(map[string]providerPersistenceProjection, len(rows))
	// 把每条数据库行映射为业务字段。
	for _, row := range rows {
		// Identity 是唯一业务键，ID 不进入结构。
		projection[row.Identity] = providerPersistenceProjection{Name: row.Name, ProviderType: row.Type, Provider: row.Provider, LookupKey: row.LookupKey, Prefix: row.Prefix, BaseURL: row.BaseURL, IsDeleted: row.IsDeleted}
	}
	// 返回可直接 DeepEqual 的确定性 map。
	return projection
}
