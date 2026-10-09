package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type credentialStatusAuthFileCall struct {
	name      string
	authIndex string
	disabled  bool
}

type credentialStatusProviderCall struct {
	providerType   string
	index          int
	excludedModels []string
}

// credentialStatusClientStub 只实现凭证开关需要的三个 CPA 能力，用于断言调用参数与错误映射。
type credentialStatusClientStub struct {
	mu sync.Mutex

	authFileCalls       []credentialStatusAuthFileCall
	authFileStatusErr   error
	authFileStatusCode  int
	authFileStatusAfter func()

	providerPayloads   map[string]*response.ProviderKeyConfigResult
	providerFetchErr   error
	providerCalls      []credentialStatusProviderCall
	providerUpdateErr  error
	providerUpdateCode int
	// providerFetchDelay 拉长读窗口，让并发用例即使缺少服务层串行化也能稳定暴露丢失更新。
	providerFetchDelay time.Duration
	// providerReadVersions 记录每次读取观察到的写入版本，供丢失更新检测使用。
	providerReadVersions map[string]int
	// providerWriteVersions 记录每个 provider 已完成的写入次数。
	providerWriteVersions map[string]int
	// providerStaleWrites 统计基于过期读取发起的写入，正常串行化下恒为 0。
	providerStaleWrites int
}

func (s *credentialStatusClientStub) UpdateAuthFileStatus(_ context.Context, name string, authIndex string, disabled bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authFileCalls = append(s.authFileCalls, credentialStatusAuthFileCall{name: name, authIndex: authIndex, disabled: disabled})
	statusCode := s.authFileStatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if s.authFileStatusErr != nil {
		return statusCode, s.authFileStatusErr
	}
	if statusCode != http.StatusOK {
		// 真实 client 对非 2xx 同时返回状态码和错误，stub 保持同样形状。
		return statusCode, fmt.Errorf("auth file status request returned status %d", statusCode)
	}
	if s.authFileStatusAfter != nil {
		s.authFileStatusAfter()
	}
	return statusCode, nil
}

func (s *credentialStatusClientStub) FetchProviderKeyConfig(_ context.Context, providerType string) (*response.ProviderKeyConfigResult, error) {
	s.mu.Lock()
	if s.providerReadVersions == nil {
		s.providerReadVersions = map[string]int{}
	}
	// 先记录本次读取观察到的写入版本再离开锁：并发用例里多次读取会稳定地都落在同一旧版本上，
	// 缺少服务层串行化时写入就会被判定为过期。
	s.providerReadVersions[providerType] = s.providerWriteVersions[providerType]
	payload, ok := s.providerPayloads[providerType]
	fetchErr := s.providerFetchErr
	fetchDelay := s.providerFetchDelay
	s.mu.Unlock()

	if fetchDelay > 0 {
		time.Sleep(fetchDelay)
	}

	if !ok || payload == nil {
		if fetchErr != nil {
			return nil, fetchErr
		}
		return &response.ProviderKeyConfigResult{StatusCode: http.StatusOK}, nil
	}
	// 返回快照，避免并发读改写共享同一份 slice。
	snapshot := &response.ProviderKeyConfigResult{StatusCode: payload.StatusCode, Body: payload.Body}
	snapshot.Payload = make([]providerconfig.ProviderKeyConfig, len(payload.Payload))
	for i, entry := range payload.Payload {
		entry.ExcludedModels = append([]string(nil), entry.ExcludedModels...)
		snapshot.Payload[i] = entry
	}
	if fetchErr != nil {
		return snapshot, fetchErr
	}
	return snapshot, nil
}

