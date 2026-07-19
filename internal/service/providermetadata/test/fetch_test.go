package providermetadata_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/service/providermetadata"
)

// TestFetchFoldsReverseCompletionInRegistryOrder 验证完成顺序不会污染 snapshot。
func TestFetchFoldsReverseCompletionInRegistryOrder(t *testing.T) {
	// fetcher 用独立 gate 控制七个 endpoint 的完成顺序。
	fetcher := newGatedProviderFetcher()
	// 任何失败路径都释放剩余 gate。
	t.Cleanup(fetcher.releaseAll)
	// resultCh 接收后台 Fetch 结果。
	resultCh := make(chan fetchOutcome, 1)
	// 后台启动 Fetch，测试线程负责精确释放顺序。
	go func() {
		// 执行公开 Fetch。
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		// 返回结果到缓冲通道。
		resultCh <- fetchOutcome{snapshot: snapshot, err: err}
	}()

	// 先确认七个 endpoint 已全部并发进入。
	waitForSources(t, fetcher.entered, registrySourceOrder)
	// reverseOrder 与 registry 完全相反。
	reverseOrder := []string{"openai", "vertex", "claude", "gemini-interactions", "gemini", "xai", "codex"}
	// 逐个释放并确认完成，确保真实完成顺序可控。
	for _, source := range reverseOrder {
		// 释放当前反向来源。
		fetcher.release(source)
		// 等待当前来源报告完成后再释放下一项。
		waitForSources(t, fetcher.done, []string{source})
	}
	// 读取全部 endpoint 完成后的 Fetch 结果。
	outcome := waitForFetchOutcome(t, resultCh)
	// 全成功来源不允许产生 warning。
	if outcome.err != nil {
		t.Fatalf("Fetch returned error: %v", outcome.err)
	}
	// fetched types 必须保持 registry 顺序而不是反向完成顺序。
	if !reflect.DeepEqual(outcome.snapshot.FetchedProviderTypes, registrySourceOrder) {
		t.Fatalf("FetchedProviderTypes = %#v", outcome.snapshot.FetchedProviderTypes)
	}
	// gotAuthIndexes 读取最终 Credential 顺序。
	gotAuthIndexes := make([]string, 0, len(outcome.snapshot.Credentials))
	// 按 snapshot 顺序收集 auth-index。
	for _, credential := range outcome.snapshot.Credentials {
		// 每条 Credential 的 auth-index 都包含来源标识。
		gotAuthIndexes = append(gotAuthIndexes, credential.AuthIndex)
	}
	// wantAuthIndexes 是 registry/source entry 的稳定顺序。
	wantAuthIndexes := []string{"codex-auth", "xai-auth", "gemini-auth", "gemini-interactions-auth", "claude-auth", "vertex-auth", "openai-auth"}
	// 完成顺序不能改变 Credential 顺序。
	if !reflect.DeepEqual(gotAuthIndexes, wantAuthIndexes) {
		t.Fatalf("auth indexes = %#v, want %#v", gotAuthIndexes, wantAuthIndexes)
	}
}

