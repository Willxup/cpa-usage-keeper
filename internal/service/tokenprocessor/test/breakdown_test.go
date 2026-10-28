package tokenprocessor_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/service/tokenprocessor"
)

const claudeAccounting = `{"schema_version":2,"quality":"complete","total_tokens":470,"input":{"total_tokens":450,"uncached_tokens":100,"cache_read_tokens":300,"cache_write_tokens":50},"output":{"total_tokens":20,"non_reasoning_tokens":15,"reasoning_tokens":5},"unclassified_tokens":0}`

func claudeReportedTokens() tokenprocessor.TokenValues {
	return tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 300, CacheReadTokens: 300, CacheReadPresent: true, CacheCreationTokens: 50, TotalTokens: 470}
}

func TestBreakdownNormalizesUnknownExecutorProtocols(t *testing.T) {
	tests := []struct {
		name      string
		reported  tokenprocessor.TokenValues
		breakdown string
		want      tokenprocessor.TokenValues
	}{
		{
			name: "Claude independent cache", reported: claudeReportedTokens(), breakdown: claudeAccounting,
			want: tokenprocessor.TokenValues{InputTokens: 450, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 300, CacheReadTokens: 300, CacheReadPresent: true, CacheCreationTokens: 50, TotalTokens: 470},
		},
		{
			name: "OpenAI inclusive input", reported: tokenprocessor.TokenValues{InputTokens: 400, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 300, CacheReadTokens: 300, CacheReadPresent: true, TotalTokens: 420},
			breakdown: `{"schema_version":2,"quality":"complete","total_tokens":420,"input":{"total_tokens":400,"uncached_tokens":100,"cache_read_tokens":300,"cache_write_tokens":0},"output":{"total_tokens":20,"non_reasoning_tokens":15,"reasoning_tokens":5},"unclassified_tokens":0}`,
			want:      tokenprocessor.TokenValues{InputTokens: 400, OutputTokens: 20, ReasoningTokens: 5, CachedTokens: 300, CacheReadTokens: 300, CacheReadPresent: true, TotalTokens: 420},
		},
		{
			name: "separate reasoning", reported: tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 30, ReasoningTokens: 10, CachedTokens: 20, CacheReadTokens: 20, CacheReadPresent: true, TotalTokens: 140},
			breakdown: `{"schema_version":2,"quality":"complete","total_tokens":140,"input":{"total_tokens":100,"uncached_tokens":80,"cache_read_tokens":20,"cache_write_tokens":0},"output":{"total_tokens":40,"non_reasoning_tokens":30,"reasoning_tokens":10},"unclassified_tokens":0}`,
			want:      tokenprocessor.TokenValues{InputTokens: 100, OutputTokens: 40, ReasoningTokens: 10, CachedTokens: 20, CacheReadTokens: 20, CacheReadPresent: true, TotalTokens: 140},
		},
		{
			name: "complete zero", reported: tokenprocessor.TokenValues{},
			breakdown: `{"schema_version":2,"quality":"complete","total_tokens":0,"input":{"total_tokens":0,"uncached_tokens":0,"cache_read_tokens":0,"cache_write_tokens":0},"output":{"total_tokens":0,"non_reasoning_tokens":0,"reasoning_tokens":0},"unclassified_tokens":0}`,
			want:      tokenprocessor.TokenValues{CacheReadPresent: true},
		},
	}
	for _, test := range tests {
		for _, executor := range []string{"executorAdapter", "FutureExecutor", "", "unknown"} {
			t.Run(test.name+"/"+executor, func(t *testing.T) {
				resolution, err := tokenprocessor.ResolveExecutorWithBreakdown(executor, test.reported, json.RawMessage(`2`), json.RawMessage(test.breakdown))
				if err != nil || resolution.NeedsIdentity() || resolution.HandlerID() != tokenprocessor.HandlerTokenBreakdown || resolution.EvidenceSource() != tokenprocessor.EvidenceTokenBreakdown {
					t.Fatalf("expected complete accounting resolution: %+v err=%v", resolution, err)
				}
				got := tokenprocessor.Process(test.reported, resolution)
				if got.Tokens != test.want || len(got.Violations) != 0 {
					t.Fatalf("got %+v, want tokens %+v", got, test.want)
				}
			})
		}
	}
}

