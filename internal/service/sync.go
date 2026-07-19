package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service/tokenprocessor"
	"cpa-usage-keeper/internal/timeutil"

	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// RecentUsageEventAppender 接收已提交入库的 usage_events，供最近窗口纯内存缓存异步维护。
type RecentUsageEventAppender interface {
	TryAppend([]entities.UsageEvent) bool
}

const (
	redisInboxProcessLimit                = 1000
	redisUsageIdentityTypeLookupBatchSize = 500
)

const (
	syncMetadataOptional = false
	syncMetadataRequired = true
)

// SyncService 负责同步 CPA metadata，并处理已经落入本地 inbox 的 usage 原始消息。
type SyncService struct {
	db                        *gorm.DB
	client                    CPAClientFetcher
	metadataFetcher           MetadataFetcher
	baseURL                   string
	now                       func() time.Time
	recentUsage               RecentUsageEventAppender
	usageHeaderQuota          quota.UsageHeaderSnapshotAppender
	cleanupUsageEventsEnabled bool
}

// NewSyncService 按生产配置组装 CPA metadata client；远端 usage 拉取由 poller 独立负责。
func NewSyncService(db *gorm.DB, cfg config.Config) *SyncService {
	return NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL:                   cfg.CPABaseURL,
		Client:                    cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify),
		CleanupUsageEventsEnabled: cfg.CleanupUsageEventsEnabled,
	})
}

// SyncServiceOptions 提供测试和局部调用需要替换的依赖。
type SyncServiceOptions struct {
	BaseURL                   string
	Client                    CPAClientFetcher
	MetadataFetcher           MetadataFetcher
	Now                       func() time.Time
	RecentUsageEvents         RecentUsageEventAppender
	UsageHeaderQuota          quota.UsageHeaderSnapshotAppender
	CleanupUsageEventsEnabled bool
}

// NewSyncServiceWithOptions 是统一构造入口，负责填充默认时钟和 metadata fetcher。
func NewSyncServiceWithOptions(db *gorm.DB, opts SyncServiceOptions) *SyncService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	metadataFetcher := opts.MetadataFetcher
	if metadataFetcher == nil {
		metadataFetcher = opts.Client
	}
	return &SyncService{
		db:                        db,
		client:                    opts.Client,
		metadataFetcher:           metadataFetcher,
		baseURL:                   strings.TrimSpace(opts.BaseURL),
		now:                       now,
		recentUsage:               opts.RecentUsageEvents,
		usageHeaderQuota:          opts.UsageHeaderQuota,
		cleanupUsageEventsEnabled: opts.CleanupUsageEventsEnabled,
	}
}

// NewSyncServiceWithClient 兼容只需要 metadata 同步的调用方和测试。
func NewSyncServiceWithClient(db *gorm.DB, baseURL string, client CPAClientFetcher) *SyncService {
	return NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: baseURL,
		Client:  client,
	})
}

// ProcessRedisUsageInbox 是 Redis 同步的本地处理阶段：只读取 pending/process_failed inbox 行并写入 usage_events。
// 成功处理后仅用 usage_event_key 记录 inbox 与最终事件的关联。
func (s *SyncService) ProcessRedisUsageInbox(ctx context.Context) (*servicedto.RedisBatchSyncResult, error) {
	if err := s.validate(syncMetadataOptional); err != nil {
		return nil, err
	}
	fetchedAt := timeutil.NormalizeStorageTime(s.now())
	// process_failed 也在这里重试，避免临时 SQLite 锁或短暂解析外问题导致数据永久卡住。
	processableRows, err := repository.ListProcessableRedisUsageInbox(s.db, redisInboxProcessLimit)
	if err != nil {
		// 列表失败时没有可靠取出行，返回 0 行失败结果供 runner 保守等待。
		return newRedisBatchSyncResult("failed", 0), fmt.Errorf("list processable redis usage inbox: %w", err)
	}
	if len(processableRows) == 0 {
		// 空轮次先做轻量 cursor 检查，只有发现 usage_events 尚未聚合时才写派生统计。
		pendingAggregation, err := repository.HasPendingUsageOverviewAggregation(ctx, s.db)
		if err != nil {
			// 空批聚合检查失败仍然是空结果，runner 需要按失败路径等待。
			result := newRedisBatchSyncResult("failed", 0)
			// Empty 明确告诉调用方本轮没有 inbox 行参与处理。
			result.Empty = true
			// 返回失败结果和原始错误，保持既有错误语义。
			return result, err
		}
		if pendingAggregation {
			if err := s.aggregateUsageEventStats(ctx, fetchedAt); err != nil {
				// 空批补聚合失败仍然不代表 inbox 有积压，runner 保守等待下一轮。
				result := newRedisBatchSyncResult("failed", 0)
				// Empty 明确告诉调用方本轮没有 inbox 行参与处理。
				result.Empty = true
				// 返回失败结果和原始错误，保持既有错误语义。
				return result, err
			}
		}
		// 空批成功时返回 0 行结果，避免 runner 误判为需要连续 drain。
		result := newRedisBatchSyncResult("empty", 0)
		// Empty 明确告诉调用方本轮没有 inbox 行参与处理。
		result.Empty = true
		// 返回空批结果，保持既有空轮次语义。
		return result, nil
	}
	logrus.WithField("row_count", len(processableRows)).Debug("redis usage inbox rows found for processing")
	return s.processRedisInboxRows(ctx, processableRows, fetchedAt)
}

