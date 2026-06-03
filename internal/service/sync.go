package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/timeutil"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// MetadataFetcher 是 metadata 同步依赖的 CPA 只读接口，测试可以用它替换真实 CPA client。
type MetadataFetcher interface {
	FetchAuthFiles(ctx context.Context) (*response.AuthFilesResult, error)
	FetchManagementAPIKeys(ctx context.Context) (*response.ManagementAPIKeysResult, error)
	FetchGeminiAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error)
	FetchClaudeAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error)
	FetchCodexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error)
	FetchVertexAPIKeys(ctx context.Context) (*response.ProviderKeyConfigResult, error)
	FetchOpenAICompatibility(ctx context.Context) (*response.OpenAICompatibilityResult, error)
}

// CPAClientFetcher 保留 CPA client 的聚合接口边界，避免业务同步代码直接依赖具体 client 类型。
type CPAClientFetcher interface {
	MetadataFetcher
}

const (
	redisInboxProcessLimit                = 1000
	redisUsageIdentityTypeLookupBatchSize = 500
)

const (
	syncMetadataOptional = false
	syncMetadataRequired = true
)

// SyncService 负责把 CPA metadata 和 Redis usage 队列同步到本地 SQLite。
type SyncService struct {
	db              *gorm.DB
	client          CPAClientFetcher
	redisQueue      RedisQueue
	redisQueueKey   string
	metadataFetcher MetadataFetcher
	baseURL         string
	now             func() time.Time
}

// NewSyncService 按生产配置组装 CPA metadata client 和 Redis queue client。
func NewSyncService(db *gorm.DB, cfg config.Config) *SyncService {
	return NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: cfg.CPABaseURL,
		Client:  cpa.NewClient(cfg.CPABaseURL, cfg.CPAManagementKey, cfg.RequestTimeout, cfg.TLSSkipVerify),
		RedisQueue: cpa.NewRedisQueueClientWithOptions(cpa.RedisQueueOptions{
			BaseURL:       cfg.CPABaseURL,
			RedisAddr:     cfg.RedisQueueAddr,
			ManagementKey: cfg.CPAManagementKey,
			Timeout:       cfg.RequestTimeout,
			QueueKey:      cfg.RedisQueueKey,
			BatchSize:     cfg.RedisQueueBatchSize,
			TLS:           cfg.RedisQueueTLS,
			TLSSkipVerify: cfg.TLSSkipVerify,
		}),
		RedisQueueKey: cfg.RedisQueueKey,
	})
}

// SyncServiceOptions 提供测试和局部调用需要替换的依赖。
type SyncServiceOptions struct {
	BaseURL         string
	Client          CPAClientFetcher
	MetadataFetcher MetadataFetcher
	RedisQueue      RedisQueue
	RedisQueueKey   string
	Now             func() time.Time
}

// NewSyncServiceWithOptions 是统一构造入口，负责填充默认时钟、metadata fetcher 和 Redis 队列名。
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
		db:              db,
		client:          opts.Client,
		redisQueue:      opts.RedisQueue,
		redisQueueKey:   redisQueueKey(opts.RedisQueueKey),
		metadataFetcher: metadataFetcher,
		baseURL:         strings.TrimSpace(opts.BaseURL),
		now:             now,
	}
}

// NewSyncServiceWithClient 兼容只需要 metadata 同步的调用方和测试。
func NewSyncServiceWithClient(db *gorm.DB, baseURL string, client CPAClientFetcher) *SyncService {
	return NewSyncServiceWithOptions(db, SyncServiceOptions{
		BaseURL: baseURL,
		Client:  client,
	})
}