// TestFetchKeepsOtherSourcesWhenOneProviderFails 验证单来源失败不取消其它 endpoint。
func TestFetchKeepsOtherSourcesWhenOneProviderFails(t *testing.T) {
	// fetcher 用 gate 证明全部来源都实际执行。
	fetcher := newGatedProviderFetcher()
	// 任何失败路径都释放剩余 gate。
	t.Cleanup(fetcher.releaseAll)
	// Gemini 注入独立 fetch error。
	fetcher.errors["gemini"] = errors.New("gemini unavailable")
	// resultCh 接收后台 Fetch 结果。
	resultCh := make(chan fetchOutcome, 1)
	// 后台启动 Fetch。
	go func() {
		// 执行公开 Fetch。
		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		// 返回结果到缓冲通道。
		resultCh <- fetchOutcome{snapshot: snapshot, err: err}
	}()

	// 七个 endpoint 必须在任一结果返回前全部进入。
	waitForSources(t, fetcher.entered, registrySourceOrder)
	// 一次释放全部来源，Gemini 返回错误，其余正常成功。
	fetcher.releaseAll()
	// 七个 endpoint 都必须报告完成。
	waitForSources(t, fetcher.done, registrySourceOrder)
	// 读取部分成功结果。
	outcome := waitForFetchOutcome(t, resultCh)
	// warning 必须只包含 Gemini 来源错误。
	if outcome.err == nil || outcome.err.Error() != "fetch gemini api keys: gemini unavailable" {
		t.Fatalf("error = %v", outcome.err)
	}
	// 失败 Gemini 不进入 fetched types，其余六来源保持 registry 相对顺序。
	wantTypes := []string{"codex", "xai", "gemini-interactions", "claude", "vertex", "openai"}
	// 实际 fetched types 必须与成功来源一致。
	if !reflect.DeepEqual(outcome.snapshot.FetchedProviderTypes, wantTypes) {
		t.Fatalf("FetchedProviderTypes = %#v, want %#v", outcome.snapshot.FetchedProviderTypes, wantTypes)
	}
	// 其它六个来源的 Credential 必须全部保留。
	if len(outcome.snapshot.Credentials) != 6 {
		t.Fatalf("Credentials = %#v", outcome.snapshot.Credentials)
	}
}

// TestFetchPreservesCompletedSourcesAndWaitsForCancellation 验证 caller 取消时的部分结果和 goroutine 退出。
func TestFetchPreservesCompletedSourcesAndWaitsForCancellation(t *testing.T) {
	// fetcher 的七个 endpoint 先全部进入独立 gate。
	fetcher := newGatedProviderFetcher()
	// 任何失败路径都释放剩余 gate。
	t.Cleanup(fetcher.releaseAll)
	// ctx 由测试在 Codex 成功后取消。
	ctx, cancel := context.WithCancel(context.Background())
	// 测试退出时确保 context 已取消。
	defer cancel()
	// resultCh 接收后台 Fetch 结果。
	resultCh := make(chan fetchOutcome, 1)
	// 后台启动 Fetch。
	go func() {
		// 执行公开 Fetch 并传入 caller context。
		snapshot, err := providermetadata.Fetch(ctx, fetcher)
		// 返回结果到缓冲通道。
		resultCh <- fetchOutcome{snapshot: snapshot, err: err}
	}()

	// 七个 endpoint 必须全部进入，证明取消不是由串行调度造成。
	waitForSources(t, fetcher.entered, registrySourceOrder)
	// 先释放 Codex，让一个来源在取消前真实成功。
	fetcher.release("codex")
	// 等待 Codex 报告完成，固定部分成功边界。
	waitForSources(t, fetcher.done, []string{"codex"})
	// 取消 caller context，使剩余六个 endpoint 返回 context warning。
	cancel()
	// 剩余六个 endpoint 必须全部退出，不允许 goroutine 泄漏。
	waitForSources(t, fetcher.done, []string{"xai", "gemini", "gemini-interactions", "claude", "vertex", "openai"})
	// 读取 context 取消后的部分成功结果。
	outcome := waitForFetchOutcome(t, resultCh)
	// Codex 成功结果必须保留。
	if !reflect.DeepEqual(outcome.snapshot.FetchedProviderTypes, []string{"codex"}) || len(outcome.snapshot.Credentials) != 1 || outcome.snapshot.Credentials[0].AuthIndex != "codex-auth" {
		t.Fatalf("snapshot = %#v", outcome.snapshot)
	}
	// 取消 warning 必须按剩余来源的 registry 顺序稳定归并。
	wantError := "fetch xai api keys: context canceled; fetch gemini api keys: context canceled; fetch interactions api keys: context canceled; fetch claude api keys: context canceled; fetch vertex api keys: context canceled; fetch openai compatibility: context canceled"
	// 实际 error 必须完整包含六个来源且顺序稳定。
	if outcome.err == nil || outcome.err.Error() != wantError {
		t.Fatalf("error = %v, want %q", outcome.err, wantError)
	}
}
