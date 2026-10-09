package test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/cpaapikeys"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type standardMetadataHook func(context.Context) (*response.ProviderKeyConfigResult, error)

type openAIMetadataHook func(context.Context) (*response.OpenAICompatibilityResult, error)

// metadataTestFetcher 是八来源并发安全的函数式测试 fetcher，默认所有 endpoint 成功返回空列表。
type metadataTestFetcher struct {
	callsMu                 sync.Mutex
	calls                   map[string]int
	authFilesResult         *response.AuthFilesResult
	authFilesErr            error
	managementAPIKeysResult *response.ManagementAPIKeysResult
	managementAPIKeysErr    error
	standardResults         map[string]*response.ProviderKeyConfigResult
	standardErrors          map[string]error
	standardHooks           map[string]standardMetadataHook
	openAIResult            *response.OpenAICompatibilityResult
	compatibilityError      error
	openAIHook              openAIMetadataHook
}

func newMetadataTestFetcher() *metadataTestFetcher {
	fetcher := &metadataTestFetcher{
		calls:                   make(map[string]int),
		authFilesResult:         &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: []authfiles.AuthFile{}}},
		managementAPIKeysResult: &response.ManagementAPIKeysResult{StatusCode: 200, Payload: cpaapikeys.ManagementAPIKeysResponse{APIKeys: []string{}}},
		standardResults:         make(map[string]*response.ProviderKeyConfigResult),
		standardErrors:          make(map[string]error),
		standardHooks:           make(map[string]standardMetadataHook),
		openAIResult:            &response.OpenAICompatibilityResult{StatusCode: 200, Payload: []providerconfig.OpenAICompatibilityConfig{}},
	}
	for _, source := range []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "meta"} {
		// 每个 source 使用独立 result 指针，测试可以只替换目标来源。
		fetcher.standardResults[source] = &response.ProviderKeyConfigResult{StatusCode: 200, Payload: []providerconfig.ProviderKeyConfig{}}
	}
	return fetcher
}

func (f *metadataTestFetcher) recordCall(source string) {
	f.callsMu.Lock()
	f.calls[source]++
	f.callsMu.Unlock()
}

func (f *metadataTestFetcher) callCount(source string) int {
	f.callsMu.Lock()
	count := f.calls[source]
	f.callsMu.Unlock()
	return count
}

func (f *metadataTestFetcher) setAuthFiles(files []authfiles.AuthFile) {
	f.authFilesResult = &response.AuthFilesResult{StatusCode: 200, Payload: authfiles.AuthFilesResponse{Files: files}}
	f.authFilesErr = nil
}

func (f *metadataTestFetcher) FetchAuthFiles(context.Context) (*response.AuthFilesResult, error) {
	f.recordCall("auth-files")
	return f.authFilesResult, f.authFilesErr
}

func (f *metadataTestFetcher) FetchManagementAPIKeys(context.Context) (*response.ManagementAPIKeysResult, error) {
	f.recordCall("management-api-keys")
	return f.managementAPIKeysResult, f.managementAPIKeysErr
}

func (f *metadataTestFetcher) fetchStandardProvider(ctx context.Context, source string) (*response.ProviderKeyConfigResult, error) {
	f.recordCall(source)
	if hook := f.standardHooks[source]; hook != nil {
		return hook(ctx)
	}
	return f.standardResults[source], f.standardErrors[source]
}

func (f *metadataTestFetcher) FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandardProvider(ctx, "codex")
}

func (f *metadataTestFetcher) FetchXAIAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandardProvider(ctx, "xai")
}

func (f *metadataTestFetcher) FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandardProvider(ctx, "gemini")
}

func (f *metadataTestFetcher) FetchInteractionsAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandardProvider(ctx, "gemini-interactions")
}

func (f *metadataTestFetcher) FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandardProvider(ctx, "claude")
}

func (f *metadataTestFetcher) FetchVertexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandardProvider(ctx, "vertex")
}

func (f *metadataTestFetcher) FetchMetaAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandardProvider(ctx, "meta")
}

func (f *metadataTestFetcher) FetchOpenAICompatibility(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
	f.recordCall("openai")
	if f.openAIHook != nil {
		return f.openAIHook(ctx)
	}
	return f.openAIResult, f.compatibilityError
}

func openMetadataTestDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), name)
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("open metadata test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load metadata test sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close metadata test database: %v", err)
		}
	})
	return db
}

func loadMetadataIdentityMap(t *testing.T, db *gorm.DB) map[string]entities.UsageIdentity {
	t.Helper()
	var rows []entities.UsageIdentity
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load metadata usage identities: %v", err)
	}
	// identities 使用 auth_type 隔离 OAuth 与 API Key 的相同 auth-index。
	identities := make(map[string]entities.UsageIdentity, len(rows))
	for _, row := range rows {
		identities[metadataIdentityKey(row.AuthType, row.Identity)] = row
	}
	return identities
}

func metadataIdentityKey(authType entities.UsageIdentityAuthType, identity string) string {
	return string(rune('0'+authType)) + ":" + identity
}

func metadataStringPointer(value string) *string {
	return &value
}

func metadataIntPointer(value int) *int {
	return &value
}

func metadataBoolPointer(value bool) *bool {
	return &value
}

func newMetadataTestSyncer(db *gorm.DB, fetcher *metadataTestFetcher, now func() time.Time) *service.SyncService {
	return service.NewSyncServiceWithOptions(db, service.SyncServiceOptions{
		BaseURL: "https://cpa.example.com", MetadataFetcher: fetcher, Now: now,
	})
}
