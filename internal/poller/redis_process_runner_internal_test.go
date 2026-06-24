package poller

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	servicedto "cpa-usage-keeper/internal/service/dto"
)

func TestRedisProcessRunnerSleepsOneSecondAfterNonFullBatch(t *testing.T) {
	// 准备一个非满批结果，模拟本地 inbox 已接近追平。
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{{
		// 非满批结果不应触发连续 drain。
		result: &servicedto.RedisBatchSyncResult{Status: "completed", ProcessedRows: 999},
	}}}
	// 构造真实 runner，只替换 syncer 和 sleep 以观察调度行为。
	runner := NewRedisProcessRunner(syncer)
	// 创建可取消 context，让测试在捕获 sleep 后退出。
	ctx, cancel := context.WithCancel(context.Background())
	// 测试结束兜底取消 context，避免循环泄漏。
	defer cancel()
	// delays 保存 runner 实际请求的 sleep 间隔。
	var delays []time.Duration
	// 替换 sleep，以便捕获等待时间并停止 runner。
	runner.sleep = func(_ context.Context, delay time.Duration) bool {
		// 记录 runner 选择的等待时间。
		delays = append(delays, delay)
		// 取消 context，防止 runner 进入额外轮次。
		cancel()
		// 返回 false 表示等待被打断，runner 应正常退出。
		return false
	}

	// 执行 runner 主循环，验证非满批路径会等待。
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// 非满批应该只处理一次，然后进入 sleep。
	if syncer.calls != 1 {
		t.Fatalf("expected one process call before sleep, got %d", syncer.calls)
	}
	// 新的空闲/非满批等待间隔应该是 1 秒。
	want := []time.Duration{time.Second}
	// 校验 runner 按 1 秒等待，而不是立即继续。
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("unexpected redis process sleep delays: got %v want %v", delays, want)
	}
}

func TestRedisProcessRunnerSkipsSleepAfterFullBatch(t *testing.T) {
	// 准备第一轮满批、第二轮空批的结果序列。
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{
		// 满批成功代表本地 inbox 可能仍有 backlog。
		{result: &servicedto.RedisBatchSyncResult{Status: "completed", ProcessedRows: 1000, BatchFull: true}},
		// 第二轮空批用于让 runner 进入 sleep 并退出测试。
		{result: &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}},
	}}
	// 构造真实 runner，只替换 syncer 和 sleep 以观察调度行为。
	runner := NewRedisProcessRunner(syncer)
	// 创建可取消 context，让测试在捕获 sleep 后退出。
	ctx, cancel := context.WithCancel(context.Background())
	// 测试结束兜底取消 context，避免循环泄漏。
	defer cancel()
	// delays 保存 runner 实际请求的 sleep 间隔。
	var delays []time.Duration
	// 替换 sleep，以便捕获等待时间并停止 runner。
	runner.sleep = func(_ context.Context, delay time.Duration) bool {
		// 记录空批后的等待时间。
		delays = append(delays, delay)
		// 取消 context，防止 runner 进入额外轮次。
		cancel()
		// 返回 false 表示等待被打断，runner 应正常退出。
		return false
	}

	// 执行 runner 主循环，验证满批后会立即进入下一轮。
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// 满批成功应该触发第二次处理，而不是先 sleep。
	if syncer.calls != 2 {
		t.Fatalf("expected full batch to process again before sleeping, got %d calls", syncer.calls)
	}
	// 只有第二轮空批后才应该产生一次 1 秒 sleep。
	want := []time.Duration{time.Second}
	// 校验满批路径确实跳过了第一轮 sleep。
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("unexpected redis process sleep delays: got %v want %v", delays, want)
	}
}

