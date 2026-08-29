package cpa_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
)

// providerEndpointResult 统一承载标准 API Key 与 OpenAI Compatibility 两种 client 返回，便于七来源共享黑盒断言。
type providerEndpointResult struct {
	// statusCode 保留 client 返回的 HTTP 状态码，供成功与 typed 404 场景共同验证。
	statusCode int
	// body 保留 client 捕获的原始响应体，证明现有 HTTP 包装合同没有丢失。
	body []byte
	// keys 只承载六类标准 API Key endpoint 的归一化条目。
	keys []providerconfig.ProviderKeyConfig
	// compatibility 只承载 OpenAI Compatibility 的 provider 层与多 key 结构。
	compatibility []providerconfig.OpenAICompatibilityConfig
}

// providerEndpointCase 描述一个 CPA management endpoint 及其 direct/wrapped 两种成功响应。
type providerEndpointCase struct {
	// name 是测试子用例名，也是 fetch helper 选择公开 client 方法的稳定标识。
	name string
	// path 是 Keeper 必须请求的 CPA management 路径。
	path string
	// directBody 验证旧版或兼容 CPA 直接返回数组时仍可解码。
	directBody string
	// wrappedBody 验证当前 CPA 使用配置段字段包裹数组时仍可解码。
	wrappedBody string
	// wantOpenAI 区分 OpenAI 专属 DTO 与六类标准 key DTO。
	wantOpenAI bool
}