// SyncMetadata 同步 CPA 的 auth files、管理 API keys 和 provider metadata，并在成功写入后刷新 usage identity 聚合。
func (s *SyncService) SyncMetadata(ctx context.Context) error {
	if err := s.validate(syncMetadataRequired); err != nil {
		return err
	}
	logrus.Debug("metadata sync started")
	// 同一轮 metadata 同步共用一个时间戳，保证 replaced/deleted 状态边界一致。
	fetchedAt := timeutil.NormalizeStorageTime(s.now())
	// 三类 metadata 先分别抓取，后续各自写库；某一类抓取失败不影响其它类先完成入库。
	authFilesResult, authFilesErr := s.metadataFetcher.FetchAuthFiles(ctx)
	apiKeysResult, apiKeysErr := s.metadataFetcher.FetchManagementAPIKeys(ctx)
	providerConfig, fetchedProviderTypes, providerMetadataErr := fetchProviderMetadata(ctx, s.metadataFetcher)
	// 写库阶段按来源拆开，便于保留部分成功结果并把错误合并返回给 runner 日志。
	authSyncErr := syncAuthFiles(ctx, s.db, authFilesResult, authFilesErr, fetchedAt)
	apiKeySyncErr := syncManagementAPIKeys(s.db, apiKeysResult, apiKeysErr, fetchedAt)
	providerSyncErr, providerWarningErr := syncProviderMetadata(ctx, s.db, providerConfig, fetchedProviderTypes, providerMetadataErr, fetchedAt)
	upsertErr := joinErrors(authSyncErr, apiKeySyncErr, providerSyncErr)
	var aggregateErr error
	if upsertErr == nil {
		// 身份来源写入全部成功后再刷新 usage_identities 派生统计，避免基于半成品 metadata 聚合。
		aggregateErr = repository.AggregateUsageIdentityStats(ctx, s.db, fetchedAt)
		if aggregateErr != nil {
			aggregateErr = fmt.Errorf("aggregate usage identity stats: %w", aggregateErr)
		}
	}
	err := joinErrors(upsertErr, aggregateErr, providerWarningErr)
	fields := logrus.Fields{
		"status": "completed",
	}
	if err != nil {
		fields["status"] = "completed_with_warnings"
		fields["error"] = err.Error()
	}
	logrus.WithFields(fields).Debug("metadata sync finished")
	return err
}

// PullRedisUsageInbox 是兼容测试和手动同步的旧拉取入口；生产远端 ingest 已由 poller 状态机接管。
func (s *SyncService) PullRedisUsageInbox(ctx context.Context) (*servicedto.RedisInboxPullResult, error) {
	if err := s.validate(syncMetadataOptional); err != nil {
		return nil, err
	}
	if s.redisQueue == nil {
		return nil, fmt.Errorf("sync service redis queue is nil")
	}
	fetchedAt := timeutil.NormalizeStorageTime(s.now())
	messages, err := s.redisQueue.PopUsage(ctx)
	if err != nil {
		return &servicedto.RedisInboxPullResult{Status: "failed"}, fmt.Errorf("fetch redis usage: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"queue_key":     s.redisQueueKey,
		"message_count": len(messages),
	}).Debug("redis usage batch popped")
	if len(messages) == 0 {
		return &servicedto.RedisInboxPullResult{Empty: true, Status: "empty"}, nil
	}
	inboxRows, err := insertRedisInboxMessages(s.db, s.redisQueueKey, messages, fetchedAt)
	if err != nil {
		return &servicedto.RedisInboxPullResult{Status: "failed"}, fmt.Errorf("insert redis usage inbox: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"queue_key": s.redisQueueKey,
		"row_count": len(inboxRows),
	}).Debug("redis usage inbox rows inserted")
	return &servicedto.RedisInboxPullResult{Status: "completed", InsertedRows: len(inboxRows)}, nil
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
		return &servicedto.RedisBatchSyncResult{Status: "failed"}, fmt.Errorf("list processable redis usage inbox: %w", err)
	}
	if len(processableRows) == 0 {
		// 空轮次先做轻量 cursor 检查，只有发现 usage_events 尚未聚合时才写派生统计。
		pendingAggregation, err := repository.HasPendingUsageOverviewAggregation(ctx, s.db)
		if err != nil {
			return &servicedto.RedisBatchSyncResult{Empty: true, Status: "failed"}, err
		}
		if pendingAggregation {
			if err := s.aggregateUsageEventStats(ctx, fetchedAt); err != nil {
				return &servicedto.RedisBatchSyncResult{Empty: true, Status: "failed"}, err
			}
		}
		return &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}, nil
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

