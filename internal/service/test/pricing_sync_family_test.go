package test

import (
	"math"
	"strings"
	"testing"
)

func TestPricingSyncRecognizesOfficialModelFamilies(t *testing.T) {
	// 来源目录中的厂商品牌与模型系列名称不同，也必须选到原厂报价。
	for _, source := range []string{"models-dev", "litellm"} {
		for _, model := range []string{
			"devstral-small-2505", "codestral-2508", "magistral-small-2506", "ministral-3b-2512",
			"pixtral-12b-2409", "voxtral-small-2507", "open-mistral-7b", "open-mixtral-8x7b",
			"open-codestral-mamba", "labs-devstral-small-2512", "labs-leanstral-1-5", "command-r-plus",
		} {
			t.Run(source+"/"+model, func(t *testing.T) {
				provider, wantProvider := "mistral", "mistral"
				if strings.HasPrefix(model, "command") {
					provider, wantProvider = "cohere", "cohere"
					if source == "litellm" {
						provider = "cohere_chat"
					}
				}
				catalog := reviewOfficialPriceCatalog(source, provider, model, "fireworks_ai", model, "0.1", "0.3", "null")
				preview := previewReviewCatalog(t, source, catalog, model, "custom/"+strings.ReplaceAll(model, "-", "_"))
				if len(preview.Matches) != 2 {
					t.Fatalf("unexpected preview: %+v", preview)
				}
				for _, match := range preview.Matches {
					if match.SourceProviderID != wantProvider || math.Abs(match.PromptPricePer1M-0.1) > 1e-10 || math.Abs(match.CompletionPricePer1M-0.3) > 1e-10 {
						t.Errorf("expected %s pricing: %+v", wantProvider, match)
					}
				}
			})
		}
	}
}

func TestPricingSyncNewOfficialFamiliesStillAllowFallback(t *testing.T) {
	for _, source := range []string{"models-dev", "litellm"} {
		for _, input := range []string{"null", "-1", "0"} {
			t.Run(source+"/"+input, func(t *testing.T) {
				catalog := reviewOfficialPriceCatalog(source, "mistral", "devstral-small-2505", "fireworks_ai", "devstral-small-2505", input, input, "null")
				preview := previewReviewCatalog(t, source, catalog, "custom/devstral-small-2505")
				want := "fireworks_ai"
				if input == "0" {
					want = "mistral"
				}
				if len(preview.Matches) != 1 || preview.Matches[0].SourceProviderID != want {
					t.Fatalf("expected %s pricing: %+v", want, preview)
				}
			})
		}
	}
}