// TestProviderAPIKeyClientsUseDedicatedEndpointsAndDecodePayloads 锁定七来源路径、认证和两种 payload 形态。
func TestProviderAPIKeyClientsUseDedicatedEndpointsAndDecodePayloads(t *testing.T) {
	// cases 按用户指定 registry 顺序列出，避免测试结构重新引入旧顺序。
	cases := []providerEndpointCase{
		// Codex 继续使用原有专属 management endpoint。
		{name: "codex", path: "/v0/management/codex-api-key", directBody: `[{"api-key":"codex-key","prefix":"codex-prefix","base-url":"https://codex.example/v1","name":"Codex","auth-index":"codex-auth"}]`, wrappedBody: `{"codex-api-key":[{"api-key":"codex-key","prefix":"codex-prefix","base-url":"https://codex.example/v1","name":"Codex","auth-index":"codex-auth"}]}`},
		// xAI 新 endpoint 允许 websockets 等 Keeper 不消费的额外字段存在。
		{name: "xai", path: "/v0/management/xai-api-key", directBody: `[{"api-key":"xai-key","prefix":"xai-prefix","base-url":"https://api.x.ai/v1","name":"xAI","websockets":true,"auth-index":"xai-auth"}]`, wrappedBody: `{"xai-api-key":[{"api-key":"xai-key","prefix":"xai-prefix","base-url":"https://api.x.ai/v1","name":"xAI","websockets":true,"auth-index":"xai-auth"}]}`},
		// Gemini 继续使用原有专属 management endpoint。
		{name: "gemini", path: "/v0/management/gemini-api-key", directBody: `[{"api-key":"gemini-key","prefix":"gemini-prefix","base-url":"https://gemini.example/v1","name":"Gemini","auth-index":"gemini-auth"}]`, wrappedBody: `{"gemini-api-key":[{"api-key":"gemini-key","prefix":"gemini-prefix","base-url":"https://gemini.example/v1","name":"Gemini","auth-index":"gemini-auth"}]}`},
		// Gemini Interactions 必须使用独立配置段和 endpoint，不能并入普通 Gemini。
		{name: "gemini-interactions", path: "/v0/management/interactions-api-key", directBody: `[{"api-key":"interactions-key","prefix":"interactions-prefix","base-url":"https://interactions.example/v1","name":"Interactions","auth-index":"interactions-auth"}]`, wrappedBody: `{"interactions-api-key":[{"api-key":"interactions-key","prefix":"interactions-prefix","base-url":"https://interactions.example/v1","name":"Interactions","auth-index":"interactions-auth"}]}`},
		// Claude 继续使用原有专属 management endpoint。
		{name: "claude", path: "/v0/management/claude-api-key", directBody: `[{"api-key":"claude-key","prefix":"claude-prefix","base-url":"https://claude.example/v1","name":"Claude","auth-index":"claude-auth"}]`, wrappedBody: `{"claude-api-key":[{"api-key":"claude-key","prefix":"claude-prefix","base-url":"https://claude.example/v1","name":"Claude","auth-index":"claude-auth"}]}`},
		// Vertex 继续使用原有专属 management endpoint。
		{name: "vertex", path: "/v0/management/vertex-api-key", directBody: `[{"api-key":"vertex-key","prefix":"vertex-prefix","base-url":"https://vertex.example/v1","name":"Vertex","auth-index":"vertex-auth"}]`, wrappedBody: `{"vertex-api-key":[{"api-key":"vertex-key","prefix":"vertex-prefix","base-url":"https://vertex.example/v1","name":"Vertex","auth-index":"vertex-auth"}]}`},
		// OpenAI Compatibility 继续使用 provider 层字段加多 key entry 的专属 DTO。
		{name: "openai", path: "/v0/management/openai-compatibility", directBody: `[{"name":"OpenRouter","prefix":"openrouter","base-url":"https://openrouter.ai/api/v1","api-key-entries":[{"api-key":"openai-key","auth-index":"openai-auth"}]}]`, wrappedBody: `{"openai-compatibility":[{"id":"OpenRouter","prefix":"openrouter","base-url":"https://openrouter.ai/api/v1","api-key-entries":[{"key":"openai-key","auth_index":"openai-auth"}]}]}`, wantOpenAI: true},
	}

	// 遍历七个来源，确保每个公开 fetch 方法都受到同一 HTTP 合同约束。
	for _, tc := range cases {
		// 复制循环变量，避免子测试闭包读取下一轮内容。
		tc := tc
		// 为每个来源分别验证 direct 和 wrapped 两种成功响应。
		for _, variant := range []struct {
			// name 标识响应形态，方便失败时快速定位兼容分支。
			name string
			// body 是测试服务返回给 Keeper client 的原始 JSON。
			body string
		}{
			// direct 覆盖顶层数组。
			{name: "direct", body: tc.directBody},
			// wrapped 覆盖 CPA 配置段对象。
			{name: "wrapped", body: tc.wrappedBody},
		} {
			// 复制响应变体，避免子测试闭包共享循环变量。
			variant := variant
			// 子测试名同时包含来源与响应形态。
			t.Run(tc.name+"/"+variant.name, func(t *testing.T) {
				// server 只接受该来源应使用的 GET management 请求。
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// management 配置读取必须保持 GET。
					if r.Method != http.MethodGet {
						t.Fatalf("method = %q, want GET", r.Method)
					}
					// 每个来源必须请求自己的 dedicated endpoint。
					if r.URL.Path != tc.path {
						t.Fatalf("path = %q, want %q", r.URL.Path, tc.path)
					}
					// 所有 management endpoint 必须继续使用 Bearer management key。
					if got := r.Header.Get("Authorization"); got != "Bearer management-secret" {
						t.Fatalf("Authorization = %q", got)
					}
					// 返回当前响应形态，交给真实 client 解码。
					_, _ = w.Write([]byte(variant.body))
				}))
				// 测试结束关闭临时 HTTP server。
				defer server.Close()

				// 使用公开构造器创建与生产相同的 CPA client。
				client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
				// 通过公开 fetch 方法取得统一测试结果。
				result, err := fetchProviderEndpoint(context.Background(), client, tc.name)
				// 成功 payload 不允许返回错误。
				if err != nil {
					t.Fatalf("fetch endpoint: %v", err)
				}
				// client 必须保留成功状态码和原始响应体。
				if result.statusCode != http.StatusOK || len(result.body) == 0 {
					t.Fatalf("result metadata = status:%d body:%q", result.statusCode, string(result.body))
				}
				// OpenAI 来源使用专属 provider 与 entry 断言。
				if tc.wantOpenAI {
					// provider 层字段和 key entry 都必须完成兼容归一化。
					if len(result.compatibility) != 1 || result.compatibility[0].Name != "OpenRouter" || result.compatibility[0].Prefix != "openrouter" || result.compatibility[0].BaseURL != "https://openrouter.ai/api/v1" || len(result.compatibility[0].APIKeyEntries) != 1 || result.compatibility[0].APIKeyEntries[0].APIKey != "openai-key" || result.compatibility[0].APIKeyEntries[0].AuthIndex != "openai-auth" {
						t.Fatalf("openai payload = %#v", result.compatibility)
					}
					// OpenAI 专属断言完成后不再检查标准 key slice。
					return
				}
				// 六类标准来源都必须保留 key、prefix、base URL、name 与 auth-index。
				if len(result.keys) != 1 || result.keys[0].APIKey == "" || result.keys[0].Prefix == "" || result.keys[0].BaseURL == "" || result.keys[0].Name == "" || result.keys[0].AuthIndex == "" {
					t.Fatalf("provider payload = %#v", result.keys)
				}
			})
		}
	}
}