// CleanupRedisUsageInbox 只清理 Redis inbox 表，供测试和单独维护入口使用；每日任务使用 CleanupStorage 统一执行。
func (s *SyncService) CleanupRedisUsageInbox(ctx context.Context) error {
	if err := s.validate(syncMetadataOptional); err != nil {
		return err
	}
	_, err := repository.CleanupRedisUsageInbox(s.db, s.now())
	return err
}

// CleanupStorage 是每日 04:30 维护任务调用的统一入口：先清 Redis inbox，按配置清 usage_events，最后 VACUUM 收缩 SQLite。
func (s *SyncService) CleanupStorage(ctx context.Context) error {
	if err := s.validate(syncMetadataOptional); err != nil {
		return err
	}
	result, err := repository.CleanupStorage(s.db, s.now(), repository.CleanupStorageOptions{
		CleanupUsageEvents: s.cleanupUsageEventsEnabled,
	})
	logrus.WithFields(logrus.Fields{
		"redis_processed_deleted": result.RedisInbox.ProcessedDeleted,
		"redis_failed_deleted":    result.RedisInbox.FailedDeleted,
		"usage_events_deleted":    result.UsageEventsDeleted,
	}).Debug("storage cleanup finished")
	return err
}

// processRedisInboxRows 只从已落库的原始消息解码和写入事件，坏消息会标记为 decode_failed，不阻塞同批其它数据。
// 可解码但入库失败的消息标记为 process_failed，后续 ProcessRedisUsageInbox 会按 id 顺序重试。
func (s *SyncService) processRedisInboxRows(ctx context.Context, inboxRows []entities.RedisUsageInbox, fetchedAt time.Time) (*servicedto.RedisBatchSyncResult, error) {
	logrus.WithField("row_count", len(inboxRows)).Debug("redis usage inbox processing started")
	// processedRows 记录本轮实际取出的原始 inbox 行数，包含后续 decode_failed/process_failed 行。
	processedRows := len(inboxRows)
	validRows := make([]entities.RedisUsageInbox, 0, len(inboxRows))
	events := make([]entities.UsageEvent, 0, len(inboxRows))
	// snapshot 与 event 保持同一索引；部分成功时只能通知已提交 ready item 对应的 snapshot。
	eventSnapshots := make([]*quota.UsageHeaderSnapshot, 0, len(inboxRows))
	decodeErrs := make([]error, 0)
	// 先完整解码本批数据，坏消息单独标记，不阻断同批其它可用消息。
	for _, row := range inboxRows {
		event, _, snapshot, decodeErr := DecodeRedisUsageMessageWithHeaders(row.RawMessage, fetchedAt)
		if decodeErr != nil {
			logrus.WithError(decodeErr).WithField("inbox_id", row.ID).Error("redis usage message decode failed")
			if markErr := repository.MarkRedisUsageInboxDecodeFailed(s.db, row.ID, decodeErr); markErr != nil {
				// 标记坏消息失败时保留本轮已取行数，runner 仍按真正失败路径等待。
				return newRedisBatchSyncResult("failed", processedRows), fmt.Errorf("mark redis usage inbox decode failed: %w", markErr)
			}
			decodeErrs = append(decodeErrs, decodeErr)
			continue
		}
		validRows = append(validRows, row)
		events = append(events, event)
		// nil 也占据当前 event 的索引，防止 ready/unresolved 分区后 snapshot 串行。
		eventSnapshots = append(eventSnapshots, snapshot)
	}
	decodeErr := joinErrors(decodeErrs...)
	logrus.WithFields(logrus.Fields{
		"row_count":           len(inboxRows),
		"valid_event_count":   len(events),
		"decode_failed_count": len(decodeErrs),
	}).Debug("redis usage inbox rows decoded")
	if len(events) == 0 {
		if decodeErr != nil {
			// 全部消息解码失败时也算本轮已消耗这些 inbox 行，避免满批坏消息误触发 sleep。
			logTokenProcessingBatch(nil, "completed_with_warnings", processedRows, 0, len(decodeErrs), redisInboxFailureCounts{})
			return newRedisBatchSyncResult("completed_with_warnings", processedRows), decodeErr
		}
		// 非空输入却没有事件且没有错误时保留已取行数供诊断。
		result := newRedisBatchSyncResult("empty", processedRows)
		// Empty 只表示本轮没有取到 inbox 行，因此这里不标记为空批。
		return result, nil
	}
	// executor-first 归一化会保留已知 executor 的 ready 结果，并只为 unresolved 事件查询 identity。
	normalizedItems, unresolvedIndexes, typeErr := normalizeRedisUsageEventsDetailed(ctx, s.db, events)
	readyRows := make([]entities.RedisUsageInbox, 0, len(events))
	readyEvents := make([]entities.UsageEvent, 0, len(events))
	headerSnapshots := make([]quota.UsageHeaderSnapshot, 0, len(events))
	for index, item := range normalizedItems {
		// ready=false 的槽位依赖失败的 identity 查询，不能按 default 猜测入库。
		if !item.ready {
			continue
		}
		// normalizedItems 与原 validRows/events/eventSnapshots 使用同一索引，事务关联不会串行。
		readyRows = append(readyRows, validRows[index])
		readyEvents = append(readyEvents, item.event)
		if eventSnapshots[index] != nil {
			// 只收集已准备入库事件的 snapshot，unresolved snapshot 不得提前通知 quota worker。
			headerSnapshots = append(headerSnapshots, *eventSnapshots[index])
		}
	}
	failureCounts := redisInboxFailureCounts{}
	if typeErr != nil {
		// identity 查询错误只影响真正依赖该查询的 unresolved 行，已知 executor ready 行继续处理。
		unresolvedRows := make([]entities.RedisUsageInbox, 0, len(unresolvedIndexes))
		for _, index := range unresolvedIndexes {
			unresolvedRows = append(unresolvedRows, validRows[index])
		}
		failureCounts = markRedisInboxRowsProcessFailed(s.db, unresolvedRows, typeErr)
		if len(readyEvents) == 0 {
			// 本批没有任何可独立提交的 ready 事件时保持原有 failed 语义，等待 unresolved 重试。
			logTokenProcessingBatch(normalizedItems, "failed", processedRows, 0, len(decodeErrs), failureCounts)
			failureResult := newRedisBatchSyncResult("failed", processedRows)
			// 即使失败状态无法确认，结果也要明确要求等待，不能把未知状态解释成“没有待重试行”。
			failureResult.RetryPending = failureCounts.requiresRetryWait()
			// 已确认丢弃由 service 输出唯一终态告警，runner 读取计数后不得重复输出批次错误。
			failureResult.DiscardedRows = failureCounts.discarded
			return failureResult, joinErrors(decodeErr, typeErr)
		}
	}
	// 后续事务只处理 ready 子集；成功行一旦提交就不会在 unresolved 重试时重复入库。
	validRows = readyRows
	events = readyEvents

	// usage_events 入库和 inbox processed 标记必须同事务提交，避免标记失败后同一 inbox 重试造成重复事件。
	logrus.WithField("event_count", len(events)).Debug("redis usage events persistence started")
	var result *servicedto.SyncResult
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var persistErr error
		result, persistErr = s.persistRedisUsageEvents(tx, events)
		if persistErr != nil {
			return persistErr
		}
		// validRows 和 events 按同一循环 append，索引一一对应。
		for i, row := range validRows {
			if markErr := repository.MarkRedisUsageInboxProcessed(tx, row.ID, events[i].EventKey, fetchedAt); markErr != nil {
				return fmt.Errorf("mark redis usage inbox processed: %w", markErr)
			}
		}
		return nil
	})
	// 事务错误或未产出持久化结果时统一走失败路径，避免两个等价分支后续漂移。
	if err != nil || result == nil {
		// 有具体错误时把可重试行标成 process_failed，异常空结果也复用同一保守返回。
		readyFailures := markRedisInboxRowsProcessFailed(s.db, validRows, err)
		failureCounts = failureCounts.add(readyFailures)
		// 失败返回仍保留本轮已取行数，但不允许 runner 立即忙循环。
		logTokenProcessingBatch(normalizedItems, "failed", processedRows, 0, len(decodeErrs), failureCounts)
		failureResult := newRedisBatchSyncResult("failed", processedRows)
		// UPDATE 或回读失败时同样保持等待信号，避免调用方仅凭零计数误判已经终止。
		failureResult.RetryPending = failureCounts.requiresRetryWait()
		// 已确认丢弃由逐行日志负责，批次结果只传递真实计数供 runner 去重。
		failureResult.DiscardedRows = failureCounts.discarded
		return failureResult, err
	}
	// 事务已经同时提交 usage_events 与对应 inbox processed，只有此时事件级 Token 日志才代表真实入库事件。
	logCommittedTokenProcessingEvents(normalizedItems)
	if result.InsertedEvents > 0 {
		// usage_events 事务已经提交后才通知最近事件缓存，避免缓存看到未落库的数据。
		if s.recentUsage != nil && !s.recentUsage.TryAppend(events) {
			// 缓存队列满只影响 realtime/边界缓存的新鲜度，不能反向阻塞或回滚写入链路。
			logrus.WithField("event_count", len(events)).Warn("recent usage event cache append skipped")
		}
		// Redis process 是 usage_events 的高频写入入口，成功插入后串行刷新依赖事件表的增量统计。
		if err := s.aggregateUsageEventStats(ctx, timeutil.NormalizeStorageTime(s.now())); err != nil {
			// 聚合失败时保留本轮已取行数，但保持失败路径的保守等待。
			logTokenProcessingBatch(normalizedItems, "failed", processedRows, result.InsertedEvents, len(decodeErrs), failureCounts)
			failureResult := newRedisBatchSyncResult("failed", processedRows)
			// 同批 unresolved 状态若仍可重试或无法确认，失败结果继续携带保守等待信号。
			failureResult.RetryPending = failureCounts.requiresRetryWait()
			// 已确认丢弃由逐行日志负责，runner 不得对同一终态再次输出批次错误。
			failureResult.DiscardedRows = failureCounts.discarded
			return failureResult, err
		}
		// header quota 需要复用窗口 token/cost 兜底统计，因此必须在 usage overview 聚合追平后再通知 worker。
		headerSnapshots = coalesceUsageHeaderSnapshotsByAuthIndex(headerSnapshots)
		if s.usageHeaderQuota != nil && len(headerSnapshots) > 0 && !s.usageHeaderQuota.TryAppendUsageHeaderSnapshots(headerSnapshots) {
			// header worker 队列满只影响 quota cache 新鲜度，不能影响 usage_events 写入成功。
			logrus.WithField("snapshot_count", len(headerSnapshots)).Warn("usage header quota cache append skipped")
		}
	}
	logrus.WithFields(logrus.Fields{
		"processed_rows":  processedRows,
		"inserted_events": result.InsertedEvents,
		"deduped_events":  result.DedupedEvents,
		"status":          result.Status,
	}).Debug("redis usage inbox rows processed")

	// 批次可部分成功：事件正常入库时仍返回 completed_with_warnings 暴露 decode 错误。
	status := result.Status
	returnErr := err
	if decodeErr != nil || typeErr != nil {
		status = "completed_with_warnings"
		// decode 与 identity 查询警告可以同时存在，全部返回给 runner 日志但不回滚已提交 ready 事件。
		returnErr = joinErrors(returnErr, decodeErr, typeErr)
	}
	// 批次汇总同时包含 inbox 处理状态与 Token outcome，字段名明确区分两套状态机。
	logTokenProcessingBatch(normalizedItems, status, processedRows, result.InsertedEvents, len(decodeErrs), failureCounts)
	return &servicedto.RedisBatchSyncResult{
		// Status 保持原有 completed/completed_with_warnings 语义。
		Status: status,
		// InsertedEvents 保持原有 usage_events 新增数量语义。
		InsertedEvents: result.InsertedEvents,
		// DedupedEvents 保持原有 usage_events 去重数量语义。
		DedupedEvents: result.DedupedEvents,
		// ProcessedRows 使用本轮取出的 inbox 行数，而不是最终写入事件数。
		ProcessedRows: processedRows,
		// BatchFull 只表示本轮取数达到上限，供 runner 判断是否继续 drain。
		BatchFull: processedRows >= redisInboxProcessLimit,
		// 仍有可重试失败行时必须让 runner 等待，避免满批 warning 在毫秒级耗尽五次机会。
		RetryPending: failureCounts.requiresRetryWait(),
		// 已确认丢弃只在 service 逐行告警一次，runner 使用该计数避免重复批次告警。
		DiscardedRows: failureCounts.discarded,
	}, returnErr
}

