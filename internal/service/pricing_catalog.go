package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultLiteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	modelPriceSourceLiteLLM  = "litellm"
	maxPricingCatalogBytes   = 20 << 20
)

type PricingCatalogFetcher interface {
	FetchPricingCatalog(context.Context) (map[string]liteLLMPricingCatalogPrice, error)
	SourceURL() string
	SourceName() string
}

type liteLLMPricingCatalogPrice struct {
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CachePricePer1M      float64
}

type liteLLMPricingCatalogFetcher struct {
	url    string
	client *http.Client
}

type liteLLMModelPriceEntry struct {
	InputCostPerToken      *float64 `json:"input_cost_per_token"`
	OutputCostPerToken     *float64 `json:"output_cost_per_token"`
	CacheReadCostPerToken  *float64 `json:"cache_read_input_token_cost"`
	LiteLLMProvider        string   `json:"litellm_provider"`
	Mode                   string   `json:"mode"`
	SupportsPromptCaching  *bool    `json:"supports_prompt_caching"`
	InputCostPerTokenBatch *float64 `json:"input_cost_per_token_batches"`
}

func newDefaultLiteLLMPricingCatalogFetcher() PricingCatalogFetcher {
	return &liteLLMPricingCatalogFetcher{
		url:    defaultLiteLLMPricingURL,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (f *liteLLMPricingCatalogFetcher) SourceURL() string {
	if f == nil || strings.TrimSpace(f.url) == "" {
		return defaultLiteLLMPricingURL
	}
	return strings.TrimSpace(f.url)
}

func (f *liteLLMPricingCatalogFetcher) SourceName() string {
	return modelPriceSourceLiteLLM
}

func (f *liteLLMPricingCatalogFetcher) FetchPricingCatalog(ctx context.Context) (map[string]liteLLMPricingCatalogPrice, error) {
	if f == nil {
		return nil, fmt.Errorf("pricing catalog fetcher is nil")
	}
	client := f.client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.SourceURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("create LiteLLM pricing request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cpa-usage-keeper/pricing-sync")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch LiteLLM pricing catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch LiteLLM pricing catalog: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPricingCatalogBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read LiteLLM pricing catalog: %w", err)
	}
	if len(body) > maxPricingCatalogBytes {
		return nil, fmt.Errorf("LiteLLM pricing catalog exceeds %d bytes", maxPricingCatalogBytes)
	}
	return parseLiteLLMPricingCatalog(body)
}

func parseLiteLLMPricingCatalog(body []byte) (map[string]liteLLMPricingCatalogPrice, error) {
	var raw map[string]liteLLMModelPriceEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse LiteLLM pricing catalog: %w", err)
	}
	result := make(map[string]liteLLMPricingCatalogPrice, len(raw))
	for model, entry := range raw {
		modelName := strings.TrimSpace(model)
		if modelName == "" || modelName == "sample_spec" {
			continue
		}
		price, ok := liteLLMEntryToPricing(entry)
		if !ok {
			continue
		}
		result[modelName] = price
	}
	return result, nil
}

func liteLLMEntryToPricing(entry liteLLMModelPriceEntry) (liteLLMPricingCatalogPrice, bool) {
	if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil {
		return liteLLMPricingCatalogPrice{}, false
	}
	inputCost := nonNegativeFloat(entry.InputCostPerToken)
	outputCost := nonNegativeFloat(entry.OutputCostPerToken)
	cacheCost := inputCost
	if entry.CacheReadCostPerToken != nil {
		cacheCost = nonNegativeFloat(entry.CacheReadCostPerToken)
	}
	return liteLLMPricingCatalogPrice{
		PromptPricePer1M:     inputCost * 1_000_000,
		CompletionPricePer1M: outputCost * 1_000_000,
		CachePricePer1M:      cacheCost * 1_000_000,
	}, true
}

func nonNegativeFloat(value *float64) float64 {
	if value == nil || *value < 0 {
		return 0
	}
	return *value
}