// CleanupStorage 是每日 03:00 维护任务调用的统一入口：先清 Redis inbox，最后 VACUUM 收缩 SQLite。
func (s *SyncService) CleanupStorage(ctx context.Context) error {
	if err := s.validate(syncMetadataOptional); err != nil {
		return err
	}
	_, err := repository.CleanupStorage(s.db, s.now())
	return err
}

// processRedisInboxRows 只从已落库的原始消息解码和写入事件，坏消息会标记为 decode_failed，不阻塞同批其它数据。
// 可解码但入库失败的消息标记为 process_failed，后续 ProcessRedisUsageInbox 会按 id 顺序重试。
func (s *SyncService) processRedisInboxRows(ctx context.Context, inboxRows []entities.RedisUsageInbox, fetchedAt time.Time) (*servicedto.RedisBatchSyncResult, error) {
	logrus.WithField("row_count", len(inboxRows)).Debug("redis usage inbox processing started")
	validRows := make([]entities.RedisUsageInbox, 0, len(inboxRows))
	events := make([]entities.UsageEvent, 0, len(inboxRows))
	decodeErrs := make([]error, 0)
	// 先完整解码本批数据，坏消息单独标记，不阻断同批其它可用消息。
	for _, row := range inboxRows {
		event, _, decodeErr := DecodeRedisUsageMessage(row.RawMessage, fetchedAt)
		if decodeErr != nil {
			logrus.WithError(decodeErr).WithField("inbox_id", row.ID).Error("redis usage message decode failed")
			if markErr := repository.MarkRedisUsageInboxDecodeFailed(s.db, row.ID, decodeErr); markErr != nil {
				return &servicedto.RedisBatchSyncResult{Status: "failed"}, fmt.Errorf("mark redis usage inbox decode failed: %w", markErr)
			}
			decodeErrs = append(decodeErrs, decodeErr)
			continue
		}
		validRows = append(validRows, row)
		events = append(events, event)
	}
	decodeErr := joinErrors(decodeErrs...)
	logrus.WithFields(logrus.Fields{
		"row_count":           len(inboxRows),
		"valid_event_count":   len(events),
		"decode_failed_count": len(decodeErrs),
	}).Debug("redis usage inbox rows decoded")
	if len(events) == 0 {
		if decodeErr != nil {
			return &servicedto.RedisBatchSyncResult{Status: "completed_with_warnings"}, decodeErr
		}
		return &servicedto.RedisBatchSyncResult{Empty: true, Status: "empty"}, nil
	}
	var typeErr error
	events, typeErr = normalizeRedisUsageEvents(ctx, s.db, events)
	if typeErr != nil {
		// type 查询失败代表当前无法可靠判断 provider 口径，不能当作“找不到 type”降级处理。
		// 将已解码行标记为 process_failed，后续重试时再按真实 type 归一化入库。
		markRedisInboxRowsProcessFailed(s.db, validRows, typeErr)
		return &servicedto.RedisBatchSyncResult{Status: "failed"}, joinErrors(decodeErr, typeErr)
	}

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
	if result == nil {
		markRedisInboxRowsProcessFailed(s.db, validRows, err)
		return nil, err
	}
	if err != nil {
		markRedisInboxRowsProcessFailed(s.db, validRows, err)
		return &servicedto.RedisBatchSyncResult{Status: "failed"}, err
	}
	if result.InsertedEvents > 0 {
		// Redis process 是 usage_events 的高频写入入口，成功插入后串行刷新依赖事件表的增量统计。
		if err := s.aggregateUsageEventStats(ctx, timeutil.NormalizeStorageTime(s.now())); err != nil {
			return &servicedto.RedisBatchSyncResult{Status: "failed"}, err
		}
	}
	logrus.WithFields(logrus.Fields{
		"processed_rows":  len(validRows),
		"inserted_events": result.InsertedEvents,
		"deduped_events":  result.DedupedEvents,
		"status":          result.Status,
	}).Debug("redis usage inbox rows processed")

	// 批次可部分成功：事件正常入库时仍返回 completed_with_warnings 暴露 decode 错误。
	status := result.Status
	returnErr := err
	if decodeErr != nil {
		status = "completed_with_warnings"
		if returnErr != nil {
			returnErr = joinErrors(returnErr, decodeErr)
		} else {
			returnErr = decodeErr
		}
	}
	return &servicedto.RedisBatchSyncResult{
		Status:         status,
		InsertedEvents: result.InsertedEvents,
		DedupedEvents:  result.DedupedEvents,
	}, returnErr
}

