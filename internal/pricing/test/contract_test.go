package test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/pricing"
)

const completeConfigJSON = `{
  "model":"sample-model","pricing_style":"openai",
  "base_prices":{"input":0,"output":0,"cache_read":0,"cache_write":0},
  "model_multiplier":0,
  "conditional_multipliers":[{"key":"reasoning_effort","value":"xhigh","multiplier":0}],
  "branches":[{"id":"branch-1","name":"Large","context":{"type":"gt","threshold":0},"period":{"type":"window","start":"20:00","end":"08:00"},"prices":{"input":0,"output":0,"cache_read":0,"cache_write":0}}]
}`

type pricingExample struct {
	Name   string                      `json:"name"`
	Config *pricing.ModelPricingConfig `json:"config"`
	Input  struct {
		Model               string `json:"model"`
		ServiceTier         string `json:"service_tier"`
		Timestamp           string `json:"timestamp"`
		InputTokens         int64  `json:"input_tokens"`
		OutputTokens        int64  `json:"output_tokens"`
		CacheReadTokens     int64  `json:"cache_read_tokens"`
		CacheCreationTokens int64  `json:"cache_creation_tokens"`
	} `json:"input"`
	Want pricing.FeeResult `json:"want"`
}

// loadPricingExamples 保持后续计价器测试与本提交固定的输入／结果用例共用同一份数据。
func loadPricingExamples(t *testing.T) []pricingExample {
	t.Helper()
	data, err := os.ReadFile("testdata/pricing_examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []pricingExample
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	return examples
}

func TestCompletePricingConfigKeepsExplicitZeroAndArrays(t *testing.T) {
	var config pricing.ModelPricingConfig
	if err := json.Unmarshal([]byte(completeConfigJSON), &config); err != nil {
		t.Fatal(err)
	}
	if config.ModelMultiplier != 0 || config.ConditionalMultipliers[0].Multiplier != 0 || *config.Branches[0].Context.Threshold != 0 {
		t.Fatalf("explicit zero was lost: %+v", config)
	}
	if config.Branches[0].Days != pricing.DaysAll {
		t.Fatalf("legacy branch days should default to all: %+v", config.Branches[0])
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"conditional_multipliers"`, `"branches"`, `"model_multiplier":0`, `"threshold":0`, `"days":"all"`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("round-trip lost %s: %s", field, encoded)
		}
	}
}

func TestCompletePricingConfigRejectsOmittedNullAndIrrelevantFields(t *testing.T) {
	for _, testCase := range []struct {
		name string
		from string
		to   string
	}{
		{"missing input price", `"input":0,`, ``},
		{"null input price", `"input":0`, `"input":null`},
		{"missing model multiplier", `"model_multiplier":0,`, ``},
		{"null model multiplier", `"model_multiplier":0`, `"model_multiplier":null`},
		{"missing branches array", `"branches":[{"id"`, `"missing":[{"id"`},
		{"null branches array", `"branches":[{"id":"branch-1","name":"Large","context":{"type":"gt","threshold":0},"period":{"type":"window","start":"20:00","end":"08:00"},"prices":{"input":0,"output":0,"cache_read":0,"cache_write":0}}]`, `"branches":null`},
		{"missing rule multiplier", `,"multiplier":0`, ``},
		{"null rule multiplier", `"multiplier":0`, `"multiplier":null`},
		{"all context with extra threshold", `"type":"gt","threshold":0`, `"type":"all","threshold":null`},
		{"gt context with range field", `"type":"gt","threshold":0`, `"type":"gt","threshold":0,"min":null`},
		{"window period with extra field", `"type":"window","start":"20:00","end":"08:00"`, `"type":"window","start":"20:00","end":"08:00","timezone":"UTC"`},
		{"all period with extra null", `"type":"window","start":"20:00","end":"08:00"`, `"type":"all","start":null`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input := strings.Replace(completeConfigJSON, testCase.from, testCase.to, 1)
			if input == completeConfigJSON {
				t.Fatalf("test mutation did not match")
			}
			var config pricing.ModelPricingConfig
			if err := json.Unmarshal([]byte(input), &config); err == nil {
				t.Fatalf("invalid config accepted: %s", input)
			}
		})
	}
}

// 这些固定输入与期望值供 CMT02 的统一计价器行为测试复用；当前只检查合同可解码。
func TestPricingExamplesAreStableAndDecodable(t *testing.T) {
	examples := loadPricingExamples(t)
	if len(examples) != 6 || examples[0].Name != "entire_request_above_context_threshold" || examples[0].Want.TotalCostUSD != 0.54 || examples[2].Want.TotalCostUSD != 3 {
		t.Fatalf("fixed pricing examples changed unexpectedly: %+v", examples)
	}
	for _, example := range examples {
		if _, err := time.Parse(time.RFC3339, example.Input.Timestamp); err != nil {
			t.Fatalf("%s has invalid CPA timestamp: %v", example.Name, err)
		}
	}
}
