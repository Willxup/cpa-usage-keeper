package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ModelPricing records the resolved model pricing and category
type ModelPricing struct {
	PromptPricePer1M     float64
	CompletionPricePer1M float64
	CacheReadPricePer1M  float64
	CacheWritePricePer1M float64
	Category             string
}

// ResolveModelPrice returns (Prompt, Completion, CacheRead) per 1M tokens
func ResolveModelPrice(model string) (float64, float64, float64) {
	p := ResolveModelPricing(model)
	return p.PromptPricePer1M, p.CompletionPricePer1M, p.CacheReadPricePer1M
}

// ResolveModelPricing resolves pricing rules based on model name
func ResolveModelPricing(model string) ModelPricing {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" || m == "unknown" {
		return ModelPricing{PromptPricePer1M: 0, CompletionPricePer1M: 0, CacheReadPricePer1M: 0, Category: "Unknown/Ignored"}
	}

	// 1. Free and special models (:free, -free, auto-review)
	if isTTSModel(m) {
		return ModelPricing{PromptPricePer1M: 0.0, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Unpriced/TTS"}
	}
	if isGPTImageModel(m) {
		return ModelPricing{PromptPricePer1M: 0.0, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Unpriced/GPT-Image"}
	}
	if isDistillModel(m) {
		return ModelPricing{PromptPricePer1M: 0.0, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Unpriced/Distill"}
	}
	if isGPT4oRealtimeAudioModel(m) {
		return ModelPricing{PromptPricePer1M: 0.0, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Unpriced/Realtime-Audio"}
	}
	if strings.Contains(m, ":free") ||
		strings.Contains(m, "-free") ||
		strings.Contains(m, "auto-review") ||
		strings.Contains(m, "moderation") {
		return ModelPricing{PromptPricePer1M: 0.0, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Free/Special"}
	}

	// 2. Embeddings (prioritized before provider-specific chat matching, e.g. qwen/qwen3-embedding-8b)
	if isEmbeddingModel(m) {
		// Gemini / Google embeddings ($0.15/1M input, $0.0 completion, $0.0 cache)
		if strings.Contains(m, "gemini") || strings.Contains(m, "embedding-001") || strings.Contains(m, "text-embedding-004") {
			return ModelPricing{PromptPricePer1M: 0.15, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Gemini Embedding Official"}
		}
		// Voyage 3.5 ($0.06/1M input, $0.0 completion, $0.0 cache)
		if strings.Contains(m, "voyage-3.5") || strings.Contains(m, "voyage-3-5") || strings.Contains(m, "voyage-3_5") {
			return ModelPricing{PromptPricePer1M: 0.06, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Voyage 3.5 Official"}
		}
		if strings.Contains(m, "3-small") || strings.Contains(m, "3_small") || strings.Contains(m, "small") {
			return ModelPricing{PromptPricePer1M: 0.02, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Embedding 3 Small Official"}
		}
		if strings.Contains(m, "3-large") || strings.Contains(m, "3_large") || strings.Contains(m, "large") {
			return ModelPricing{PromptPricePer1M: 0.13, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Embedding 3 Large Official"}
		}
		if strings.Contains(m, "ada-002") || strings.Contains(m, "ada") {
			return ModelPricing{PromptPricePer1M: 0.10, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Embedding Ada Official"}
		}
		return ModelPricing{PromptPricePer1M: 0.02, CompletionPricePer1M: 0.0, CacheReadPricePer1M: 0.0, Category: "Embedding Fallback"}
	}

	// 3. Official pricing alignment
	// OpenAI
	if pricing, ok := resolveOpenAIPricing(m); ok {
		return pricing
	}

	// DeepSeek
	if strings.Contains(m, "deepseek") && !strings.Contains(m, "distill") {
		if (strings.Contains(m, "reasoner") || strings.Contains(m, "r1")) && !strings.Contains(m, "distill") {
			return ModelPricing{PromptPricePer1M: 0.55, CompletionPricePer1M: 2.19, CacheReadPricePer1M: 0.14, Category: "DeepSeek Reasoner Official"}
		}
		return ModelPricing{PromptPricePer1M: 0.147, CompletionPricePer1M: 0.147, CacheReadPricePer1M: 0.0147, Category: "DeepSeek Official"}
	}

	// Qwen
	if strings.Contains(m, "qwen") && !strings.Contains(m, "distill") {
		if strings.Contains(m, "coder-plus") {
			return ModelPricing{PromptPricePer1M: 1.0, CompletionPricePer1M: 5.0, CacheReadPricePer1M: 0.20, Category: "Qwen Coder Plus Official"}
		}
		if strings.Contains(m, "max") {
			return ModelPricing{PromptPricePer1M: 2.4, CompletionPricePer1M: 9.6, CacheReadPricePer1M: 0.24, Category: "Qwen Max Official"}
		}
		return ModelPricing{PromptPricePer1M: 0.147, CompletionPricePer1M: 0.147, CacheReadPricePer1M: 0.0, Category: "Qwen Standard Official"}
	}

	// Claude
	if isClaudeModel(m) {
		if strings.Contains(m, "haiku") {
			if isClaude35Haiku(m) {
				return ModelPricing{PromptPricePer1M: 0.8, CompletionPricePer1M: 4.0, CacheReadPricePer1M: 0.08, Category: "Claude 3.5 Haiku Official"}
			}
			if isClaude3Haiku(m) {
				return ModelPricing{PromptPricePer1M: 0.25, CompletionPricePer1M: 1.25, CacheReadPricePer1M: 0.025, Category: "Claude 3 Haiku Official"}
			}
			return ModelPricing{PromptPricePer1M: 1.0, CompletionPricePer1M: 5.0, CacheReadPricePer1M: 0.1, Category: "Claude Haiku Official"}
		}
		if strings.Contains(m, "instant") {
			return ModelPricing{PromptPricePer1M: 0.80, CompletionPricePer1M: 2.40, CacheReadPricePer1M: 0.08, Category: "Claude Instant Official"}
		}
		if strings.Contains(m, "sonnet") {
			return ModelPricing{PromptPricePer1M: 3.0, CompletionPricePer1M: 15.0, CacheReadPricePer1M: 0.3, Category: "Claude Sonnet Official"}
		}
		if strings.Contains(m, "opus") {
			if isModernOpus(m) {
				return ModelPricing{PromptPricePer1M: 5.0, CompletionPricePer1M: 25.0, CacheReadPricePer1M: 0.5, Category: "Claude Opus 4.5 Official"}
			}
			return ModelPricing{PromptPricePer1M: 15.0, CompletionPricePer1M: 75.0, CacheReadPricePer1M: 1.5, Category: "Claude Opus Official"}
		}
		if isClaudeLegacyModel(m) {
			return ModelPricing{PromptPricePer1M: 8.0, CompletionPricePer1M: 24.0, CacheReadPricePer1M: 0.8, Category: "Claude Legacy Official"}
		}
		return ModelPricing{PromptPricePer1M: 3.0, CompletionPricePer1M: 15.0, CacheReadPricePer1M: 0.3, Category: "Claude Sonnet Fallback"}
	}

	// Gemini / Agnes
	if strings.Contains(m, "gemini") || strings.Contains(m, "agnes") {
		if strings.Contains(m, "2.0-flash-lite") || strings.Contains(m, "2-0-flash-lite") || strings.Contains(m, "2_0_flash_lite") || strings.Contains(m, "2.0_flash_lite") || strings.Contains(m, "2-flash-lite") {
			return ModelPricing{PromptPricePer1M: 0.075, CompletionPricePer1M: 0.30, CacheReadPricePer1M: 0.01875, Category: "Gemini 2.0 Flash-Lite Official"}
		}
		if strings.Contains(m, "flash-lite") || strings.Contains(m, "extra-low") || strings.Contains(m, "flash-image") {
			return ModelPricing{PromptPricePer1M: 0.1, CompletionPricePer1M: 0.4, CacheReadPricePer1M: 0.025, Category: "Gemini Flash-Lite/Image Official"}
		}
		if strings.Contains(m, "flash-high") || strings.Contains(m, "pro-low") {
			return ModelPricing{PromptPricePer1M: 1.5, CompletionPricePer1M: 9.0, CacheReadPricePer1M: 0.15, Category: "Gemini Flash-High/Pro-Low Official"}
		}
		if strings.Contains(m, "3.1-pro") || strings.Contains(m, "pro-preview") {
			return ModelPricing{PromptPricePer1M: 2.0, CompletionPricePer1M: 12.0, CacheReadPricePer1M: 0.2, Category: "Gemini Pro-Preview Official"}
		}
		if strings.Contains(m, "1.5-pro") || strings.Contains(m, "1-5-pro") || strings.Contains(m, "1_5_pro") {
			return ModelPricing{PromptPricePer1M: 1.25, CompletionPricePer1M: 5.0, CacheReadPricePer1M: 0.3125, Category: "Gemini 1.5 Pro Official"}
		}
		if strings.Contains(m, "3.0-flash") || strings.Contains(m, "3-flash") || strings.Contains(m, "3_0_flash") || strings.Contains(m, "3_flash") {
			return ModelPricing{PromptPricePer1M: 0.50, CompletionPricePer1M: 3.00, CacheReadPricePer1M: 0.125, Category: "Gemini 3 Flash Official"}
		}
		if strings.Contains(m, "2.0-flash") || strings.Contains(m, "2-flash") || strings.Contains(m, "2_0_flash") || strings.Contains(m, "2.0_flash") {
			return ModelPricing{PromptPricePer1M: 0.10, CompletionPricePer1M: 0.40, CacheReadPricePer1M: 0.025, Category: "Gemini 2.0 Flash Official"}
		}
		if strings.Contains(m, "1.5-flash") || strings.Contains(m, "1-5-flash") || strings.Contains(m, "1_5_flash") {
			return ModelPricing{PromptPricePer1M: 0.075, CompletionPricePer1M: 0.30, CacheReadPricePer1M: 0.01875, Category: "Gemini 1.5 Flash Official"}
		}
		if strings.Contains(m, "flash") {
			return ModelPricing{PromptPricePer1M: 0.3, CompletionPricePer1M: 2.5, CacheReadPricePer1M: 0.075, Category: "Gemini Flash Official"}
		}
		if strings.Contains(m, "pro") || strings.Contains(m, "agent") {
			return ModelPricing{PromptPricePer1M: 1.25, CompletionPricePer1M: 10.0, CacheReadPricePer1M: 0.31, Category: "Gemini Pro/Agent Official"}
		}
		return ModelPricing{PromptPricePer1M: 0.3, CompletionPricePer1M: 2.5, CacheReadPricePer1M: 0.075, Category: "Gemini Flash Default"}
	}

	// GLM
	if strings.Contains(m, "glm") {
		if strings.Contains(m, "flash") {
			return ModelPricing{PromptPricePer1M: 0.1, CompletionPricePer1M: 0.1, CacheReadPricePer1M: 0.0, Category: "GLM Flash Official"}
		}
		return ModelPricing{PromptPricePer1M: 1.0, CompletionPricePer1M: 3.0, CacheReadPricePer1M: 0.2, Category: "GLM Standard Official"}
	}

	// Kimi / Moonshot
	if strings.Contains(m, "kimi") || strings.Contains(m, "moonshot") || strings.HasPrefix(m, "k3") || strings.Contains(m, "/k3") {
		return ModelPricing{PromptPricePer1M: 0.95, CompletionPricePer1M: 4.0, CacheReadPricePer1M: 0.16, Category: "Kimi Official"}
	}

	// MiniMax
	if strings.Contains(m, "minimax") {
		return ModelPricing{PromptPricePer1M: 1.0, CompletionPricePer1M: 3.0, CacheReadPricePer1M: 0.2, Category: "MiniMax Official"}
	}

	// SenseNova / StepFun
	if strings.Contains(m, "sensenova") || isStepFunModel(m) {
		return ModelPricing{PromptPricePer1M: 0.2, CompletionPricePer1M: 0.5, CacheReadPricePer1M: 0.02, Category: "SenseNova/StepFun Official"}
	}

	// Hunyuan
	if strings.Contains(m, "hy3") || strings.Contains(m, "hunyuan") {
		return ModelPricing{PromptPricePer1M: 1.0, CompletionPricePer1M: 2.0, CacheReadPricePer1M: 0.1, Category: "Hunyuan Official"}
	}

	// Yi (01-ai)
	if strings.Contains(m, "yi-") || strings.Contains(m, "01-ai") {
		return ModelPricing{PromptPricePer1M: 2.8, CompletionPricePer1M: 2.8, CacheReadPricePer1M: 0.0, Category: "Yi Official"}
	}

	// Mistral / Codestral
	if strings.Contains(m, "mistral") || strings.Contains(m, "codestral") || strings.Contains(m, "ministral") {
		if strings.Contains(m, "large") {
			return ModelPricing{PromptPricePer1M: 2.0, CompletionPricePer1M: 6.0, CacheReadPricePer1M: 0.0, Category: "Mistral Large Official"}
		}
		if strings.Contains(m, "codestral") {
			return ModelPricing{PromptPricePer1M: 0.30, CompletionPricePer1M: 0.90, CacheReadPricePer1M: 0.0, Category: "Codestral Official"}
		}
		if strings.Contains(m, "ministral-3b") {
			return ModelPricing{PromptPricePer1M: 0.04, CompletionPricePer1M: 0.04, CacheReadPricePer1M: 0.0, Category: "Ministral 3B Official"}
		}
		if strings.Contains(m, "ministral-8b") || strings.Contains(m, "nemo") {
			return ModelPricing{PromptPricePer1M: 0.10, CompletionPricePer1M: 0.10, CacheReadPricePer1M: 0.0, Category: "Mistral Nemo/8B Official"}
		}
		if strings.Contains(m, "small") {
			return ModelPricing{PromptPricePer1M: 0.10, CompletionPricePer1M: 0.30, CacheReadPricePer1M: 0.0, Category: "Mistral Small Official"}
		}
		return ModelPricing{PromptPricePer1M: 0.10, CompletionPricePer1M: 0.30, CacheReadPricePer1M: 0.0, Category: "Mistral Official"}
	}

	// 4. Fallback to GPT tier baseline
	// Mini
	if isMiniModel(m) {
		return ModelPricing{PromptPricePer1M: 0.75, CompletionPricePer1M: 4.5, CacheReadPricePer1M: 0.075, Category: "GPT Mini Baseline"}
	}

	// Flagship
	if strings.Contains(m, "-ultra") ||
		strings.Contains(m, "-max") ||
		strings.Contains(m, "340b") ||
		strings.Contains(m, "550b") ||
		strings.Contains(m, "gpt-5.5") ||
		strings.Contains(m, "gpt-6") ||
		strings.Contains(m, "o3") {
		return ModelPricing{PromptPricePer1M: 5.0, CompletionPricePer1M: 30.0, CacheReadPricePer1M: 0.5, Category: "GPT Flagship Baseline"}
	}

	// Standard
	return ModelPricing{PromptPricePer1M: 1.75, CompletionPricePer1M: 14.0, CacheReadPricePer1M: 0.175, Category: "GPT Standard Baseline"}
}

func resolveOpenAIPricing(m string) (ModelPricing, bool) {
	mCore := m
	if idx := strings.LastIndex(m, "/"); idx != -1 {
		mCore = m[idx+1:]
	}
	if strings.HasPrefix(mCore, "openai:") {
		mCore = strings.TrimPrefix(mCore, "openai:")
	}

	// 0. OpenAI Fine-tuned models: ft:<base_model>:<org>:<suffix>:<id> or similar
	if strings.HasPrefix(mCore, "ft:") {
		ftRest := strings.TrimPrefix(mCore, "ft:")
		baseModel := ftRest
		if idx := strings.Index(ftRest, ":"); idx != -1 {
			baseModel = ftRest[:idx]
		}
		if pricing, ok := resolveOpenAIFineTunedPricing(baseModel); ok {
			return pricing, true
		}
	}

	// Realtime & audio models (gpt-4o-mini-realtime & gpt-4o-realtime)
	if strings.Contains(mCore, "gpt-4o-mini-realtime") || strings.Contains(mCore, "gpt-4o-mini-audio") {
		return ModelPricing{PromptPricePer1M: 0.60, CompletionPricePer1M: 2.40, CacheReadPricePer1M: 0.30, Category: "OpenAI GPT-4o-mini Realtime Official"}, true
	}

	// GPT-5 family
	if isGPT5Pro(mCore) {
		return ModelPricing{PromptPricePer1M: 15.00, CompletionPricePer1M: 120.00, CacheReadPricePer1M: 1.50, Category: "OpenAI GPT-5 Pro Official"}, true
	}
	if isGPT5Nano(mCore) {
		return ModelPricing{PromptPricePer1M: 0.05, CompletionPricePer1M: 0.40, CacheReadPricePer1M: 0.005, Category: "OpenAI GPT-5-nano Official"}, true
	}
	if isGPT5Mini(mCore) {
		return ModelPricing{PromptPricePer1M: 0.25, CompletionPricePer1M: 2.00, CacheReadPricePer1M: 0.025, Category: "OpenAI GPT-5-mini Official"}, true
	}
	if isGPT5CodexMini(mCore) {
		return ModelPricing{PromptPricePer1M: 0.25, CompletionPricePer1M: 2.00, CacheReadPricePer1M: 0.025, Category: "OpenAI GPT-5-mini Official"}, true
	}
	if strings.Contains(mCore, "codex-mini") {
		return ModelPricing{PromptPricePer1M: 1.50, CompletionPricePer1M: 6.00, CacheReadPricePer1M: 0.375, Category: "OpenAI Codex Mini Official"}, true
	}
	if isGPT5Standard(mCore) {
		return ModelPricing{PromptPricePer1M: 1.25, CompletionPricePer1M: 10.00, CacheReadPricePer1M: 0.125, Category: "OpenAI GPT-5 Official"}, true
	}

	// 1. gpt-4o-mini
	if mCore == "gpt-4o-mini" || strings.HasPrefix(mCore, "gpt-4o-mini-") || strings.HasPrefix(mCore, "gpt-4o-mini:") || strings.HasPrefix(mCore, "gpt-4o-mini_") {
		return ModelPricing{PromptPricePer1M: 0.15, CompletionPricePer1M: 0.6, CacheReadPricePer1M: 0.075, Category: "OpenAI GPT-4o-mini Official"}, true
	}

	// 2. gpt-4o-2024-05-13 older snapshot tier ($5.0 / $15.0 / $2.5)
	if strings.Contains(mCore, "gpt-4o-2024-05-13") {
		return ModelPricing{PromptPricePer1M: 5.0, CompletionPricePer1M: 15.0, CacheReadPricePer1M: 2.5, Category: "OpenAI GPT-4o-2024-05-13 Official"}, true
	}

	// 2. chatgpt-4o-latest ($5.0 / $15.0 / $2.5)
	if strings.Contains(mCore, "chatgpt-4o-latest") {
		return ModelPricing{PromptPricePer1M: 5.0, CompletionPricePer1M: 15.0, CacheReadPricePer1M: 2.5, Category: "OpenAI ChatGPT-4o-latest Official"}, true
	}

	// 2. gpt-4o
	if mCore == "gpt-4o" || strings.HasPrefix(mCore, "gpt-4o-") || strings.HasPrefix(mCore, "gpt-4o:") || strings.HasPrefix(mCore, "gpt-4o_") {
		return ModelPricing{PromptPricePer1M: 2.5, CompletionPricePer1M: 10.0, CacheReadPricePer1M: 1.25, Category: "OpenAI GPT-4o Official"}, true
	}

	// 3. gpt-4.1 variants (nano, mini, standard)
	if mCore == "gpt-4.1-nano" || strings.HasPrefix(mCore, "gpt-4.1-nano-") || strings.HasPrefix(mCore, "gpt-4.1-nano:") || strings.HasPrefix(mCore, "gpt-4.1-nano_") {
		return ModelPricing{PromptPricePer1M: 0.1, CompletionPricePer1M: 0.4, CacheReadPricePer1M: 0.025, Category: "OpenAI GPT-4.1-nano Official"}, true
	}
	if mCore == "gpt-4.1-mini" || strings.HasPrefix(mCore, "gpt-4.1-mini-") || strings.HasPrefix(mCore, "gpt-4.1-mini:") || strings.HasPrefix(mCore, "gpt-4.1-mini_") {
		return ModelPricing{PromptPricePer1M: 0.4, CompletionPricePer1M: 1.6, CacheReadPricePer1M: 0.1, Category: "OpenAI GPT-4.1-mini Official"}, true
	}
	if mCore == "gpt-4.1" || strings.HasPrefix(mCore, "gpt-4.1-") || strings.HasPrefix(mCore, "gpt-4.1:") || strings.HasPrefix(mCore, "gpt-4.1_") {
		return ModelPricing{PromptPricePer1M: 2.0, CompletionPricePer1M: 8.0, CacheReadPricePer1M: 0.5, Category: "OpenAI GPT-4.1 Official"}, true
	}

	// 4. gpt-4.5
	if mCore == "gpt-4.5" || strings.HasPrefix(mCore, "gpt-4.5-") || strings.HasPrefix(mCore, "gpt-4.5:") || strings.HasPrefix(mCore, "gpt-4.5_") {
		return ModelPricing{PromptPricePer1M: 75.0, CompletionPricePer1M: 150.0, CacheReadPricePer1M: 37.5, Category: "OpenAI GPT-4.5 Official"}, true
	}

	// 4. gpt-4-turbo & preview dates (including gpt-4-vision-preview)
	if strings.Contains(mCore, "gpt-4-turbo") ||
		strings.Contains(mCore, "gpt-4-vision") ||
		strings.HasPrefix(mCore, "gpt-4-0125") ||
		strings.HasPrefix(mCore, "gpt-4-1106") {
		return ModelPricing{PromptPricePer1M: 10.0, CompletionPricePer1M: 30.0, CacheReadPricePer1M: 5.0, Category: "OpenAI GPT-4 Turbo Official"}, true
	}

	// 5. gpt-4-32k legacy ($60.0 / $120.0 / $0.0)
	if strings.Contains(mCore, "gpt-4-32k") || strings.Contains(mCore, "gpt-4_32k") || strings.Contains(mCore, "gpt-4:32k") {
		return ModelPricing{PromptPricePer1M: 60.0, CompletionPricePer1M: 120.0, CacheReadPricePer1M: 0.0, Category: "OpenAI GPT-4 32K Official"}, true
	}

	// 5. gpt-4 legacy (gpt-4, gpt-4-32k, gpt-4-0613, gpt-4-0314)
	if mCore == "gpt-4" || strings.HasPrefix(mCore, "gpt-4-") || strings.HasPrefix(mCore, "gpt-4:") || strings.HasPrefix(mCore, "gpt-4_") {
		return ModelPricing{PromptPricePer1M: 30.0, CompletionPricePer1M: 60.0, CacheReadPricePer1M: 0.0, Category: "OpenAI GPT-4 Legacy Official"}, true
	}

	// 6. gpt-3.5-turbo and snapshots
	if strings.Contains(mCore, "gpt-3.5") || strings.Contains(mCore, "gpt-35") {
		if strings.Contains(mCore, "instruct") {
			return ModelPricing{PromptPricePer1M: 1.5, CompletionPricePer1M: 2.0, CacheReadPricePer1M: 0.0, Category: "OpenAI GPT-3.5 Turbo Instruct Official"}, true
		}
		if strings.Contains(mCore, "16k") {
			return ModelPricing{PromptPricePer1M: 3.0, CompletionPricePer1M: 4.0, CacheReadPricePer1M: 0.0, Category: "OpenAI GPT-3.5 Turbo 16K Official"}, true
		}
		if strings.Contains(mCore, "1106") {
			return ModelPricing{PromptPricePer1M: 1.0, CompletionPricePer1M: 2.0, CacheReadPricePer1M: 0.0, Category: "OpenAI GPT-3.5 Turbo 1106 Official"}, true
		}
		if strings.Contains(mCore, "0613") || strings.Contains(mCore, "0301") {
			return ModelPricing{PromptPricePer1M: 1.5, CompletionPricePer1M: 2.0, CacheReadPricePer1M: 0.0, Category: "OpenAI GPT-3.5 Turbo Legacy Official"}, true
		}
		return ModelPricing{PromptPricePer1M: 0.5, CompletionPricePer1M: 1.5, CacheReadPricePer1M: 0.0, Category: "OpenAI GPT-3.5 Turbo Official"}, true
	}

	// 7. Reasoning models: o1, o3, o4
	if strings.Contains(mCore, "o3-deep-research") {
		return ModelPricing{PromptPricePer1M: 10.0, CompletionPricePer1M: 40.0, CacheReadPricePer1M: 2.5, Category: "OpenAI o3 Deep Research Official"}, true
	}
	if strings.Contains(mCore, "o4-mini-deep-research") {
		return ModelPricing{PromptPricePer1M: 2.0, CompletionPricePer1M: 8.0, CacheReadPricePer1M: 0.5, Category: "OpenAI o4-mini Deep Research Official"}, true
	}
	if mCore == "o1-mini" || strings.HasPrefix(mCore, "o1-mini-") || strings.HasPrefix(mCore, "o1-mini:") || strings.HasPrefix(mCore, "o1-mini_") {
		return ModelPricing{PromptPricePer1M: 1.1, CompletionPricePer1M: 4.4, CacheReadPricePer1M: 0.55, Category: "OpenAI o1-mini Official"}, true
	}
	if mCore == "o1-pro" || strings.HasPrefix(mCore, "o1-pro-") || strings.HasPrefix(mCore, "o1-pro:") || strings.HasPrefix(mCore, "o1-pro_") {
		return ModelPricing{PromptPricePer1M: 150.0, CompletionPricePer1M: 600.0, CacheReadPricePer1M: 75.0, Category: "OpenAI o1-pro Official"}, true
	}
	if mCore == "o1" || strings.HasPrefix(mCore, "o1-") || strings.HasPrefix(mCore, "o1:") || strings.HasPrefix(mCore, "o1_") {
		return ModelPricing{PromptPricePer1M: 15.0, CompletionPricePer1M: 60.0, CacheReadPricePer1M: 7.5, Category: "OpenAI o1 Official"}, true
	}
	if mCore == "o3-mini" || strings.HasPrefix(mCore, "o3-mini-") || strings.HasPrefix(mCore, "o3-mini:") || strings.HasPrefix(mCore, "o3-mini_") {
		return ModelPricing{PromptPricePer1M: 1.1, CompletionPricePer1M: 4.4, CacheReadPricePer1M: 0.55, Category: "OpenAI o3-mini Official"}, true
	}
	if mCore == "o3-pro" || strings.HasPrefix(mCore, "o3-pro-") || strings.HasPrefix(mCore, "o3-pro:") || strings.HasPrefix(mCore, "o3-pro_") {
		return ModelPricing{PromptPricePer1M: 20.0, CompletionPricePer1M: 80.0, CacheReadPricePer1M: 5.0, Category: "OpenAI o3-pro Official"}, true
	}
	if mCore == "o3" || strings.HasPrefix(mCore, "o3-") || strings.HasPrefix(mCore, "o3:") || strings.HasPrefix(mCore, "o3_") {
		return ModelPricing{PromptPricePer1M: 2.0, CompletionPricePer1M: 8.0, CacheReadPricePer1M: 0.5, Category: "OpenAI o3 Official"}, true
	}
	if mCore == "o4-mini" || strings.HasPrefix(mCore, "o4-mini-") || strings.HasPrefix(mCore, "o4-mini:") || strings.HasPrefix(mCore, "o4-mini_") {
		return ModelPricing{PromptPricePer1M: 1.1, CompletionPricePer1M: 4.4, CacheReadPricePer1M: 0.275, Category: "OpenAI o4-mini Official"}, true
	}

	// 8. Base models: babbage-002, davinci-002
	if strings.Contains(mCore, "babbage-002") {
		return ModelPricing{PromptPricePer1M: 0.40, CompletionPricePer1M: 0.40, CacheReadPricePer1M: 0.0, Category: "OpenAI Base Babbage-002 Official"}, true
	}
	if strings.Contains(mCore, "davinci-002") {
		return ModelPricing{PromptPricePer1M: 2.00, CompletionPricePer1M: 2.00, CacheReadPricePer1M: 0.0, Category: "OpenAI Base Davinci-002 Official"}, true
	}

	return ModelPricing{}, false
}

func resolveOpenAIFineTunedPricing(base string) (ModelPricing, bool) {
	b := strings.ToLower(strings.TrimSpace(base))
	// 1. gpt-4o-mini fine-tuned: $0.30 / $1.20 / $0.15
	if strings.Contains(b, "gpt-4o-mini") {
		return ModelPricing{PromptPricePer1M: 0.30, CompletionPricePer1M: 1.20, CacheReadPricePer1M: 0.15, Category: "OpenAI Fine-Tuned GPT-4o-mini Official"}, true
	}
	// 2. gpt-4o fine-tuned: $3.75 / $15.00 / $1.875
	if strings.Contains(b, "gpt-4o") {
		return ModelPricing{PromptPricePer1M: 3.75, CompletionPricePer1M: 15.00, CacheReadPricePer1M: 1.875, Category: "OpenAI Fine-Tuned GPT-4o Official"}, true
	}
	// 3. gpt-4.1 fine-tuned
	if strings.Contains(b, "gpt-4.1-nano") {
		return ModelPricing{PromptPricePer1M: 0.20, CompletionPricePer1M: 0.80, CacheReadPricePer1M: 0.05, Category: "OpenAI Fine-Tuned GPT-4.1-nano Official"}, true
	}
	if strings.Contains(b, "gpt-4.1-mini") {
		return ModelPricing{PromptPricePer1M: 0.80, CompletionPricePer1M: 3.20, CacheReadPricePer1M: 0.20, Category: "OpenAI Fine-Tuned GPT-4.1-mini Official"}, true
	}
	if strings.Contains(b, "gpt-4.1") {
		return ModelPricing{PromptPricePer1M: 3.00, CompletionPricePer1M: 12.00, CacheReadPricePer1M: 0.75, Category: "OpenAI Fine-Tuned GPT-4.1 Official"}, true
	}
	// GPT-5 fine-tuned
	if strings.Contains(b, "gpt-5-nano") || strings.Contains(b, "gpt-5.1-nano") {
		return ModelPricing{PromptPricePer1M: 0.10, CompletionPricePer1M: 0.80, CacheReadPricePer1M: 0.01, Category: "OpenAI Fine-Tuned GPT-5-nano Official"}, true
	}
	if strings.Contains(b, "gpt-5-mini") || strings.Contains(b, "gpt-5.1-mini") {
		return ModelPricing{PromptPricePer1M: 0.50, CompletionPricePer1M: 4.00, CacheReadPricePer1M: 0.05, Category: "OpenAI Fine-Tuned GPT-5-mini Official"}, true
	}
	if strings.Contains(b, "gpt-5") || strings.Contains(b, "gpt-5.1") {
		return ModelPricing{PromptPricePer1M: 2.50, CompletionPricePer1M: 20.00, CacheReadPricePer1M: 0.25, Category: "OpenAI Fine-Tuned GPT-5 Official"}, true
	}
	// 4. gpt-3.5-turbo fine-tuned: $3.00 / $6.00 / $0.0
	if strings.Contains(b, "gpt-3.5") || strings.Contains(b, "gpt-35") {
		return ModelPricing{PromptPricePer1M: 3.00, CompletionPricePer1M: 6.00, CacheReadPricePer1M: 0.0, Category: "OpenAI Fine-Tuned GPT-3.5 Turbo Official"}, true
	}
	// 5. babbage-002 & davinci-002
	if strings.Contains(b, "babbage-002") {
		return ModelPricing{PromptPricePer1M: 1.60, CompletionPricePer1M: 1.60, CacheReadPricePer1M: 0.0, Category: "OpenAI Fine-Tuned Babbage-002 Official"}, true
	}
	if strings.Contains(b, "davinci-002") {
		return ModelPricing{PromptPricePer1M: 12.00, CompletionPricePer1M: 12.00, CacheReadPricePer1M: 0.0, Category: "OpenAI Fine-Tuned Davinci-002 Official"}, true
	}
	return ModelPricing{}, false
}

func isModernOpus(m string) bool {
	for _, marker := range []string{"4-5", "4.5", "4-6", "4.6", "4-7", "4.7"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

func isClaude35Haiku(m string) bool {
	for _, marker := range []string{"3-5", "3.5", "3_5"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

func isClaude3Haiku(m string) bool {
	if isClaude35Haiku(m) {
		return false
	}
	for _, marker := range []string{"3-haiku", "3.0-haiku", "3_haiku", "haiku-3", "haiku:3", "haiku/3", "haiku_3", "claude-3-haiku"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	if strings.Contains(m, "claude-3") || strings.Contains(m, "claude:3") || strings.Contains(m, "claude/3") {
		return true
	}
	return false
}

func isClaudeLegacyModel(m string) bool {
	for _, marker := range []string{"claude-1", "claude-2", "claude_1", "claude_2", "claude:1", "claude:2", "claude/1", "claude/2"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

func isGPT5Pro(m string) bool {
	return isGPTModelPrefix(m, "gpt-5-pro") || isGPTModelPrefix(m, "gpt-5.1-pro")
}

func isGPT5Nano(m string) bool {
	return isGPTModelPrefix(m, "gpt-5-nano") || isGPTModelPrefix(m, "gpt-5.1-nano")
}

func isGPT5Mini(m string) bool {
	return isGPTModelPrefix(m, "gpt-5-mini") || isGPTModelPrefix(m, "gpt-5.1-mini")
}

func isGPT5CodexMini(m string) bool {
	return isGPTModelPrefix(m, "gpt-5-codex-mini") || isGPTModelPrefix(m, "gpt-5.1-codex-mini")
}

func isGPT5Standard(m string) bool {
	return isGPTModelPrefix(m, "gpt-5") || isGPTModelPrefix(m, "gpt-5.1")
}

func isGPT4oRealtimeAudioModel(m string) bool {
	mCore := m
	if idx := strings.LastIndex(m, "/"); idx != -1 {
		mCore = m[idx+1:]
	}
	return strings.Contains(mCore, "gpt-4o-realtime") || strings.Contains(mCore, "gpt-4o-audio")
}

func isGPTModelPrefix(m, prefix string) bool {
	if m == prefix {
		return true
	}
	return strings.HasPrefix(m, prefix+"-") || strings.HasPrefix(m, prefix+":") || strings.HasPrefix(m, prefix+"_")
}

func isEmbeddingModel(m string) bool {
	return strings.Contains(m, "embedding") ||
		strings.Contains(m, "embed") ||
		strings.HasPrefix(m, "voyage-") ||
		strings.Contains(m, "/voyage-") ||
		strings.Contains(m, ":voyage-")
}

func isTTSModel(m string) bool {
	if m == "tts" || strings.HasPrefix(m, "tts-") || strings.HasPrefix(m, "tts/") || strings.HasPrefix(m, "tts:") || strings.HasPrefix(m, "tts_") {
		return true
	}
	if strings.Contains(m, "-tts") || strings.Contains(m, "/tts") || strings.Contains(m, "_tts") || strings.Contains(m, ":tts") {
		return true
	}
	return false
}

func isGPTImageModel(m string) bool {
	return strings.Contains(m, "gpt-image")
}

func isDistillModel(m string) bool {
	return strings.Contains(m, "distill")
}

func isClaudeModel(m string) bool {
	if strings.Contains(m, "claude") || strings.Contains(m, "anthropic") {
		return true
	}
	// Bare aliases used in ingestion payloads (e.g. model: "sonnet", "haiku", "opus", "instant")
	if m == "sonnet" || m == "haiku" || m == "opus" || m == "instant" {
		return true
	}
	for _, sep := range []string{"/", ":", "_"} {
		if strings.HasSuffix(m, sep+"sonnet") || strings.HasSuffix(m, sep+"haiku") || strings.HasSuffix(m, sep+"opus") || strings.HasSuffix(m, sep+"instant") {
			return true
		}
	}
	for _, prefix := range []string{"sonnet-", "sonnet:", "sonnet_", "haiku-", "haiku:", "haiku_", "opus-", "opus:", "instant-", "instant:", "instant_"} {
		if strings.HasPrefix(m, prefix) && !strings.HasPrefix(m, "opus-mt") && !strings.HasPrefix(m, "opus-tc") {
			return true
		}
	}
	for _, sep := range []string{"/", ":"} {
		for _, prefix := range []string{"sonnet-", "haiku-", "opus-", "instant-"} {
			if strings.Contains(m, sep+prefix) && !strings.Contains(m, sep+"opus-mt") && !strings.Contains(m, sep+"opus-tc") {
				return true
			}
		}
	}
	return false
}

func isStepFunModel(m string) bool {
	if strings.Contains(m, "stepfun") {
		return true
	}
	if strings.HasPrefix(m, "step-") || strings.HasPrefix(m, "step1") || strings.HasPrefix(m, "step2") {
		return true
	}
	for _, sep := range []string{"/", ":", "_", "."} {
		if strings.Contains(m, sep+"step-") || strings.Contains(m, sep+"step1") || strings.Contains(m, sep+"step2") {
			return true
		}
	}
	return false
}

func isMiniModel(m string) bool {
	for _, marker := range []string{"-mini", ":mini", "/mini", "_mini", "-nano", ":nano", "/nano", "_nano", "-lite", ":lite", "/lite", "_lite", "-small", ":small", "/small", "_small"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return hasSmallModelSize(m)
}

func hasSmallModelSize(m string) bool {
	for _, size := range []string{"3b", "8b"} {
		idx := 0
		for {
			pos := strings.Index(m[idx:], size)
			if pos == -1 {
				break
			}
			actualPos := idx + pos
			idx = actualPos + len(size)
			if actualPos > 0 {
				prev := m[actualPos-1]
				if prev >= '0' && prev <= '9' {
					continue
				}
			}
			if idx < len(m) {
				next := m[idx]
				if next != '-' && next != '_' && next != '/' && next != ':' && next != '.' {
					continue
				}
			}
			return true
		}
	}
	return false
}

// ComputeMissingModelPricings filters candidateModels against existingModels,
// computes default prices for missing models, and returns them sorted by model name.
func ComputeMissingModelPricings(existingModels []string, candidateModels []string) []entities.ModelPriceSetting {
	if len(candidateModels) == 0 {
		return nil
	}

	existingSet := make(map[string]struct{}, len(existingModels))
	for _, em := range existingModels {
		trimmed := strings.TrimSpace(em)
		if trimmed != "" {
			existingSet[trimmed] = struct{}{}
		}
	}

	candidateMap := make(map[string]struct{})
	for _, cm := range candidateModels {
		trimmed := strings.TrimSpace(cm)
		normalized := strings.ToLower(trimmed)
		if trimmed != "" && !strings.EqualFold(trimmed, "unknown") && !isTTSModel(normalized) && !isGPTImageModel(normalized) && !isDistillModel(normalized) && !isGPT4oRealtimeAudioModel(normalized) {
			candidateMap[trimmed] = struct{}{}
		}
	}

	var toInsert []entities.ModelPriceSetting
	for m := range candidateMap {
		if _, exists := existingSet[m]; !exists {
			p, c, k := ResolveModelPrice(m)
			toInsert = append(toInsert, entities.ModelPriceSetting{
				Model:                m,
				PricingStyle:         pricingStyleForModel(m),
				PromptPricePer1M:     p,
				CompletionPricePer1M: c,
				CacheReadPricePer1M:  k,
			})
		}
	}

	if len(toInsert) == 0 {
		return nil
	}

	sort.Slice(toInsert, func(i, j int) bool {
		return toInsert[i].Model < toInsert[j].Model
	})

	return toInsert
}

// EnsureModelsPricing checks candidate models against the database, computes
// missing prices, and publishes them through the normal pricing mutation path.
func (s *pricingService) EnsureModelsPricing(ctx context.Context, candidateModels []string) ([]entities.ModelPriceSetting, error) {
	if s == nil || s.db == nil || len(candidateModels) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var existingModels []string
	if err := s.db.WithContext(ctx).Model(&entities.ModelPriceSetting{}).Pluck("model", &existingModels).Error; err != nil {
		return nil, fmt.Errorf("query existing model price settings: %w", err)
	}

	missing := ComputeMissingModelPricings(existingModels, candidateModels)
	if len(missing) == 0 {
		return nil, nil
	}

	inputs := make([]repodto.ModelPriceSettingInput, len(missing))
	settings := make([]entities.ModelPriceSetting, len(missing))
	for index := range missing {
		inputs[index] = repodto.ModelPriceSettingInput{
			Model:                missing[index].Model,
			PricingStyle:         missing[index].PricingStyle,
			PromptPricePer1M:     missing[index].PromptPricePer1M,
			CompletionPricePer1M: missing[index].CompletionPricePer1M,
			CacheReadPricePer1M:  missing[index].CacheReadPricePer1M,
			CacheWritePricePer1M: missing[index].CacheWritePricePer1M,
		}
	}

	if _, err := s.mutatePricing(ctx, func(tx *gorm.DB) error {
		for index := range inputs {
			setting, mutationErr := repository.UpsertModelPriceSetting(tx, inputs[index])
			if mutationErr != nil {
				return mutationErr
			}
			settings[index] = *setting
		}
		return nil
	}); err != nil {
		if errors.Is(err, repository.ErrInvalidPricingSnapshot) {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPricingInput, err)
		}
		return nil, err
	}

	logrus.Infof("Auto-populated pricing for %d new models", len(settings))
	return settings, nil
}

func pricingStyleForModel(model string) string {
	if isClaudeModel(strings.ToLower(strings.TrimSpace(model))) {
		return entities.ModelPricingStyleClaude
	}
	return entities.ModelPricingStyleOpenAI
}