func (s *credentialStatusClientStub) UpdateProviderKeyExcludedModels(_ context.Context, providerType string, index int, excludedModels []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providerCalls = append(s.providerCalls, credentialStatusProviderCall{
		providerType:   providerType,
		index:          index,
		excludedModels: append([]string(nil), excludedModels...),
	})
	if s.providerWriteVersions == nil {
		s.providerWriteVersions = map[string]int{}
	}
	if s.providerReadVersions[providerType] != s.providerWriteVersions[providerType] {
		s.providerStaleWrites++
	}
	s.providerWriteVersions[providerType]++
	// 写回 payload，让下一次读取看到最新结果；丢失更新会因此变成可见的状态回退。
	if payload, ok := s.providerPayloads[providerType]; ok && payload != nil && index >= 0 && index < len(payload.Payload) {
		payload.Payload[index].ExcludedModels = append([]string(nil), excludedModels...)
	}
	statusCode := s.providerUpdateCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if s.providerUpdateErr != nil {
		return statusCode, s.providerUpdateErr
	}
	if statusCode != http.StatusOK {
		return statusCode, fmt.Errorf("provider key patch returned status %d", statusCode)
	}
	return statusCode, nil
}

type credentialStatusRefresherStub struct {
	mu    sync.Mutex
	calls int
}

func (s *credentialStatusRefresherStub) RequestLocalMetadataRefresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
}

func (s *credentialStatusRefresherStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func seedCredentialStatusIdentity(t *testing.T, db *gorm.DB, row entities.UsageIdentity) {
	t.Helper()
	if row.AuthTypeName == "" {
		if row.AuthType == entities.UsageIdentityAuthTypeAuthFile {
			row.AuthTypeName = "oauth"
		} else {
			row.AuthTypeName = "apikey"
		}
	}
	if row.Provider == "" {
		row.Provider = row.Name
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed usage identity: %v", err)
	}
}

func loadCredentialStatusDisabled(t *testing.T, db *gorm.DB, authType entities.UsageIdentityAuthType, identity string) *bool {
	t.Helper()
	var row entities.UsageIdentity
	if err := db.Where("auth_type = ? AND identity = ?", authType, identity).First(&row).Error; err != nil {
		t.Fatalf("load usage identity: %v", err)
	}
	return row.Disabled
}

func seedAuthFileCredential(t *testing.T, db *gorm.DB, fileName string, identity string) {
	t.Helper()
	name := fileName
	seedCredentialStatusIdentity(t, db, entities.UsageIdentity{
		Name:      name,
		AuthType:  entities.UsageIdentityAuthTypeAuthFile,
		Identity:  identity,
		Type:      "codex",
		FileName:  &name,
		IsDeleted: false,
	})
}

func seedProviderCredential(t *testing.T, db *gorm.DB, providerType string, identity string, lookupKey string) {
	t.Helper()
	seedCredentialStatusIdentity(t, db, entities.UsageIdentity{
		Name:      providerType + " team",
		AuthType:  entities.UsageIdentityAuthTypeAIProvider,
		Identity:  identity,
		Type:      providerType,
		LookupKey: lookupKey,
		IsDeleted: false,
	})
}

func TestCredentialStatusServiceAuthFileUsesStoredFileNameAndAuthIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-auth-file.db")
	seedAuthFileCredential(t, db, "codex-user.json", "idx-a")
	client := &credentialStatusClientStub{}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{})

	result, err := provider.SetAuthFileDisabled(context.Background(), "idx-a", true)
	if err != nil {
		t.Fatalf("SetAuthFileDisabled returned error: %v", err)
	}
	if result.AuthIndex != "idx-a" || !result.Disabled {
		t.Fatalf("unexpected response: %+v", result)
	}
	// CPA 需要文件名定位，同时用 auth_index 校验同名文件确实指向目标账号。
	if len(client.authFileCalls) != 1 {
		t.Fatalf("expected one CPA call, got %+v", client.authFileCalls)
	}
	call := client.authFileCalls[0]
	if call.name != "codex-user.json" || call.authIndex != "idx-a" || !call.disabled {
		t.Fatalf("unexpected CPA status request: %+v", call)
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAuthFile, "idx-a"); disabled == nil || !*disabled {
		t.Fatalf("expected local disabled state to be true, got %v", disabled)
	}
	if refresher.count() != 1 {
		t.Fatalf("expected one metadata refresh request, got %d", refresher.count())
	}

	// 再次开启必须复用同一条身份，并把本地状态写回 false。
	if _, err := provider.SetAuthFileDisabled(context.Background(), "idx-a", false); err != nil {
		t.Fatalf("SetAuthFileDisabled re-enable returned error: %v", err)
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAuthFile, "idx-a"); disabled == nil || *disabled {
		t.Fatalf("expected local disabled state to be false, got %v", disabled)
	}
	if refresher.count() != 2 {
		t.Fatalf("expected two metadata refresh requests, got %d", refresher.count())
	}
}

