package poller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	servicedto "cpa-usage-keeper/internal/service/dto"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
)

// redisInboxProcessInterval 控制本地 inbox 解码处理频率；远端 ingest 负责写 inbox，本 runner 只处理本地积压。
const redisInboxProcessInterval = 3 * time.Second

// RedisProcessSyncer 抽象已有 service.ProcessRedisUsageInbox，避免 process runner 依赖完整 SyncService 类型。
type RedisProcessSyncer interface {
	// ProcessRedisUsageInbox 把 redis_usage_inboxes 中的 raw message 解码写入 usage_events。
	ProcessRedisUsageInbox(ctx context.Context) (*servicedto.RedisBatchSyncResult, error)
}

type RedisProcessRunner struct {
	// syncer 是既有 inbox -> usage_events 处理逻辑，当前重构不改变它。
	syncer RedisProcessSyncer
	// now 便于测试状态更新时间。
	now func() time.Time
	// sleep 便于测试替换等待，并保证生产关停可打断。
	sleep func(context.Context, time.Duration) bool
	// mu 保护运行状态和最近处理结果。
	mu sync.Mutex
	// running 表示后台 process runner 是否已启动。
	running bool
	// processRunning 表示当前是否正在执行一次 inbox process。
	processRunning bool
	// lastRunAt 记录最近一次处理结果更新时间。
	lastRunAt time.Time
	// lastError 保存最近一次不可恢复处理错误。
	lastError string
	// lastWarning 保存最近一次部分成功/带警告处理错误。
	lastWarning string
	// lastStatus 保存最近一次处理状态。
	lastStatus string
}

func NewRedisProcessRunner(syncer RedisProcessSyncer) *RedisProcessRunner {
	// 构造时只保存 syncer，不启动处理循环。
	return &RedisProcessRunner{syncer: syncer, now: time.Now, sleep: sleepContext}
}

func (r *RedisProcessRunner) Run(ctx context.Context) error {
	// 先校验依赖，避免后台 goroutine 中 panic。
	if err := r.validate(); err != nil {
		return err
	}
	// 标记本地 process runner 已启动。
	r.setRunning(true)
	// 退出时清理 running 状态。
	defer r.setRunning(false)
	// 启动日志说明本地处理循环已开始，与远端 ingest 日志区分。
	logrus.WithField("interval", redisInboxProcessInterval.String()).Info("redis inbox process task started")
	// 持续轮询本地 inbox。
	for {
		select {
		case <-ctx.Done():
			// 应用关闭时正常退出，不当作错误。
			return nil
		default:
		}
		// 每轮处理一次本地 inbox 批次。
		result, err := r.ProcessOnce(ctx)
		if err != nil && !errors.Is(err, ErrSyncCompletedWithWarnings) {
			// 真正失败才按 error 日志输出；带 warning 的部分成功由状态保存。
			if shouldLogSyncError(err) {
				// 失败日志只输出统计字段，不输出 raw usage payload。
				r.logBatchFailure(result, err)
			}
		}
		// 固定间隔等待下一轮处理，关停时可被 context 打断。
		if !r.sleep(ctx, redisInboxProcessInterval) {
			return nil
		}
	}
}

func (r *RedisProcessRunner) ProcessOnce(ctx context.Context) (*servicedto.RedisBatchSyncResult, error) {
	// 单次处理和后台循环都复用同一校验逻辑。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// processRunning 需要锁保护，防止同一 runner 并发 decode 同一批 inbox。
	r.mu.Lock()
	if r.processRunning {
		r.mu.Unlock()
		return nil, ErrSyncAlreadyRunning
	}
	// 标记当前正在处理本地 inbox。
	r.processRunning = true
	r.mu.Unlock()
	defer func() {
		// 本轮结束后清理 processRunning。
		r.mu.Lock()
		r.processRunning = false
		r.mu.Unlock()
	}()
	// 调用既有业务逻辑，保持 decode、usage_events、聚合语义不变。
	result, err := r.syncer.ProcessRedisUsageInbox(ctx)
	// 默认返回原始错误。
	returnErr := err
	if err != nil && result != nil && result.Status != "" && result.Status != "failed" {
		// 部分成功/带 warning 的场景对外包装成 ErrSyncCompletedWithWarnings。
		returnErr = fmt.Errorf("%w: %v", ErrSyncCompletedWithWarnings, err)
	}
	// 无论成功失败，都记录状态供 Status 展示。
	r.recordProcessResult(result, err)
	return result, returnErr
}

func (r *RedisProcessRunner) Status() Status {
	// 状态读取加锁，避免与 ProcessOnce 写状态竞争。
	r.mu.Lock()
	defer r.mu.Unlock()
	return Status{
		// Running 表示 process 后台循环是否启动。
		Running: r.running,
		// LastRunAt 表示最近一次本地处理时间。
		LastRunAt: r.lastRunAt,
		// LastError 表示最近一次失败。
		LastError: r.lastError,
		// LastWarning 表示最近一次部分成功 warning。
		LastWarning: r.lastWarning,
		// LastStatus 表示最近一次处理状态。
		LastStatus: r.lastStatus,
		// SyncRunning 表示当前是否正在处理 inbox。
		SyncRunning: r.processRunning,
	}
}

