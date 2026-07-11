package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/accessscope"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

const maxAPIKeyAuthFileScopeCount = 100

var (
	ErrAPIKeyAuthFileScopeInvalid       = errors.New("api key auth file scope is invalid")
	ErrAPIKeyAuthFileScopeNotConfigured = errors.New("api key auth file scope is not configured")
	ErrAPIKeyAuthFileScopeUnresolved    = errors.New("api key auth file scope cannot be resolved")
)

// APIKeyAuthFileScopeProvider 管理 API Key 对认证文件的查看范围，并为 Viewer 请求解析强制过滤条件。
type APIKeyAuthFileScopeProvider interface {
	ListAPIKeyAuthFileScopes(context.Context, int64) ([]string, error)
	ReplaceAPIKeyAuthFileScopes(context.Context, int64, []string) ([]string, error)
	ResolveAPIKeyViewerScope(context.Context, int64) (accessscope.ViewerScope, error)
}

type apiKeyAuthFileScopeService struct {
	db *gorm.DB
}

// NewAPIKeyAuthFileScopeService 创建 API Key 认证文件范围服务。
func NewAPIKeyAuthFileScopeService(db *gorm.DB) APIKeyAuthFileScopeProvider {
	return &apiKeyAuthFileScopeService{db: db}
}

func (s *apiKeyAuthFileScopeService) ListAPIKeyAuthFileScopes(ctx context.Context, cpaAPIKeyID int64) ([]string, error) {
	if err := s.ensureActiveCPAAPIKey(ctx, cpaAPIKeyID); err != nil {
		return nil, err
	}
	rows, err := repository.ListAPIKeyAuthFileScopes(ctx, s.db, cpaAPIKeyID)
	if err != nil {
		return nil, err
	}
	return apiKeyAuthFileScopeNames(rows), nil
}

func (s *apiKeyAuthFileScopeService) ReplaceAPIKeyAuthFileScopes(ctx context.Context, cpaAPIKeyID int64, authFileNames []string) ([]string, error) {
	if err := s.ensureActiveCPAAPIKey(ctx, cpaAPIKeyID); err != nil {
		return nil, err
	}
	names, err := normalizeAPIKeyAuthFileScopeNames(authFileNames)
	if err != nil {
		return nil, err
	}
	if len(names) > 0 {
		if _, err := s.resolveActiveAuthFileIdentities(ctx, names); err != nil {
			return nil, err
		}
	}
	if err := repository.ReplaceAPIKeyAuthFileScopes(ctx, s.db, cpaAPIKeyID, names); err != nil {
		return nil, err
	}
	return names, nil
}

func (s *apiKeyAuthFileScopeService) ResolveAPIKeyViewerScope(ctx context.Context, cpaAPIKeyID int64) (accessscope.ViewerScope, error) {
	apiKey, err := s.findActiveCPAAPIKey(ctx, cpaAPIKeyID)
	if err != nil {
		return accessscope.ViewerScope{}, err
	}
	rows, err := repository.ListAPIKeyAuthFileScopes(ctx, s.db, cpaAPIKeyID)
	if err != nil {
		return accessscope.ViewerScope{}, err
	}
	names := apiKeyAuthFileScopeNames(rows)
	if len(names) == 0 {
		return accessscope.ViewerScope{}, ErrAPIKeyAuthFileScopeNotConfigured
	}
	identities, err := s.resolveActiveAuthFileIdentities(ctx, names)
	if err != nil {
		return accessscope.ViewerScope{}, err
	}
	authIndexes := make([]string, 0, len(identities))
	for _, identity := range identities {
		index := strings.TrimSpace(identity.Identity)
		if index != "" {
			authIndexes = append(authIndexes, index)
		}
	}
	if len(authIndexes) != len(names) {
		return accessscope.ViewerScope{}, ErrAPIKeyAuthFileScopeUnresolved
	}
	sort.Strings(authIndexes)
	return accessscope.ViewerScope{
		CPAAPIKeyID:   cpaAPIKeyID,
		APIGroupKey:   apiKey.APIKey,
		AuthFileNames: names,
		AuthIndexes:   authIndexes,
	}, nil
}

func (s *apiKeyAuthFileScopeService) ensureActiveCPAAPIKey(ctx context.Context, cpaAPIKeyID int64) error {
	_, err := s.findActiveCPAAPIKey(ctx, cpaAPIKeyID)
	return err
}

func (s *apiKeyAuthFileScopeService) findActiveCPAAPIKey(ctx context.Context, cpaAPIKeyID int64) (entities.CPAAPIKey, error) {
	if s == nil || s.db == nil {
		return entities.CPAAPIKey{}, fmt.Errorf("database is nil")
	}
	if cpaAPIKeyID <= 0 {
		return entities.CPAAPIKey{}, gorm.ErrRecordNotFound
	}
	return repository.FindActiveCPAAPIKeyByID(s.db.WithContext(ctx), cpaAPIKeyID)
}

func (s *apiKeyAuthFileScopeService) resolveActiveAuthFileIdentities(ctx context.Context, names []string) ([]entities.UsageIdentity, error) {
	identities, err := repository.ListActiveAuthFileUsageIdentitiesByFileNames(ctx, s.db, names)
	if err != nil {
		return nil, err
	}
	byFileName := make(map[string]entities.UsageIdentity, len(identities))
	for _, identity := range identities {
		if identity.FileName == nil {
			continue
		}
		fileName := strings.TrimSpace(*identity.FileName)
		if fileName == "" {
			continue
		}
		if _, duplicate := byFileName[fileName]; duplicate {
			return nil, fmt.Errorf("%w: duplicate active auth file name %q", ErrAPIKeyAuthFileScopeUnresolved, fileName)
		}
		byFileName[fileName] = identity
	}
	resolved := make([]entities.UsageIdentity, 0, len(names))
	for _, name := range names {
		identity, ok := byFileName[name]
		if !ok || strings.TrimSpace(identity.Identity) == "" {
			return nil, fmt.Errorf("%w: auth file %q", ErrAPIKeyAuthFileScopeUnresolved, name)
		}
		resolved = append(resolved, identity)
	}
	return resolved, nil
}

func normalizeAPIKeyAuthFileScopeNames(values []string) ([]string, error) {
	if len(values) > maxAPIKeyAuthFileScopeCount {
		return nil, fmt.Errorf("%w: at most %d auth files are allowed", ErrAPIKeyAuthFileScopeInvalid, maxAPIKeyAuthFileScopeCount)
	}
	seen := make(map[string]struct{}, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			return nil, fmt.Errorf("%w: auth file name is required", ErrAPIKeyAuthFileScopeInvalid)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func apiKeyAuthFileScopeNames(rows []entities.APIKeyAuthFileScope) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.AuthFileName)
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
