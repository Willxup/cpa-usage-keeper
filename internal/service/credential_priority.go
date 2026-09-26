package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	UpdateAuthFilePriority(context.Context, string, int) (int, error)
	FetchPriorityProviderConfig(context.Context, string) (*response.ProviderKeyConfigResult, error)
	UpdateProviderPriority(context.Context, string, int, int) (int, error)
	FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error)
	UpdateOpenAICompatibilityPriority(context.Context, int, int) (int, error)
}

type CredentialPriorityProvider interface {
	SetAuthFilePriority(context.Context, string, int) (CredentialPriorityResponse, error)
	SetAIProviderPriority(context.Context, string, int) (CredentialPriorityResponse, error)
}

type CredentialPriorityResponse struct {
	AuthIndex string `json:"auth_index"`
	Priority  int    `json:"priority"`
}

type credentialPriorityService struct {
	db      *gorm.DB
	client  CredentialPriorityClient
	refresh MetadataRefresher
	locks   *CredentialMutationLocks
}

func NewCredentialPriorityService(db *gorm.DB, client CredentialPriorityClient, refresh MetadataRefresher, locks *CredentialMutationLocks) CredentialPriorityProvider {
	return &credentialPriorityService{db: db, client: client, refresh: refresh, locks: locks}
}

func (s *credentialPriorityService) SetAuthFilePriority(ctx context.Context, authIndex string, priority int) (CredentialPriorityResponse, error) {
	authIndex, err := s.validate(authIndex)
	if err != nil {
		return CredentialPriorityResponse{}, err
	}
	identity, err := s.findIdentity(ctx, entities.UsageIdentityAuthTypeAuthFile, authIndex)
	if err != nil {
		return CredentialPriorityResponse{}, err
	}
	name := ""
	if identity.FileName != nil {
		name = strings.TrimSpace(*identity.FileName)
	}
	if name == "" {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: auth file name is unavailable", ErrCredentialPriorityValidation)
	}

	defer s.locks.lockAuthFile(name)()

	statusCode, err := s.client.UpdateAuthFilePriority(ctx, name, priority)
	if err != nil {
		return CredentialPriorityResponse{}, priorityWriteError(statusCode, err)
	}
	defer s.requestRefresh()
	if err := repository.UpdateUsageIdentityPriority(ctx, s.db, entities.UsageIdentityAuthTypeAuthFile, authIndex, priority); err != nil {
		return CredentialPriorityResponse{}, fmt.Errorf("persist auth file priority: %w", err)
	}
	return CredentialPriorityResponse{AuthIndex: authIndex, Priority: priority}, nil
}

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
	defer s.locks.lock("ai-provider:" + authIndex)()

	result, err := s.client.FetchPriorityProviderConfig(ctx, providerType)
	if err != nil || result == nil {
		return CredentialPriorityResponse{}, fmt.Errorf("fetch %s priority target: %w", providerType, priorityFetchError(err))
	}
	index, found := findPriorityProviderIndex(result.Payload, authIndex)
	if !found {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: %s credential", ErrCredentialPriorityNotFound, providerType)
	}
	statusCode, err := s.client.UpdateProviderPriority(ctx, providerType, index, priority)
	if err != nil {
		return CredentialPriorityResponse{}, priorityWriteError(statusCode, err)
	}
	defer s.requestRefresh()
	if err := repository.UpdateUsageIdentityPriority(ctx, s.db, entities.UsageIdentityAuthTypeAIProvider, authIndex, priority); err != nil {
		return CredentialPriorityResponse{}, fmt.Errorf("persist %s priority: %w", providerType, err)
	}
	return CredentialPriorityResponse{AuthIndex: authIndex, Priority: priority}, nil
}

// OpenAI Compatibility 的 priority 属于外层 provider，按其中所有 auth-index 组成的目标锁串行写入。
func (s *credentialPriorityService) setOpenAIProviderPriority(ctx context.Context, authIndex string, priority int) (CredentialPriorityResponse, error) {
	initial, err := s.client.FetchOpenAICompatibility(ctx)
	if err != nil || initial == nil {
		return CredentialPriorityResponse{}, fmt.Errorf("fetch openai priority target: %w", priorityFetchError(err))
	}
	_, provider, found := findOpenAIProvider(initial.Payload, authIndex)
	if !found {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: openai credential", ErrCredentialPriorityNotFound)
	}
	// 初始 GET 仅用于选锁，锁内再次 GET 并重新按 auth-index 定位原始数组下标。
	defer s.locks.lock("openai-provider:" + openAIProviderLockKey(provider))()
	current, err := s.client.FetchOpenAICompatibility(ctx)
	if err != nil || current == nil {
		return CredentialPriorityResponse{}, fmt.Errorf("fetch locked openai priority target: %w", priorityFetchError(err))
	}
	index, lockedProvider, found := findOpenAIProvider(current.Payload, authIndex)
	if !found {
		return CredentialPriorityResponse{}, fmt.Errorf("%w: openai credential", ErrCredentialPriorityNotFound)
	}
	statusCode, err := s.client.UpdateOpenAICompatibilityPriority(ctx, index, priority)
	if err != nil {
		return CredentialPriorityResponse{}, priorityWriteError(statusCode, err)
	}
	defer s.requestRefresh()
	indexes := openAIProviderAuthIndexes(lockedProvider)
	if err := repository.UpdateOpenAIProviderPriority(ctx, s.db, indexes, priority); err != nil {
		return CredentialPriorityResponse{}, fmt.Errorf("persist openai priority: %w", err)
	}
	return CredentialPriorityResponse{AuthIndex: authIndex, Priority: priority}, nil
}

func findPriorityProviderIndex(payload []providerconfig.ProviderKeyConfig, authIndex string) (int, bool) {
	for index, entry := range payload {
		if strings.TrimSpace(entry.AuthIndex) == authIndex {
			return index, true
		}
	}
	return 0, false
}

func findOpenAIProvider(payload []providerconfig.OpenAICompatibilityConfig, authIndex string) (int, providerconfig.OpenAICompatibilityConfig, bool) {
	for index, provider := range payload {
		for _, entry := range provider.APIKeyEntries {
			if strings.TrimSpace(entry.AuthIndex) == authIndex {
				return index, provider, true
			}
		}
	}
	return 0, providerconfig.OpenAICompatibilityConfig{}, false
}

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

func openAIProviderLockKey(provider providerconfig.OpenAICompatibilityConfig) string {
	indexes := openAIProviderAuthIndexes(provider)
	// 同组所有 key 产生同一把锁；外部重排仍无法在 CPA 的 GET/PATCH 之间保证原子性。
	slices.Sort(indexes)
	return strings.Join(indexes, "\x00")
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