func TestCredentialStatusServiceAuthFileRejectsUnknownIdentity(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-auth-file-missing.db")
	client := &credentialStatusClientStub{}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	_, err := provider.SetAuthFileDisabled(context.Background(), "idx-missing", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	if len(client.authFileCalls) != 0 {
		t.Fatalf("expected no CPA call for unknown identity, got %+v", client.authFileCalls)
	}
}

func TestCredentialStatusServiceAuthFileMapsUpstream404ToNotFound(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-auth-file-404.db")
	seedAuthFileCredential(t, db, "gone.json", "idx-gone")
	client := &credentialStatusClientStub{authFileStatusCode: http.StatusNotFound}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	_, err := provider.SetAuthFileDisabled(context.Background(), "idx-gone", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	// CPA 未落库时不能留下本地“已停用”假象。
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAuthFile, "idx-gone"); disabled != nil {
		t.Fatalf("expected local disabled state to stay untouched, got %v", *disabled)
	}
}

func TestCredentialStatusServiceAuthFileMapsUpstream409ToConflict(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-auth-file-409.db")
	seedAuthFileCredential(t, db, "plugin-multi.json", "idx-virtual-child")
	// CPA 对插件多账号文件展开出的虚拟子账号返回 409，重试永远不会成功。
	client := &credentialStatusClientStub{authFileStatusCode: http.StatusConflict}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{})

	_, err := provider.SetAuthFileDisabled(context.Background(), "idx-virtual-child", true)
	if !errors.Is(err, service.ErrCredentialStatusConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}
	// 拒绝时不能留下本地“已停用”假象，也不该排队 metadata 同步。
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAuthFile, "idx-virtual-child"); disabled != nil {
		t.Fatalf("expected local disabled state to stay untouched, got %v", *disabled)
	}
	if refresher.count() != 0 {
		t.Fatalf("expected no metadata refresh on conflict, got %d", refresher.count())
	}
}

func TestCredentialStatusServiceAuthFileRequiresAuthIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-auth-file-validation.db")
	client := &credentialStatusClientStub{}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	if _, err := provider.SetAuthFileDisabled(context.Background(), "  ", true); !errors.Is(err, service.ErrCredentialStatusValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if len(client.authFileCalls) != 0 {
		t.Fatalf("expected no CPA call for invalid request, got %+v", client.authFileCalls)
	}
}