type usageEventTypeResolver struct {
	byAuthIndex map[string]string
}

func normalizeRedisUsageEvents(ctx context.Context, db *gorm.DB, events []entities.UsageEvent) ([]entities.UsageEvent, error) {
	resolver, err := buildUsageEventTypeResolver(ctx, db, events)
	if err != nil {
		return nil, err
	}
	normalized := make([]entities.UsageEvent, len(events))
	for i, event := range events {
		usageType := resolveUsageEventType(event, resolver)
		if usageType == "" {
			logrus.WithFields(logrus.Fields{
				"auth_type":  event.AuthType,
				"auth_index": event.AuthIndex,
				"event_key":  event.EventKey,
			}).Warn("usage identity type not found for redis usage event")
			usageType = "openai"
		}
		normalized[i] = NormalizeUsageEventTokens(event, usageType)
	}
	return normalized, nil
}

func buildUsageEventTypeResolver(ctx context.Context, db *gorm.DB, events []entities.UsageEvent) (usageEventTypeResolver, error) {
	resolver := usageEventTypeResolver{byAuthIndex: map[string]string{}}
	authIndexes := redisUsageAPIKeyAuthIndexes(events)
	if len(authIndexes) == 0 {
		return resolver, nil
	}
	if db == nil {
		return resolver, fmt.Errorf("database is nil")
	}
	activeRows, err := loadRedisUsageIdentityTypeRows(ctx, db, authIndexes, false)
	if err != nil {
		return resolver, fmt.Errorf("load active usage identity types for redis usage: %w", err)
	}
	addRedisUsageIdentityTypes(resolver.byAuthIndex, activeRows)

	missing := missingRedisUsageAuthIndexes(authIndexes, resolver.byAuthIndex)
	if len(missing) == 0 {
		return resolver, nil
	}
	deletedRows, err := loadRedisUsageIdentityTypeRows(ctx, db, missing, true)
	if err != nil {
		return resolver, fmt.Errorf("load deleted usage identity types for redis usage: %w", err)
	}
	addRedisUsageIdentityTypes(resolver.byAuthIndex, deletedRows)
	return resolver, nil
}

func loadRedisUsageIdentityTypeRows(ctx context.Context, db *gorm.DB, authIndexes []string, isDeleted bool) ([]entities.UsageIdentity, error) {
	rows := make([]entities.UsageIdentity, 0)
	for start := 0; start < len(authIndexes); start += redisUsageIdentityTypeLookupBatchSize {
		end := start + redisUsageIdentityTypeLookupBatchSize
		if end > len(authIndexes) {
			end = len(authIndexes)
		}
		var batchRows []entities.UsageIdentity
		// Redis inbox 单批最多可达 1000+ 条，SELECT IN 必须单独限批，避免 SQLite 变量上限导致整批反复 process_failed。
		if err := db.WithContext(ctx).
			Select("identity, type, is_deleted").
			Where("auth_type = ? AND identity IN ? AND is_deleted = ?", entities.UsageIdentityAuthTypeAIProvider, authIndexes[start:end], isDeleted).
			Find(&batchRows).Error; err != nil {
			return nil, err
		}
		rows = append(rows, batchRows...)
	}
	return rows, nil
}

func addRedisUsageIdentityTypes(byAuthIndex map[string]string, rows []entities.UsageIdentity) {
	for _, row := range rows {
		identity := strings.TrimSpace(row.Identity)
		usageType := strings.TrimSpace(row.Type)
		if identity != "" && usageType != "" {
			byAuthIndex[identity] = usageType
		}
	}
}

