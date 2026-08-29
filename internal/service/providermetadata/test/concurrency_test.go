package providermetadata_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service/providermetadata"
)

// registrySourceOrder 复用用户指定的稳定七来源顺序。
var registrySourceOrder = []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "openai"}

// gatedProviderFetcher 用独立 gate 控制七个 endpoint 的进入和完成时序。
type gatedProviderFetcher struct {
	// entered 记录每个 endpoint 已开始执行。
	entered chan string
	// done 记录每个 endpoint 已返回结果。
	done chan string
	// gates 让测试按来源精确释放 endpoint，不依赖 sleep 排序。
	gates map[string]chan struct{}
	// errors 为指定来源注入 fetch error。
	errors map[string]error
	// closeOnce 保证测试清理时每个 gate 只关闭一次。
	closeOnce map[string]*sync.Once
}

// newGatedProviderFetcher 为七个来源创建独立 gate 和足量事件缓冲。
func newGatedProviderFetcher() *gatedProviderFetcher {
	// fetcher 保存所有并发测试共享的 channel 状态。
	fetcher := &gatedProviderFetcher{
		// entered 缓冲七项，避免测试清理时发送阻塞。
		entered: make(chan string, len(registrySourceOrder)),
		// done 缓冲七项，避免 Fetch 等待测试接收。
		done: make(chan string, len(registrySourceOrder)),
		// gates 按来源名定位释放通道。
		gates: make(map[string]chan struct{}, len(registrySourceOrder)),
		// errors 默认没有来源失败。
		errors: make(map[string]error),
		// closeOnce 为每个 gate 提供幂等关闭。
		closeOnce: make(map[string]*sync.Once, len(registrySourceOrder)),
	}
	// 为固定 registry 的每个来源创建独立 gate。
	for _, source := range registrySourceOrder {
		// 当前来源默认保持阻塞直到测试显式释放。
		fetcher.gates[source] = make(chan struct{})
		// 当前来源获得独立的关闭保护。
		fetcher.closeOnce[source] = &sync.Once{}
	}
	// 返回可复用的并发 fetcher。
	return fetcher
}

// release 只释放指定来源一次。
func (f *gatedProviderFetcher) release(source string) {
	// sync.Once 避免正常路径和 Cleanup 重复关闭 channel。
	f.closeOnce[source].Do(func() {
		// 关闭 gate 让当前 endpoint 立即继续。
		close(f.gates[source])
	})
}

// releaseAll 解除所有来源阻塞，保证失败测试也不泄漏 goroutine。
func (f *gatedProviderFetcher) releaseAll() {
	// 按固定 registry 遍历所有 gate。
	for _, source := range registrySourceOrder {
		// 每个 gate 通过幂等 helper 关闭。
		f.release(source)
	}
}

// wait 在当前来源进入后等待 gate 或 caller context。
func (f *gatedProviderFetcher) wait(ctx context.Context, source string) error {
	// 通知测试当前 endpoint 已真正开始执行。
	f.entered <- source
	// gate 与 context 竞争，模拟正常完成或调用方取消。
	select {
	case <-f.gates[source]:
		// gate 释放后读取该来源预设 error。
	case <-ctx.Done():
		// context 取消时记录完成，证明 goroutine 可退出。
		f.done <- source
		// 返回真实 context error 供 source 生成 warning。
		return ctx.Err()
	}
	// 读取当前来源预设的独立 error。
	err := f.errors[source]
	// 通知测试当前 endpoint 已完成。
	f.done <- source
	// 返回预设 error 或 nil。
	return err
}

// FetchCodexAPIKeys 在 codex gate 释放后返回一条标准 Credential 输入。
func (f *gatedProviderFetcher) FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 等待测试释放 Codex 或取消 context。
	if err := f.wait(ctx, "codex"); err != nil {
		return nil, err
	}
	// 返回带唯一 auth-index 的成功 Codex payload。
	return standardSuccessResult("codex"), nil
}

// FetchXAIAPIKeys 在 xai gate 释放后返回一条标准 Credential 输入。
func (f *gatedProviderFetcher) FetchXAIAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 等待测试释放 xAI 或取消 context。
	if err := f.wait(ctx, "xai"); err != nil {
		return nil, err
	}
	// 返回带唯一 auth-index 的成功 xAI payload。
	return standardSuccessResult("xai"), nil
}

// FetchGeminiAPIKeys 在 gemini gate 释放后返回一条标准 Credential 输入。
func (f *gatedProviderFetcher) FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 等待测试释放 Gemini 或取消 context。
	if err := f.wait(ctx, "gemini"); err != nil {
		return nil, err
	}
	// 返回带唯一 auth-index 的成功 Gemini payload。
	return standardSuccessResult("gemini"), nil
}

// FetchInteractionsAPIKeys 在 interactions gate 释放后返回一条标准 Credential 输入。
func (f *gatedProviderFetcher) FetchInteractionsAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 等待测试释放 Interactions 或取消 context。
	if err := f.wait(ctx, "gemini-interactions"); err != nil {
		return nil, err
	}
	// 返回带唯一 auth-index 的成功 Interactions payload。
	return standardSuccessResult("gemini-interactions"), nil
}

// FetchClaudeAPIKeys 在 claude gate 释放后返回一条标准 Credential 输入。
func (f *gatedProviderFetcher) FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 等待测试释放 Claude 或取消 context。
	if err := f.wait(ctx, "claude"); err != nil {
		return nil, err
	}
	// 返回带唯一 auth-index 的成功 Claude payload。
	return standardSuccessResult("claude"), nil
}

