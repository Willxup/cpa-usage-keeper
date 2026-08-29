package test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/cpaapikeys"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// standardMetadataHook 允许单个标准 provider endpoint 在测试中控制结果、错误或完成时序。
type standardMetadataHook func(context.Context) (*response.ProviderKeyConfigResult, error)

// openAIMetadataHook 为 OpenAI Compatibility 的专属响应类型提供同样的测试控制能力。
type openAIMetadataHook func(context.Context) (*response.OpenAICompatibilityResult, error)

// metadataTestFetcher 是七来源并发安全的函数式测试 fetcher，默认所有 endpoint 成功返回空列表。
type metadataTestFetcher struct {
	// callsMu 保护并发 provider goroutine 写入调用次数。
	callsMu sync.Mutex
	// calls 按稳定 endpoint 名记录本轮实际调用次数。
	calls map[string]int
	// authFilesResult 保存 Auth Files 的直接响应。
	authFilesResult *response.AuthFilesResult
	// authFilesErr 注入 Auth Files fetch failure。
	authFilesErr error
	// managementAPIKeysResult 保存管理 API Keys 的直接响应。
	managementAPIKeysResult *response.ManagementAPIKeysResult
	// managementAPIKeysErr 注入管理 API Keys fetch failure。
	managementAPIKeysErr error
	// standardResults 按 source 保存六类标准 API Key endpoint 响应。
	standardResults map[string]*response.ProviderKeyConfigResult
	// standardErrors 按 source 注入独立 fetch error。
	standardErrors map[string]error
	// standardHooks 可覆盖单个标准 endpoint 的结果和阻塞行为。
	standardHooks map[string]standardMetadataHook
	// openAIResult 保存 OpenAI Compatibility 专属响应。
	openAIResult *response.OpenAICompatibilityResult
	// compatibilityError 注入 OpenAI Compatibility fetch error。
	compatibilityError error
	// openAIHook 可覆盖 OpenAI Compatibility 的结果和阻塞行为。
	openAIHook openAIMetadataHook
}

// newMetadataTestFetcher 创建默认全成功空列表 fetcher，避免每个测试复制七个空实现。
func newMetadataTestFetcher() *metadataTestFetcher {
	// fetcher 初始化全部 endpoint 的显式成功响应。
	fetcher := &metadataTestFetcher{
		// calls 从空 map 开始累计每轮调用。
		calls: make(map[string]int),
		// Auth Files 的 200 空列表会正常进入 stale 语义。
		authFilesResult: &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: []authfiles.AuthFile{}}},
		// 管理 API Keys 的 200 空列表会正常替换本地 key 状态。
		managementAPIKeysResult: &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{}}},
		// standardResults 为六个标准 provider source 预留独立结果。
		standardResults: make(map[string]*response.ProviderKeyConfigResult),
		// standardErrors 默认没有来源失败。
		standardErrors: make(map[string]error),
		// standardHooks 默认不改变 endpoint 行为。
		standardHooks: make(map[string]standardMetadataHook),
		// OpenAI Compatibility 默认成功返回空 provider 列表。
		openAIResult: &response.OpenAICompatibilityResult{StatusCode: 200, Payload: []providerconfig.OpenAICompatibilityConfig{}},
	}
	// 六个标准 endpoint 都显式设置为 200 空 payload。
	for _, source := range []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex"} {
		// 每个 source 使用独立 result 指针，测试可以只替换目标来源。
		fetcher.standardResults[source] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	}
	// 返回可直接注入 SyncServiceOptions 的 fetcher。
	return fetcher
}

// recordCall 在锁内记录 endpoint 调用，保证 provider 并发测试没有数据竞争。
func (f *metadataTestFetcher) recordCall(source string) {
	// 锁住 calls map 的写入。
	f.callsMu.Lock()
	// 当前 endpoint 的调用次数增加一次。
	f.calls[source]++
	// 写入完成后立即释放锁。
	f.callsMu.Unlock()
}

// callCount 返回单个 endpoint 已完成记录的调用次数。
func (f *metadataTestFetcher) callCount(source string) int {
	// 锁住 calls map 的读取，配合 race test。
	f.callsMu.Lock()
	// 读取目标 endpoint 的稳定计数。
	count := f.calls[source]
	// 读取完成后释放锁。
	f.callsMu.Unlock()
	// 返回调用次数供编排断言。
	return count
}

// setAuthFiles 用成功响应替换当前 Auth Files payload，供同一 fetcher 重复同步。
func (f *metadataTestFetcher) setAuthFiles(files []authfiles.AuthFile) {
	// 200 响应表示来源成功，即使 files 为空也应执行 stale。
	f.authFilesResult = &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: files}}
	// 成功响应清除之前注入的 fetch error。
	f.authFilesErr = nil
}

// FetchAuthFiles 返回测试配置的 Auth Files 结果，并记录独立调用次数。
func (f *metadataTestFetcher) FetchAuthFiles(context.Context) (*response.AuthFilesResult, error) {
	// Auth Files 是 metadata 编排的第一类来源。
	f.recordCall("auth-files")
	// 返回预设结果与错误，允许 nil/error 合同测试。
	return f.authFilesResult, f.authFilesErr
}

// FetchManagementAPIKeys 返回测试配置的管理 API Keys 结果。
func (f *metadataTestFetcher) FetchManagementAPIKeys(context.Context) (*response.ManagementAPIKeysResult, error) {
	// 管理 API Keys 是 metadata 编排的第二类来源。
	f.recordCall("management-api-keys")
	// 返回预设结果与错误。
	return f.managementAPIKeysResult, f.managementAPIKeysErr
}

