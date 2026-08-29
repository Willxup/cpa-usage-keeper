package repository

import (
	"sort"
	"strings"

	"gorm.io/gorm"
)

// modelDimensionOption is the minimal projection needed to build model filter options.
type modelDimensionOption struct {
	Model      string
	ModelAlias *string `gorm:"column:model_alias"`
}

// modelDimensionKey returns the user-visible and aggregation key for a model.
// A non-empty trimmed alias takes precedence over the trimmed upstream model.
func modelDimensionKey(model, alias string) string {
	model = strings.TrimSpace(model)
	if alias = strings.TrimSpace(alias); alias != "" {
		return alias
	}
	return model
}

// modelDimensionKeyFromPointer adapts nullable database/event aliases to modelDimensionKey.
func modelDimensionKeyFromPointer(model string, alias *string) string {
	if alias == nil {
		return modelDimensionKey(model, "")
	}
	return modelDimensionKey(model, *alias)
}

// modelDimensionGroupKey names the model grouping dimension used by usage aggregates.
func modelDimensionGroupKey(model, alias string) string {
	return modelDimensionKey(model, alias)
}

// whereModelDimension applies the display/group key semantics to an event query.
// A raw model value matches only rows without a non-empty alias; an alias value
// matches rows whose trimmed alias is that value.
func whereModelDimension(query *gorm.DB, value string) *gorm.DB {
	value = strings.TrimSpace(value)
	if value == "" {
		return query
	}
	return query.Where(
		"(trim(COALESCE(model_alias, '')) = ? OR (trim(COALESCE(model_alias, '')) = '' AND trim(model) = ?))",
		value,
		value,
	)
}

// listModelDimensionOptions deduplicates display/group keys from model/alias pairs.
func listModelDimensionOptions(query *gorm.DB) ([]string, error) {
	var pairs []modelDimensionOption
	if err := query.Select("DISTINCT model, model_alias").Find(&pairs).Error; err != nil {
		return nil, err
	}
	return modelDimensionOptionsFromPairs(pairs), nil
}

func modelDimensionOptionsFromPairs(pairs []modelDimensionOption) []string {
	seen := make(map[string]struct{}, len(pairs))
	values := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		value := modelDimensionKeyFromPointer(pair.Model, pair.ModelAlias)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
