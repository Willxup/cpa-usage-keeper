package service

import (
	"context"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type AuthFileProvider interface {
	ListAuthFiles(context.Context) ([]models.AuthFile, error)
}

type authFileService struct {
	db *gorm.DB
}

func NewAuthFileService(db *gorm.DB) AuthFileProvider {
	return &authFileService{db: db}
}

func (s *authFileService) ListAuthFiles(context.Context) ([]models.AuthFile, error) {
	return repository.ListAuthFiles(s.db)
}