func newRedisBatchSyncResult(status string, processedRows int) *servicedto.RedisBatchSyncResult {
	// 负数输入没有业务意义，统一收敛为 0，避免调用方误报批次大小。
	if processedRows < 0 {
		// 只修正异常输入，不影响正常批次行数。
		processedRows = 0
	}
	// BatchFull 只看取出的原始行数是否达到 service 批次上限。
	batchFull := processedRows >= redisInboxProcessLimit
	// 返回统一填充批次信号的 Redis 处理结果。
	return &servicedto.RedisBatchSyncResult{Status: status, ProcessedRows: processedRows, BatchFull: batchFull}
}

func coalesceUsageHeaderSnapshotsByAuthIndex(snapshots []quota.UsageHeaderSnapshot) []quota.UsageHeaderSnapshot {
	if len(snapshots) <= 1 {
		return snapshots
	}
	indexByAuthIndex := make(map[string]int, len(snapshots))
	coalesced := make([]quota.UsageHeaderSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		authIndex := strings.TrimSpace(snapshot.AuthIndex)
		if authIndex == "" {
			continue
		}
		snapshot.AuthIndex = authIndex
		if index, ok := indexByAuthIndex[authIndex]; ok {
			if !snapshot.ObservedAt.Before(coalesced[index].ObservedAt) {
				coalesced[index] = snapshot
			}
			continue
		}
		indexByAuthIndex[authIndex] = len(coalesced)
		coalesced = append(coalesced, snapshot)
	}
	return coalesced
}

