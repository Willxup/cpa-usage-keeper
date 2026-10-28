package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

var (
	// ErrCredentialStatusValidation 表示请求本身不完整，例如缺少 auth_index 或缺少可用的文件名。
	ErrCredentialStatusValidation = errors.New("credential status request validation failed")
	// ErrCredentialStatusNotFound 表示 Keeper 身份或 CPA 目标配置不存在，通常是列表已过期。
	ErrCredentialStatusNotFound = errors.New("credential status target not found")
	// ErrCredentialStatusUnsupported 表示该类凭证没有对应的 CPA 停用语义，例如 OpenAI 兼容供应商。
	ErrCredentialStatusUnsupported = errors.New("credential status is not supported")
	// ErrCredentialStatusConflict 表示 CPA 明确拒绝单独操作该凭证，例如插件多账号文件展开出的虚拟子账号。
	ErrCredentialStatusConflict = errors.New("credential status target cannot be changed on its own")
	// ErrCredentialConfigChanged 表示供应商 GET 后配置已变化，本次 PUT 未保存。
	ErrCredentialConfigChanged = errors.New("credential configuration changed")
)

// MetadataRefresher 让本地操作后尽快与 CPA 对齐，不改变当前轮询/通知模式。
type MetadataRefresher interface {
	RequestLocalMetadataRefresh()
}

// CredentialStatusClient 是凭证开关需要的 CPA 能力子集。
type CredentialStatusClient interface {
	UpdateCredentialStatus(ctx context.Context, name string, authIndex string, disabled bool) (int, error)
	FetchProviderKeyConfig(ctx context.Context, providerType string) (*response.ProviderKeyConfigResult, error)
	UpdateProviderConfig(ctx context.Context, providerType string, document *providerconfig.Document, etag string) (int, error)
}

// CredentialStatusProvider 暴露单条凭证开关，调用方只提供 CPA auth_index。
type CredentialStatusProvider interface {
	SetCredentialDisabled(ctx context.Context, authIndex string, disabled bool) (CredentialStatusResponse, error)
	SetAIProviderDisabled(ctx context.Context, authIndex string, disabled bool) (CredentialStatusResponse, error)
}

type CredentialStatusResponse struct {
	AuthIndex string `json:"auth_index"`
	Disabled  bool   `json:"disabled"`
}

type credentialStatusService struct {
	db      *gorm.DB
	client  CredentialStatusClient
	refresh MetadataRefresher
	// locks 按凭证名或供应商配置路径串行化读改写，避免跨操作互相覆盖。
	locks    *CredentialMutationLocks
	metadata *CredentialMetadataCoordinator
}

func NewCredentialStatusService(db *gorm.DB, client CredentialStatusClient, refresh MetadataRefresher, locks *CredentialMutationLocks, metadata *CredentialMetadataCoordinator) CredentialStatusProvider {
	return &credentialStatusService{db: db, client: client, refresh: refresh, locks: locks, metadata: metadata}
}

// SetCredentialDisabled 用列表里的 auth_index 反查文件名，再按 CPA 的 name + auth_index 定位唯一账号。
func (s *credentialStatusService) SetCredentialDisabled(ctx context.Context, authIndex string, disabled bool) (CredentialStatusResponse, error) {
	resolvedAuthIndex, err := s.validate(authIndex)
	if err != nil {
		return CredentialStatusResponse{}, err
	}

	identity, err := repository.FindActiveUsageIdentityByAuthTypeAndIdentity(ctx, s.db, entities.UsageIdentityAuthTypeCredential, resolvedAuthIndex)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CredentialStatusResponse{}, fmt.Errorf("%w: credential identity", ErrCredentialStatusNotFound)
		}
		return CredentialStatusResponse{}, err
	}
	name := ""
	if identity.FileName != nil {
		name = strings.TrimSpace(*identity.FileName)
	}
	if name == "" {
		return CredentialStatusResponse{}, fmt.Errorf("%w: credential name is unavailable", ErrCredentialStatusValidation)
	}

	defer s.locks.lockCredential(name)()

	statusCode, err := s.client.UpdateCredentialStatus(ctx, name, resolvedAuthIndex, disabled)
	if err != nil {
		if statusCode == http.StatusNotFound {
			return CredentialStatusResponse{}, fmt.Errorf("%w: credential", ErrCredentialStatusNotFound)
		}
		// CPA 只在插件虚拟子账号上返回 409：它没有独立文件，只能整组操作源文件，重试不会成功。
		if statusCode == http.StatusConflict {
			return CredentialStatusResponse{}, fmt.Errorf("%w: credential", ErrCredentialStatusConflict)
		}
		return CredentialStatusResponse{}, fmt.Errorf("update credential status: %w", err)
	}
	if err := s.persistDisabled(ctx, entities.UsageIdentityAuthTypeCredential, resolvedAuthIndex, disabled); err != nil {
		return CredentialStatusResponse{}, err
	}
	return CredentialStatusResponse{AuthIndex: resolvedAuthIndex, Disabled: disabled}, nil
}