func TestCredentialStatusServiceProviderTogglesWildcardExcludedModel(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider.db")
	seedProviderCredential(t, db, "gemini", "idx-gemini", "secret-gemini")
	client := &credentialStatusClientStub{providerPayloads: map[string]*response.ProviderKeyConfigResult{
		"gemini": {
			StatusCode: http.StatusOK,
			Payload: []providerconfig.ProviderKeyConfig{
				{APIKey: "secret-other", AuthIndex: "idx-other", ExcludedModels: []string{"gpt-5"}},
				{APIKey: "secret-gemini", AuthIndex: "idx-gemini", ExcludedModels: []string{"gpt-5"}},
			},
		},
	}}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{})

	result, err := provider.SetAIProviderDisabled(context.Background(), "idx-gemini", true)
	if err != nil {
		t.Fatalf("SetAIProviderDisabled returned error: %v", err)
	}
	if result.AuthIndex != "idx-gemini" || !result.Disabled {
		t.Fatalf("unexpected response: %+v", result)
	}
	if len(client.providerCalls) != 1 {
		t.Fatalf("expected one provider patch, got %+v", client.providerCalls)
	}
	patch := client.providerCalls[0]
	// 必须按 CPA 配置数组下标改，值匹配在重复 API Key 时会命中第一条。
	if patch.providerType != "gemini" || patch.index != 1 {
		t.Fatalf("unexpected provider patch target: %+v", patch)
	}
	// 停用只追加精确 "*"，必须保留用户已有规则。
	if len(patch.excludedModels) != 2 || patch.excludedModels[0] != "gpt-5" || patch.excludedModels[1] != "*" {
		t.Fatalf("unexpected excluded models on disable: %#v", patch.excludedModels)
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAIProvider, "idx-gemini"); disabled == nil || !*disabled {
		t.Fatalf("expected local disabled state to be true, got %v", disabled)
	}
	if refresher.count() != 1 {
		t.Fatalf("expected one metadata refresh request, got %d", refresher.count())
	}

	// 开启只移除 "*"，其它规则原样保留。
	if _, err := provider.SetAIProviderDisabled(context.Background(), "idx-gemini", false); err != nil {
		t.Fatalf("SetAIProviderDisabled re-enable returned error: %v", err)
	}
	last := client.providerCalls[len(client.providerCalls)-1]
	if len(last.excludedModels) != 1 || last.excludedModels[0] != "gpt-5" {
		t.Fatalf("unexpected excluded models on enable: %#v", last.excludedModels)
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAIProvider, "idx-gemini"); disabled == nil || *disabled {
		t.Fatalf("expected local disabled state to be false, got %v", disabled)
	}
}

func TestCredentialStatusServiceProviderNormalizesExistingExclusions(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-normalize.db")
	seedProviderCredential(t, db, "claude", "idx-claude", "secret-claude")
	client := &credentialStatusClientStub{providerPayloads: map[string]*response.ProviderKeyConfigResult{
		"claude": {
			StatusCode: http.StatusOK,
			Payload: []providerconfig.ProviderKeyConfig{{
				APIKey: "secret-claude", AuthIndex: "idx-claude",
				// 手工写入的顺序、空白与重复项按 CPA 的归一化口径收敛。
				ExcludedModels: []string{"*", " GPT-5 ", "gpt-5", ""},
			}},
		},
	}}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	if _, err := provider.SetAIProviderDisabled(context.Background(), "idx-claude", true); err != nil {
		t.Fatalf("SetAIProviderDisabled returned error: %v", err)
	}
	last := client.providerCalls[len(client.providerCalls)-1]
	if len(last.excludedModels) != 2 || last.excludedModels[0] != "gpt-5" || last.excludedModels[1] != "*" {
		t.Fatalf("unexpected excluded models: %#v", last.excludedModels)
	}

	client.providerPayloads["claude"].Payload[0].ExcludedModels = last.excludedModels
	if _, err := provider.SetAIProviderDisabled(context.Background(), "idx-claude", false); err != nil {
		t.Fatalf("SetAIProviderDisabled re-enable returned error: %v", err)
	}
	enabled := client.providerCalls[len(client.providerCalls)-1]
	if len(enabled.excludedModels) != 1 || enabled.excludedModels[0] != "gpt-5" {
		t.Fatalf("unexpected excluded models after enable: %#v", enabled.excludedModels)
	}
}