type usageEventTypeResolver struct {
	byIdentity map[usageEventIdentityKey]string
}

type usageEventIdentityKey struct {
	authType entities.UsageIdentityAuthType
	identity string
}

// normalizedUsageEvent 保持单条事件、opaque 路由证明和纯 Token 结果，供批次日志与部分成功事务共同消费。
type normalizedUsageEvent struct {
	event      entities.UsageEvent
	resolution tokenprocessor.HandlerResolution
	result     tokenprocessor.ProcessResult
	ready      bool
}

func normalizeRedisUsageEventsDetailed(ctx context.Context, db *gorm.DB, events []entities.UsageEvent) ([]normalizedUsageEvent, []int, error) {
	// 结果切片与输入索引一一对应，后续 row/event/snapshot 分区不依赖 append 顺序猜测。
	items := make([]normalizedUsageEvent, len(events))
	unresolvedIndexes := make([]int, 0)
	unresolvedEvents := make([]entities.UsageEvent, 0)
	for index, event := range events {
		// executor 先描述本次真实 CPA parser；已知类型无需读取凭证 identity。
		resolution, err := tokenprocessor.ResolveExecutor(event.ExecutorType)
		if err != nil {
			// registry 错误影响全部事件，返回所有索引让调用方按 process_failed 保守处理。
			allIndexes := make([]int, len(events))
			for itemIndex := range events {
				allIndexes[itemIndex] = itemIndex
			}
			return items, allIndexes, err
		}
		// unresolved 槽位也保存初始 resolution，identity 查询失败时仍能统计和记录 unknown executor。
		items[index] = normalizedUsageEvent{event: event, resolution: resolution}
		if resolution.NeedsIdentity() {
			// 只有空、unknown 或未注册 executor 才进入现有联合键 identity 批量查询。
			unresolvedIndexes = append(unresolvedIndexes, index)
			unresolvedEvents = append(unresolvedEvents, event)
			continue
		}
		// 已知 executor 立即完成纯 Token 处理，identity 查询失败也不能回滚这条 ready 结果。
		items[index] = processResolvedUsageEvent(event, resolution)
	}

	if len(unresolvedEvents) == 0 {
		// 全部事件都由 executor 证明时完全跳过 usage_identities 查询。
		return items, nil, nil
	}
	// unresolved 仍按 auth_type+auth_index、active→deleted、每组 500 条执行现有批量查询。
	resolver, err := buildUsageEventTypeResolver(ctx, db, unresolvedEvents)
	if err != nil {
		// 返回已完成 ready 槽位和准确 unresolved 索引，调用方可以部分提交而不重复成功事件。
		return items, unresolvedIndexes, err
	}

	for unresolvedPosition, event := range unresolvedEvents {
		index := unresolvedIndexes[unresolvedPosition]
		// OAuth 缺 identity 时沿用 provider，API Key 缺 identity 时保持空值并进入 strict default。
		identityType := resolveUsageEventType(event, resolver)
		// 空 executor 与 CPA 固定 unknown 保留原有缺 identity 告警；真正未注册 executor 改由信息更完整的 routing observation 告警一次。
		if identityType == "" && (items[index].resolution == nil || !items[index].resolution.UnknownExecutor()) {
			logrus.WithFields(logrus.Fields{
				"auth_type": event.AuthType,
				"event_key": event.EventKey,
			}).Warn("usage identity type not found for redis usage event")
		}
		// ResolveIdentity 内部再次保证 executor 优先，并只授予 identity_hint 而非 parser_contract。
		resolution, resolveErr := tokenprocessor.ResolveIdentity(event.ExecutorType, identityType)
		if resolveErr != nil {
			return items, unresolvedIndexes, resolveErr
		}
		items[index] = processResolvedUsageEvent(event, resolution)
	}
	return items, nil, nil
}

