package repository

import (
	"context"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"

	"gorm.io/gorm"
)

// ListAPIKeyAuthFileScopes 返回指定 CPA API Key 的认证文件范围配置。
func ListAPIKeyAuthFileScopes(ctx context.Context, db *gorm.DB, cpaAPIKeyID int64) ([]entities.APIKeyAuthFileScope, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if cpaAPIKeyID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var rows []entities.APIKeyAuthFileScope
	if err := db.WithContext(ctx).
		Where("cpa_api_key_id = ?", cpaAPIKeyID).
		Order("auth_file_name ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list api key auth file scopes: %w", err)
	}
	return rows, nil
}

// ReplaceAPIKeyAuthFileScopes 用给定的认证文件名称完整替换一个 API Key 的可见范围。
func ReplaceAPIKeyAuthFileScopes(ctx context.Context, db *gorm.DB, cpaAPIKeyID int64, authFileNames []string) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if cpaAPIKeyID <= 0 {
		return gorm.ErrRecordNotFound
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("cpa_api_key_id = ?", cpaAPIKeyID).Delete(&entities.APIKeyAuthFileScope{}).Error; err != nil {
			return fmt.Errorf("delete api key auth file scopes: %w", err)
		}
		if len(authFileNames) == 0 {
			return nil
		}
		rows := make([]entities.APIKeyAuthFileScope, 0, len(authFileNames))
		for _, authFileName := range authFileNames {
			rows = append(rows, entities.APIKeyAuthFileScope{
				CPAAPIKeyID:  cpaAPIKeyID,
				AuthFileName: strings.TrimSpace(authFileName),
			})
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("create api key auth file scopes: %w", err)
		}
		return nil
	})
}

// ListActiveAuthFileUsageIdentitiesByFileNames 按 CPA 认证文件名读取仍有效的 Auth File 身份。
func ListActiveAuthFileUsageIdentitiesByFileNames(ctx context.Context, db *gorm.DB, authFileNames []string) ([]entities.UsageIdentity, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	names := normalizeUniqueAuthFileNames(authFileNames)
	if len(names) == 0 {
		return []entities.UsageIdentity{}, nil
	}
	var identities []entities.UsageIdentity
	if err := db.WithContext(ctx).
		Select(usageIdentityReadColumns).
		Where("auth_type = ? AND is_deleted = ? AND file_name IN ?", entities.UsageIdentityAuthTypeAuthFile, false, names).
		Order("file_name ASC, id ASC").
		Find(&identities).Error; err != nil {
		return nil, fmt.Errorf("list active auth file usage identities by file names: %w", err)
	}
	return identities, nil
}

func normalizeUniqueAuthFileNames(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}