// SetAIProviderDisabled 在锁内读取最新分组，修改首个匹配成员的有效排除列表，再 PUT 完整供应商列表。
func (s *credentialStatusService) SetAIProviderDisabled(ctx context.Context, authIndex string, disabled bool) (CredentialStatusResponse, error) {
	resolvedAuthIndex, err := s.validate(authIndex)
	if err != nil {
		return CredentialStatusResponse{}, err
	}

	identity, err := repository.FindActiveUsageIdentityByAuthTypeAndIdentity(ctx, s.db, entities.UsageIdentityAuthTypeAIProvider, resolvedAuthIndex)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CredentialStatusResponse{}, fmt.Errorf("%w: AI provider identity", ErrCredentialStatusNotFound)
		}
		return CredentialStatusResponse{}, err
	}
	providerType := strings.TrimSpace(identity.Type)
	if !cpa.ProviderKeyStatusSupported(providerType) {
		return CredentialStatusResponse{}, fmt.Errorf("%w: provider type %q", ErrCredentialStatusUnsupported, providerType)
	}

	defer s.locks.lockProviderConfig(providerType)()

	result, err := s.client.FetchProviderKeyConfig(ctx, providerType)
	if err != nil {
		if result != nil && result.StatusCode == http.StatusNotFound {
			return CredentialStatusResponse{}, fmt.Errorf("%w: %s provider configuration", ErrCredentialStatusNotFound, providerType)
		}
		return CredentialStatusResponse{}, fmt.Errorf("fetch %s api keys: %w", providerType, err)
	}
	if result == nil || result.Document == nil {
		return CredentialStatusResponse{}, fmt.Errorf("%w: %s configuration is unavailable", ErrCredentialStatusNotFound, providerType)
	}
	location, found := result.Document.FindFirst(resolvedAuthIndex)
	if !found {
		return CredentialStatusResponse{}, fmt.Errorf("%w: %s credential", ErrCredentialStatusNotFound, providerType)
	}
	entry, found := findProviderKeyConfig(result.Payload, resolvedAuthIndex)
	if !found {
		return CredentialStatusResponse{}, fmt.Errorf("%w: %s credential", ErrCredentialStatusNotFound, providerType)
	}

	// 从继承完成的有效规则生成成员覆盖，启用时只移除精确 *，保留其他模型排除项。
	// 写到成员而不是组上，避免一次开关影响同组其他 Key；空数组也必须显式保存。
	nextExcludedModels := setProviderKeyDisabledExcludedModels(entry.ExcludedModels, disabled)
	if err := result.Document.SetKeyExcludedModels(location, nextExcludedModels); err != nil {
		return CredentialStatusResponse{}, err
	}
	statusCode, err := s.client.UpdateProviderConfig(ctx, providerType, result.Document, result.ETag)
	if err != nil {
		if statusCode == http.StatusPreconditionFailed {
			return CredentialStatusResponse{}, fmt.Errorf("%w: %s provider configuration", ErrCredentialConfigChanged, providerType)
		}
		if statusCode == http.StatusNotFound {
			return CredentialStatusResponse{}, fmt.Errorf("%w: %s provider credential", ErrCredentialStatusNotFound, providerType)
		}
		return CredentialStatusResponse{}, fmt.Errorf("update %s api key status: %w", providerType, err)
	}
	if err := s.persistDisabled(ctx, entities.UsageIdentityAuthTypeAIProvider, resolvedAuthIndex, disabled); err != nil {
		return CredentialStatusResponse{}, err
	}
	return CredentialStatusResponse{AuthIndex: resolvedAuthIndex, Disabled: disabled}, nil
}

// findProviderKeyConfig 与 Document.FindFirst 使用同一响应顺序，避免重复标识取到另一成员的规则。
func findProviderKeyConfig(payload []providerconfig.ProviderKeyConfig, authIndex string) (providerconfig.ProviderKeyConfig, bool) {
	for _, entry := range payload {
		if strings.TrimSpace(entry.AuthIndex) == authIndex {
			return entry, true
		}
	}
	return providerconfig.ProviderKeyConfig{}, false
}

func (s *credentialStatusService) validate(authIndex string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("credential status service is nil")
	}
	if s.db == nil {
		return "", fmt.Errorf("database is nil")
	}
	if s.client == nil {
		return "", fmt.Errorf("credential status client is not configured")
	}
	resolvedAuthIndex := strings.TrimSpace(authIndex)
	if resolvedAuthIndex == "" {
		return "", fmt.Errorf("%w: auth_index is required", ErrCredentialStatusValidation)
	}
	return resolvedAuthIndex, nil
}

// persistDisabled 立即写回本地状态，让页面刷新前就看到一致结果；后续 metadata 同步会再对齐一次。
func (s *credentialStatusService) persistDisabled(ctx context.Context, authType entities.UsageIdentityAuthType, authIndex string, disabled bool) error {
	// CPA 已成功时，本地失败也要失效旧快照并排队刷新；通知放在协调器短锁之外。
	defer func() {
		if s.refresh != nil {
			s.refresh.RequestLocalMetadataRefresh()
		}
	}()
	err := s.metadata.completeMutation(func() error {
		return repository.UpdateUsageIdentityDisabled(ctx, s.db, authType, authIndex, disabled)
	})
	if err != nil {
		return fmt.Errorf("update usage identity disabled state: %w", err)
	}
	return nil
}

// setProviderKeyDisabledExcludedModels 保留用户已有的模型排除规则，只用精确的 "*" 表达整条停用。
func setProviderKeyDisabledExcludedModels(models []string, disabled bool) []string {
	// CPA 侧会把 excluded-models 归一化为小写去重列表，这里保持同样顺序与去重口径。
	seen := make(map[string]struct{}, len(models)+1)
	out := make([]string, 0, len(models)+1)
	for _, item := range models {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if key == cpa.ProviderKeyDisabledExcludedModel {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if disabled {
		out = append(out, cpa.ProviderKeyDisabledExcludedModel)
	}
	return out
}

// keyedMutex 为每个 key 提供独立互斥锁，调用方只关心成对的加解锁。
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedMutexEntry
}

type keyedMutexEntry struct {
	mu   sync.Mutex
	refs int
}

func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*keyedMutexEntry)
	}
	entry := k.locks[key]
	if entry == nil {
		entry = &keyedMutexEntry{}
		k.locks[key] = entry
	}
	entry.refs++
	k.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		k.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