func TestRedisProcessRunnerSkipsSleepAfterWarningFullBatch(t *testing.T) {
	// 准备第一轮满批 warning、第二轮空批的结果序列。
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{
		// completed_with_warnings 表示部分成功，不应按真正失败处理。
		{result: &servicedto.RedisBatchSyncResult{Status: "completed_with_warnings", ProcessedRows: 1000, BatchFull: true}, err: errors.New("decode warning")},
		// 第二轮空批用于让 runner 进入 sleep 并退出测试。
		{result: &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}},
	}}
	// 构造真实 runner，只替换 syncer 和 sleep 以观察调度行为。
	runner := NewRedisProcessRunner(syncer)
	// 创建可取消 context，让测试在捕获 sleep 后退出。
	ctx, cancel := context.WithCancel(context.Background())
	// 测试结束兜底取消 context，避免循环泄漏。
	defer cancel()
	// delays 保存 runner 实际请求的 sleep 间隔。
	var delays []time.Duration
	// 替换 sleep，以便捕获等待时间并停止 runner。
	runner.sleep = func(_ context.Context, delay time.Duration) bool {
		// 记录空批后的等待时间。
		delays = append(delays, delay)
		// 取消 context，防止 runner 进入额外轮次。
		cancel()
		// 返回 false 表示等待被打断，runner 应正常退出。
		return false
	}

	// 执行 runner 主循环，验证 warning 满批后会立即进入下一轮。
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// warning 满批仍然代表本轮消耗了完整批次，应继续 drain。
	if syncer.calls != 2 {
		t.Fatalf("expected warning full batch to process again before sleeping, got %d calls", syncer.calls)
	}
	// 只有第二轮空批后才应该产生一次 1 秒 sleep。
	want := []time.Duration{time.Second}
	// 校验 warning 满批路径没有被误判为真正失败。
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("unexpected redis process sleep delays: got %v want %v", delays, want)
	}
}

func TestRedisProcessRunnerSleepsAfterFailedFullBatch(t *testing.T) {
	// 准备一个满批但真正失败的结果，模拟 SQLite 锁等不可立即重试的错误。
	syncer := &sequenceRedisProcessSyncer{results: []redisProcessSyncerResult{{
		// Status failed 代表本轮没有安全完成，runner 不能立即重试。
		result: &servicedto.RedisBatchSyncResult{Status: "failed", ProcessedRows: 1000, BatchFull: true},
		// err 是真正失败，不会被 ErrSyncCompletedWithWarnings 包装。
		err: errors.New("sqlite locked"),
	}}}
	// 构造真实 runner，只替换 syncer 和 sleep 以观察调度行为。
	runner := NewRedisProcessRunner(syncer)
	// 创建可取消 context，让测试在捕获 sleep 后退出。
	ctx, cancel := context.WithCancel(context.Background())
	// 测试结束兜底取消 context，避免循环泄漏。
	defer cancel()
	// delays 保存 runner 实际请求的 sleep 间隔。
	var delays []time.Duration
	// 替换 sleep，以便捕获等待时间并停止 runner。
	runner.sleep = func(_ context.Context, delay time.Duration) bool {
		// 记录失败后的等待时间。
		delays = append(delays, delay)
		// 取消 context，防止 runner 进入额外轮次。
		cancel()
		// 返回 false 表示等待被打断，runner 应正常退出。
		return false
	}

	// 执行 runner 主循环，验证真正失败不会立即继续。
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// 真正失败应该只处理一次，然后进入 sleep。
	if syncer.calls != 1 {
		t.Fatalf("expected failed batch to sleep before retrying, got %d calls", syncer.calls)
	}
	// 失败路径应该保留 1 秒等待，避免 busy loop。
	want := []time.Duration{time.Second}
	// 校验满批失败没有跳过 sleep。
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("unexpected redis process sleep delays: got %v want %v", delays, want)
	}
}

// redisProcessSyncerResult 表示 fake syncer 单轮返回的结果和错误。
type redisProcessSyncerResult struct {
	// result 是本轮 ProcessRedisUsageInbox 的返回结果。
	result *servicedto.RedisBatchSyncResult
	// err 是本轮 ProcessRedisUsageInbox 的返回错误。
	err error
}

// sequenceRedisProcessSyncer 按顺序返回预设结果，便于测试 runner 循环。
type sequenceRedisProcessSyncer struct {
	// results 保存每轮 fake syncer 应返回的结果。
	results []redisProcessSyncerResult
	// calls 记录 runner 已调用 fake syncer 的次数。
	calls int
}

func (s *sequenceRedisProcessSyncer) ProcessRedisUsageInbox(context.Context) (*servicedto.RedisBatchSyncResult, error) {
	// 如果预设结果已经用完，返回空批，防止测试意外无限循环。
	if s.calls >= len(s.results) {
		// 递增调用次数，方便测试定位额外调用。
		s.calls++
		// 返回空批结果，模拟本地 inbox 已追平。
		return &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}, nil
	}
	// 读取当前调用应该返回的结果。
	result := s.results[s.calls]
	// 递增调用次数，准备下一轮返回。
	s.calls++
	// 返回当前预设结果和错误。
	return result.result, result.err
}
