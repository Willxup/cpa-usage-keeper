package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

var (
	ErrCredentialPriorityValidation  = errors.New("credential priority request validation failed")
	ErrCredentialPriorityNotFound    = errors.New("credential priority target not found")
	ErrCredentialPriorityUnsupported = errors.New("credential priority is not supported")
	ErrCredentialPriorityConflict    = errors.New("credential priority target cannot be changed on its own")
)

type CredentialPriorityClient interface {
	UpdateCredentialPriority(context.Context, string, int) (int, error)
	FetchPriorityProviderConfig(context.Context, string) (*response.ProviderKeyConfigResult, error)
	UpdateProviderConfig(context.Context, string, *providerconfig.Document, string) (int, error)
	FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error)
}

type CredentialPriorityProvider interface {
	SetCredentialPriority(context.Context, string, int) (CredentialPriorityResponse, error)
	SetAIProviderPriority(context.Context, string, int) (CredentialPriorityResponse, error)
}

type CredentialPriorityResponse struct {
	AuthIndex string `json:"auth_index"`
	Priority  int    `json:"priority"`
}

type credentialPriorityService struct {
	db       *gorm.DB
	client   CredentialPriorityClient
	refresh  MetadataRefresher
	locks    *CredentialMutationLocks
	metadata *CredentialMetadataCoordinator
}

func NewCredentialPriorityService(db *gorm.DB, client CredentialPriorityClient, refresh MetadataRefresher, locks *CredentialMutationLocks, metadata *CredentialMetadataCoordinator) CredentialPriorityProvider {
	return &credentialPriorityService{db: db, client: client, refresh: refresh, locks: locks, metadata: metadata}
}

func (s *credentialPriorityService) SetCredentialPriority(ctx context.Context, authIndex string, priority int) (CredentialPriorityResponse, error) {
	authIndex, err := s.validate(authIndex)
	if err != nil {
		return CredentialPriorityResponse{}, err
	}
	identity, err := s.findIdentity(ctx, entities.UsageIdentityAuthTypeCredential, authIndex)
	if err != nil {
		return CredentialPriorityResponse{}, err
	}
	name := ""
	if identity.FileName != nil {
		name = strings.TrimSpace(*identity.FileName)
	}
	if name == "" {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: credential name is unavailable", ErrCredentialPriorityValidation)
	}

	defer s.locks.lockCredential(name)()

	statusCode, err := s.client.UpdateCredentialPriority(ctx, name, priority)
	if err != nil {
		return CredentialPriorityResponse{}, priorityWriteError(statusCode, err)
	}
	// 远端成功后更新本地并请求刷新；本地落库失败时也保留刷新机会，不回滚远端。
	defer s.requestRefresh()
	if err := s.metadata.completeMutation(func() error {
		return repository.UpdateUsageIdentityPriority(ctx, s.db, entities.UsageIdentityAuthTypeCredential, authIndex, priority)
	}); err != nil {
		return CredentialPriorityResponse{}, fmt.Errorf("persist credential priority: %w", err)
	}
	return CredentialPriorityResponse{AuthIndex: authIndex, Priority: priority}, nil
}

// SetAIProviderPriority 按身份类型选择配置路径，读取最新列表后只修改首个匹配成员。
// 标准供应商写成员覆盖值；OpenAI 兼容供应商单独走组级优先级流程。
func (s *credentialPriorityService) SetAIProviderPriority(ctx context.Context, authIndex string, priority int) (CredentialPriorityResponse, error) {
	authIndex, err := s.validate(authIndex)
	if err != nil {
		return CredentialPriorityResponse{}, err
	}
	identity, err := s.findIdentity(ctx, entities.UsageIdentityAuthTypeAIProvider, authIndex)
	if err != nil {
		return CredentialPriorityResponse{}, err
	}
	providerType := strings.ToLower(strings.TrimSpace(identity.Type))
	if !cpa.ProviderPrioritySupported(providerType) {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: provider type %q", ErrCredentialPriorityUnsupported, providerType)
	}
	if providerType == "openai" {
		return s.setOpenAIProviderPriority(ctx, authIndex, priority)
	}
	defer s.locks.lockProviderConfig(providerType)()

	result, err := s.client.FetchPriorityProviderConfig(ctx, providerType)
	if err != nil || result == nil || result.Document == nil {
		return CredentialPriorityResponse{}, fmt.Errorf("fetch %s priority target: %w", providerType, priorityFetchError(err))
	}
	location, found := result.Document.FindFirst(authIndex)
	if !found {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: %s credential", ErrCredentialPriorityNotFound, providerType)
	}
	// 显式写入成员优先级，即使值为 0 也覆盖组默认值，不影响同组其他 Key。
	result.Document.SetKeyPriority(location, priority)
	statusCode, err := s.client.UpdateProviderConfig(ctx, providerType, result.Document, result.ETag)
	if err != nil {
		return CredentialPriorityResponse{}, priorityWriteError(statusCode, err)
	}
	// 远端成功后更新本地并请求刷新；本地落库失败时也保留刷新机会，不回滚远端。
	defer s.requestRefresh()
	if err := s.metadata.completeMutation(func() error {
		return repository.UpdateUsageIdentityPriority(ctx, s.db, entities.UsageIdentityAuthTypeAIProvider, authIndex, priority)
	}); err != nil {
		return CredentialPriorityResponse{}, fmt.Errorf("persist %s priority: %w", providerType, err)
	}
	return CredentialPriorityResponse{AuthIndex: authIndex, Priority: priority}, nil
}

