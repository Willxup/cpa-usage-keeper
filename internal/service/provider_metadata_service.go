package service

import (
	"context"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type ProviderMetadataProvider interface {
	ListProviderMetadata(context.Context) ([]models.ProviderMetadata, error)
}

type providerMetadataService struct {
	db *gorm.DB
}

func NewProviderMetadataService(db *gorm.DB) ProviderMetadataProvider {
	return &providerMetadataService{db: db}
}

func (s *providerMetadataService) ListProviderMetadata(context.Context) ([]models.ProviderMetadata, error) {
	return repository.ListProviderMetadata(s.db)
}