// FetchVertexAPIKeys 在 vertex gate 释放后返回一条标准 Credential 输入。
func (f *gatedProviderFetcher) FetchVertexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	// 等待测试释放 Vertex 或取消 context。
	if err := f.wait(ctx, "vertex"); err != nil {
		return nil, err
	}
	// 返回带唯一 auth-index 的成功 Vertex payload。
	return standardSuccessResult("vertex"), nil
}

// FetchOpenAICompatibility 在 openai gate 释放后返回一条专属 Credential 输入。
func (f *gatedProviderFetcher) FetchOpenAICompatibility(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
	// 等待测试释放 OpenAI 或取消 context。
	if err := f.wait(ctx, "openai"); err != nil {
		return nil, err
	}
	// 返回带唯一 auth-index 的成功 OpenAI provider 与 entry。
	return &response.OpenAICompatibilityResult{Payload: []providerconfig.OpenAICompatibilityConfig{{Name: "openai", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "openai-key", AuthIndex: "openai-auth"}}}}}, nil
}

// standardSuccessResult 为标准来源生成稳定 key/name/auth-index 测试 payload。
func standardSuccessResult(source string) *response.ProviderKeyConfigResult {
	// 每个来源的字段都包含 source 名，便于验证最终 registry 顺序。
	return &response.ProviderKeyConfigResult{Payload: []providerconfig.ProviderKeyConfig{{APIKey: source + "-key", Name: source, AuthIndex: source + "-auth"}}}
}

// TestFetchStartsAllProviderEndpointsBeforeWaiting 通过同步屏障拒绝串行执行。
func TestFetchStartsAllProviderEndpointsBeforeWaiting(t *testing.T) {
	// fetcher 的七个方法都会先发送 entered 再等待各自 gate。
	fetcher := newGatedProviderFetcher()
	// 失败或成功退出时都释放全部 gate，避免遗留 goroutine。
	t.Cleanup(fetcher.releaseAll)
	// resultCh 接收后台 Fetch 的最终 snapshot/error。
	resultCh := make(chan fetchOutcome, 1)
	// 后台执行 Fetch，让测试线程可以观察并发进入屏障。
	go func() {
		// 执行公开 Fetch。
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		// 缓冲通道保证测试清理后发送也不会阻塞。
		resultCh <- fetchOutcome{snapshot: snapshot, err: err}
	}()

	// 等待七个 endpoint 全部进入；串行实现只能进入第一个并触发超时失败。
	waitForSources(t, fetcher.entered, registrySourceOrder)
	// 全部进入后一次性释放，证明 Fetch 等待所有结果而不是 fail-fast。
	fetcher.releaseAll()
	// 获取最终 Fetch 结果。
	outcome := waitForFetchOutcome(t, resultCh)
	// 七个成功来源不允许产生 warning。
	if outcome.err != nil {
		t.Fatalf("Fetch returned error: %v", outcome.err)
	}
	// 七个 endpoint 都必须贡献一条 Credential。
	if len(outcome.snapshot.Credentials) != len(registrySourceOrder) {
		t.Fatalf("Credentials = %#v", outcome.snapshot.Credentials)
	}
}

// fetchOutcome 统一通过 channel 返回 Fetch 的 snapshot 与 error。
type fetchOutcome struct {
	// snapshot 保存部分成功或全成功结果。
	snapshot providermetadata.Snapshot
	// err 保存稳定归并后的 warning。
	err error
}

// waitForSources 在明确 deadline 内确认期望来源集合全部发送事件。
func waitForSources(t *testing.T, events <-chan string, want []string) {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// seen 记录实际收到的来源集合。
	seen := make(map[string]struct{}, len(want))
	// timer 防止串行实现或 goroutine 泄漏让测试永久阻塞。
	timer := time.NewTimer(time.Second)
	// helper 返回前释放 timer。
	defer timer.Stop()
	// 接收直到期望来源数量全部出现。
	for len(seen) < len(want) {
		// 事件和 deadline 竞争。
		select {
		case source := <-events:
			// 记录当前已进入或完成的来源。
			seen[source] = struct{}{}
		case <-timer.C:
			// 超时输出实际来源集合，串行实现会在这里明确失败。
			t.Fatalf("timed out waiting for sources: got=%v want=%v", seen, want)
		}
	}
	// 遍历期望集合，避免相同来源重复事件伪装成全部完成。
	for _, source := range want {
		// 每个固定来源都必须真实出现。
		if _, ok := seen[source]; !ok {
			t.Fatalf("source %q did not run: %v", source, seen)
		}
	}
}

// waitForFetchOutcome 在明确 deadline 内取得 Fetch 返回值。
func waitForFetchOutcome(t *testing.T, resultCh <-chan fetchOutcome) fetchOutcome {
	// helper 失败应报告调用测试位置。
	t.Helper()
	// 等待 Fetch 返回或触发泄漏保护 deadline。
	select {
	case outcome := <-resultCh:
		// 返回真实 Fetch 结果供调用方断言。
		return outcome
	case <-time.After(time.Second):
		// 超时表示仍有 endpoint goroutine 未退出。
		t.Fatal("timed out waiting for Fetch to return")
		// Fatal 会结束 goroutine，此返回只满足编译器控制流分析。
		return fetchOutcome{err: fmt.Errorf("fetch timeout")}
	}
}
