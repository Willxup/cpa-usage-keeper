package providermetadata_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service/providermetadata"
)

type providerFetcherStub struct {
	codexResult        *response.ProviderKeyConfigResult
	codexErr           error
	xaiResult          *response.ProviderKeyConfigResult
	xaiErr             error
	geminiResult       *response.ProviderKeyConfigResult
	geminiErr          error
	interactionsResult *response.ProviderKeyConfigResult
	interactionsErr    error
	claudeResult       *response.ProviderKeyConfigResult
	claudeErr          error
	vertexResult       *response.ProviderKeyConfigResult
	vertexErr          error
	metaResult         *response.ProviderKeyConfigResult
	metaErr            error
	openAIResult       *response.OpenAICompatibilityResult
	openAIErr          error
}

func (s *providerFetcherStub) FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.codexResult, s.codexErr
}

func (s *providerFetcherStub) FetchXAIAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.xaiResult, s.xaiErr
}

func (s *providerFetcherStub) FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.geminiResult, s.geminiErr
}

func (s *providerFetcherStub) FetchInteractionsAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.interactionsResult, s.interactionsErr
}

func (s *providerFetcherStub) FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.claudeResult, s.claudeErr
}

func (s *providerFetcherStub) FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.vertexResult, s.vertexErr
}

func (s *providerFetcherStub) FetchMetaAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.metaResult, s.metaErr
}

func (s *providerFetcherStub) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
	return s.openAIResult, s.openAIErr
}

func successfulProviderFetcher() *providerFetcherStub {
	return &providerFetcherStub{
		codexResult:        &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		xaiResult:          &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		geminiResult:       &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		interactionsResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		claudeResult:       &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		vertexResult:       &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		metaResult:         &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		openAIResult:       &response.OpenAICompatibilityResult{StatusCode: http.StatusOK},
	}
}

func TestFetchNormalizesEightSourcesInRegistryOrder(t *testing.T) {
	priority := 7
	disabled := false
	note := "primary codex"
	fetcher := successfulProviderFetcher()
	// Codex 同时放入精确 auth-index 重复项和空 Key 过滤项。
	fetcher.codexResult.Payload = []providerconfig.ProviderKeyConfig{
		{APIKey: "codex-key", Prefix: "codex-prefix", Name: "Codex Team", BaseURL: "https://codex.example/v1", AuthIndex: "codex-auth", Priority: &priority, Disabled: &disabled, Note: &note},
		{APIKey: "codex-duplicate", Prefix: "duplicate-prefix", Name: "Duplicate", BaseURL: "https://duplicate.example/v1", AuthIndex: "codex-auth"},
		{APIKey: " \t", Name: "Invalid"},
	}
	fetcher.xaiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "xai-key", Prefix: "xai-prefix", BaseURL: "https://api.x.ai/v1", AuthIndex: "xai-auth"}}
	fetcher.geminiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "gemini-key", Prefix: "gemini-prefix", BaseURL: "https://gemini.example/v1", AuthIndex: "gemini-auth"}}
	fetcher.interactionsResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "interactions-key", Prefix: "interactions-prefix", BaseURL: "https://interactions.example/v1", AuthIndex: "interactions-auth"}}
	fetcher.claudeResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude Team", BaseURL: "https://claude.example/v1", AuthIndex: "claude-auth"}}
	fetcher.vertexResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "vertex-key", Prefix: "vertex-prefix", BaseURL: "https://vertex.example/v1", AuthIndex: "vertex-auth"}}
	fetcher.metaResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "meta-key", Prefix: "meta-prefix", BaseURL: "https://meta.example/v1", AuthIndex: "meta-auth", Priority: &priority, Disabled: &disabled, Note: &note}}
	fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{{Name: "OpenRouter", Prefix: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "openai-key", AuthIndex: "openai-auth"}, {AuthIndex: "openai-invalid"}}}}

	snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	wantTypes := registrySourceOrder
	if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
		t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
	}
	wantCredentials := []providermetadata.Credential{
		{LookupKey: "codex-key", Prefix: "codex-prefix", ProviderType: "codex", DisplayName: "Codex Team", AuthIndex: "codex-auth", BaseURL: "https://codex.example/v1", Priority: &priority, Disabled: &disabled, Note: &note},
		{LookupKey: "xai-key", Prefix: "xai-prefix", ProviderType: "xai", DisplayName: "xAI", AuthIndex: "xai-auth", BaseURL: "https://api.x.ai/v1"},
		{LookupKey: "gemini-key", Prefix: "gemini-prefix", ProviderType: "gemini", DisplayName: "gemini", AuthIndex: "gemini-auth", BaseURL: "https://gemini.example/v1"},
		{LookupKey: "interactions-key", Prefix: "interactions-prefix", ProviderType: "gemini-interactions", DisplayName: "Gemini Interactions", AuthIndex: "interactions-auth", BaseURL: "https://interactions.example/v1"},
		{LookupKey: "claude-key", Prefix: "claude-prefix", ProviderType: "claude", DisplayName: "Claude Team", AuthIndex: "claude-auth", BaseURL: "https://claude.example/v1"},
		{LookupKey: "vertex-key", Prefix: "vertex-prefix", ProviderType: "vertex", DisplayName: "vertex", AuthIndex: "vertex-auth", BaseURL: "https://vertex.example/v1"},
		{LookupKey: "meta-key", Prefix: "meta-prefix", ProviderType: "meta", DisplayName: "Meta", AuthIndex: "meta-auth", BaseURL: "https://meta.example/v1", Priority: &priority, Disabled: &disabled, Note: &note},
		{LookupKey: "openai-key", Prefix: "openrouter", ProviderType: "openai", DisplayName: "OpenRouter", AuthIndex: "openai-auth", BaseURL: "https://openrouter.ai/api/v1"},
	}
	if !reflect.DeepEqual(snapshot.Credentials, wantCredentials) {
		t.Fatalf("Credentials = %#v, want %#v", snapshot.Credentials, wantCredentials)
	}
}