func (r *RedisProcessRunner) recordProcessResult(result *servicedto.RedisBatchSyncResult, err error) {
	// 处理结果写状态需要锁保护。
	r.mu.Lock()
	defer r.mu.Unlock()
	// 更新时间统一走项目时间处理。
	r.lastRunAt = timeutil.NormalizeStorageTime(r.now())
	// 默认状态从 result 中取。
	status := ""
	if result != nil {
		status = result.Status
	}
	if status == "" && err == nil {
		// 没有 result 且没有错误时，视作 completed。
		status = "completed"
	}
	// 保存本次状态。
	r.lastStatus = status
	// 每次记录先清空旧错误。
	r.lastError = ""
	// 每次记录先清空旧 warning。
	r.lastWarning = ""
	if err != nil {
		if result != nil && result.Status != "" && result.Status != "failed" {
			// 状态不是 failed 的错误视为 warning，表示部分处理完成。
			r.lastWarning = err.Error()
		} else {
			// failed 或无 result 的错误视为真正失败。
			r.lastError = err.Error()
		}
	}
}

func (r *RedisProcessRunner) logBatchFailure(result *servicedto.RedisBatchSyncResult, err error) {
	// 失败日志只包含聚合统计字段，避免输出原始 usage 消息。
	fields := logrus.Fields{"status": "", "empty": false, "inserted_events": 0, "deduped_events": 0}
	if result != nil {
		// result 存在时补充批次状态。
		fields["status"] = result.Status
		// empty 标记是否本轮没有可处理数据。
		fields["empty"] = result.Empty
		// inserted_events 方便判断是否有实际写入。
		fields["inserted_events"] = result.InsertedEvents
		// deduped_events 方便判断是否大部分数据已去重。
		fields["deduped_events"] = result.DedupedEvents
	}
	// 本地处理失败使用 error 日志级别。
	logrus.WithError(err).WithFields(fields).Error("redis process batch failed")
}

func (r *RedisProcessRunner) setRunning(running bool) {
	// running 状态由后台 Run 生命周期维护，需要锁保护。
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = running
}

func (r *RedisProcessRunner) validate() error {
	if r == nil {
		// nil runner 是调用方错误。
		return fmt.Errorf("redis process runner is nil")
	}
	if r.syncer == nil {
		// syncer 缺失会导致无法处理本地 inbox。
		return fmt.Errorf("redis process syncer is nil")
	}
	if r.now == nil {
		// 测试替换后如果为空，恢复默认系统时间。
		r.now = time.Now
	}
	if r.sleep == nil {
		// sleep 为空时恢复 context-aware sleep。
		r.sleep = sleepContext
	}
	return nil
}

type RedisPoller struct {
	// ingest 管理远端 CPA -> redis_usage_inboxes。
	ingest *RedisIngestRunner
	// process 管理 redis_usage_inboxes -> usage_events。
	process *RedisProcessRunner
}

func NewRedisPoller(ingest *RedisIngestRunner, process *RedisProcessRunner) *RedisPoller {
	// RedisPoller 只聚合远端 ingest 和本地 process 状态，不作为后台 runner 启动。
	return &RedisPoller{ingest: ingest, process: process}
}

func (p *RedisPoller) Status() Status {
	if p == nil {
		// nil poller 返回空状态，保持状态接口稳定。
		return Status{}
	}
	// 默认 ingest 状态为空。
	ingestStatus := Status{}
	if p.ingest != nil {
		// ingest 存在时读取远端拉取状态。
		ingestStatus = p.ingest.Status()
	}
	// 默认 process 状态为空。
	processStatus := Status{}
	if p.process != nil {
		// process 存在时读取本地处理状态。
		processStatus = p.process.Status()
	}
	// 以 ingest 状态为基础，因为远端拉取是新状态机的主状态。
	status := ingestStatus
	if processStatus.LastRunAt.After(status.LastRunAt) {
		// 本地处理时间更新时，用本地处理的时间和状态覆盖 LastRunAt/LastStatus。
		status.LastRunAt = processStatus.LastRunAt
		status.LastStatus = processStatus.LastStatus
	}
	if status.LastError != "" && processStatus.LastError != "" {
		// 两边都有错误时合并展示。
		status.LastError += "; " + processStatus.LastError
	} else if processStatus.LastError != "" {
		// 只有 process 有错误时直接使用 process 错误。
		status.LastError = processStatus.LastError
	}
	if processStatus.LastWarning != "" {
		// process warning 优先展示，保持原本处理 warning 可见。
		status.LastWarning = processStatus.LastWarning
	}
	// 总 Running 是远端 ingest 或本地 process 任一后台任务运行即可。
	status.Running = ingestStatus.Running || processStatus.Running
	// 总 SyncRunning 是远端拉取/写入或本地处理任一正在运行即可。
	status.SyncRunning = ingestStatus.SyncRunning || processStatus.SyncRunning
	return status
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	// 每次 sleep 创建独立 timer，避免共享 timer 状态复杂度。
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		// context 取消时返回 false，调用方按正常关停处理。
		return false
	case <-timer.C:
		// timer 到期返回 true，调用方继续下一轮。
		return true
	}
}
