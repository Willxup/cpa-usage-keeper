package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type credentialStatusCredentialCall struct {
	name      string
	authIndex string
	disabled  bool
}

type credentialStatusProviderCall struct {
	providerType   string
	group          int
	excludedModels []string
}

// credentialStatusClientStub 只实现凭证开关需要的三个 CPA 能力，用于断言调用参数与错误映射。
type credentialStatusClientStub struct {
	mu sync.Mutex

	credentialCalls       []credentialStatusCredentialCall
	credentialStatusErr   error
	credentialStatusCode  int
	credentialStatusAfter func()

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

func (s *credentialStatusClientStub) UpdateCredentialStatus(_ context.Context, name string, authIndex string, disabled bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentialCalls = append(s.credentialCalls, credentialStatusCredentialCall{name: name, authIndex: authIndex, disabled: disabled})
	statusCode := s.credentialStatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if s.credentialStatusErr != nil {
		return statusCode, s.credentialStatusErr
	}
	if statusCode != http.StatusOK {
		// 真实 client 对非 2xx 同时返回状态码和错误，stub 保持同样形状。
		return statusCode, fmt.Errorf("credential status request returned status %d", statusCode)
	}
	if s.credentialStatusAfter != nil {
		s.credentialStatusAfter()
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
		return &response.ProviderKeyConfigResult{StatusCode: http.StatusOK, Document: &providerconfig.Document{}}, nil
	}
	// 返回快照，避免并发读改写共享同一份 slice。
	snapshot := &response.ProviderKeyConfigResult{StatusCode: payload.StatusCode, Body: payload.Body}
	snapshot.Payload = make([]providerconfig.ProviderKeyConfig, len(payload.Payload))
	for i, entry := range payload.Payload {
		entry.ExcludedModels = append([]string(nil), entry.ExcludedModels...)
		snapshot.Payload[i] = entry
	}
	snapshot.Document = testProviderDocument(snapshot.Payload)
	if fetchErr != nil {
		return snapshot, fetchErr
	}
	return snapshot, nil
}

func (s *credentialStatusClientStub) UpdateProviderConfig(_ context.Context, providerType string, document *providerconfig.Document, _ string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := document.ProviderKeys("")
	if err != nil {
		return 0, err
	}
	groupIndex := 0
	if previous := s.providerPayloads[providerType]; previous != nil {
		before := testProviderDocument(previous.Payload)
		for index := range document.Groups {
			if !reflect.DeepEqual(before.Groups[index].Keys[0].Fields["excluded-models"], document.Groups[index].Keys[0].Fields["excluded-models"]) {
				groupIndex = index
				break
			}
		}
	}
	var models []string
	json.Unmarshal(document.Groups[groupIndex].Keys[0].Fields["excluded-models"], &models)
	s.providerCalls = append(s.providerCalls, credentialStatusProviderCall{providerType: providerType, group: groupIndex, excludedModels: models})
	statusCode := s.providerUpdateCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if s.providerUpdateErr != nil {
		return statusCode, s.providerUpdateErr
	}
	if statusCode != http.StatusOK {
		return statusCode, fmt.Errorf("provider list PUT returned status %d", statusCode)
	}
	if s.providerWriteVersions == nil {
		s.providerWriteVersions = map[string]int{}
	}
	if s.providerReadVersions[providerType] != s.providerWriteVersions[providerType] {
		s.providerStaleWrites++
	}
	s.providerWriteVersions[providerType]++
	if payload := s.providerPayloads[providerType]; payload != nil {
		payload.Payload = updated
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
		if row.AuthType == entities.UsageIdentityAuthTypeCredential {
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

func seedCredentialCredential(t *testing.T, db *gorm.DB, fileName string, identity string) {
	t.Helper()
	name := fileName
	seedCredentialStatusIdentity(t, db, entities.UsageIdentity{
		Name:      name,
		AuthType:  entities.UsageIdentityAuthTypeCredential,
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

func TestCredentialStatusServiceCredentialUsesStoredFileNameAndAuthIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-credential.db")
	seedCredentialCredential(t, db, "codex-user.json", "idx-a")
	client := &credentialStatusClientStub{}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{}, nil)

	result, err := provider.SetCredentialDisabled(context.Background(), "idx-a", true)
	if err != nil {
		t.Fatalf("SetCredentialDisabled returned error: %v", err)
	}
	if result.AuthIndex != "idx-a" || !result.Disabled {
		t.Fatalf("unexpected response: %+v", result)
	}
	// CPA 需要文件名定位，同时用 auth_index 校验同名文件确实指向目标账号。
	if len(client.credentialCalls) != 1 {
		t.Fatalf("expected one CPA call, got %+v", client.credentialCalls)
	}
	call := client.credentialCalls[0]
	if call.name != "codex-user.json" || call.authIndex != "idx-a" || !call.disabled {
		t.Fatalf("unexpected CPA status request: %+v", call)
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeCredential, "idx-a"); disabled == nil || !*disabled {
		t.Fatalf("expected local disabled state to be true, got %v", disabled)
	}
	if refresher.count() != 1 {
		t.Fatalf("expected one metadata refresh request, got %d", refresher.count())
	}

	// 再次开启必须复用同一条身份，并把本地状态写回 false。
	if _, err := provider.SetCredentialDisabled(context.Background(), "idx-a", false); err != nil {
		t.Fatalf("SetCredentialDisabled re-enable returned error: %v", err)
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeCredential, "idx-a"); disabled == nil || *disabled {
		t.Fatalf("expected local disabled state to be false, got %v", disabled)
	}
	if refresher.count() != 2 {
		t.Fatalf("expected two metadata refresh requests, got %d", refresher.count())
	}
}

func TestCredentialStatusServiceCredentialRejectsUnknownIdentity(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-credential-missing.db")
	client := &credentialStatusClientStub{}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	_, err := provider.SetCredentialDisabled(context.Background(), "idx-missing", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	if len(client.credentialCalls) != 0 {
		t.Fatalf("expected no CPA call for unknown identity, got %+v", client.credentialCalls)
	}
}

func TestCredentialStatusServiceCredentialMapsUpstream404ToNotFound(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-credential-404.db")
	seedCredentialCredential(t, db, "gone.json", "idx-gone")
	client := &credentialStatusClientStub{credentialStatusCode: http.StatusNotFound}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	_, err := provider.SetCredentialDisabled(context.Background(), "idx-gone", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	// CPA 未落库时不能留下本地“已停用”假象。
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeCredential, "idx-gone"); disabled != nil {
		t.Fatalf("expected local disabled state to stay untouched, got %v", *disabled)
	}
}

func TestCredentialStatusServiceCredentialMapsUpstream409ToConflict(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-credential-409.db")
	seedCredentialCredential(t, db, "plugin-multi.json", "idx-virtual-child")
	// CPA 对插件多账号文件展开出的虚拟子账号返回 409，重试永远不会成功。
	client := &credentialStatusClientStub{credentialStatusCode: http.StatusConflict}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{}, nil)

	_, err := provider.SetCredentialDisabled(context.Background(), "idx-virtual-child", true)
	if !errors.Is(err, service.ErrCredentialStatusConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}
	// 拒绝时不能留下本地“已停用”假象，也不该排队 metadata 同步。
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeCredential, "idx-virtual-child"); disabled != nil {
		t.Fatalf("expected local disabled state to stay untouched, got %v", *disabled)
	}
	if refresher.count() != 0 {
		t.Fatalf("expected no metadata refresh on conflict, got %d", refresher.count())
	}
}

func TestCredentialStatusServiceCredentialRequiresAuthIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-credential-validation.db")
	client := &credentialStatusClientStub{}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	if _, err := provider.SetCredentialDisabled(context.Background(), "  ", true); !errors.Is(err, service.ErrCredentialStatusValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if len(client.credentialCalls) != 0 {
		t.Fatalf("expected no CPA call for invalid request, got %+v", client.credentialCalls)
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
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{}, nil)

	result, err := provider.SetAIProviderDisabled(context.Background(), "idx-gemini", true)
	if err != nil {
		t.Fatalf("SetAIProviderDisabled returned error: %v", err)
	}
	if result.AuthIndex != "idx-gemini" || !result.Disabled {
		t.Fatalf("unexpected response: %+v", result)
	}
	if len(client.providerCalls) != 1 {
		t.Fatalf("expected one provider PUT, got %+v", client.providerCalls)
	}
	patch := client.providerCalls[0]
	// 只修改目标 auth_index 所在的成员，其他组保持不变。
	if patch.providerType != "gemini" || patch.group != 1 {
		t.Fatalf("unexpected provider PUT target: %+v", patch)
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
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

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

func TestCredentialStatusServiceProviderTargetsDistinctAuthIndexInSecondGroup(t *testing.T) {
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
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	if _, err := provider.SetAIProviderDisabled(context.Background(), "idx-second", true); err != nil {
		t.Fatalf("SetAIProviderDisabled returned error: %v", err)
	}
	if len(client.providerCalls) != 1 {
		t.Fatalf("expected one provider PUT, got %+v", client.providerCalls)
	}
	patch := client.providerCalls[0]
	// 相同 API Key 在不同 base-url 下有不同 auth_index，仍按标识定位。
	if patch.group != 1 {
		t.Fatalf("expected group 1 for the second duplicate key, got %+v", patch)
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
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	// 相同 auth_index 的重复配置是有效上游输入，继续修改首项。
	if _, err := provider.SetAIProviderDisabled(context.Background(), "idx-codex", true); err != nil {
		t.Fatalf("SetAIProviderDisabled returned error: %v", err)
	}
	if len(client.providerCalls) != 1 || client.providerCalls[0].group != 0 {
		t.Fatalf("expected PUT updating the first entry, got %+v", client.providerCalls)
	}
	if models := client.providerCalls[0].excludedModels; len(models) != 2 || models[0] != "first-model" || models[1] != "*" {
		t.Fatalf("unexpected excluded models: %#v", models)
	}
}

func TestCredentialStatusServiceProviderRejectsUnsupportedType(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-unsupported.db")
	seedProviderCredential(t, db, "openai", "idx-openai", "secret-openai")
	client := &credentialStatusClientStub{}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	_, err := provider.SetAIProviderDisabled(context.Background(), "idx-openai", true)
	if !errors.Is(err, service.ErrCredentialStatusUnsupported) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
	if len(client.providerCalls) != 0 {
		t.Fatalf("expected no provider PUT for unsupported type, got %+v", client.providerCalls)
	}
}

func TestCredentialStatusServiceProviderRejectsUnknownAuthIndex(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-provider-missing.db")
	seedProviderCredential(t, db, "codex", "idx-codex", "secret-codex")
	client := &credentialStatusClientStub{providerPayloads: map[string]*response.ProviderKeyConfigResult{
		"codex": {StatusCode: http.StatusOK, Payload: []providerconfig.ProviderKeyConfig{{APIKey: "secret-other", AuthIndex: "idx-other"}}},
	}}
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	_, err := provider.SetAIProviderDisabled(context.Background(), "idx-codex", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	// 定位失败时绝不能退化成“改第一条”。
	if len(client.providerCalls) != 0 {
		t.Fatalf("expected no provider PUT when auth index is absent, got %+v", client.providerCalls)
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
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

	_, err := provider.SetAIProviderDisabled(context.Background(), "idx-codex", true)
	if !errors.Is(err, service.ErrCredentialStatusNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
	if len(client.providerCalls) != 0 {
		t.Fatalf("expected no provider PUT after fetch 404, got %+v", client.providerCalls)
	}
}

func TestCredentialStatusServiceSkipsLocalWriteWhenUpstreamFails(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-refresh.db")
	seedCredentialCredential(t, db, "codex-refresh.json", "idx-refresh")
	client := &credentialStatusClientStub{credentialStatusErr: errors.New("upstream unavailable")}
	refresher := &credentialStatusRefresherStub{}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{}, nil)

	if _, err := provider.SetCredentialDisabled(context.Background(), "idx-refresh", true); err == nil {
		t.Fatal("expected upstream error")
	}
	if refresher.count() != 0 {
		t.Fatalf("expected no metadata refresh on failure, got %d", refresher.count())
	}
	if disabled := loadCredentialStatusDisabled(t, db, entities.UsageIdentityAuthTypeCredential, "idx-refresh"); disabled != nil {
		t.Fatalf("expected local disabled state to stay untouched, got %v", *disabled)
	}
}

func TestCredentialStatusServiceRequestsMetadataRefreshWhenLocalWriteFailsAfterUpstreamSuccess(t *testing.T) {
	db := openMetadataTestDatabase(t, "credential-status-local-write-failure.db")
	seedCredentialCredential(t, db, "codex-local-write-failure.json", "idx-local-write-failure")
	refresher := &credentialStatusRefresherStub{}
	client := &credentialStatusClientStub{}
	client.credentialStatusAfter = func() {
		if err := db.Migrator().DropTable(&entities.UsageIdentity{}); err != nil {
			t.Fatalf("drop usage identities table: %v", err)
		}
	}
	provider := service.NewCredentialStatusService(db, client, refresher, &service.CredentialMutationLocks{}, nil)

	if _, err := provider.SetCredentialDisabled(context.Background(), "idx-local-write-failure", true); err == nil {
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
	provider := service.NewCredentialStatusService(db, client, nil, &service.CredentialMutationLocks{}, nil)

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
		t.Fatalf("expected four serialized provider PUTes, got %d", len(client.providerCalls))
	}
	if client.providerStaleWrites != 0 {
		t.Fatalf("expected every read-modify-write to observe the previous PUT, got %d stale writes", client.providerStaleWrites)
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
