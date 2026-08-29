package providermetadata_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service/providermetadata"
)

// providerFetcherStub 为七个公开 endpoint 提供彼此独立的 result/error。
type providerFetcherStub struct {
	// codexResult 保存 Codex endpoint result。
	codexResult *response.ProviderKeyConfigResult
	// codexErr 保存 Codex endpoint error。
	codexErr error
	// xaiResult 保存 xAI endpoint result。
	xaiResult *response.ProviderKeyConfigResult
	// xaiErr 保存 xAI endpoint error。
	xaiErr error
	// geminiResult 保存普通 Gemini endpoint result。
	geminiResult *response.ProviderKeyConfigResult
	// geminiErr 保存普通 Gemini endpoint error。
	geminiErr error
	// interactionsResult 保存 Gemini Interactions endpoint result。
	interactionsResult *response.ProviderKeyConfigResult
	// interactionsErr 保存 Gemini Interactions endpoint error。
	interactionsErr error
	// claudeResult 保存 Claude endpoint result。
	claudeResult *response.ProviderKeyConfigResult
	// claudeErr 保存 Claude endpoint error。
	claudeErr error
	// vertexResult 保存 Vertex endpoint result。
	vertexResult *response.ProviderKeyConfigResult
	// vertexErr 保存 Vertex endpoint error。
	vertexErr error
	// openAIResult 保存 OpenAI Compatibility endpoint result。
	openAIResult *response.OpenAICompatibilityResult
	// openAIErr 保存 OpenAI Compatibility endpoint error。
	openAIErr error
}

// FetchCodexAPIKeys 返回测试配置的 Codex 结果。
func (s *providerFetcherStub) FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	// endpoint 间不共享状态，便于组合部分失败矩阵。
	return s.codexResult, s.codexErr
}

// FetchXAIAPIKeys 返回测试配置的 xAI 结果。
func (s *providerFetcherStub) FetchXAIAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	// endpoint 间不共享状态，便于验证 optional 404。
	return s.xaiResult, s.xaiErr
}

// FetchGeminiAPIKeys 返回测试配置的普通 Gemini 结果。
func (s *providerFetcherStub) FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	// endpoint 间不共享状态，便于验证 nil response。
	return s.geminiResult, s.geminiErr
}

// FetchInteractionsAPIKeys 返回测试配置的 Interactions 结果。
func (s *providerFetcherStub) FetchInteractionsAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	// endpoint 间不共享状态，便于验证 optional 404。
	return s.interactionsResult, s.interactionsErr
}

// FetchClaudeAPIKeys 返回测试配置的 Claude 结果。
func (s *providerFetcherStub) FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	// endpoint 间不共享状态，便于验证 transport/decode error。
	return s.claudeResult, s.claudeErr
}

// FetchVertexAPIKeys 返回测试配置的 Vertex 结果。
func (s *providerFetcherStub) FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	// endpoint 间不共享状态，便于验证成功空列表。
	return s.vertexResult, s.vertexErr
}

// FetchOpenAICompatibility 返回测试配置的 OpenAI Compatibility 结果。
func (s *providerFetcherStub) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
	// OpenAI 保留专属 result 类型，不压成标准 key result。
	return s.openAIResult, s.openAIErr
}

// successfulProviderFetcher 构造七个 200 空列表，调用方只覆盖当前场景需要的来源。
func successfulProviderFetcher() *providerFetcherStub {
	// 每个 result 都显式为 200，空 payload 仍表示来源成功抓取。
	return &providerFetcherStub{
		// Codex 默认成功空列表。
		codexResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// xAI 默认成功空列表。
		xaiResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// Gemini 默认成功空列表。
		geminiResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// Interactions 默认成功空列表。
		interactionsResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// Claude 默认成功空列表。
		claudeResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// Vertex 默认成功空列表。
		vertexResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// OpenAI 默认成功空列表。
		openAIResult: &response.OpenAICompatibilityResult{StatusCode: http.StatusOK},
	}
}

