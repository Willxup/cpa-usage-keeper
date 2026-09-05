package service

import (
	"math"
	"testing"
)

func TestPricingResolver(t *testing.T) {
	const floatEpsilon = 1e-6
	isClose := func(a, b float64) bool {
		return math.Abs(a-b) < floatEpsilon
	}

	tests := []struct {
		name           string
		model          string
		wantPrompt     float64
		wantCompletion float64
		wantCache      float64
		wantCategory   string
	}{
		// 空模型名
		{
			name:           "Empty string",
			model:          "",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Unknown/Ignored",
		},
		{
			name:           "Unknown sentinel",
			model:          "unknown",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Unknown/Ignored",
		},
		// 免费与特殊模型
		{
			name:           "Free suffix with colon",
			model:          "moonshotai/kimi-k2.6:free",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Free/Special",
		},
		{
			name:           "Free suffix with hyphen",
			model:          "tencent/hy3-free",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Free/Special",
		},
		{
			name:           "Auto review internal tool",
			model:          "vibecode/codex-auto-review",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Free/Special",
		},
		{
			name:           "TTS model unpriced",
			model:          "tts-1",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Unpriced/TTS",
		},
		{
			name:           "GPT image model is unpriced and excluded from chat rates",
			model:          "gpt-image-1.5",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Unpriced/GPT-Image",
		},
		{
			name:           "OpenAI omni moderation free model",
			model:          "omni-moderation-latest",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Free/Special",
		},
		{
			name:           "OpenAI text moderation free model",
			model:          "text-moderation-latest",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Free/Special",
		},
		// OpenAI 系列
		{
			name:           "OpenAI gpt-4o 2024-05-13 older snapshot",
			model:          "gpt-4o-2024-05-13",
			wantPrompt:     5.0,
			wantCompletion: 15.0,
			wantCache:      2.5,
			wantCategory:   "OpenAI GPT-4o-2024-05-13 Official",
		},
		{
			name:           "OpenAI gpt-4o realtime preview",
			model:          "gpt-4o-realtime-preview",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Unpriced/Realtime-Audio",
		},
		{
			name:           "OpenAI gpt-4o-mini realtime preview",
			model:          "gpt-4o-mini-realtime-preview",
			wantPrompt:     0.60,
			wantCompletion: 2.40,
			wantCache:      0.30,
			wantCategory:   "OpenAI GPT-4o-mini Realtime Official",
		},
		{
			name:           "OpenAI codex-mini-latest",
			model:          "codex-mini-latest",
			wantPrompt:     1.50,
			wantCompletion: 6.00,
			wantCache:      0.375,
			wantCategory:   "OpenAI Codex Mini Official",
		},
		{
			name:           "OpenAI gpt-5-pro",
			model:          "gpt-5-pro",
			wantPrompt:     15.00,
			wantCompletion: 120.00,
			wantCache:      1.50,
			wantCategory:   "OpenAI GPT-5 Pro Official",
		},
		{
			name:           "OpenAI gpt-5",
			model:          "gpt-5",
			wantPrompt:     1.25,
			wantCompletion: 10.00,
			wantCache:      0.125,
			wantCategory:   "OpenAI GPT-5 Official",
		},
		{
			name:           "OpenAI gpt-5-mini",
			model:          "gpt-5-mini",
			wantPrompt:     0.25,
			wantCompletion: 2.00,
			wantCache:      0.025,
			wantCategory:   "OpenAI GPT-5-mini Official",
		},
		{
			name:           "OpenAI gpt-5-nano",
			model:          "gpt-5-nano",
			wantPrompt:     0.05,
			wantCompletion: 0.40,
			wantCache:      0.005,
			wantCategory:   "OpenAI GPT-5-nano Official",
		},
		{
			name:           "OpenAI gpt-5.1",
			model:          "gpt-5.1",
			wantPrompt:     1.25,
			wantCompletion: 10.00,
			wantCache:      0.125,
			wantCategory:   "OpenAI GPT-5 Official",
		},
		{
			name:           "OpenAI gpt-5.1-codex",
			model:          "gpt-5.1-codex",
			wantPrompt:     1.25,
			wantCompletion: 10.00,
			wantCache:      0.125,
			wantCategory:   "OpenAI GPT-5 Official",
		},
		{
			name:           "OpenAI gpt-5.1-codex-mini",
			model:          "gpt-5.1-codex-mini",
			wantPrompt:     0.25,
			wantCompletion: 2.00,
			wantCache:      0.025,
			wantCategory:   "OpenAI GPT-5-mini Official",
		},
		{
			name:           "OpenAI gpt-5.1-pro",
			model:          "gpt-5.1-pro",
			wantPrompt:     15.00,
			wantCompletion: 120.00,
			wantCache:      1.50,
			wantCategory:   "OpenAI GPT-5 Pro Official",
		},
		{
			name:           "OpenAI gpt-5.1-mini",
			model:          "gpt-5.1-mini",
			wantPrompt:     0.25,
			wantCompletion: 2.00,
			wantCache:      0.025,
			wantCategory:   "OpenAI GPT-5-mini Official",
		},
		{
			name:           "OpenAI gpt-5.1-nano",
			model:          "gpt-5.1-nano",
			wantPrompt:     0.05,
			wantCompletion: 0.40,
			wantCache:      0.005,
			wantCategory:   "OpenAI GPT-5-nano Official",
		},
		{
			name:           "OpenAI gpt-4o",
			model:          "gpt-4o",
			wantPrompt:     2.5,
			wantCompletion: 10.0,
			wantCache:      1.25,
			wantCategory:   "OpenAI GPT-4o Official",
		},
		{
			name:           "OpenAI gpt-4o snapshot date",
			model:          "gpt-4o-2024-08-06",
			wantPrompt:     2.5,
			wantCompletion: 10.0,
			wantCache:      1.25,
			wantCategory:   "OpenAI GPT-4o Official",
		},
		{
			name:           "OpenAI chatgpt-4o-latest",
			model:          "chatgpt-4o-latest",
			wantPrompt:     5.0,
			wantCompletion: 15.0,
			wantCache:      2.5,
			wantCategory:   "OpenAI ChatGPT-4o-latest Official",
		},
		{
			name:           "OpenAI gpt-4o-mini",
			model:          "gpt-4o-mini",
			wantPrompt:     0.15,
			wantCompletion: 0.6,
			wantCache:      0.075,
			wantCategory:   "OpenAI GPT-4o-mini Official",
		},
		{
			name:           "OpenAI gpt-4o-mini snapshot date",
			model:          "gpt-4o-mini-2024-07-18",
			wantPrompt:     0.15,
			wantCompletion: 0.6,
			wantCache:      0.075,
			wantCategory:   "OpenAI GPT-4o-mini Official",
		},
		{
			name:           "OpenAI gpt-4.1 with provider prefix",
			model:          "openai/gpt-4.1",
			wantPrompt:     2.0,
			wantCompletion: 8.0,
			wantCache:      0.5,
			wantCategory:   "OpenAI GPT-4.1 Official",
		},
		{
			name:           "OpenAI gpt-4.1-mini",
			model:          "gpt-4.1-mini",
			wantPrompt:     0.4,
			wantCompletion: 1.6,
			wantCache:      0.1,
			wantCategory:   "OpenAI GPT-4.1-mini Official",
		},
		{
			name:           "OpenAI gpt-4.1-nano",
			model:          "gpt-4.1-nano",
			wantPrompt:     0.1,
			wantCompletion: 0.4,
			wantCache:      0.025,
			wantCategory:   "OpenAI GPT-4.1-nano Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-4.1-nano",
			model:          "ft:gpt-4.1-nano:org:suffix:id",
			wantPrompt:     0.20,
			wantCompletion: 0.80,
			wantCache:      0.05,
			wantCategory:   "OpenAI Fine-Tuned GPT-4.1-nano Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-5",
			model:          "ft:gpt-5:org:suffix:id",
			wantPrompt:     2.50,
			wantCompletion: 20.00,
			wantCache:      0.25,
			wantCategory:   "OpenAI Fine-Tuned GPT-5 Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-5-mini",
			model:          "ft:gpt-5-mini:org:suffix:id",
			wantPrompt:     0.50,
			wantCompletion: 4.00,
			wantCache:      0.05,
			wantCategory:   "OpenAI Fine-Tuned GPT-5-mini Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-5-nano",
			model:          "ft:gpt-5-nano:org:suffix:id",
			wantPrompt:     0.10,
			wantCompletion: 0.80,
			wantCache:      0.01,
			wantCategory:   "OpenAI Fine-Tuned GPT-5-nano Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-5.1-nano",
			model:          "ft:gpt-5.1-nano:org:suffix:id",
			wantPrompt:     0.10,
			wantCompletion: 0.80,
			wantCache:      0.01,
			wantCategory:   "OpenAI Fine-Tuned GPT-5-nano Official",
		},
		{
			name:           "OpenAI gpt-4.5",
			model:          "gpt-4.5-preview",
			wantPrompt:     75.0,
			wantCompletion: 150.0,
			wantCache:      37.5,
			wantCategory:   "OpenAI GPT-4.5 Official",
		},
		{
			name:           "OpenAI gpt-4-turbo",
			model:          "gpt-4-turbo",
			wantPrompt:     10.0,
			wantCompletion: 30.0,
			wantCache:      5.0,
			wantCategory:   "OpenAI GPT-4 Turbo Official",
		},
		{
			name:           "OpenAI gpt-4-vision-preview",
			model:          "gpt-4-vision-preview",
			wantPrompt:     10.0,
			wantCompletion: 30.0,
			wantCache:      5.0,
			wantCategory:   "OpenAI GPT-4 Turbo Official",
		},
		{
			name:           "OpenAI gpt-4-32k legacy",
			model:          "gpt-4-32k",
			wantPrompt:     60.0,
			wantCompletion: 120.0,
			wantCache:      0.0,
			wantCategory:   "OpenAI GPT-4 32K Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-4o-mini",
			model:          "ft:gpt-4o-mini-2024-07-18:org:suffix:id",
			wantPrompt:     0.30,
			wantCompletion: 1.20,
			wantCache:      0.15,
			wantCategory:   "OpenAI Fine-Tuned GPT-4o-mini Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-4o",
			model:          "ft:gpt-4o:my-org:custom:123",
			wantPrompt:     3.75,
			wantCompletion: 15.00,
			wantCache:      1.875,
			wantCategory:   "OpenAI Fine-Tuned GPT-4o Official",
		},
		{
			name:           "OpenAI fine-tuned gpt-3.5-turbo",
			model:          "ft:gpt-3.5-turbo-0125:org:suffix:id",
			wantPrompt:     3.00,
			wantCompletion: 6.00,
			wantCache:      0.0,
			wantCategory:   "OpenAI Fine-Tuned GPT-3.5 Turbo Official",
		},
		{
			name:           "OpenAI gpt-4 legacy",
			model:          "gpt-4-0613",
			wantPrompt:     30.0,
			wantCompletion: 60.0,
			wantCache:      0.0,
			wantCategory:   "OpenAI GPT-4 Legacy Official",
		},
		{
			name:           "OpenAI gpt-3.5-turbo",
			model:          "gpt-3.5-turbo",
			wantPrompt:     0.5,
			wantCompletion: 1.5,
			wantCache:      0.0,
			wantCategory:   "OpenAI GPT-3.5 Turbo Official",
		},
		{
			name:           "OpenAI gpt-3.5-turbo 16k",
			model:          "gpt-3.5-turbo-16k",
			wantPrompt:     3.0,
			wantCompletion: 4.0,
			wantCache:      0.0,
			wantCategory:   "OpenAI GPT-3.5 Turbo 16K Official",
		},
		{
			name:           "OpenAI gpt-3.5-turbo 1106",
			model:          "gpt-3.5-turbo-1106",
			wantPrompt:     1.0,
			wantCompletion: 2.0,
			wantCache:      0.0,
			wantCategory:   "OpenAI GPT-3.5 Turbo 1106 Official",
		},
		{
			name:           "OpenAI gpt-3.5-turbo legacy 0613",
			model:          "gpt-3.5-turbo-0613",
			wantPrompt:     1.5,
			wantCompletion: 2.0,
			wantCache:      0.0,
			wantCategory:   "OpenAI GPT-3.5 Turbo Legacy Official",
		},
		{
			name:           "OpenAI gpt-3.5-turbo-instruct",
			model:          "gpt-3.5-turbo-instruct",
			wantPrompt:     1.5,
			wantCompletion: 2.0,
			wantCache:      0.0,
			wantCategory:   "OpenAI GPT-3.5 Turbo Instruct Official",
		},
		{
			name:           "OpenAI o1",
			model:          "o1",
			wantPrompt:     15.0,
			wantCompletion: 60.0,
			wantCache:      7.5,
			wantCategory:   "OpenAI o1 Official",
		},
		{
			name:           "OpenAI o1-pro",
			model:          "o1-pro",
			wantPrompt:     150.0,
			wantCompletion: 600.0,
			wantCache:      75.0,
			wantCategory:   "OpenAI o1-pro Official",
		},
		{
			name:           "OpenAI o1-preview",
			model:          "o1-preview",
			wantPrompt:     15.0,
			wantCompletion: 60.0,
			wantCache:      7.5,
			wantCategory:   "OpenAI o1 Official",
		},
		{
			name:           "OpenAI o1-mini",
			model:          "o1-mini",
			wantPrompt:     1.1,
			wantCompletion: 4.4,
			wantCache:      0.55,
			wantCategory:   "OpenAI o1-mini Official",
		},
		{
			name:           "OpenAI o3-mini",
			model:          "o3-mini",
			wantPrompt:     1.1,
			wantCompletion: 4.4,
			wantCache:      0.55,
			wantCategory:   "OpenAI o3-mini Official",
		},
		{
			name:           "OpenAI o3",
			model:          "o3",
			wantPrompt:     2.0,
			wantCompletion: 8.0,
			wantCache:      0.5,
			wantCategory:   "OpenAI o3 Official",
		},
		{
			name:           "OpenAI o3-pro",
			model:          "o3-pro",
			wantPrompt:     20.0,
			wantCompletion: 80.0,
			wantCache:      5.0,
			wantCategory:   "OpenAI o3-pro Official",
		},
		{
			name:           "OpenAI o4-mini",
			model:          "o4-mini",
			wantPrompt:     1.1,
			wantCompletion: 4.4,
			wantCache:      0.275,
			wantCategory:   "OpenAI o4-mini Official",
		},
		{
			name:           "OpenAI o3-deep-research",
			model:          "o3-deep-research",
			wantPrompt:     10.0,
			wantCompletion: 40.0,
			wantCache:      2.5,
			wantCategory:   "OpenAI o3 Deep Research Official",
		},
		{
			name:           "OpenAI o4-mini-deep-research",
			model:          "o4-mini-deep-research",
			wantPrompt:     2.0,
			wantCompletion: 8.0,
			wantCache:      0.5,
			wantCategory:   "OpenAI o4-mini Deep Research Official",
		},
		{
			name:           "OpenAI base babbage-002",
			model:          "babbage-002",
			wantPrompt:     0.40,
			wantCompletion: 0.40,
			wantCache:      0.0,
			wantCategory:   "OpenAI Base Babbage-002 Official",
		},
		{
			name:           "OpenAI base davinci-002",
			model:          "davinci-002",
			wantPrompt:     2.00,
			wantCompletion: 2.00,
			wantCache:      0.0,
			wantCategory:   "OpenAI Base Davinci-002 Official",
		},
		// DeepSeek 系列
		{
			name:           "DeepSeek v4 flash",
			model:          "deepseek-ai/deepseek-v4-flash",
			wantPrompt:     0.147,
			wantCompletion: 0.147,
			wantCache:      0.0147,
			wantCategory:   "DeepSeek Official",
		},
		{
			name:           "DeepSeek reasoner",
			model:          "deepseek-reasoner",
			wantPrompt:     0.55,
			wantCompletion: 2.19,
			wantCache:      0.14,
			wantCategory:   "DeepSeek Reasoner Official",
		},
		{
			name:           "DeepSeek r1",
			model:          "deepseek-r1",
			wantPrompt:     0.55,
			wantCompletion: 2.19,
			wantCache:      0.14,
			wantCategory:   "DeepSeek Reasoner Official",
		},
		{
			name:           "DeepSeek R1 distill is treated as unpriced distill",
			model:          "deepseek-ai/DeepSeek-R1-Distill-Qwen-32B",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Unpriced/Distill",
		},
		{
			name:           "DeepSeek R1 distill Llama is treated as unpriced distill",
			model:          "deepseek-ai/DeepSeek-R1-Distill-Llama-70B",
			wantPrompt:     0.0,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Unpriced/Distill",
		},
		// Qwen 系列
		{
			name:           "Qwen standard chat",
			model:          "qwen-chat",
			wantPrompt:     0.147,
			wantCompletion: 0.147,
			wantCache:      0.0,
			wantCategory:   "Qwen Standard Official",
		},
		{
			name:           "Qwen max flagship",
			model:          "qwen3.7-max",
			wantPrompt:     2.4,
			wantCompletion: 9.6,
			wantCache:      0.24,
			wantCategory:   "Qwen Max Official",
		},
		{
			name:           "Qwen3 Coder Plus",
			model:          "qwen3-coder-plus",
			wantPrompt:     1.0,
			wantCompletion: 5.0,
			wantCache:      0.20,
			wantCategory:   "Qwen Coder Plus Official",
		},
		// Claude 系列
		{
			name:           "Claude haiku 4.5",
			model:          "claude-haiku-4-5",
			wantPrompt:     1.0,
			wantCompletion: 5.0,
			wantCache:      0.1,
			wantCategory:   "Claude Haiku Official",
		},
		{
			name:           "Claude 3 haiku legacy date",
			model:          "claude-3-haiku-20240307",
			wantPrompt:     0.25,
			wantCompletion: 1.25,
			wantCache:      0.025,
			wantCategory:   "Claude 3 Haiku Official",
		},
		{
			name:           "Claude 3.5 haiku hyphen",
			model:          "claude-3-5-haiku-20241022",
			wantPrompt:     0.8,
			wantCompletion: 4.0,
			wantCache:      0.08,
			wantCategory:   "Claude 3.5 Haiku Official",
		},
		{
			name:           "Claude 3.5 haiku dot",
			model:          "claude-3.5-haiku",
			wantPrompt:     0.8,
			wantCompletion: 4.0,
			wantCache:      0.08,
			wantCategory:   "Claude 3.5 Haiku Official",
		},
		{
			name:           "Claude 3 haiku bare alias prefix",
			model:          "haiku-3",
			wantPrompt:     0.25,
			wantCompletion: 1.25,
			wantCache:      0.025,
			wantCategory:   "Claude 3 Haiku Official",
		},
		{
			name:           "Claude instant 1.2",
			model:          "claude-instant-1.2",
			wantPrompt:     0.80,
			wantCompletion: 2.40,
			wantCache:      0.08,
			wantCategory:   "Claude Instant Official",
		},
		{
			name:           "Claude sonnet",
			model:          "claude-3-7-sonnet",
			wantPrompt:     3.0,
			wantCompletion: 15.0,
			wantCache:      0.3,
			wantCategory:   "Claude Sonnet Official",
		},
		{
			name:           "Claude opus",
			model:          "claude-3-opus",
			wantPrompt:     15.0,
			wantCompletion: 75.0,
			wantCache:      1.5,
			wantCategory:   "Claude Opus Official",
		},
		{
			name:           "Claude opus 4.5 hyphen",
			model:          "claude-opus-4-5",
			wantPrompt:     5.0,
			wantCompletion: 25.0,
			wantCache:      0.5,
			wantCategory:   "Claude Opus 4.5 Official",
		},
		{
			name:           "Claude opus 4.5 dot",
			model:          "claude-opus-4.5",
			wantPrompt:     5.0,
			wantCompletion: 25.0,
			wantCache:      0.5,
			wantCategory:   "Claude Opus 4.5 Official",
		},
		{
			name:           "Claude 3 opus legacy date",
			model:          "claude-3-opus-20240229",
			wantPrompt:     15.0,
			wantCompletion: 75.0,
			wantCache:      1.5,
			wantCategory:   "Claude Opus Official",
		},
		{
			name:           "Claude opus 4 legacy price",
			model:          "claude-opus-4",
			wantPrompt:     15.0,
			wantCompletion: 75.0,
			wantCache:      1.5,
			wantCategory:   "Claude Opus Official",
		},
		{
			name:           "Claude opus 4.1 legacy price",
			model:          "claude-opus-4-1",
			wantPrompt:     15.0,
			wantCompletion: 75.0,
			wantCache:      1.5,
			wantCategory:   "Claude Opus Official",
		},
		{
			name:           "Claude opus 4.1 dot legacy price",
			model:          "claude-opus-4.1",
			wantPrompt:     15.0,
			wantCompletion: 75.0,
			wantCache:      1.5,
			wantCategory:   "Claude Opus Official",
		},
		{
			name:           "Claude 2.1 legacy generation",
			model:          "claude-2.1",
			wantPrompt:     8.0,
			wantCompletion: 24.0,
			wantCache:      0.8,
			wantCategory:   "Claude Legacy Official",
		},
		// Embedding models
		{
			name:           "Text embedding 3 small not misclassified as mini",
			model:          "text-embedding-3-small",
			wantPrompt:     0.02,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding 3 Small Official",
		},
		{
			name:           "Text embedding 3 large",
			model:          "text-embedding-3-large",
			wantPrompt:     0.13,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding 3 Large Official",
		},
		{
			name:           "Text embedding ada 002",
			model:          "text-embedding-ada-002",
			wantPrompt:     0.10,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding Ada Official",
		},
		{
			name:           "Provider prefixed text embedding small",
			model:          "openai/text-embedding-3-small",
			wantPrompt:     0.02,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding 3 Small Official",
		},
		{
			name:           "Generic embedding fallback",
			model:          "baichuan-text-embedding",
			wantPrompt:     0.02,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding Fallback",
		},
		{
			name:           "Gemini embedding 001",
			model:          "gemini-embedding-001",
			wantPrompt:     0.15,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Gemini Embedding Official",
		},
		{
			name:           "Google text embedding 004",
			model:          "text-embedding-004",
			wantPrompt:     0.15,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Gemini Embedding Official",
		},
		{
			name:           "Voyage embedding fallback",
			model:          "voyage-3-lite",
			wantPrompt:     0.02,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding Fallback",
		},
		{
			name:           "Voyage 3.5 embedding",
			model:          "voyage-3.5",
			wantPrompt:     0.06,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Voyage 3.5 Official",
		},
		{
			name:           "Qwen embedding not misclassified as Qwen chat",
			model:          "qwen/qwen3-embedding-8b",
			wantPrompt:     0.02,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding Fallback",
		},
		{
			name:           "DeepSeek embedding not misclassified as DeepSeek chat",
			model:          "deepseek-ai/deepseek-embedding",
			wantPrompt:     0.02,
			wantCompletion: 0.0,
			wantCache:      0.0,
			wantCategory:   "Embedding Fallback",
		},
		// Gemini / Agnes 系列
		{
			name:           "Gemini 3 flash lite",
			model:          "Antigravity/gemini-3.1-flash-lite",
			wantPrompt:     0.1,
			wantCompletion: 0.4,
			wantCache:      0.025,
			wantCategory:   "Gemini Flash-Lite/Image Official",
		},
		{
			name:           "Gemini 3 flash preview",
			model:          "gemini-3-flash-preview",
			wantPrompt:     0.50,
			wantCompletion: 3.00,
			wantCache:      0.125,
			wantCategory:   "Gemini 3 Flash Official",
		},
		{
			name:           "Gemini 3 flash standard",
			model:          "Antigravity/gemini-3-flash",
			wantPrompt:     0.50,
			wantCompletion: 3.00,
			wantCache:      0.125,
			wantCategory:   "Gemini 3 Flash Official",
		},
		{
			name:           "Gemini 2.0 flash lite",
			model:          "gemini-2.0-flash-lite",
			wantPrompt:     0.075,
			wantCompletion: 0.30,
			wantCache:      0.01875,
			wantCategory:   "Gemini 2.0 Flash-Lite Official",
		},
		{
			name:           "Gemini 2.0 flash",
			model:          "gemini-2.0-flash",
			wantPrompt:     0.10,
			wantCompletion: 0.40,
			wantCache:      0.025,
			wantCategory:   "Gemini 2.0 Flash Official",
		},
		{
			name:           "Gemini 1.5 flash",
			model:          "gemini-1.5-flash",
			wantPrompt:     0.075,
			wantCompletion: 0.30,
			wantCache:      0.01875,
			wantCategory:   "Gemini 1.5 Flash Official",
		},
		{
			name:           "Gemini 1.5 pro consistent context tier",
			model:          "gemini-1.5-pro",
			wantPrompt:     1.25,
			wantCompletion: 5.0,
			wantCache:      0.3125,
			wantCategory:   "Gemini 1.5 Pro Official",
		},
		{
			name:           "Gemini 3.7 flash high",
			model:          "gemini-3.7-flash-high",
			wantPrompt:     1.5,
			wantCompletion: 9.0,
			wantCache:      0.15,
			wantCategory:   "Gemini Flash-High/Pro-Low Official",
		},
		{
			name:           "Gemini pro agent",
			model:          "gemini-pro-agent",
			wantPrompt:     1.25,
			wantCompletion: 10.0,
			wantCache:      0.31,
			wantCategory:   "Gemini Pro/Agent Official",
		},
		{
			name:           "Agnes 2.5 pro alpha",
			model:          "agnes-2.5-pro-alpha",
			wantPrompt:     1.25,
			wantCompletion: 10.0,
			wantCache:      0.31,
			wantCategory:   "Gemini Pro/Agent Official",
		},
		// GLM / 智谱 系列
		{
			name:           "GLM standard",
			model:          "glm-5.2",
			wantPrompt:     1.0,
			wantCompletion: 3.0,
			wantCache:      0.2,
			wantCategory:   "GLM Standard Official",
		},
		{
			name:           "GLM flash",
			model:          "glm-5.3-flash",
			wantPrompt:     0.1,
			wantCompletion: 0.1,
			wantCache:      0.0,
			wantCategory:   "GLM Flash Official",
		},
		// Kimi / Moonshot 系列
		{
			name:           "Kimi k3",
			model:          "kimi-k3",
			wantPrompt:     0.95,
			wantCompletion: 4.0,
			wantCache:      0.16,
			wantCategory:   "Kimi Official",
		},
		// MiniMax 系列
		{
			name:           "MiniMax m2.7",
			model:          "minimaxai/minimax-m2.7",
			wantPrompt:     1.0,
			wantCompletion: 3.0,
			wantCache:      0.2,
			wantCategory:   "MiniMax Official",
		},
		// SenseNova / StepFun 系列
		{
			name:           "SenseNova fast",
			model:          "sensenova-u1-fast",
			wantPrompt:     0.2,
			wantCompletion: 0.5,
			wantCache:      0.02,
			wantCategory:   "SenseNova/StepFun Official",
		},
		{
			name:           "StepFun flash",
			model:          "stepfun-ai/step-3.7-flash",
			wantPrompt:     0.2,
			wantCompletion: 0.5,
			wantCache:      0.02,
			wantCategory:   "SenseNova/StepFun Official",
		},
		// 混元
		{
			name:           "Hunyuan hy3",
			model:          "hy3",
			wantPrompt:     1.0,
			wantCompletion: 2.0,
			wantCache:      0.1,
			wantCategory:   "Hunyuan Official",
		},
		// 零一万物
		{
			name:           "Yi large",
			model:          "01-ai/yi-large",
			wantPrompt:     2.8,
			wantCompletion: 2.8,
			wantCache:      0.0,
			wantCategory:   "Yi Official",
		},
		// Mistral / Codestral
		{
			name:           "Mistral small 2506",
			model:          "mistral-small-2506",
			wantPrompt:     0.10,
			wantCompletion: 0.30,
			wantCache:      0.0,
			wantCategory:   "Mistral Small Official",
		},
		{
			name:           "Mistral large latest",
			model:          "mistral-large-latest",
			wantPrompt:     2.0,
			wantCompletion: 6.0,
			wantCache:      0.0,
			wantCategory:   "Mistral Large Official",
		},
		{
			name:           "Codestral latest",
			model:          "codestral-latest",
			wantPrompt:     0.30,
			wantCompletion: 0.90,
			wantCache:      0.0,
			wantCategory:   "Codestral Official",
		},
		{
			name:           "Ministral 3B",
			model:          "ministral-3b-2410",
			wantPrompt:     0.04,
			wantCompletion: 0.04,
			wantCache:      0.0,
			wantCategory:   "Ministral 3B Official",
		},
		{
			name:           "Ministral 8B",
			model:          "ministral-8b-2410",
			wantPrompt:     0.10,
			wantCompletion: 0.10,
			wantCache:      0.0,
			wantCategory:   "Mistral Nemo/8B Official",
		},
		{
			name:           "Mistral Nemo",
			model:          "mistral-nemo",
			wantPrompt:     0.10,
			wantCompletion: 0.10,
			wantCache:      0.0,
			wantCategory:   "Mistral Nemo/8B Official",
		},
		// GPT 兜底系列
		{
			name:           "GPT standard baseline",
			model:          "gpt-5.4",
			wantPrompt:     1.75,
			wantCompletion: 14.0,
			wantCache:      0.175,
			wantCategory:   "GPT Standard Baseline",
		},
		{
			name:           "Unknown third-party standard fallback to GPT",
			model:          "gpt-oss-120b-medium",
			wantPrompt:     1.75,
			wantCompletion: 14.0,
			wantCache:      0.175,
			wantCategory:   "GPT Standard Baseline",
		},
		{
			name:           "GPT mini baseline",
			model:          "vibecode/gpt-5.4-mini",
			wantPrompt:     0.75,
			wantCompletion: 4.5,
			wantCache:      0.075,
			wantCategory:   "GPT Mini Baseline",
		},
		{
			name:           "Third-party small fallback to GPT mini",
			model:          "meta/llama-3.2-3b-instruct",
			wantPrompt:     0.75,
			wantCompletion: 4.5,
			wantCache:      0.075,
			wantCategory:   "GPT Mini Baseline",
		},
		{
			name:           "GPT flagship baseline",
			model:          "gpt-6-astra",
			wantPrompt:     5.0,
			wantCompletion: 30.0,
			wantCache:      0.5,
			wantCategory:   "GPT Flagship Baseline",
		},
		{
			name:           "Ultra flagship fallback to GPT flagship",
			model:          "llama-3.1-nemotron-ultra-253b-v1",
			wantPrompt:     5.0,
			wantCompletion: 30.0,
			wantCache:      0.5,
			wantCategory:   "GPT Flagship Baseline",
		},
		// Mini 变体优先级高于 Flagship 族系测试
		{
			name:           "GPT-5.5 mini prioritized over gpt-5.5 flagship",
			model:          "gpt-5.5-mini",
			wantPrompt:     0.75,
			wantCompletion: 4.5,
			wantCache:      0.075,
			wantCategory:   "GPT Mini Baseline",
		},
		{
			name:           "GPT-6 mini prioritized over gpt-6 flagship",
			model:          "gpt-6-mini",
			wantPrompt:     0.75,
			wantCompletion: 4.5,
			wantCache:      0.075,
			wantCategory:   "GPT Mini Baseline",
		},
		// TTS 单词边界测试：防止包含 tts 字母序列的普通模型被误杀
		{
			name:           "Reflection model not misclassified as TTS",
			model:          "mattshumer/reflection-70b",
			wantPrompt:     1.75,
			wantCompletion: 14.0,
			wantCache:      0.175,
			wantCategory:   "GPT Standard Baseline",
		},
		// Claude 约束匹配测试：防止非 Claude 的 opus-mt 翻译模型被误判为 Opus
		{
			name:           "Translation opus-mt model not misclassified as Claude Opus",
			model:          "Helsinki-NLP/opus-mt-en-fr",
			wantPrompt:     1.75,
			wantCompletion: 14.0,
			wantCache:      0.175,
			wantCategory:   "GPT Standard Baseline",
		},
		{
			name:           "Standalone opus-mt model not misclassified as Claude Opus",
			model:          "opus-mt-zh-en",
			wantPrompt:     1.75,
			wantCompletion: 14.0,
			wantCache:      0.175,
			wantCategory:   "GPT Standard Baseline",
		},
		{
			name:           "Anthropic provider prefix matches Claude Sonnet",
			model:          "anthropic/sonnet-3.7",
			wantPrompt:     3.0,
			wantCompletion: 15.0,
			wantCache:      0.3,
			wantCategory:   "Claude Sonnet Official",
		},
		{
			name:           "Standalone Claude alias sonnet prefix",
			model:          "sonnet-4-6",
			wantPrompt:     3.0,
			wantCompletion: 15.0,
			wantCache:      0.3,
			wantCategory:   "Claude Sonnet Official",
		},
		{
			name:           "Standalone Claude alias opus prefix",
			model:          "opus-3",
			wantPrompt:     15.0,
			wantCompletion: 75.0,
			wantCache:      1.5,
			wantCategory:   "Claude Opus Official",
		},
		// Bare Claude aliases (as emitted by Redis ingestion payloads)
		{
			name:           "Bare Claude sonnet alias",
			model:          "sonnet",
			wantPrompt:     3.0,
			wantCompletion: 15.0,
			wantCache:      0.3,
			wantCategory:   "Claude Sonnet Official",
		},
		{
			name:           "Bare Claude haiku alias",
			model:          "haiku",
			wantPrompt:     1.0,
			wantCompletion: 5.0,
			wantCache:      0.1,
			wantCategory:   "Claude Haiku Official",
		},
		{
			name:           "Bare Claude opus alias",
			model:          "opus",
			wantPrompt:     15.0,
			wantCompletion: 75.0,
			wantCache:      1.5,
			wantCategory:   "Claude Opus Official",
		},
		{
			name:           "Bare Claude instant alias",
			model:          "instant",
			wantPrompt:     0.80,
			wantCompletion: 2.40,
			wantCache:      0.08,
			wantCategory:   "Claude Instant Official",
		},
		{
			name:           "Provider delimited bare Claude sonnet alias",
			model:          "provider/sonnet",
			wantPrompt:     3.0,
			wantCompletion: 15.0,
			wantCache:      0.3,
			wantCategory:   "Claude Sonnet Official",
		},
		// StepFun token/prefix matching & negative matches (avoiding substring false-positives)
		{
			name:           "StepFun prefixed model",
			model:          "step-1-8k",
			wantPrompt:     0.2,
			wantCompletion: 0.5,
			wantCache:      0.02,
			wantCategory:   "SenseNova/StepFun Official",
		},
		{
			name:           "StepFun provider prefixed model",
			model:          "stepfun/step-2-16k",
			wantPrompt:     0.2,
			wantCompletion: 0.5,
			wantCache:      0.02,
			wantCategory:   "SenseNova/StepFun Official",
		},
		{
			name:           "Multistep reasoner not misclassified as StepFun",
			model:          "acme/multistep-reasoner",
			wantPrompt:     1.75,
			wantCompletion: 14.0,
			wantCache:      0.175,
			wantCategory:   "GPT Standard Baseline",
		},
		{
			name:           "Timestep diffuser not misclassified as StepFun",
			model:          "timestep-diffuser",
			wantPrompt:     1.75,
			wantCompletion: 14.0,
			wantCache:      0.175,
			wantCategory:   "GPT Standard Baseline",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := ResolveModelPricing(tc.model)
			if res.Category != tc.wantCategory {
				t.Errorf("model %q: got category %q, want %q", tc.model, res.Category, tc.wantCategory)
			}
			if !isClose(res.PromptPricePer1M, tc.wantPrompt) {
				t.Errorf("model %q: got prompt price %f, want %f", tc.model, res.PromptPricePer1M, tc.wantPrompt)
			}
			if !isClose(res.CompletionPricePer1M, tc.wantCompletion) {
				t.Errorf("model %q: got completion price %f, want %f", tc.model, res.CompletionPricePer1M, tc.wantCompletion)
			}
			if !isClose(res.CacheReadPricePer1M, tc.wantCache) {
				t.Errorf("model %q: got cache price %f, want %f", tc.model, res.CacheReadPricePer1M, tc.wantCache)
			}

			// 同时测试 ResolveModelPrice 简化函数
			p, c, ca := ResolveModelPrice(tc.model)
			if !isClose(p, tc.wantPrompt) || !isClose(c, tc.wantCompletion) || !isClose(ca, tc.wantCache) {
				t.Errorf("ResolveModelPrice(%q) = (%f, %f, %f), want (%f, %f, %f)", tc.model, p, c, ca, tc.wantPrompt, tc.wantCompletion, tc.wantCache)
			}
		})
	}
}
func TestComputeMissingModelPricings(t *testing.T) {
	existing := []string{"gpt-4o", "claude-sonnet"}
	candidates := []string{
		"gpt-4o",
		"deepseek-ai/deepseek-v4-flash",
		"vibecode/codex-auto-review",
		"qwen3.7-max",
		"unknown-model-mini",
		"openai/tts-1",
		"openai/gpt-image-1",
		"omni-moderation-latest",
		"deepseek-ai/DeepSeek-R1-Distill-Qwen-32B",
		"  deepseek-ai/deepseek-v4-flash  ", // duplicate with whitespace
		"unknown",                           // sentinel should be ignored
		"  unknown  ",                       // sentinel with whitespace should be ignored
	}

	missing := ComputeMissingModelPricings(existing, candidates)
	if len(missing) != 5 {
		t.Fatalf("expected 5 missing models, got %d", len(missing))
	}

	// Should be sorted alphabetically by model name
	expectedModels := []string{
		"deepseek-ai/deepseek-v4-flash",
		"omni-moderation-latest",
		"qwen3.7-max",
		"unknown-model-mini",
		"vibecode/codex-auto-review",
	}
	for i, m := range missing {
		if m.Model != expectedModels[i] {
			t.Errorf("expected model[%d] = %q, got %q", i, expectedModels[i], m.Model)
		}
	}

	// Verify deepseek price (0.147 / 0.147 / 0.0)
	if missing[0].PromptPricePer1M != 0.147 || missing[0].CompletionPricePer1M != 0.147 || missing[0].CacheReadPricePer1M != 0.0147 {
		t.Errorf("unexpected deepseek price: %+v", missing[0])
	}
	// Verify moderation price (0.0 / 0.0 / 0.0)
	if missing[1].PromptPricePer1M != 0.0 || missing[1].CompletionPricePer1M != 0.0 || missing[1].CacheReadPricePer1M != 0.0 {
		t.Errorf("unexpected moderation price: %+v", missing[1])
	}
	// Verify qwen max price (2.4 / 9.6 / 0.24)
	if missing[2].PromptPricePer1M != 2.4 || missing[2].CompletionPricePer1M != 9.6 || missing[2].CacheReadPricePer1M != 0.24 {
		t.Errorf("unexpected qwen max price: %+v", missing[1])
	}
	// Verify mini fallback price (0.75 / 4.5 / 0.075)
	if missing[3].PromptPricePer1M != 0.75 || missing[3].CompletionPricePer1M != 4.5 || missing[3].CacheReadPricePer1M != 0.075 {
		t.Errorf("unexpected mini fallback price: %+v", missing[2])
	}
	// Verify free special model price (0.0 / 0.0 / 0.0)
	if missing[4].PromptPricePer1M != 0.0 || missing[4].CompletionPricePer1M != 0.0 || missing[4].CacheReadPricePer1M != 0.0 {
		t.Errorf("unexpected auto-review price: %+v", missing[3])
	}

	// Test empty candidates
	if empty := ComputeMissingModelPricings(existing, nil); empty != nil {
		t.Errorf("expected nil for empty candidates, got %+v", empty)
	}

	// Test all candidates already exist
	if allExist := ComputeMissingModelPricings(existing, []string{"gpt-4o", "claude-sonnet"}); allExist != nil {
		t.Errorf("expected nil when all candidates exist, got %+v", allExist)
	}

	// Test empty existing (all candidates are missing)
	if allMissing := ComputeMissingModelPricings(nil, []string{"model-a"}); len(allMissing) != 1 || allMissing[0].Model != "model-a" {
		t.Errorf("expected 1 missing model when existing is nil, got %+v", allMissing)
	}
}

func TestComputeMissingModelPricingsSkipsRealtimeAudioModels(t *testing.T) {
	missing := ComputeMissingModelPricings(nil, []string{
		"gpt-4o-realtime-preview",
		"gpt-4o-audio-preview",
	})
	if len(missing) != 0 {
		t.Fatalf("expected realtime/audio models to remain unpriced, got %+v", missing)
	}
}
