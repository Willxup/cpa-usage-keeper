package providermetadata

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Fetch 并发执行固定 registry，并在全部来源完成后按声明顺序确定性归并一轮 provider metadata。
func Fetch(ctx context.Context, fetcher Fetcher) (Snapshot, error) {
	// nil fetcher 无法读取任何 endpoint，直接返回明确配置错误。
	if fetcher == nil {
		return Snapshot{}, fmt.Errorf("provider metadata fetcher is nil")
	}
	// sources 使用固定 registry 顺序构造本轮来源。
	sources := providerSources()
	// registry 结构错误必须在发起任何远端请求前失败。
	if err := validateSources(sources); err != nil {
		return Snapshot{}, err
	}
	// results 预先按 registry 长度分配，让每个 endpoint 只写自己的固定下标。
	results := make([]sourceResult, len(sources))
	// waitGroup 保证所有来源结束后才开始单线程归并，避免读取未完成结果。
	var waitGroup sync.WaitGroup
	// 每个固定来源都必须启动一次，即使 sibling 已失败也不能提前停止。
	waitGroup.Add(len(sources))
	// 按 registry 遍历只决定结果槽位，不要求 endpoint 按该顺序完成。
	for index, item := range sources {
		// 每个 goroutine 只执行一个 endpoint，并写入与 registry 对应的唯一槽位。
		go func(resultIndex int, currentSource source) {
			// 当前 endpoint 返回时减少等待计数，包括错误和 context 取消路径。
			defer waitGroup.Done()
			// 独占下标写入避免并发 append，也不让完成顺序影响最终结果。
			results[resultIndex] = currentSource.fetch(ctx, fetcher)
		}(index, item)
	}
	// 等待七个来源全部结束，不因单来源 warning 取消仍在执行的 sibling。
	waitGroup.Wait()
	// snapshot 只由当前 goroutine 按 registry 顺序写入。
	snapshot := Snapshot{}
	// seenAuthIndexes 完全沿用当前 Keeper 的精确 auth-index 首项保留规则。
	seenAuthIndexes := make(map[string]struct{})
	// warnings 按 registry 顺序收集，最终使用稳定分隔符连接。
	warnings := make([]string, 0)
	// 单线程按 registry 下标读取已完成结果，保持串行 golden 的全部 fold 语义。
	for index, item := range sources {
		// 单个 source 的结果已在对应下标完成，不受 goroutine 完成顺序影响。
		result := results[index]
		// 成功 endpoint 即使结果为空也必须进入 stale scope。
		if result.fetched {
			snapshot.FetchedProviderTypes = append(snapshot.FetchedProviderTypes, item.providerType)
		}
		// 保持 source entry 顺序并只按精确 auth-index 去重。
		for _, credential := range result.credentials {
			// source 已完成必填校验，这里只规范化去重键。
			authIndex := strings.TrimSpace(credential.AuthIndex)
			// 相同 auth-index 的后续项沿用当前先出现项。
			if _, ok := seenAuthIndexes[authIndex]; ok {
				continue
			}
			// 记录首次出现的 auth-index。
			seenAuthIndexes[authIndex] = struct{}{}
			// Credential 按 registry/source 顺序加入最终 snapshot。
			snapshot.Credentials = append(snapshot.Credentials, credential)
		}
		// 单个来源 warning 不阻止后续来源执行。
		if result.warning != nil {
			warnings = append(warnings, strings.TrimSpace(result.warning.Error()))
		}
	}
	// 没有 warning 时返回成功 snapshot。
	if len(warnings) == 0 {
		return snapshot, nil
	}
	// 多来源 warning 使用旧 joinErrors 相同的稳定分隔语义。
	return snapshot, fmt.Errorf("%s", strings.Join(warnings, "; "))
}