// fetchStandardProvider 复用六类标准 API Key endpoint 的测试分派逻辑。
func (f *metadataTestFetcher) fetchStandardProvider(ctx context.Context, source string) (*response.ProviderKeyConfigResult, error) {
	// 每个 provider endpoint 都独立记录调用次数。
	f.recordCall(source)
	// hook 存在时由测试完全控制该 endpoint 的时序和结果。
	if hook := f.standardHooks[source]; hook != nil {
		// 返回 hook 的真实结果，不额外包装错误。
		return hook(ctx)
	}
	// 没有 hook 时返回当前 source 的静态结果与错误。
	return f.standardResults[source], f.standardErrors[source]
}

// FetchCodexAPIKeys 读取 Codex 测试 endpoint。
func (f *metadataTestFetcher) FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// Codex 使用标准 provider 分派。
	return f.fetchStandardProvider(ctx, "codex")
}

// FetchXAIAPIKeys 读取 xAI API Key 测试 endpoint。
func (f *metadataTestFetcher) FetchXAIAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// xAI 使用标准 provider 分派。
	return f.fetchStandardProvider(ctx, "xai")
}

// FetchGeminiAPIKeys 读取普通 Gemini 测试 endpoint。
func (f *metadataTestFetcher) FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 普通 Gemini 使用标准 provider 分派。
	return f.fetchStandardProvider(ctx, "gemini")
}

// FetchInteractionsAPIKeys 读取 Gemini Interactions 测试 endpoint。
func (f *metadataTestFetcher) FetchInteractionsAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// Interactions 使用标准 provider 分派但保留独立 source 名。
	return f.fetchStandardProvider(ctx, "gemini-interactions")
}

// FetchClaudeAPIKeys 读取 Claude 测试 endpoint。
func (f *metadataTestFetcher) FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// Claude 使用标准 provider 分派。
	return f.fetchStandardProvider(ctx, "claude")
}

// FetchVertexAPIKeys 读取 Vertex 测试 endpoint。
func (f *metadataTestFetcher) FetchVertexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// Vertex 使用标准 provider 分派。
	return f.fetchStandardProvider(ctx, "vertex")
}

// FetchOpenAICompatibility 返回 OpenAI Compatibility 专属结果。
func (f *metadataTestFetcher) FetchOpenAICompatibility(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
	// OpenAI Compatibility 独立记录调用次数。
	f.recordCall("openai")
	// hook 存在时由测试控制多 key provider 的时序和结果。
	if f.openAIHook != nil {
		// 返回 hook 的真实结果。
		return f.openAIHook(ctx)
	}
	// 没有 hook 时返回静态专属响应与错误。
	return f.openAIResult, f.compatibilityError
}

// openMetadataTestDatabase 创建带真实 migration 和 serializer 的临时 SQLite。
func openMetadataTestDatabase(t *testing.T, name string) *gorm.DB {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// dbPath 放在测试临时目录，不污染项目运行数据。
	dbPath := filepath.Join(t.TempDir(), name)
	// db 使用生产 OpenDatabase 建立完整 schema。
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	// migration 或连接失败属于测试环境错误。
	if err != nil {
		// 输出初始化错误。
		t.Fatalf("open metadata test database: %v", err)
	}
	// sqlDB 用于关闭底层连接。
	sqlDB, err := db.DB()
	// 底层连接必须可用。
	if err != nil {
		// 输出连接错误。
		t.Fatalf("load metadata test sql database: %v", err)
	}
	// 测试结束时关闭临时数据库。
	t.Cleanup(func() {
		// 关闭失败同样表示资源异常。
		if err := sqlDB.Close(); err != nil {
			// 报告清理错误。
			t.Fatalf("close metadata test database: %v", err)
		}
	})
	// 返回真实 GORM 连接。
	return db
}

// loadMetadataIdentityMap 读取全部 identity，并按 auth_type 与精确 identity 组合键索引。
func loadMetadataIdentityMap(t *testing.T, db *gorm.DB) map[string]entities.UsageIdentity {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// rows 接收 active 与自定义 deleted 全量历史。
	var rows []entities.UsageIdentity
	// ID 排序只让失败输出稳定，不参与业务匹配。
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		// 查询失败表示 schema 或持久化错误。
		t.Fatalf("load metadata usage identities: %v", err)
	}
	// identities 使用 auth_type 隔离 OAuth 与 API Key 的相同 auth-index。
	identities := make(map[string]entities.UsageIdentity, len(rows))
	// 遍历真实数据库行建立断言索引。
	for _, row := range rows {
		// 组合键保持 auth_type + identity 的生产关联规则。
		identities[metadataIdentityKey(row.AuthType, row.Identity)] = row
	}
	// 返回完整索引供测试断言。
	return identities
}

// metadataIdentityKey 构造测试侧的 auth_type + identity 精确键。
func metadataIdentityKey(authType entities.UsageIdentityAuthType, identity string) string {
	// 使用可读前缀避免把不同 auth type 的相同 auth-index 合并。
	return string(rune('0'+authType)) + ":" + identity
}

// metadataStringPointer 创建可选字符串测试值。
func metadataStringPointer(value string) *string {
	// 返回独立地址以模拟 CPA nullable 字段。
	return &value
}

// metadataIntPointer 创建可选整数测试值。
func metadataIntPointer(value int) *int {
	// 返回独立地址以模拟 CPA nullable priority。
	return &value
}

// metadataBoolPointer 创建可选布尔测试值。
func metadataBoolPointer(value bool) *bool {
	// 返回独立地址以模拟 CPA nullable disabled。
	return &value
}
