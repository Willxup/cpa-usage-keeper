package providermetadata_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service/providermetadata"
)

var registrySourceOrder = []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "meta", "openai"}

// gatedProviderFetcher 用独立 gate 控制所有 endpoint 的进入和完成时序。
type gatedProviderFetcher struct {
	entered   chan string
	done      chan string
	gates     map[string]chan struct{}
	errors    map[string]error
	closeOnce map[string]*sync.Once
}

func newGatedProviderFetcher(t *testing.T) *gatedProviderFetcher {
	t.Helper()
	fetcher := &gatedProviderFetcher{
		entered:   make(chan string, len(registrySourceOrder)),
		done:      make(chan string, len(registrySourceOrder)),
		gates:     make(map[string]chan struct{}, len(registrySourceOrder)),
		errors:    make(map[string]error),
		closeOnce: make(map[string]*sync.Once, len(registrySourceOrder)),
	}
	for _, source := range registrySourceOrder {
		fetcher.gates[source] = make(chan struct{})
		fetcher.closeOnce[source] = &sync.Once{}
	}
	t.Cleanup(fetcher.releaseAll)
	return fetcher
}

func (f *gatedProviderFetcher) release(source string) {
	f.closeOnce[source].Do(func() {
		close(f.gates[source])
	})
}

func (f *gatedProviderFetcher) releaseAll() {
	for _, source := range registrySourceOrder {
		f.release(source)
	}
}

func (f *gatedProviderFetcher) wait(ctx context.Context, source string) error {
	f.entered <- source
	defer func() { f.done <- source }()
	select {
	case <-f.gates[source]:
	case <-ctx.Done():
		return ctx.Err()
	}
	return f.errors[source]
}

func (f *gatedProviderFetcher) fetchStandard(ctx context.Context, source string) (*response.ProviderKeyConfigResult, error) {
	if err := f.wait(ctx, source); err != nil {
		return nil, err
	}
	return standardSuccessResult(source), nil
}

func (f *gatedProviderFetcher) FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandard(ctx, "codex")
}

func (f *gatedProviderFetcher) FetchXAIAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandard(ctx, "xai")
}

func (f *gatedProviderFetcher) FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandard(ctx, "gemini")
}

func (f *gatedProviderFetcher) FetchInteractionsAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandard(ctx, "gemini-interactions")
}

func (f *gatedProviderFetcher) FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandard(ctx, "claude")
}

func (f *gatedProviderFetcher) FetchVertexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandard(ctx, "vertex")
}

func (f *gatedProviderFetcher) FetchMetaAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error) {
	return f.fetchStandard(ctx, "meta")
}

func (f *gatedProviderFetcher) FetchOpenAICompatibility(ctx context.Context) (*response.OpenAICompatibilityResult, error) {
	if err := f.wait(ctx, "openai"); err != nil {
		return nil, err
	}
	return &response.OpenAICompatibilityResult{Payload: []providerconfig.OpenAICompatibilityConfig{{Name: "openai", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "openai-key", AuthIndex: "openai-auth"}}}}}, nil
}

func standardSuccessResult(source string) *response.ProviderKeyConfigResult {
	return &response.ProviderKeyConfigResult{Payload: []providerconfig.ProviderKeyConfig{{APIKey: source + "-key", Name: source, AuthIndex: source + "-auth"}}}
}

type fetchOutcome struct {
	snapshot providermetadata.Snapshot
	err      error
}

func waitForSources(t *testing.T, events <-chan string, want []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(want))
	// timer 防止串行实现或 goroutine 泄漏让测试永久阻塞。
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for len(seen) < len(want) {
		select {
		case source := <-events:
			seen[source] = struct{}{}
		case <-timer.C:
			t.Fatalf("timed out waiting for sources: got=%v want=%v", seen, want)
		}
	}
	for _, source := range want {
		if _, ok := seen[source]; !ok {
			t.Fatalf("source %q did not run: %v", source, seen)
		}
	}
}

func waitForFetchOutcome(t *testing.T, resultCh <-chan fetchOutcome) fetchOutcome {
	t.Helper()
	select {
	case outcome := <-resultCh:
		return outcome
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Fetch to return")
		return fetchOutcome{}
	}
}

// 缓冲结果通道保证测试提前失败后，后台 Fetch 仍能退出。
func startProviderFetch(ctx context.Context, fetcher *gatedProviderFetcher) <-chan fetchOutcome {
	resultCh := make(chan fetchOutcome, 1)
	go func() {
		snapshot, err := providermetadata.Fetch(ctx, fetcher)
		resultCh <- fetchOutcome{snapshot: snapshot, err: err}
	}()
	return resultCh
}