func processResolvedUsageEvent(event entities.UsageEvent, resolution tokenprocessor.HandlerResolution) normalizedUsageEvent {
	// service 层只做 UsageEvent 与 TokenValues 的机械字段拷贝，协议语义全部进入 tokenprocessor.Process。
	result := tokenprocessor.Process(tokenValuesFromUsageEvent(event), resolution)
	event = applyTokenProcessResult(event, result)
	// 归一化阶段只返回纯处理结果；事件日志必须等待 usage_events 与 inbox processed 的共同事务提交。
	return normalizedUsageEvent{event: event, resolution: resolution, result: result, ready: true}
}

func logCommittedTokenProcessingEvents(items []normalizedUsageEvent) {
	// normalizedItems 保持原批次索引；只有 ready=true 的槽位进入了刚刚成功提交的事务。
	for _, item := range items {
		if !item.ready {
			// unresolved 行尚未入库，重试期间不得提前输出或重复事件级 Token 告警。
			continue
		}
		// 只为已提交的纠正、未解决冲突、溢出或真正未注册 executor 输出事件级日志。
		logTokenProcessingEvent(item)
	}
}

func tokenValuesFromUsageEvent(event entities.UsageEvent) tokenprocessor.TokenValues {
	// Token-only DTO 明确阻断 model/provider/request 等非 Token 字段进入处理包。
	return tokenprocessor.TokenValues{
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CachedTokens:        event.CachedTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheReadPresent:    event.CacheReadPresent,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         event.TotalTokens,
	}
}

func applyTokenProcessResult(event entities.UsageEvent, result tokenprocessor.ProcessResult) entities.UsageEvent {
	// 这里只把纯 Token 结果写回原事件，其他业务字段保持 DecodeRedisUsageMessage 的原值。
	event.InputTokens = result.Tokens.InputTokens
	event.OutputTokens = result.Tokens.OutputTokens
	event.ReasoningTokens = result.Tokens.ReasoningTokens
	event.CachedTokens = result.Tokens.CachedTokens
	event.CacheReadTokens = result.Tokens.CacheReadTokens
	event.CacheCreationTokens = result.Tokens.CacheCreationTokens
	event.TotalTokens = result.Tokens.TotalTokens
	return event
}

func logTokenProcessingEvent(item normalizedUsageEvent) {
	// normalized 和常规 compatibility 是高频正常路径，只进入批次计数，避免逐事件 warning 噪音。
	requiresAttention := item.result.Outcome == tokenprocessor.TokenOutcomeCorrected ||
		item.result.Outcome == tokenprocessor.TokenOutcomeAmbiguous ||
		item.result.Outcome == tokenprocessor.TokenOutcomeOverflow ||
		(item.resolution != nil && item.resolution.UnknownExecutor())
	if !requiresAttention {
		return
	}

	// 字段只来自已提交事件、opaque 路由证明和纯 Token 结果，不读取 raw message、API key 或 auth_index。
	fields := logrus.Fields{
		"event_key":        item.event.EventKey,
		"executor_type":    item.event.ExecutorType,
		"handler_id":       item.resolution.HandlerID(),
		"evidence_source":  item.resolution.EvidenceSource(),
		"token_outcome":    item.result.Outcome,
		"token_actions":    tokenActionLogDetails(item.result.Actions),
		"token_violations": tokenViolationCodes(item.result.Violations),
		"unknown_executor": item.resolution.UnknownExecutor(),
	}
	// identity_type 只有实际执行 fallback 查询时才记录；不输出 auth_index、API key 或凭证正文。
	if item.resolution.EvidenceSource() == tokenprocessor.EvidenceIdentity && item.resolution.IdentityType() != "" {
		fields["identity_type"] = item.resolution.IdentityType()
	}
	// corrected/ambiguous/overflow 与真正新 executor 都需要维护者处理，因此在事务提交后只输出一次 Warn。
	logrus.WithFields(fields).Warn("redis usage token event requires attention")
}