// TestFetchNormalizesSevenSourcesInRegistryOrder 验证七来源字段和稳定顺序。
func TestFetchNormalizesSevenSourcesInRegistryOrder(t *testing.T) {
	// priority 验证可选数值字段按指针原样传播。
	priority := 7
	// disabled 验证 false 指针不会被误当成缺失。
	disabled := false
	// note 验证可选备注按指针原样传播。
	note := "primary codex"
	// fetcher 从全成功空列表开始，只填充当前完整字段矩阵。
	fetcher := successfulProviderFetcher()
	// Codex 同时放入精确 auth-index 重复项和缺 auth-index 无效项。
	fetcher.codexResult.Payload = []providerconfig.ProviderKeyConfig{
		// 第一项必须胜出并保留全部标准字段。
		{APIKey: "codex-key", Prefix: "codex-prefix", Name: "Codex Team", BaseURL: "https://codex.example/v1", AuthIndex: "codex-auth", Priority: &priority, Disabled: &disabled, Note: &note},
		// 相同 auth-index 的后续项必须沿用当前首项保留规则。
		{APIKey: "codex-duplicate", Prefix: "duplicate-prefix", Name: "Duplicate", BaseURL: "https://duplicate.example/v1", AuthIndex: "codex-auth"},
		// 缺 auth-index 的条目不能生成 Credential。
		{APIKey: "codex-invalid", Name: "Invalid"},
	}
	// xAI 缺 name 时必须使用来源默认展示名。
	fetcher.xaiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "xai-key", Prefix: "xai-prefix", BaseURL: "https://api.x.ai/v1", AuthIndex: "xai-auth"}}
	// 普通 Gemini 缺 name 时必须保持原默认名 gemini。
	fetcher.geminiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "gemini-key", Prefix: "gemini-prefix", BaseURL: "https://gemini.example/v1", AuthIndex: "gemini-auth"}}
	// Interactions 缺 name 时必须使用独立产品名。
	fetcher.interactionsResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "interactions-key", Prefix: "interactions-prefix", BaseURL: "https://interactions.example/v1", AuthIndex: "interactions-auth"}}
	// Claude 自带 name 时必须优先于默认名。
	fetcher.claudeResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude Team", BaseURL: "https://claude.example/v1", AuthIndex: "claude-auth"}}
	// Vertex 缺 name 时必须保持原默认名 vertex。
	fetcher.vertexResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "vertex-key", Prefix: "vertex-prefix", BaseURL: "https://vertex.example/v1", AuthIndex: "vertex-auth"}}
	// OpenAI provider 层字段必须传播到有效 key entry，空 key entry 必须跳过。
	fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{{Name: "OpenRouter", Prefix: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "openai-key", AuthIndex: "openai-auth"}, {AuthIndex: "openai-invalid"}}}}

	// 通过唯一公开入口执行七来源归一化。
	snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
	// 全成功输入不允许产生 warning。
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	// wantTypes 锁定用户指定 registry 顺序。
	wantTypes := []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "openai"}
	// 实际 fetched types 必须与 registry 完全一致。
	if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
		t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
	}
	// wantCredentials 逐字段锁定七个有效 Credential 的来源和顺序。
	wantCredentials := []providermetadata.Credential{
		// Codex 保留自带 name 和全部可选字段。
		{LookupKey: "codex-key", Prefix: "codex-prefix", ProviderType: "codex", DisplayName: "Codex Team", AuthIndex: "codex-auth", BaseURL: "https://codex.example/v1", Priority: &priority, Disabled: &disabled, Note: &note},
		// xAI 使用默认展示名且保留原 provider type。
		{LookupKey: "xai-key", Prefix: "xai-prefix", ProviderType: "xai", DisplayName: "xAI", AuthIndex: "xai-auth", BaseURL: "https://api.x.ai/v1"},
		// 普通 Gemini 保持 gemini 类型和默认名。
		{LookupKey: "gemini-key", Prefix: "gemini-prefix", ProviderType: "gemini", DisplayName: "gemini", AuthIndex: "gemini-auth", BaseURL: "https://gemini.example/v1"},
		// Interactions 保持 gemini-interactions 类型和独立默认名。
		{LookupKey: "interactions-key", Prefix: "interactions-prefix", ProviderType: "gemini-interactions", DisplayName: "Gemini Interactions", AuthIndex: "interactions-auth", BaseURL: "https://interactions.example/v1"},
		// Claude 优先使用 CPA name。
		{LookupKey: "claude-key", Prefix: "claude-prefix", ProviderType: "claude", DisplayName: "Claude Team", AuthIndex: "claude-auth", BaseURL: "https://claude.example/v1"},
		// Vertex 保持 vertex 正确拼写。
		{LookupKey: "vertex-key", Prefix: "vertex-prefix", ProviderType: "vertex", DisplayName: "vertex", AuthIndex: "vertex-auth", BaseURL: "https://vertex.example/v1"},
		// OpenAI entry 接收 provider 层字段。
		{LookupKey: "openai-key", Prefix: "openrouter", ProviderType: "openai", DisplayName: "OpenRouter", AuthIndex: "openai-auth", BaseURL: "https://openrouter.ai/api/v1"},
	}
	// 归一化结果必须与完整字段 golden 完全一致。
	if !reflect.DeepEqual(snapshot.Credentials, wantCredentials) {
		t.Fatalf("Credentials = %#v, want %#v", snapshot.Credentials, wantCredentials)
	}
}

