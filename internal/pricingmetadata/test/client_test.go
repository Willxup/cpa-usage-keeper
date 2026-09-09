package test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/pricingmetadata"
)

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPriceSourcesDecodeEquivalentBasePrices(t *testing.T) {
	cases := []struct{ source, url, body string }{
		{"", "https://models.dev/api.json", `{"anthropic":{"name":"Anthropic","models":{"claude-sonnet":{"cost":{"input":3,"output":15,"cache_read":0.3,"cache_write":3.75}}}}}`},
		{"litellm", "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json", `{
			"claude-sonnet":{"litellm_provider":"anthropic","mode":"chat","input_cost_per_token":0.000003,"output_cost_per_token":0.000015,"cache_read_input_token_cost":0.0000003,"cache_creation_input_token_cost":0.00000375,"input_cost_per_token_above_200k_tokens":0.000006},
			"image":{"mode":"image_generation","input_cost_per_token":1,"output_cost_per_token":2},
			"incomplete":{"mode":"chat","input_cost_per_token":1},
			"sample_spec":{"mode":"documentation"}
		}`},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			client := pricingmetadata.NewClient(&http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != tc.url {
					t.Errorf("unexpected URL: %s", r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			catalog, err := client.Fetch(context.Background(), tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.Entries) != 1 {
				t.Fatalf("unexpected entries: %+v", catalog.Entries)
			}
			entry := catalog.Entries[0]
			if entry.ProviderID != "anthropic" || entry.Model.ID != "claude-sonnet" {
				t.Fatalf("unexpected identity: %+v", entry)
			}
			for name, pair := range map[string][2]float64{
				"input": {*entry.Model.Cost.Input, 3}, "output": {*entry.Model.Cost.Output, 15},
				"read": {*entry.Model.Cost.CacheRead, 0.3}, "write": {*entry.Model.Cost.CacheWrite, 3.75},
			} {
				if math.Abs(pair[0]-pair[1]) > 1e-10 {
					t.Errorf("%s price: %v", name, pair)
				}
			}
		})
	}
}

func TestLiteLLMNormalizesProvidersAndPreservesMissingAndZeroPrices(t *testing.T) {
	client := pricingmetadata.NewClient(&http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{
			"gemini/free":{"litellm_provider":"gemini","mode":"chat","input_cost_per_token":0,"output_cost_per_token":0},
			"bedrock/model-v1:0":{"litellm_provider":"bedrock","mode":"chat","input_cost_per_token":0.000001,"output_cost_per_token":0.000002}
		}`))}, nil
	})})
	catalog, err := client.Fetch(context.Background(), "litellm")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Entries) != 2 {
		t.Fatalf("unexpected entries: %+v", catalog.Entries)
	}
	if catalog.Entries[0].ProviderID != "amazon-bedrock" || catalog.Entries[0].Model.ID != "bedrock/model-v1:0" {
		t.Fatalf("lost provider/version: %+v", catalog.Entries[0])
	}
	free := catalog.Entries[1]
	if free.ProviderID != "google" || free.Model.Cost.Input == nil || *free.Model.Cost.Input != 0 || free.Model.Cost.CacheWrite != nil {
		t.Fatalf("unexpected free/missing values: %+v", free)
	}
}

func TestPriceSourceRejectsUnknownSourceBeforeFetching(t *testing.T) {
	client := pricingmetadata.NewClient(&http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid source must not make a request")
		return nil, nil
	})})
	_, err := client.Fetch(context.Background(), "https://example.com/prices")
	if !errors.Is(err, pricingmetadata.ErrInvalidSource) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPriceSourcePreservesTimeoutCause(t *testing.T) {
	client := pricingmetadata.NewClient(&http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })})
	_, err := client.Fetch(context.Background(), "litellm")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost timeout cause: %v", err)
	}
}