func tokenActionLogDetails(actions []tokenprocessor.Action) []logrus.Fields {
	// action 日志只包含 Token 字段、规则和前后数值，不携带请求正文或凭证。
	details := make([]logrus.Fields, 0, len(actions))
	for _, action := range actions {
		details = append(details, logrus.Fields{
			"code":   action.Code,
			"field":  action.Field,
			"before": action.Before,
			"after":  action.After,
			"rule":   action.Rule,
		})
	}
	return details
}

func tokenViolationCodes(violations []tokenprocessor.Violation) []string {
	// 批次与事件日志使用稳定 violation code，详细原因保留在纯结果中供测试和未来诊断扩展。
	codes := make([]string, 0, len(violations))
	for _, violation := range violations {
		codes = append(codes, violation.Code)
	}
	return codes
}

func logTokenProcessingBatch(items []normalizedUsageEvent, inboxStatus string, processedRows, successfulEvents, decodeFailed int, failures redisInboxFailureCounts) {
	// outcome map 预先放入全部枚举，零计数也稳定出现在结构化日志中，便于监控直接聚合。
	outcomeCounts := map[string]int{
		string(tokenprocessor.TokenOutcomeValid):         0,
		string(tokenprocessor.TokenOutcomeNormalized):    0,
		string(tokenprocessor.TokenOutcomeCompatibility): 0,
		string(tokenprocessor.TokenOutcomeCorrected):     0,
		string(tokenprocessor.TokenOutcomeAmbiguous):     0,
		string(tokenprocessor.TokenOutcomeOverflow):      0,
	}
	actionCounts := map[string]int{}
	violationCounts := map[string]int{}
	readyCount := 0
	unknownExecutorCount := 0
	for _, item := range items {
		if item.resolution != nil && item.resolution.UnknownExecutor() {
			unknownExecutorCount++
		}
		if !item.ready {
			continue
		}
		readyCount++
		outcomeCounts[string(item.result.Outcome)]++
		for _, action := range item.result.Actions {
			actionCounts[action.Code]++
		}
		for _, violation := range item.result.Violations {
			violationCounts[violation.Code]++
		}
	}

	// 批次统计只供主动排障和本地分析，固定使用 Debug，不能在生产默认 Info 下形成每批日志。
	logrus.WithFields(logrus.Fields{
		"inbox_status":               inboxStatus,
		"inbox_row_count":            processedRows,
		"token_ready_count":          readyCount,
		"token_success_count":        successfulEvents,
		"token_decode_failed_count":  decodeFailed,
		"token_process_failed_count": failures.processFailed,
		"token_discarded_count":      failures.discarded,
		"token_outcome_counts":       outcomeCounts,
		"token_action_counts":        actionCounts,
		"token_violation_counts":     violationCounts,
		"unknown_executor_count":     unknownExecutorCount,
	}).Debug("redis usage token processing summary")
}

func buildUsageEventTypeResolver(ctx context.Context, db *gorm.DB, events []entities.UsageEvent) (usageEventTypeResolver, error) {
	// 联合键同时包含 auth_type 与 identity，避免相同 auth_index 在 OAuth/API Key 两类凭证间串用类型。
	resolver := usageEventTypeResolver{byIdentity: map[usageEventIdentityKey]string{}}
	keys := redisUsageIdentityKeys(events)
	if len(keys) == 0 {
		return resolver, nil
	}
	if db == nil {
		return resolver, fmt.Errorf("database is nil")
	}
	// active identity 是当前配置事实，必须先查并拥有高于历史 deleted identity 的优先级。
	activeRows, err := loadRedisUsageIdentityTypeRows(ctx, db, keys, false)
	if err != nil {
		return resolver, fmt.Errorf("load active usage identity types for redis usage: %w", err)
	}
	addRedisUsageIdentityTypes(resolver.byIdentity, activeRows)

	// 只为 active 未命中的联合键查历史记录，避免 deleted 数据覆盖仍有效的当前配置。
	missing := missingRedisUsageIdentityKeys(keys, resolver.byIdentity)
	if len(missing) == 0 {
		return resolver, nil
	}
	// deleted fallback 只服务历史 inbox；没有历史记录仍由 OAuth/provider 或 API Key strict 规则决定，不能猜测。
	deletedRows, err := loadRedisUsageIdentityTypeRows(ctx, db, missing, true)
	if err != nil {
		return resolver, fmt.Errorf("load deleted usage identity types for redis usage: %w", err)
	}
	addRedisUsageIdentityTypes(resolver.byIdentity, deletedRows)
	return resolver, nil
}