func TestCredentialStatusServiceProviderTargetsSecondDuplicateKeyByIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-duplicate-key.db")
	// 同一 API Key 两条配置、不同 BaseURL；用户点的是第二条。
	seedCredentialStatusIdentity(t, db, entities.UsageIdentity{
		Name:      "Gemini second",
		AuthType:  entities.UsageIdentityAuthTypeAIProvider,
		Identity:  "idx-second",
		Type:      "gemini",
		LookupKey: "shared-key",
		BaseURL:   "https://second.example/v1",
		IsDeleted: false,
	})
	client := &credentialStatusClientStub{providerPayloads: map[string]*response.ProviderKeyConfigResult{
		"gemini": {
			StatusCode: http.StatusOK,
			Payload: []providerconfig.ProviderKeyConfig{
				{APIKey: "shared-key", AuthIndex: "idx-first", BaseURL: "https://first.example/v1", ExcludedModels: []string{"first-model"}},
				{APIKey: "shared-key", AuthIndex: "idx-second", BaseURL: "https://second.example/v1", ExcludedModels: []string{"second-model"}},
			},
		},
	}}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	if _, err := provider.SetAIProviderDisabled(context.Background(), "idx-second", true); err != nil {
		t.Fatalf("SetAIProviderDisabled returned error: %v", err)
	}
	if len(client.providerCalls) != 1 {
		t.Fatalf("expected one provider patch, got %+v", client.providerCalls)
	}
	patch := client.providerCalls[0]
	// CPA 只按 match 改第一条，因此这里必须传第二条的下标。
	if patch.index != 1 {
		t.Fatalf("expected patch index 1 for the second duplicate key, got %+v", patch)
	}
	// 只能带上目标条目自己的排除规则，第一条的规则不能被波及。
	if len(patch.excludedModels) != 2 || patch.excludedModels[0] != "second-model" || patch.excludedModels[1] != "*" {
		t.Fatalf("unexpected excluded models: %#v", patch.excludedModels)
	}
}

func TestCredentialStatusServiceProviderKeepsFirstEntryForRepeatedAuthIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-repeated-index.db")
	seedProviderCredential(t, db, "codex", "idx-codex", "shared-key")
	client := &credentialStatusClientStub{providerPayloads: map[string]*response.ProviderKeyConfigResult{
		"codex": {
			StatusCode: http.StatusOK,
			Payload: []providerconfig.ProviderKeyConfig{
				{APIKey: "shared-key", AuthIndex: "idx-codex", ExcludedModels: []string{"first-model"}},
				{APIKey: "shared-key", AuthIndex: "idx-codex", ExcludedModels: []string{"second-model"}},
			},
		},
	}}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	// 这是防御性用例：CPA 的稳定 ID 生成器会给哈希相同的重复项追加 -N 后缀，因此经 CPA API
	// 不可达，重复 auth-index 只能来自异常数据。此时沿用首项下标，与 Keeper metadata 侧保留
	// 首项的精确 auth-index 去重口径一致。真正守住 P1-1 的是断言 index==1 的
	// TestCredentialStatusServiceProviderTargetsSecondDuplicateKeyByIndex。
	if _, err := provider.SetAIProviderDisabled(context.Background(), "idx-codex", true); err != nil {
		t.Fatalf("SetAIProviderDisabled returned error: %v", err)
	}
	if len(client.providerCalls) != 1 || client.providerCalls[0].index != 0 {
		t.Fatalf("expected patch against the first entry, got %+v", client.providerCalls)
	}
	if models := client.providerCalls[0].excludedModels; len(models) != 2 || models[0] != "first-model" || models[1] != "*" {
		t.Fatalf("unexpected excluded models: %#v", models)
	}
}

func TestCredentialStatusServiceProviderRejectsUnsupportedType(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-unsupported.db")
	seedProviderCredential(t, db, "openai", "idx-openai", "secret-openai")
	client := &credentialStatusClientStub{}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	_, err := provider.SetAIProviderDisabled(context.Background(), "idx-openai", true)
	if !errors.Is(err, service.ErrCredentialStatusUnsupported) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
	if len(client.providerCalls) != 0 {
		t.Fatalf("expected no provider patch for unsupported type, got %+v", client.providerCalls)
	}
}