// OpenAI priority 属于组；不同组也共写同一供应商列表，使用路径级共享锁。
func (s *credentialPriorityService) setOpenAIProviderPriority(ctx context.Context, authIndex string, priority int) (CredentialPriorityResponse, error) {
	defer s.locks.lockProviderConfig("openai")()
	current, err := s.client.FetchOpenAICompatibility(ctx)
	if err != nil || current == nil || current.Document == nil {
		return CredentialPriorityResponse{}, fmt.Errorf("fetch openai priority target: %w", priorityFetchError(err))
	}
	location, found := current.Document.FindFirst(authIndex)
	if !found {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: openai credential", ErrCredentialPriorityNotFound)
	}
	current.Document.SetGroupPriority(location, priority)
	statusCode, err := s.client.UpdateProviderConfig(ctx, "openai", current.Document, current.ETag)
	if err != nil {
		return CredentialPriorityResponse{}, priorityWriteError(statusCode, err)
	}
	// 远端成功后更新本地并请求刷新；本地落库失败时也保留刷新机会，不回滚远端。
	defer s.requestRefresh()
	// 远端只修改首个匹配组；本地按该组成员的 auth_index 更新。
	// 跨组同标识仍共用一条身份记录，本地没有独立的组隔离。
	indexes := openAIProviderAuthIndexes(current.Payload[location.Group])
	if err := s.metadata.completeMutation(func() error {
		return repository.UpdateOpenAIProviderPriority(ctx, s.db, indexes, priority)
	}); err != nil {
		return CredentialPriorityResponse{}, fmt.Errorf("persist openai priority: %w", err)
	}
	return CredentialPriorityResponse{AuthIndex: authIndex, Priority: priority}, nil
}

// openAIProviderAuthIndexes 去掉空标识和组内重复项，供同一组的本地批量更新使用。
func openAIProviderAuthIndexes(provider providerconfig.OpenAICompatibilityConfig) []string {
	indexes := make([]string, 0, len(provider.APIKeyEntries))
	seen := make(map[string]struct{}, len(provider.APIKeyEntries))
	for _, entry := range provider.APIKeyEntries {
		index := strings.TrimSpace(entry.AuthIndex)
		if index == "" {
			continue
		}
		if _, exists := seen[index]; !exists {
			seen[index] = struct{}{}
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func (s *credentialPriorityService) validate(authIndex string) (string, error) {
	if s == nil || s.db == nil || s.client == nil {
		return "", fmt.Errorf("credential priority service is not configured")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return "", fmt.Errorf("%w: auth_index is required", ErrCredentialPriorityValidation)
	}
	return authIndex, nil
}

func (s *credentialPriorityService) findIdentity(ctx context.Context, authType entities.UsageIdentityAuthType, authIndex string) (entities.UsageIdentity, error) {
	identity, err := repository.FindActiveUsageIdentityByAuthTypeAndIdentity(ctx, s.db, authType, authIndex)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return entities.UsageIdentity{}, fmt.Errorf("%w: usage identity", ErrCredentialPriorityNotFound)
	}
	return identity, err
}

func (s *credentialPriorityService) requestRefresh() {
	if s.refresh != nil {
		s.refresh.RequestLocalMetadataRefresh()
	}
}

func priorityWriteError(statusCode int, err error) error {
	switch statusCode {
	case http.StatusPreconditionFailed:
		return fmt.Errorf("%w: upstream configuration", ErrCredentialConfigChanged)
	case http.StatusNotFound:
		return fmt.Errorf("%w: upstream target", ErrCredentialPriorityNotFound)
	case http.StatusConflict:
		return fmt.Errorf("%w: upstream target", ErrCredentialPriorityConflict)
	default:
		return fmt.Errorf("update upstream priority: %w", err)
	}
}

func priorityFetchError(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("upstream response is unavailable")
}