// Redis usage payload 只有 auth_type/auth_index，这里按 keeper identity 类型批量查出真实 provider type。
func loadRedisUsageIdentityTypeRows(ctx context.Context, db *gorm.DB, keys []usageEventIdentityKey, isDeleted bool) ([]entities.UsageIdentity, error) {
	rows := make([]entities.UsageIdentity, 0)
	// 不同 auth_type 必须拆开查询，SQL 条件才能保持联合键语义而不是只按 identity 匹配。
	keysByAuthType := groupRedisUsageIdentityKeysByAuthType(keys)
	for authType, identities := range keysByAuthType {
		// 每组再按 500 条切分，既保留批量性能，也不触发 SQLite 变量上限导致整批重试。
		for start := 0; start < len(identities); start += redisUsageIdentityTypeLookupBatchSize {
			end := start + redisUsageIdentityTypeLookupBatchSize
			if end > len(identities) {
				end = len(identities)
			}
			var batchRows []entities.UsageIdentity
			// Redis inbox 单批最多可达 1000+ 条，SELECT IN 必须单独限批，避免 SQLite 变量上限导致整批反复 process_failed。
			if err := db.WithContext(ctx).
				Select("auth_type, identity, type, is_deleted").
				Where("auth_type = ? AND identity IN ? AND is_deleted = ?", authType, identities[start:end], isDeleted).
				Find(&batchRows).Error; err != nil {
				return nil, err
			}
			rows = append(rows, batchRows...)
		}
	}
	return rows, nil
}

func addRedisUsageIdentityTypes(byIdentity map[usageEventIdentityKey]string, rows []entities.UsageIdentity) {
	for _, row := range rows {
		identity := strings.TrimSpace(row.Identity)
		usageType := strings.TrimSpace(row.Type)
		if identity != "" && usageType != "" {
			byIdentity[usageEventIdentityKey{authType: row.AuthType, identity: identity}] = usageType
		}
	}
}

