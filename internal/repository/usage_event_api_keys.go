package repository

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

// ListUsageEventAPIKeyFilterOptions returns selectable keys with matching events.
// EXISTS uses the key/time index and stops at the first match for each key.
func ListUsageEventAPIKeyFilterOptions(db *gorm.DB, filter dto.UsageQueryFilter) ([]entities.CPAAPIKey, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	// The candidate list must not constrain itself to the currently selected key.
	filter.APIGroupKey = ""
	events := applyUsageEventListQuery(queryUsageEvents(db), filter).
		Select("1").Where("usage_events.api_group_key = cpa_api_keys.api_key")
	rows := make([]entities.CPAAPIKey, 0)
	if err := db.Model(&entities.CPAAPIKey{}).
		Where("is_deleted = ? AND api_key <> ?", false, "").
		Where("EXISTS (?)", events).
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load usage event API key filter options: %w", err)
	}
	return rows, nil
}