func TestFetchPropagatesOpenAIProviderFieldsToEveryValidEntry(t *testing.T) {
	priority := 3
	disabled := true
	note := "shared provider"
	fetcher := successfulProviderFetcher()
	// OpenAI provider 故意缺 name，两个有效 entry 应共同使用默认名 openai。
	fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{{Prefix: "shared-prefix", BaseURL: "https://shared.example/v1", Priority: &priority, Disabled: &disabled, Note: &note, APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "first-key", AuthIndex: "first-auth"}, {APIKey: "second-key", AuthIndex: "second-auth"}, {APIKey: " \t"}}}}

	snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(snapshot.Credentials) != 2 {
		t.Fatalf("Credentials = %#v", snapshot.Credentials)
	}
	for index, want := range []struct {
		key       string
		authIndex string
	}{
		{key: "first-key", authIndex: "first-auth"},
		{key: "second-key", authIndex: "second-auth"},
	} {
		credential := snapshot.Credentials[index]
		if credential.LookupKey != want.key || credential.AuthIndex != want.authIndex || credential.ProviderType != "openai" || credential.DisplayName != "openai" || credential.Prefix != "shared-prefix" || credential.BaseURL != "https://shared.example/v1" || credential.Priority != &priority || credential.Disabled != &disabled || credential.Note != &note {
			t.Fatalf("credential[%d] = %#v", index, credential)
		}
	}
}

func TestFetchFiltersIncompleteOpenAIEntries(t *testing.T) {
	for _, state := range []struct {
		name     string
		disabled bool
	}{
		{name: "enabled", disabled: false},
		{name: "disabled", disabled: true},
	} {
		for _, mixed := range []bool{true, false} {
			name := "all skipped"
			if mixed {
				name = "mixed"
			}
			t.Run(state.name+"/"+name, func(t *testing.T) {
				entries := []providerconfig.OpenAIApiKeyEntry{
					{APIKey: "missing-auth-key"},
					{APIKey: "blank-auth-key", AuthIndex: " \t"},
					{AuthIndex: "empty-key-auth"},
					{APIKey: " \t", AuthIndex: "blank-key-auth"},
				}
				var want []providermetadata.Credential
				if mixed {
					// 有效成员位于无效成员两侧，不能被缺标识的成员中断。
					entries = append([]providerconfig.OpenAIApiKeyEntry{{APIKey: " first-key ", AuthIndex: " first-auth "}}, entries...)
					entries = append(entries, providerconfig.OpenAIApiKeyEntry{APIKey: "second-key", AuthIndex: "second-auth"})
					want = []providermetadata.Credential{
						{LookupKey: "first-key", AuthIndex: "first-auth", ProviderType: "openai", DisplayName: "OpenAI Team", Disabled: &state.disabled},
						{LookupKey: "second-key", AuthIndex: "second-auth", ProviderType: "openai", DisplayName: "OpenAI Team", Disabled: &state.disabled},
					}
				}
				fetcher := successfulProviderFetcher()
				fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{{Name: "OpenAI Team", Disabled: &state.disabled, APIKeyEntries: entries}}

				snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
				if err != nil {
					t.Fatalf("Fetch returned error: %v", err)
				}
				if !reflect.DeepEqual(snapshot.FetchedProviderTypes, registrySourceOrder) {
					t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, registrySourceOrder)
				}
				if !reflect.DeepEqual(snapshot.Credentials, want) {
					t.Fatalf("Credentials = %#v, want %#v", snapshot.Credentials, want)
				}
			})
		}
	}
}