// TestFetchPropagatesOpenAIProviderFieldsToEveryValidEntry 验证 OpenAI provider 层字段、多 key 和缺省名。
func TestFetchPropagatesOpenAIProviderFieldsToEveryValidEntry(t *testing.T) {
	// priority 验证 provider 层数值传播到每个 entry。
	priority := 3
	// disabled 验证 provider 层 true 指针传播到每个 entry。
	disabled := true
	// note 验证 provider 层备注传播到每个 entry。
	note := "shared provider"
	// fetcher 默认其它六来源成功空列表。
	fetcher := successfulProviderFetcher()
	// OpenAI provider 故意缺 name，两个有效 entry 应共同使用默认名 openai。
	fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{{Prefix: "shared-prefix", BaseURL: "https://shared.example/v1", Priority: &priority, Disabled: &disabled, Note: &note, APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "first-key", AuthIndex: "first-auth"}, {APIKey: "second-key", AuthIndex: "second-auth"}, {APIKey: "missing-auth"}}}}

	// 通过公开 Fetch 观察 OpenAI 专属归一化结果。
	snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
	// 全成功输入不允许产生 warning。
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	// 两个有效 entry 必须各自生成一条 Credential。
	if len(snapshot.Credentials) != 2 {
		t.Fatalf("Credentials = %#v", snapshot.Credentials)
	}
	// 两条 Credential 必须共享 provider 层字段但保留各自 key/auth-index。
	for index, want := range []struct {
		// key 是当前 entry 的 lookup key。
		key string
		// authIndex 是当前 entry 的稳定 identity。
		authIndex string
	}{
		// 第一条 entry 的独立字段。
		{key: "first-key", authIndex: "first-auth"},
		// 第二条 entry 的独立字段。
		{key: "second-key", authIndex: "second-auth"},
	} {
		// credential 读取对应 entry 的归一化结果。
		credential := snapshot.Credentials[index]
		// provider 缺名时使用 openai，provider 层其它字段全部传播。
		if credential.LookupKey != want.key || credential.AuthIndex != want.authIndex || credential.ProviderType != "openai" || credential.DisplayName != "openai" || credential.Prefix != "shared-prefix" || credential.BaseURL != "https://shared.example/v1" || credential.Priority != &priority || credential.Disabled != &disabled || credential.Note != &note {
			t.Fatalf("credential[%d] = %#v", index, credential)
		}
	}
}

