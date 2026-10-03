package service

import (
	"context"
	"cpa-usage-keeper/internal/entities"
	"crypto/sha256"
	"fmt"
	"strconv"
)

type ReportingAPIKey struct {
	ID, APIKey string
	Historical bool
}
type ReportingAPIKeyProvider interface {
	ListReportingAPIKeys(context.Context) ([]ReportingAPIKey, error)
}

// Reporting includes revoked keys and retained events predating key synchronization.
// Authentication and administrative operations continue to use active keys only.
func (s *cpaAPIKeyService) ListReportingAPIKeys(ctx context.Context) ([]ReportingAPIKey, error) {
	var rows []entities.CPAAPIKey
	if err := s.db.WithContext(ctx).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := []ReportingAPIKey{}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.APIKey] = true
		result = append(result, ReportingAPIKey{strconv.FormatInt(row.ID, 10), row.APIKey, row.IsDeleted})
	}
	var raw []string
	if err := s.db.WithContext(ctx).Model(&entities.UsageEvent{}).Distinct("api_group_key").Pluck("api_group_key", &raw).Error; err != nil {
		return nil, err
	}
	for _, key := range raw {
		if key == "" || seen[key] {
			continue
		}
		digest := sha256.Sum256([]byte(key))
		result = append(result, ReportingAPIKey{fmt.Sprintf("historical:%x", digest[:12]), key, true})
	}
	return result, nil
}