func TestCredentialStatusServiceProviderRejectsUnknownAuthIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-missing.db")
	seedProviderCredential(t, db, "codex", "idx-codex", "secret-codex")
	client := &credentialStatusClientStub{providerPayloads: map[string]*response.ProviderKeyConfigResult{
		"codex": {StatusCode: http.StatusOK, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-other", AuthIndex: "idx-other"}}},
	}}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	_, err := provider.SetAIProviderDisabled(context.Background(), "idx-codex", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	// 定位失败时绝不能退化成“改第一条”。
	if len(client.providerCalls) != 0 {
		t.Fatalf("expected no provider patch when auth index is absent, got %+v", client.providerCalls)
	}
}

func TestCredentialStatusServiceProviderMapsUpstream404ToNotFound(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-404.db")
	seedProviderCredential(t, db, "codex", "idx-codex", "secret-codex")
	client := &credentialStatusClientStub{
		providerPayloads: map[string]*response.ProviderKeyConfigResult{
			"codex": {StatusCode: http.StatusNotFound},
		},
		providerFetchErr: errors.New("provider config returned 404"),
	}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	_, err := provider.SetAIProviderDisabled(context.Background(), "idx-codex", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	if len(client.providerCalls) != 0 {
		t.Fatalf("expected no provider patch after fetch 404, got %+v", client.providerCalls)
	}
}

func TestCredentialStatusServiceSkipsLocalWriteWhenUpstreamFails(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-refresh.db")
	seedAuthFileCredential(t, db, "codex-refresh.json", "idx-refresh")
	client := &credentialStatusClientStub{authFileStatusErr: errors.New("upstream unavailable")}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{})

	if _, err := provider.SetAuthFileDisabled(context.Background(), "idx-refresh", true); err == nil {
		t.Fatal("expected upstream error")
	}
	if refresher.count() != 0 {
		t.Fatalf("expected no metadata refresh on failure, got %d", refresher.count())
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeAuthFile, "idx-refresh"); disabled != nil {
		t.Fatalf("expected local disabled state to stay untouched, got %v", *disabled)
	}
}

func TestCredentialStatusServiceRequestsMetadataRefreshWhenLocalWriteFailsAfterUpstreamSuccess(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-local-write-failure.db")
	seedAuthFileCredential(t, db, "codex-local-write-failure.json", "idx-local-write-failure")
	refresher := &credentialStatusRefresherStub{}
	client := &credentialStatusClientStub{}
	client.authFileStatusAfter = func() {
		if err := db.Migrator().DropTable(&entities.UsageIdentity{}); err != nil {
			t.Fatalf("drop usage identities table: %v", err)
		}
	}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{})

	if _, err := provider.SetAuthFileDisabled(context.Background(), "idx-local-write-failure", true); err == nil {
		t.Fatal("expected local persistence error")
	}
	if refresher.count() != 1 {
		t.Fatalf("expected one metadata refresh request after local persistence failure, got %d", refresher.count())
	}
}

func TestCredentialStatusServiceSerializesConcurrentProviderToggles(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-concurrent.db")
	seedProviderCredential(t, db, "gemini", "idx-gemini", "secret-gemini")
	client := &credentialStatusClientStub{providerPayloads: map[string]*response.ProviderKeyConfigResult{
		"gemini": {StatusCode: http.StatusOK, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-gemini", AuthIndex: "idx-gemini", ExcludedModels: []string{"gpt-5"}}}},
	}}
	// 拉长读窗口，缺少服务层串行化时四次读取必然全部发生在任何写入之前。
	client.providerFetchDelay = 20 * time.Millisecond
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(disabled bool) {
			defer wg.Done()
			// 同一 auth_index 的并发开关必须串行，避免读改写互相覆盖 excluded-models。
			_, _ = provider.SetAIProviderDisabled(context.Background(), "idx-gemini", disabled)
		}(i%2 == 0)
	}
	wg.Wait()
	if len(client.providerCalls) != 4 {
		t.Fatalf("expected four serialized provider patches, got %d", len(client.providerCalls))
	}
	if client.providerStaleWrites != 0 {
		t.Fatalf("expected every read-modify-write to observe the previous patch, got %d stale writes", client.providerStaleWrites)
	}
	// 串行化后每次读改写都能看到上一轮结果，最终停用列表里 "*" 至多出现一次。
	last := client.providerCalls[len(client.providerCalls)-1]
	wildcards := 0
	for _, item := range last.excludedModels {
		if item == "*" {
			wildcards++
		}
	}
	if wildcards > 1 {
		t.Fatalf("expected at most one wildcard entry, got %#v", last.excludedModels)
	}
}
