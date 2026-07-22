package service

import (
	"context"
	"errors"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

var ErrInvalidID = errors.New("invalid id")

type CPAAPIKeyProvider interface {
	ListCPAAPIKeys(ctx context.Context) ([]entities.CPAAPIKey, error)
	FindActiveCPAAPIKeyByValue(ctx context.Context, apiKey string) (entities.CPAAPIKey, error)
	FindActiveCPAAPIKeyByID(ctx context.Context, id int64) (entities.CPAAPIKey, error)
	UpdateCPAAPIKeyAlias(ctx context.Context, id int64, keyAlias string) (entities.CPAAPIKey, error)
}

type cpaAPIKeyService struct {
	db *gorm.DB
}

func NewCPAAPIKeyService(db *gorm.DB) CPAAPIKeyProvider {
	return &cpaAPIKeyService{db: db}
}

func (s *cpaAPIKeyService) ListCPAAPIKeys(context.Context) ([]entities.CPAAPIKey, error) {
	return repository.ListActiveCPAAPIKeys(s.db)
}

func (s *cpaAPIKeyService) FindActiveCPAAPIKeyByValue(_ context.Context, apiKey string) (entities.CPAAPIKey, error) {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return entities.CPAAPIKey{}, gorm.ErrRecordNotFound
	}
	return repository.FindActiveCPAAPIKeyByValue(s.db, trimmed)
}

func (s *cpaAPIKeyService) FindActiveCPAAPIKeyByID(_ context.Context, id int64) (entities.CPAAPIKey, error) {
	if id <= 0 {
		return entities.CPAAPIKey{}, gorm.ErrRecordNotFound
	}
	return repository.FindActiveCPAAPIKeyByID(s.db, id)
}

func (s *cpaAPIKeyService) UpdateCPAAPIKeyAlias(_ context.Context, id int64, keyAlias string) (entities.CPAAPIKey, error) {
	if id <= 0 {
		return entities.CPAAPIKey{}, ErrInvalidID
	}
	// UPDATE 由 dbresolver 自动路由 writer；结果回读再用官方 Write clause 固定到同一物理池。
	if err := repository.UpdateCPAAPIKeyAlias(s.db, id, keyAlias); err != nil {
		return entities.CPAAPIKey{}, err
	}
	return repository.FindActiveCPAAPIKeyByID(s.db.Clauses(dbresolver.Write), id)
}