func redisUsageAPIKeyAuthIndexes(events []entities.UsageEvent) []string {
	seen := make(map[string]struct{}, len(events))
	authIndexes := make([]string, 0, len(events))
	for _, event := range events {
		if normalizeRedisAuthType(event.AuthType) != "apikey" {
			continue
		}
		authIndex := strings.TrimSpace(event.AuthIndex)
		if authIndex == "" {
			continue
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		authIndexes = append(authIndexes, authIndex)
	}
	return authIndexes
}

func missingRedisUsageAuthIndexes(authIndexes []string, byAuthIndex map[string]string) []string {
	missing := make([]string, 0)
	for _, authIndex := range authIndexes {
		if _, ok := byAuthIndex[authIndex]; ok {
			continue
		}
		missing = append(missing, authIndex)
	}
	return missing
}

func resolveUsageEventType(event entities.UsageEvent, resolver usageEventTypeResolver) string {
	switch normalizeRedisAuthType(event.AuthType) {
	case "oauth":
		return strings.TrimSpace(event.Provider)
	case "apikey":
		return strings.TrimSpace(resolver.byAuthIndex[strings.TrimSpace(event.AuthIndex)])
	default:
		return "openai"
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
func insertRedisInboxMessages(db *gorm.DB, queueKey string, messages []string, poppedAt time.Time) ([]entities.RedisUsageInbox, error) {
	return repository.InsertRedisUsageInboxRawMessages(db, queueKey, messages, poppedAt)
}

// markRedisInboxRowsProcessFailed 记录可重试处理失败；达到仓储阈值后会转为 discarded 并打警告日志。
func markRedisInboxRowsProcessFailed(db *gorm.DB, rows []entities.RedisUsageInbox, err error) {
	if err == nil {
		return
	}
	for _, row := range rows {
		if markErr := repository.MarkRedisUsageInboxProcessFailed(db, row.ID, err); markErr != nil {
			logrus.WithError(markErr).WithField("inbox_id", row.ID).Warn("failed to mark redis usage inbox process failure")
			continue
		}
		// 重新读取仓储更新后的状态，只在真正丢弃时输出包含定位字段的日志。
		var stored entities.RedisUsageInbox
		if loadErr := db.First(&stored, row.ID).Error; loadErr != nil {
			logrus.WithError(loadErr).WithField("inbox_id", row.ID).Warn("failed to load redis usage inbox after process failure")
			continue
		}
		if stored.Status == repository.RedisUsageInboxStatusDiscarded {
			logrus.WithFields(logrus.Fields{
				"inbox_id":      stored.ID,
				"queue_key":     stored.QueueKey,
				"message_hash":  stored.MessageHash,
				"attempt_count": stored.AttemptCount,
				"last_error":    stored.LastError,
				"popped_at":     stored.PoppedAt,
			}).Warn("discarded redis usage inbox row after repeated process failures")
		}
	}
}

// redisQueueKey 统一 Redis usage 队列名默认值，避免构造测试服务时重复传常量。
func redisQueueKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return cpa.ManagementUsageQueueKey
	}
	return trimmed
}

// errorMessage 把可选错误转成仓储 DTO 使用的稳定字符串。
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// syncAuthFiles 将 CPA auth_files 映射为 OAuth 类 usage identities，并按 auth_type 整体替换。
func syncAuthFiles(ctx context.Context, db *gorm.DB, result *response.AuthFilesResult, fetchErr error, now time.Time) error {
	if fetchErr != nil {
		return fmt.Errorf("fetch auth files: %w", fetchErr)
	}
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if result == nil {
		return fmt.Errorf("fetch auth files: empty response")
	}

	identities := make([]entities.UsageIdentity, 0, len(result.Payload.Files))
	for _, file := range result.Payload.Files {
		identities = append(identities, authFileUsageIdentity(file))
	}
	if err := repository.ReplaceUsageIdentitiesForAuthType(ctx, db, identities, entities.UsageIdentityAuthTypeAuthFile, now); err != nil {
		return fmt.Errorf("sync auth file usage identities: %w", err)
	}
	return nil
}

// syncManagementAPIKeys 同步 CPA 管理 API key 清单；原值只在本地保存，对外查询时再脱敏。
func syncManagementAPIKeys(db *gorm.DB, result *response.ManagementAPIKeysResult, fetchErr error, now time.Time) error {
	if fetchErr != nil {
		return fmt.Errorf("fetch management api keys: %w", fetchErr)
	}
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if result == nil {
		return fmt.Errorf("fetch management api keys: empty response")
	}
	if err := repository.SyncCPAAPIKeys(db, result.Payload.APIKeys, now); err != nil {
		return fmt.Errorf("sync management api keys: %w", err)
	}
	return nil
}

type authFileUsageIdentityExtension func(authfiles.AuthFile, *entities.UsageIdentity)

var authFileUsageIdentityExtensions = map[string]authFileUsageIdentityExtension{
	"codex": extendCodexAuthFileUsageIdentity,
}

// auth_files 先走通用身份映射，再按 type 追加各来源特有字段，方便后续扩展新类型。
func authFileUsageIdentity(file authfiles.AuthFile) entities.UsageIdentity {
	identity := baseAuthFileUsageIdentity(file)
	if extend, ok := authFileUsageIdentityExtensions[strings.ToLower(strings.TrimSpace(file.Type))]; ok {
		extend(file, &identity)
	}
	identity.ProjectID = resolveAuthFileProjectID(file)
	return identity
}

// baseAuthFileUsageIdentity 写入所有 auth_files 共享的身份字段，特殊字段由扩展函数补充。
func baseAuthFileUsageIdentity(file authfiles.AuthFile) entities.UsageIdentity {
	return entities.UsageIdentity{
		Name:         firstNonEmpty(file.Email, file.Label, file.Name, file.AuthIndex),
		AuthType:     entities.UsageIdentityAuthTypeAuthFile,
		AuthTypeName: "oauth",
		Identity:     file.AuthIndex,
		Type:         file.Type,
		Provider:     file.Provider,
		Prefix:       file.Prefix,
		FileName:     stringValue(file.Name),
		FilePath:     stringValue(file.Path),
		Priority:     file.Priority,
		Disabled:     file.Disabled,
		Note:         file.Note,
	}
}

// Codex 的 ChatGPT id_token 字段只在 type=codex 且字段存在时写入；缺失字段保持 nil，入库后就是 NULL。
func extendCodexAuthFileUsageIdentity(file authfiles.AuthFile, identity *entities.UsageIdentity) {
	identity.AccountID = resolveCodexAccountID(file)
	identity.ActiveStart = resolveCodexActiveStart(file)
	identity.ActiveUntil = resolveCodexActiveUntil(file)
	identity.PlanType = resolveCodexPlanType(file)
}

// fetchProviderMetadata 分别拉取各 AI provider 配置，并记录本轮实际成功返回的 provider 类型。
func fetchProviderMetadata(ctx context.Context, fetcher MetadataFetcher) (providerconfig.ProviderMetadataConfig, []string, error) {
	var cfg providerconfig.ProviderMetadataConfig
	var fetchedProviderTypes []string
	var errs []error

	// 每个 provider 独立收集错误，避免单一来源失败导致其它来源 metadata 无法更新。
	if result, err := fetcher.FetchGeminiAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch gemini api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("gemini api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "gemini")
		cfg.GeminiAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchClaudeAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch claude api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("claude api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "claude")
		cfg.ClaudeAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchCodexAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch codex api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("codex api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "codex")
		cfg.CodexAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchVertexAPIKeys(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch vertex api keys: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("vertex api keys response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "vertex")
		cfg.VertexAPIKeys = result.Payload
	}
	if result, err := fetcher.FetchOpenAICompatibility(ctx); err != nil {
		errs = append(errs, fmt.Errorf("fetch openai compatibility: %w", err))
	} else if result == nil {
		errs = append(errs, fmt.Errorf("openai compatibility response is nil"))
	} else {
		fetchedProviderTypes = append(fetchedProviderTypes, "openai")
		cfg.OpenAICompatibility = result.Payload
	}

	return cfg, fetchedProviderTypes, joinErrors(errs...)
}

// syncProviderMetadata 用成功抓到的 provider 类型做替换；抓取警告延后返回，不阻止成功来源入库。
func syncProviderMetadata(ctx context.Context, db *gorm.DB, cfg providerconfig.ProviderMetadataConfig, fetchedProviderTypes []string, fetchErr error, now time.Time) (error, error) {
	if db == nil {
		return fmt.Errorf("database is nil"), nil
	}

	inputs := flattenProviderMetadata(cfg)
	identities := providerMetadataUsageIdentities(inputs)
	if err := repository.ReplaceUsageIdentitiesForProviderTypes(ctx, db, identities, fetchedProviderTypes, now); err != nil {
		return fmt.Errorf("sync provider usage identities: %w", err), nil
	}
	if fetchErr != nil {
		return nil, fmt.Errorf("fetch provider metadata: %w", fetchErr)
	}
	return nil, nil
}

// providerMetadataUsageIdentities 把扁平 provider metadata 转成 usage identity 记录。
func providerMetadataUsageIdentities(inputs []servicedto.ProviderMetadataInput) []entities.UsageIdentity {
	identities := make([]entities.UsageIdentity, 0, len(inputs))
	for _, input := range inputs {
		identities = append(identities, entities.UsageIdentity{
			Name:         input.DisplayName,
			AuthType:     entities.UsageIdentityAuthTypeAIProvider,
			AuthTypeName: "apikey",
			Identity:     input.AuthIndex,
			Type:         input.ProviderType,
			Provider:     input.DisplayName,
			LookupKey:    input.LookupKey,
			Prefix:       input.Prefix,
			BaseURL:      input.BaseURL,
			Priority:     input.Priority,
			Disabled:     input.Disabled,
			Note:         input.Note,
		})
	}
	return identities
}

// flattenProviderMetadata 将不同 provider 的 CPA 配置压平成统一输入，供仓储层按 provider 类型替换。
func flattenProviderMetadata(cfg providerconfig.ProviderMetadataConfig) []servicedto.ProviderMetadataInput {
	items := make([]servicedto.ProviderMetadataInput, 0)
	seen := make(map[string]struct{})
	// Provider metadata 只生成 auth-index 身份；prefix 作为同一身份的附加字段保存，不再生成独立行。
	appendItem := func(lookupKey, prefix, providerType, displayName, authIndex, baseURL string, priority *int, disabled *bool, note *string) {
		lookupKey = strings.TrimSpace(lookupKey)
		prefix = strings.TrimSpace(prefix)
		providerType = strings.TrimSpace(providerType)
		displayName = strings.TrimSpace(displayName)
		authIndex = strings.TrimSpace(authIndex)
		baseURL = strings.TrimSpace(baseURL)
		if lookupKey == "" || providerType == "" || displayName == "" || authIndex == "" {
			return
		}
		if _, ok := seen[authIndex]; ok {
			return
		}
		seen[authIndex] = struct{}{}
		items = append(items, servicedto.ProviderMetadataInput{
			LookupKey:    lookupKey,
			Prefix:       prefix,
			ProviderType: providerType,
			DisplayName:  displayName,
			AuthIndex:    authIndex,
			BaseURL:      baseURL,
			Priority:     priority,
			Disabled:     disabled,
			Note:         note,
		})
	}
	appendProviderEntries := func(providerType string, configs []providerconfig.ProviderKeyConfig) {
		for _, cfg := range configs {
			displayName := firstNonEmpty(cfg.Name, providerType)
			appendItem(cfg.APIKey, cfg.Prefix, providerType, displayName, cfg.AuthIndex, cfg.BaseURL, cfg.Priority, cfg.Disabled, cfg.Note)
		}
	}

	appendProviderEntries("gemini", cfg.GeminiAPIKeys)
	appendProviderEntries("claude", cfg.ClaudeAPIKeys)
	appendProviderEntries("codex", cfg.CodexAPIKeys)
	appendProviderEntries("vertex", cfg.VertexAPIKeys)

	// OpenAI compatibility 的 prefix/baseURL 在 provider 层，API key/auth_index 在 entry 层，需要组合后再落库。
	for _, provider := range cfg.OpenAICompatibility {
		displayName := firstNonEmpty(provider.Name, "openai")
		for _, entry := range provider.APIKeyEntries {
			appendItem(entry.APIKey, provider.Prefix, "openai", displayName, entry.AuthIndex, provider.BaseURL, provider.Priority, provider.Disabled, provider.Note)
		}
	}

	return items
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