func TestFetchRejectsWholeSourceWhenNonEmptyKeyHasNoAuthIndex(t *testing.T) {
	for _, source := range registrySourceOrder {
		if source == "openai" {
			continue
		}
		for _, authIndex := range []string{"", " \t"} {
			name := "missing"
			if authIndex != "" {
				name = "blank"
			}
			t.Run(source+"/"+name, func(t *testing.T) {
				fetcher := successfulProviderFetcher()
				standardResults := map[string]*response.ProviderKeyConfigResult{
					"codex": fetcher.codexResult, "xai": fetcher.xaiResult, "gemini": fetcher.geminiResult,
					"gemini-interactions": fetcher.interactionsResult, "claude": fetcher.claudeResult,
					"vertex": fetcher.vertexResult, "meta": fetcher.metaResult,
				}
				// 合法条目先出现，随后缺标识的条目必须让整个来源失败，不能返回部分凭证。
				standardResults[source].Payload = []providerconfig.ProviderKeyConfig{
					{APIKey: "valid-key", AuthIndex: "valid-auth"},
					{APIKey: " do-not-expose-api-key ", AuthIndex: authIndex},
				}
				snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
				if err == nil || !strings.Contains(err.Error(), "auth_index") || strings.Contains(err.Error(), "do-not-expose-api-key") {
					t.Errorf("warning = %v", err)
				}
				if slices.Contains(snapshot.FetchedProviderTypes, source) || len(snapshot.FetchedProviderTypes) != 7 || len(snapshot.Credentials) != 0 {
					t.Errorf("source %s fetched = %v, fetched count = %d, credentials = %d", source, slices.Contains(snapshot.FetchedProviderTypes, source), len(snapshot.FetchedProviderTypes), len(snapshot.Credentials))
				}
			})
		}
	}
}

func TestFetchClassifiesFailuresAndSuccessfulEmptySources(t *testing.T) {
	t.Run("404 failures remain independent", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.xaiResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		fetcher.xaiErr = errors.New("xai endpoint unavailable")
		fetcher.interactionsResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		fetcher.interactionsErr = errors.New("interactions endpoint unavailable")
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		if err == nil {
			t.Fatal("failed sources returned no warning")
		}
		wantTypes := []string{"codex", "gemini", "claude", "vertex", "meta", "openai"}
		if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
			t.Fatalf("FetchedProviderTypes = %#v", snapshot.FetchedProviderTypes)
		}
	})

	// 请求错误文本不能被当成成功空来源。
	t.Run("nil result containing 404 text", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.xaiResult = nil
		fetcher.xaiErr = errors.New("request returned status 404")

		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		if err == nil || err.Error() != "fetch xai api keys: request returned status 404" {
			t.Fatalf("error = %v", err)
		}
		if slices.Contains(snapshot.FetchedProviderTypes, "xai") {
			t.Fatalf("xai unexpectedly marked fetched: %#v", snapshot.FetchedProviderTypes)
		}
	})

	t.Run("stable warning order", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.codexResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		fetcher.codexErr = errors.New("codex missing")
		fetcher.geminiResult = nil
		fetcher.claudeErr = errors.New("claude decode failed")

		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		wantError := "fetch codex api keys: codex missing; gemini api keys response is nil; fetch claude api keys: claude decode failed"
		if err == nil || err.Error() != wantError {
			t.Fatalf("error = %v, want %q", err, wantError)
		}
		wantTypes := []string{"xai", "gemini-interactions", "vertex", "meta", "openai"}
		if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
			t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
		}
	})

	t.Run("successful empty and blank keys", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.geminiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: " \t"}, {AuthIndex: "missing-key"}}
		fetcher.metaResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: " \t"}, {AuthIndex: "meta-missing-key"}}

		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		if err != nil {
			t.Fatalf("Fetch returned error: %v", err)
		}
		if len(snapshot.FetchedProviderTypes) != 8 {
			t.Fatalf("FetchedProviderTypes = %#v", snapshot.FetchedProviderTypes)
		}
		if len(snapshot.Credentials) != 0 {
			t.Fatalf("Credentials = %#v", snapshot.Credentials)
		}
	})
}