// TestFetchClassifiesOptionalFailuresAndSuccessfulEmptySources 锁定来源状态转换和 warning 顺序。
func TestFetchClassifiesOptionalFailuresAndSuccessfulEmptySources(t *testing.T) {
	// optional typed 404 必须静默跳过且不进入 fetched types。
	t.Run("optional typed 404", func(t *testing.T) {
		// fetcher 默认七来源成功空列表。
		fetcher := successfulProviderFetcher()
		// xAI 返回带状态码的 typed 404。
		fetcher.xaiResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		// xAI error 模拟旧 CPA 不支持 endpoint。
		fetcher.xaiErr = errors.New("xai endpoint unavailable")
		// Interactions 返回带状态码的 typed 404。
		fetcher.interactionsResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		// Interactions error 模拟旧 CPA 不支持 endpoint。
		fetcher.interactionsErr = errors.New("interactions endpoint unavailable")

		// 执行本轮来源状态归并。
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		// optional 404 不应返回 warning。
		if err != nil {
			t.Fatalf("Fetch returned error: %v", err)
		}
		// optional 来源不进入 fetched types，其余成功来源仍按 registry 顺序保留。
		wantTypes := []string{"codex", "gemini", "claude", "vertex", "openai"}
		// fetched types 必须精确匹配剩余成功来源。
		if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
			t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
		}
	})

	// 只有 typed result 的 404 才能按 optional 处理，不能解析 error 文本。
	t.Run("nil result containing 404 text", func(t *testing.T) {
		// fetcher 默认其它来源成功。
		fetcher := successfulProviderFetcher()
		// nil result 刻意移除 typed 状态码。
		fetcher.xaiResult = nil
		// error 文本包含 404，但不能触发 optional 分支。
		fetcher.xaiErr = errors.New("request returned status 404")

		// 执行本轮来源状态归并。
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		// nil result 的错误必须作为稳定 warning 返回。
		if err == nil || err.Error() != "fetch xai api keys: request returned status 404" {
			t.Fatalf("error = %v", err)
		}
		// 失败 xAI 不进入 fetched types。
		if reflect.DeepEqual(snapshot.FetchedProviderTypes, []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "openai"}) {
			t.Fatalf("xai unexpectedly marked fetched: %#v", snapshot.FetchedProviderTypes)
		}
	})

	// 多个既有来源失败时仍继续执行并按 registry 顺序合并 warning。
	t.Run("stable warning order", func(t *testing.T) {
		// fetcher 默认其它来源成功。
		fetcher := successfulProviderFetcher()
		// Codex 404 对既有 endpoint 仍是普通 warning。
		fetcher.codexResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		// Codex error 模拟旧 endpoint 失败。
		fetcher.codexErr = errors.New("codex missing")
		// Gemini nil response 无 error 仍是 warning。
		fetcher.geminiResult = nil
		// Claude 模拟 transport/decode failure。
		fetcher.claudeErr = errors.New("claude decode failed")

		// 执行本轮来源状态归并。
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		// warning 必须按 registry 中 codex、gemini、claude 的顺序连接。
		wantError := "fetch codex api keys: codex missing; gemini api keys response is nil; fetch claude api keys: claude decode failed"
		// 实际 warning 文本必须与稳定合同一致。
		if err == nil || err.Error() != wantError {
			t.Fatalf("error = %v, want %q", err, wantError)
		}
		// 失败来源不进入 fetched types，成功来源继续保留。
		wantTypes := []string{"xai", "gemini-interactions", "vertex", "openai"}
		// fetched types 必须精确匹配成功来源。
		if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
			t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
		}
	})

	// 成功空列表和全部无效条目都必须标记 fetched。
	t.Run("successful empty and invalid entries", func(t *testing.T) {
		// fetcher 默认所有来源成功空列表。
		fetcher := successfulProviderFetcher()
		// Gemini 返回非空但全部缺必填字段的 payload。
		fetcher.geminiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "missing-auth"}, {AuthIndex: "missing-key"}}

		// 执行本轮来源状态归并。
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		// 成功空列表和无效条目不产生 warning。
		if err != nil {
			t.Fatalf("Fetch returned error: %v", err)
		}
		// 七个成功 endpoint 都必须进入 fetched types。
		if len(snapshot.FetchedProviderTypes) != 7 {
			t.Fatalf("FetchedProviderTypes = %#v", snapshot.FetchedProviderTypes)
		}
		// 所有 payload 都为空或无效，因此不能生成 Credential。
		if len(snapshot.Credentials) != 0 {
			t.Fatalf("Credentials = %#v", snapshot.Credentials)
		}
	})
}