// TestProviderAPIKeyClientClassifiesEmptyAndBlankBodies 锁定成功空列表与空白 HTTP body 的不同语义。
func TestProviderAPIKeyClientClassifiesEmptyAndBlankBodies(t *testing.T) {
	// cases 覆盖 client 兼容的三种 JSON 空列表和一个不可解码的空白 body。
	cases := []struct {
		// name 标识空响应形态。
		name string
		// body 是临时 CPA server 返回的原始响应体。
		body string
		// wantError 表示该响应应保留 decode error 而不是成功空列表。
		wantError bool
	}{
		// direct array 是成功空列表。
		{name: "direct-array", body: `[]`},
		// JSON null 解码为 nil slice，也属于成功空列表。
		{name: "direct-null", body: `null`},
		// wrapped empty array 表示该来源已成功同步但没有凭证。
		{name: "wrapped-array", body: `{"xai-api-key":[]}`},
		// 2xx 空白 body 没有合法 JSON，继续返回 decode error。
		{name: "blank-body", body: "   \n", wantError: true},
	}

	// 每种空响应都通过同一个新 xAI fetch 方法验证共享 decoder。
	for _, tc := range cases {
		// 复制循环变量，避免子测试闭包共享状态。
		tc := tc
		// 用独立 server 验证当前空响应分类。
		t.Run(tc.name, func(t *testing.T) {
			// server 返回测试指定的 2xx body。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// 把空响应原样写给 client。
				_, _ = w.Write([]byte(tc.body))
			}))
			// 测试结束关闭临时 server。
			defer server.Close()

			// 创建真实 client，避免直接测试私有 decoder。
			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			// 调用新 xAI endpoint 方法触发共享解码路径。
			result, err := client.FetchXAIAPIKeys(context.Background())
			// blank body 必须返回错误且保留非 nil result。
			if tc.wantError {
				// 空白 body 不能被误判为成功空列表。
				if err == nil || result == nil {
					t.Fatalf("blank body result=%#v err=%v", result, err)
				}
				// 错误分支验证完成后直接返回。
				return
			}
			// 合法 JSON 空列表必须成功。
			if err != nil {
				t.Fatalf("empty payload: %v", err)
			}
			// 成功空列表必须保留 200 且 payload 长度为零。
			if result.StatusCode != http.StatusOK || len(result.Payload) != 0 {
				t.Fatalf("empty result = %#v", result)
			}
		})
	}
}

