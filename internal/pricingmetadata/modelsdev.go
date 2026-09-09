package pricingmetadata

import (
	"encoding/json"
	"sort"
	"strings"
)

type modelsDevProvider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Models map[string]Model `json:"models"`
}

func decodeModelsDev(decoder *json.Decoder) ([]Entry, error) {
	var providers map[string]modelsDevProvider
	if err := decoder.Decode(&providers); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0)
	for _, key := range sortedKeys(providers) {
		provider := providers[key]
		id := strings.TrimSpace(provider.ID)
		if id == "" {
			id = strings.TrimSpace(key)
		}
		if id == "" {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = id
		}
		for _, modelKey := range sortedKeys(provider.Models) {
			model := provider.Models[modelKey]
			if strings.TrimSpace(model.ID) == "" {
				model.ID = strings.TrimSpace(modelKey)
			}
			if model.ID == "" && strings.TrimSpace(model.Name) == "" {
				continue
			}
			entries = append(entries, Entry{ProviderID: id, ProviderName: name, Model: model})
		}
	}
	return entries, nil
}

func sortedKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