func TestBreakdownNeverChangesRegisteredExecutors(t *testing.T) {
	for _, executor := range []string{
		"ClaudeExecutor", "GeminiExecutor", "GeminiVertexExecutor", "GeminiCLIExecutor", "AIStudioExecutor", "AntigravityExecutor",
		"CodexExecutor", "CodexWebsocketsExecutor", "CodexAutoExecutor", "XAIExecutor", "XAIWebsocketsExecutor", "XAIAutoExecutor",
		"KimiExecutor", "OpenAICompatExecutor", "MetaExecutor", "DevinExecutor",
	} {
		t.Run(executor, func(t *testing.T) {
			baseline := mustResolveExecutor(t, executor)
			for _, values := range []tokenprocessor.TokenValues{
				claudeReportedTokens(),
				{InputTokens: 100, OutputTokens: 30, ReasoningTokens: 10, TotalTokens: 140},
				{TotalTokens: 2120},
			} {
				for _, raw := range []string{claudeAccounting, `"invalid"`, `null`} {
					resolved, err := tokenprocessor.ResolveExecutorWithBreakdown(executor, values, json.RawMessage(`2`), json.RawMessage(raw))
					if err != nil || !reflect.DeepEqual(resolved, baseline) {
						t.Fatalf("registered executor route changed: got=%+v want=%+v err=%v", resolved, baseline, err)
					}
					got, want := tokenprocessor.Process(values, resolved), tokenprocessor.Process(values, baseline)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("registered executor result changed: got=%+v want=%+v", got, want)
					}
				}
			}
		})
	}
}

func TestBreakdownRejectsUnavailableOrContradictoryAccounting(t *testing.T) {
	for name, raw := range map[string]string{
		"absent": "", "null": `null`, "wrong type": `[]`, "malformed": `{`,
	} {
		t.Run(name, func(t *testing.T) {
			assertBreakdownUnresolved(t, claudeReportedTokens(), "2", raw)
		})
	}
	for name, replacement := range map[string][2]string{
		"missing input":     {`"input":`, `"ignored_input":`},
		"missing output":    {`"output":`, `"ignored_output":`},
		"missing zero":      {`,"unclassified_tokens":0`, ""},
		"null bucket":       {`"uncached_tokens":100`, `"uncached_tokens":null`},
		"fractional count":  {`"uncached_tokens":100`, `"uncached_tokens":100.5`},
		"string count":      {`"uncached_tokens":100`, `"uncached_tokens":"100"`},
		"negative count":    {`"uncached_tokens":100`, `"uncached_tokens":-100`},
		"overflow":          {`"uncached_tokens":100`, `"uncached_tokens":9223372036854775807`},
		"out of int64":      {`"uncached_tokens":100`, `"uncached_tokens":9223372036854775808`},
		"schema mismatch":   {`"schema_version":2`, `"schema_version":3`},
		"unclassified":      {`"complete"`, `"unclassified"`},
		"inconsistent":      {`"complete"`, `"inconsistent"`},
		"unknown quality":   {`"complete"`, `"future"`},
		"nonzero remainder": {`"unclassified_tokens":0`, `"unclassified_tokens":1`},
		"wrong input sum":   {`"uncached_tokens":100`, `"uncached_tokens":99`},
		"wrong output sum":  {`"non_reasoning_tokens":15`, `"non_reasoning_tokens":14`},
		"wrong total sum":   {`"total_tokens":470`, `"total_tokens":469`},
	} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(claudeAccounting, replacement[0], replacement[1], 1)
			assertBreakdownUnresolved(t, claudeReportedTokens(), "2", raw)
		})
	}
	for _, version := range []string{"", `null`, `"2"`, `2.5`, `1`, `3`, `{}`} {
		t.Run("envelope version/"+version, func(t *testing.T) {
			assertBreakdownUnresolved(t, claudeReportedTokens(), version, claudeAccounting)
		})
	}
	for name, mutate := range map[string]func(*tokenprocessor.TokenValues){
		"total conflict":       func(v *tokenprocessor.TokenValues) { v.TotalTokens = 471 },
		"input would shrink":   func(v *tokenprocessor.TokenValues) { v.InputTokens = 451 },
		"output would shrink":  func(v *tokenprocessor.TokenValues) { v.OutputTokens = 21 },
		"cache read conflict":  func(v *tokenprocessor.TokenValues) { v.CacheReadTokens = 301 },
		"explicit zero read":   func(v *tokenprocessor.TokenValues) { v.CacheReadTokens = 0 },
		"cache write conflict": func(v *tokenprocessor.TokenValues) { v.CacheCreationTokens = 51 },
		"reasoning conflict":   func(v *tokenprocessor.TokenValues) { v.ReasoningTokens = 6 },
		"negative raw count":   func(v *tokenprocessor.TokenValues) { v.InputTokens = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			values := claudeReportedTokens()
			mutate(&values)
			assertBreakdownUnresolved(t, values, "2", claudeAccounting)
		})
	}
}

func assertBreakdownUnresolved(t *testing.T, values tokenprocessor.TokenValues, version, raw string) {
	t.Helper()
	resolution, err := tokenprocessor.ResolveExecutorWithBreakdown("executorAdapter", values, json.RawMessage(version), json.RawMessage(raw))
	if err != nil || !reflect.DeepEqual(resolution, mustResolveExecutor(t, "executorAdapter")) {
		t.Fatalf("unusable accounting must leave the original identity fallback intact: %+v err=%v", resolution, err)
	}
}
