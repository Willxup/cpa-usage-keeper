package pricingmetadata

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type LocalModelItem struct {
	ID                  string   `json:"id"`
	Model               string   `json:"model"`
	Name                string   `json:"name"`
	Provider            string   `json:"provider"`
	ContextWindowTokens int64    `json:"context_window_tokens"`
	ContextWindow       int64    `json:"context_window"`
	MaxOutputTokens     int64    `json:"max_output_tokens"`
	MaxTokens           int64    `json:"max_tokens"`
	PromptPrice         *float64 `json:"prompt_price_per_1m"`
	CompletionPrice     *float64 `json:"completion_price_per_1m"`
	CacheReadPrice      *float64 `json:"cache_read_price_per_1m"`
	CacheWritePrice     *float64 `json:"cache_write_price_per_1m"`
	Cost                *Cost    `json:"cost"`
}

type LocalModelCatalog struct {
	Models []LocalModelItem `json:"models"`
}

func decodeModelJSON(reader io.Reader) ([]Entry, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var catalog LocalModelCatalog
	if err := json.Unmarshal(data, &catalog); err == nil && len(catalog.Models) > 0 {
		return convertLocalModelItems(catalog.Models), nil
	}

	var items []LocalModelItem
	if err := json.Unmarshal(data, &items); err == nil && len(items) > 0 {
		return convertLocalModelItems(items), nil
	}

	var itemMap map[string]LocalModelItem
	if err := json.Unmarshal(data, &itemMap); err == nil && len(itemMap) > 0 {
		items = make([]LocalModelItem, 0, len(itemMap))
		for id, item := range itemMap {
			if item.ID == "" && item.Model == "" {
				item.ID = id
			}
			items = append(items, item)
		}
		return convertLocalModelItems(items), nil
	}

	return nil, fmt.Errorf("unable to parse model.json schema")
}

func convertLocalModelItems(items []LocalModelItem) []Entry {
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = strings.TrimSpace(item.Model)
		}
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		provider := strings.ToLower(strings.TrimSpace(item.Provider))
		if provider == "" {
			provider = "custom"
		}
		ctxTokens := item.ContextWindowTokens
		if ctxTokens <= 0 {
			ctxTokens = item.ContextWindow
		}
		maxTokens := item.MaxOutputTokens
		if maxTokens <= 0 {
			maxTokens = item.MaxTokens
		}

		var cost Cost
		if item.Cost != nil {
			cost = *item.Cost
		} else {
			cost = Cost{
				Input:      item.PromptPrice,
				Output:     item.CompletionPrice,
				CacheRead:  item.CacheReadPrice,
				CacheWrite: item.CacheWritePrice,
			}
		}

		entries = append(entries, Entry{
			ProviderID:   provider,
			ProviderName: provider,
			Model: Model{
				ID:                  id,
				Name:                name,
				ContextWindowTokens: ctxTokens,
				MaxOutputTokens:     maxTokens,
				Cost:                cost,
			},
		})
	}
	return entries
}

func DecodeModelJSONForTest(reader io.Reader) ([]Entry, error) {
	return decodeModelJSON(reader)
}

func LoadModelJSONFile(path string) (Catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return Catalog{}, err
	}
	defer file.Close()

	entries, err := decodeModelJSON(file)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{
		Source: Source{
			ID:   "model-json",
			Name: "Model JSON",
			URL:  "file://" + path,
		},
		Entries: entries,
	}, nil
}