// TestOptionalProviderEndpointsReturnTyped404WithoutLeakingBody 验证两个新 endpoint 能由 source 层按状态码识别旧 CPA。
func TestOptionalProviderEndpointsReturnTyped404WithoutLeakingBody(t *testing.T) {
	// fetchers 只包含两个被定义为 optional 的新 endpoint。
	fetchers := []struct {
		// name 标识 optional 来源。
		name string
		// fetch 调用对应公开 client 方法并统一返回 HTTP 元数据。
		fetch func(context.Context, *cpa.Client) (providerEndpointResult, error)
	}{
		// Interactions 404 必须保留 typed result。
		{name: "gemini-interactions", fetch: func(ctx context.Context, client *cpa.Client) (providerEndpointResult, error) {
			// 调用 Interactions 专属 client 方法。
			result, err := client.FetchInteractionsAPIKeys(ctx)
			// 把真实 result 转成测试统一结构。
			return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
		}},
		// xAI 404 必须保留 typed result。
		{name: "xai", fetch: func(ctx context.Context, client *cpa.Client) (providerEndpointResult, error) {
			// 调用 xAI 专属 client 方法。
			result, err := client.FetchXAIAPIKeys(ctx)
			// 把真实 result 转成测试统一结构。
			return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
		}},
	}

	// 分别验证两个新 endpoint 的 typed 404 合同。
	for _, tc := range fetchers {
		// 复制循环变量，避免子测试闭包共享函数值。
		tc := tc
		// 每个来源使用独立 server 返回同一敏感错误体。
		t.Run(tc.name, func(t *testing.T) {
			// server 返回旧 CPA 不支持 endpoint 时的 404。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// 设置 404 状态码供 client 包装。
				w.WriteHeader(http.StatusNotFound)
				// 错误体包含唯一文本，用于证明 error 不泄漏 response body。
				_, _ = w.Write([]byte(`{"secret":"body-must-not-leak"}`))
			}))
			// 测试结束关闭临时 server。
			defer server.Close()

			// 创建真实 client 以验证公共 HTTP error 包装。
			client := cpa.NewClient(server.URL, "management-secret", 2*time.Second, false)
			// 调用当前 optional endpoint。
			result, err := tc.fetch(context.Background(), client)
			// 404 必须同时返回 error 和带状态码的非零结果。
			if err == nil || result.statusCode != http.StatusNotFound {
				t.Fatalf("typed 404 result=%#v err=%v", result, err)
			}
			// source 层只依赖状态码，error 文本不能携带 response body。
			if strings.Contains(err.Error(), "body-must-not-leak") {
				t.Fatalf("error leaked response body: %v", err)
			}
		})
	}
}

// fetchProviderEndpoint 只为黑盒测试调用七个公开 client 方法并统一返回结构。
func fetchProviderEndpoint(ctx context.Context, client *cpa.Client, source string) (providerEndpointResult, error) {
	// source 与测试表的七项稳定标识一一对应。
	switch source {
	case "codex":
		// Codex 使用标准 key result。
		result, err := client.FetchCodexAPIKeys(ctx)
		// 返回 Codex HTTP 元数据与 payload。
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "xai":
		// xAI 使用新增标准 key result。
		result, err := client.FetchXAIAPIKeys(ctx)
		// 返回 xAI HTTP 元数据与 payload。
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "gemini":
		// Gemini 使用标准 key result。
		result, err := client.FetchGeminiAPIKeys(ctx)
		// 返回 Gemini HTTP 元数据与 payload。
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "gemini-interactions":
		// Interactions 使用新增标准 key result。
		result, err := client.FetchInteractionsAPIKeys(ctx)
		// 返回 Interactions HTTP 元数据与 payload。
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "claude":
		// Claude 使用标准 key result。
		result, err := client.FetchClaudeAPIKeys(ctx)
		// 返回 Claude HTTP 元数据与 payload。
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "vertex":
		// Vertex 使用标准 key result。
		result, err := client.FetchVertexAPIKeys(ctx)
		// 返回 Vertex HTTP 元数据与 payload。
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, keys: result.Payload}, err
	case "openai":
		// OpenAI Compatibility 使用专属 result。
		result, err := client.FetchOpenAICompatibility(ctx)
		// 返回 OpenAI HTTP 元数据与专属 payload。
		return providerEndpointResult{statusCode: result.StatusCode, body: result.Body, compatibility: result.Payload}, err
	default:
		// 测试表出现未知来源属于测试编排错误。
		return providerEndpointResult{}, &unknownProviderSourceError{source: source}
	}
}

// unknownProviderSourceError 让未知测试来源返回可读错误而不是 panic。
type unknownProviderSourceError struct {
	// source 保存未注册的测试来源名。
	source string
}

// Error 返回未知来源的稳定错误文本。
func (e *unknownProviderSourceError) Error() string {
	// 错误文本只包含来源名，不包含任何 API Key。
	return "unknown provider source: " + e.source
}
