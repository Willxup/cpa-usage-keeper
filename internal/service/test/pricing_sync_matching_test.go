package test

import (
	"context"
	"net/http"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service"
)

func TestPricingSyncPreservesModelVersionSuffixes(t *testing.T) {
	transport := http.DefaultTransport
	http.DefaultTransport = pricingCatalogTransport{body: `{
		"amazon-bedrock": {"models": {
			"global.anthropic.claude-haiku-v1:0": {"cost": {"input": 1, "output": 5}},
			"global.anthropic.claude-sonnet-v1:0": {"cost": {"input": 3, "output": 15}}
		}}
	}`}
	t.Cleanup(func() { http.DefaultTransport = transport })

	cases := map[string]string{
		"global.anthropic.claude-haiku-v1:0":                 "global.anthropic.claude-haiku-v1:0",
		"global.anthropic.claude-sonnet-v1:0":                "global.anthropic.claude-sonnet-v1:0",
		"custom/global.anthropic.claude-sonnet-v1:0":         "global.anthropic.claude-sonnet-v1:0",
		"custom:global.anthropic.claude-sonnet-v1:0":         "global.anthropic.claude-sonnet-v1:0",
		"custom:bedrock/global.anthropic.claude-sonnet-v1:0": "global.anthropic.claude-sonnet-v1:0",
	}
	modelList := make([]models.ModelInfo, 0, len(cases))
	for model := range cases {
		modelList = append(modelList, models.ModelInfo{ID: model})
	}
	provider := service.NewPricingService(openPricingServiceTestDatabase(t), emptyPricingCatalogForTest(),
		stubModelsFetcher{result: &response.ModelsResult{Payload: models.ModelsResponse{Data: modelList}}})
	preview, err := provider.PreviewPricingSync(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Matches) != len(cases) {
		t.Fatalf("expected all versioned models to match, got %+v", preview)
	}
	for _, match := range preview.Matches {
		if match.MatchedModel != cases[match.Model] {
			t.Errorf("%s matched %s, want %s", match.Model, match.MatchedModel, cases[match.Model])
		}
	}
}
