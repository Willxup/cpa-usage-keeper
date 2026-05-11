package quota

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

const (
	defaultRefreshWorkerLimit = 5
	defaultRefreshTaskTTL     = 20 * time.Minute
	defaultRefreshTaskTimeout = 20 * time.Second
)

type Service struct {
	db       *gorm.DB
	registry ProviderRegistry

	refreshMu            sync.Mutex
	refreshTasks         map[string]*RefreshTaskRecord
	refreshTaskIDsByAuth map[string]string
	refreshWorkerTokens  chan struct{}
	refreshTaskTTL       time.Duration
	refreshTaskSeq       uint64
}

type CheckRequest struct {
	AuthIndex string `json:"auth_index"`
}

type CheckResponse struct {
	ID    string     `json:"id"`
	Quota []QuotaRow `json:"quota"`
}

func NewService(db *gorm.DB, caller ManagementAPICaller) *Service {
	return NewServiceWithRegistry(db, NewDefaultProviderRegistry(caller, DefaultProviderConfigs()))
}

func NewServiceWithRegistry(db *gorm.DB, registry ProviderRegistry) *Service {
	return &Service{
		db:                   db,
		registry:             registry,
		refreshTasks:         make(map[string]*RefreshTaskRecord),
		refreshTaskIDsByAuth: make(map[string]string),
		refreshWorkerTokens:  make(chan struct{}, defaultRefreshWorkerLimit),
		refreshTaskTTL:       defaultRefreshTaskTTL,
	}
}

func (s *Service) Check(ctx context.Context, request CheckRequest) (CheckResponse, error) {
	// 单条查询以 auth_index 为唯一入口，前端不需要知道具体 provider 的 API 细节。
	authIndex := strings.TrimSpace(request.AuthIndex)
	if authIndex == "" {
		return CheckResponse{}, fmt.Errorf("%w: auth_index is required", ErrValidation)
	}
	// 只允许 auth files 身份查询限额，AI provider 身份不进入 provider 调用链路。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CheckResponse{}, fmt.Errorf("%w: %s", ErrNotFound, authIndex)
		}
		return CheckResponse{}, err
	}
	// 按相邻项目规则先匹配 provider 再匹配 type，解析出实际要调用的 quota handler。
	_, handler, ok := s.resolveQuotaHandler(identity.Provider, identity.Type)
	if !ok {
		return CheckResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedType, normalizeIdentityType(identity.Provider))
	}
	// provider 返回各自原始结构后，再统一转换为前端可复用的 quota rows。
	providerOutput, err := handler.Check(ctx, ProviderInput{Identity: identity})
	if err != nil {
		return CheckResponse{}, err
	}
	return CheckResponse{
		ID:    authIndex,
		Quota: NormalizeQuotaRows(providerOutput),
	}, nil
}

func (s *Service) resolveQuotaHandler(provider string, identityType string) (string, ProviderHandler, bool) {
	for _, candidate := range resolveQuotaIdentityTypes(provider, identityType) {
		if handler, ok := s.registry.Provider(candidate); ok {
			return candidate, handler, true
		}
	}
	return "", nil, false
}

func resolveQuotaIdentityTypes(provider string, identityType string) []string {
	candidates := make([]string, 0, 2)
	for _, value := range []string{provider, identityType} {
		normalized := normalizeIdentityType(value)
		if normalized == "" || slices.Contains(candidates, normalized) {
			continue
		}
		candidates = append(candidates, normalized)
	}
	return candidates
}