func redisUsageIdentityKeys(events []entities.UsageEvent) []usageEventIdentityKey {
	seen := make(map[usageEventIdentityKey]struct{}, len(events))
	keys := make([]usageEventIdentityKey, 0, len(events))
	for _, event := range events {
		authIndex := strings.TrimSpace(event.AuthIndex)
		if authIndex == "" {
			continue
		}
		authType, ok := redisUsageIdentityAuthType(event.AuthType)
		if !ok {
			continue
		}
		key := usageEventIdentityKey{authType: authType, identity: authIndex}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func redisUsageIdentityAuthType(authType string) (entities.UsageIdentityAuthType, bool) {
	switch normalizeRedisAuthType(authType) {
	case "oauth":
		return entities.UsageIdentityAuthTypeAuthFile, true
	case "apikey":
		return entities.UsageIdentityAuthTypeAIProvider, true
	default:
		return 0, false
	}
}

func groupRedisUsageIdentityKeysByAuthType(keys []usageEventIdentityKey) map[entities.UsageIdentityAuthType][]string {
	grouped := make(map[entities.UsageIdentityAuthType][]string)
	for _, key := range keys {
		grouped[key.authType] = append(grouped[key.authType], key.identity)
	}
	return grouped
}

func missingRedisUsageIdentityKeys(keys []usageEventIdentityKey, byIdentity map[usageEventIdentityKey]string) []usageEventIdentityKey {
	missing := make([]usageEventIdentityKey, 0)
	for _, key := range keys {
		if _, ok := byIdentity[key]; ok {
			continue
		}
		missing = append(missing, key)
	}
	return missing
}

// OAuth 优先信任已同步的 auth file type；查不到时再沿用历史 provider 兜底。
func resolveUsageEventType(event entities.UsageEvent, resolver usageEventTypeResolver) string {
	switch normalizeRedisAuthType(event.AuthType) {
	case "oauth":
		if authIndex := strings.TrimSpace(event.AuthIndex); authIndex != "" {
			key := usageEventIdentityKey{authType: entities.UsageIdentityAuthTypeAuthFile, identity: authIndex}
			if usageType := strings.TrimSpace(resolver.byIdentity[key]); usageType != "" {
				return usageType
			}
		}
		return strings.TrimSpace(event.Provider)
	case "apikey":
		key := usageEventIdentityKey{authType: entities.UsageIdentityAuthTypeAIProvider, identity: strings.TrimSpace(event.AuthIndex)}
		return strings.TrimSpace(resolver.byIdentity[key])
	default:
		return "default"
	}
}

// aggregateUsageEventStats 串行追平 usage_events 派生统计；空 inbox 时也调用它补偿上次失败的聚合。
func (s *SyncService) aggregateUsageEventStats(ctx context.Context, now time.Time) error {
	if err := repository.AggregateUsageIdentityStats(ctx, s.db, now); err != nil {
		return fmt.Errorf("aggregate usage identity stats after redis inbox processing: %w", err)
	}
	if err := repository.AggregateUsageOverviewStats(ctx, s.db, now); err != nil {
		return fmt.Errorf("aggregate usage overview stats after redis inbox processing: %w", err)
	}
	return nil
}

// persistRedisUsageEvents 写入 Redis inbox 解码出的 usage_events。
func (s *SyncService) persistRedisUsageEvents(db *gorm.DB, events []entities.UsageEvent) (*servicedto.SyncResult, error) {
	logrus.WithField("event_count", len(events)).Debug("usage events insert started")
	// InsertUsageEvents 当前不再按 request_id/event_key 去重，Redis 队列中每条消息都入库为独立事件。
	inserted, deduped, err := repository.InsertUsageEvents(db, events)
	if err != nil {
		return &servicedto.SyncResult{Status: "failed"}, fmt.Errorf("insert usage events: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"inserted_events": inserted,
		"deduped_events":  deduped,
	}).Debug("usage events insert finished")
	return &servicedto.SyncResult{Status: "completed", InsertedEvents: inserted, DedupedEvents: deduped}, nil
}

// validate 只校验当前入口真正需要的依赖；Redis pull/process 不强制要求 metadata client。
func (s *SyncService) validate(syncMetadata bool) error {
	if s == nil {
		return fmt.Errorf("sync service is nil")
	}
	if s.db == nil {
		return fmt.Errorf("sync service database is nil")
	}
	if syncMetadata {
		// 老构造入口可能只传 client，没有单独传 metadataFetcher，这里在使用前补齐。
		if s.metadataFetcher == nil && s.client != nil {
			s.metadataFetcher = s.client
		}
		if s.metadataFetcher == nil {
			return fmt.Errorf("sync service metadata fetcher is nil")
		}
	}
	return nil
}

// insertRedisInboxMessages 在解码前先把 Redis 原始消息落库，降低 LPOP 后本地处理失败导致的数据丢失风险。
func insertRedisInboxMessages(db *gorm.DB, source string, messages []string, poppedAt time.Time) ([]entities.RedisUsageInbox, error) {
	// source 已经是完整来源名，这里只透传给仓储层做统一 trim/兜底。
	return repository.InsertRedisUsageInboxRawMessages(db, source, messages, poppedAt)
}

// redisInboxFailureCounts 同时保存批次计数与逐行真实状态，避免重试判断和事件日志各自猜测数据库结果。
type redisInboxFailureCounts struct {
	processFailed   int
	discarded       int
	statusUncertain bool
}

func (counts redisInboxFailureCounts) add(other redisInboxFailureCounts) redisInboxFailureCounts {
	// 同批 identity 失败与 ready 事务失败可以同时存在，汇总时必须分别累加而不是覆盖。
	counts.processFailed += other.processFailed
	counts.discarded += other.discarded
	// 任一行状态无法确认都必须传递到最终批次，不能被其它成功行或确认计数覆盖。
	counts.statusUncertain = counts.statusUncertain || other.statusUncertain
	return counts
}

func (counts redisInboxFailureCounts) requiresRetryWait() bool {
	// 已确认 process_failed 表示确实要重试；状态未知则按“仍可能要重试”保守等待。
	return counts.processFailed > 0 || counts.statusUncertain
}

// markRedisInboxRowsProcessFailed 记录可重试处理失败；达到仓储阈值后会转为 discarded 并返回准确日志计数。
func markRedisInboxRowsProcessFailed(db *gorm.DB, rows []entities.RedisUsageInbox, err error) redisInboxFailureCounts {
	// 计数只记录完成 UPDATE 且成功回读的数据，未知状态绝不填入猜测值。
	counts := redisInboxFailureCounts{}
	if err == nil {
		return counts
	}
	for _, row := range rows {
		if markErr := repository.MarkRedisUsageInboxProcessFailed(db, row.ID, err); markErr != nil {
			// UPDATE 失败后数据库可能仍是 pending 或发生不确定提交，必须保留状态未知信号。
			counts.statusUncertain = true
			logrus.WithError(markErr).WithField("inbox_id", row.ID).Warn("failed to mark redis usage inbox process failure")
			continue
		}
		// 重新读取仓储更新后的状态，只在真正丢弃时输出包含定位字段的日志。
		var stored entities.RedisUsageInbox
		if loadErr := db.First(&stored, row.ID).Error; loadErr != nil {
			// UPDATE 成功但回读失败时无法判断是 process_failed 还是 discarded，必须保守等待且不猜测计数。
			counts.statusUncertain = true
			logrus.WithError(loadErr).WithField("inbox_id", row.ID).Warn("failed to load redis usage inbox after process failure")
			continue
		}
		if stored.Status == repository.RedisUsageInboxStatusDiscarded {
			// 第五次失败已进入终态，单独计入 discarded，不能继续算作可重试 process_failed。
			counts.discarded++
			// 丢弃日志保留 source，便于区分 subscribe、redis_pull 和 http_pull 写入的历史原始消息。
			logrus.WithFields(logrus.Fields{
				"inbox_id":      stored.ID,
				"source":        stored.Source,
				"message_hash":  stored.MessageHash,
				"attempt_count": stored.AttemptCount,
				"last_error":    stored.LastError,
				"popped_at":     stored.PoppedAt,
			}).Warn("discarded redis usage inbox row after repeated process failures")
			continue
		}
		if stored.Status == repository.RedisUsageInboxStatusProcessFailed {
			// 未达到五次的行仍由下一轮 ProcessRedisUsageInbox 重试。
			counts.processFailed++
		}
	}
	return counts
}

// errorMessage 把可选错误转成仓储 DTO 使用的稳定字符串。
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// firstNonEmpty 返回第一个非空字符串，用于统一处理 CPA 字段缺省优先级。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// joinErrors 合并多个独立同步错误，保留同一轮部分成功/部分失败的上下文。
func joinErrors(errs ...error) error {
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		messages = append(messages, strings.TrimSpace(err.Error()))
	}
	if len(messages) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(messages, "; "))
}
