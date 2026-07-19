package service

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service/providermetadata"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
)

// SyncMetadata 同步三类 CPA metadata，并只在三类数据库写入都成功后追平 identity 历史统计。
func (s *SyncService) SyncMetadata(ctx context.Context) error {
	// metadata 入口仍复用原 validate，并保留老构造器的 client 回填行为。
	if err := s.validate(syncMetadataRequired); err != nil {
		// 配置不完整时不发起任何远端读取。
		return err
	}
	// 开始日志保持原有 debug 级别和 message。
	logrus.Debug("metadata sync started")
	// 同一轮 Auth Files、管理 key、provider 和统计补算共用一个规范化时间。
	fetchedAt := timeutil.NormalizeStorageTime(s.now())
	// Auth Files 先读取，但失败不跳过后续两类 metadata。
	authFilesResult, authFilesErr := s.metadataFetcher.FetchAuthFiles(ctx)
	// 管理 API Keys 保持第二个读取位置，失败同样不阻止 provider。
	apiKeysResult, apiKeysErr := s.metadataFetcher.FetchManagementAPIKeys(ctx)
	// 七个 provider endpoint 只在纯包内并发，返回按 registry 确定性归并的 snapshot。
	providerSnapshot, providerFetchErr := providermetadata.Fetch(ctx, s.metadataFetcher)
	// Auth Files 先进入自己的 repository 事务，保持原有写入顺序。
	authSyncErr := syncAuthFiles(ctx, s.db, authFilesResult, authFilesErr, fetchedAt)
	// 管理 API Keys 第二个串行写入，不与 SQLite provider 写入并发。
	apiKeySyncErr := syncManagementAPIKeys(s.db, apiKeysResult, apiKeysErr, fetchedAt)
	// provider snapshot 最后一次进入 scoped replace 单事务，并分开返回 persistence error 与 fetch warning。
	providerSyncErr, providerWarningErr := persistProviderMetadata(ctx, s.db, providerSnapshot, providerFetchErr, fetchedAt)
	// 三类持久化错误按 Auth Files、管理 key、provider 的既有顺序合并。
	upsertErr := joinErrors(authSyncErr, apiKeySyncErr, providerSyncErr)
	// aggregateErr 默认为空，只有全部 metadata 写入成功才可能赋值。
	var aggregateErr error
	// 任一数据库写入失败都阻止基于半成品 identity 做历史补算。
	if upsertErr == nil {
		// provider fetch warning 不属于持久化失败，因此仍允许成功 identity 追平历史 usage_events。
		aggregateErr = repository.AggregateUsageIdentityStats(ctx, s.db, fetchedAt)
		// 聚合失败继续保持独立错误分类。
		if aggregateErr != nil {
			// 包装文本保持原有 service 对外错误语义。
			aggregateErr = fmt.Errorf("aggregate usage identity stats: %w", aggregateErr)
		}
	}
	// 最终顺序保持 upsert、aggregate、provider warning，provider persistence 失败时 warning 已由持久化入口抑制。
	err := joinErrors(upsertErr, aggregateErr, providerWarningErr)
	// 完成日志默认沿用 completed 状态。
	fields := logrus.Fields{
		// status 字段保持现有 dashboard/runtime 观察语义。
		"status": "completed",
	}
	// 任一 warning 或错误都只改变完成状态，不回滚已提交的独立事务。
	if err != nil {
		// completed_with_warnings 保持原有非 fail-fast 表达。
		fields["status"] = "completed_with_warnings"
		// error 只写稳定合并后的错误；provider client 已保证不泄漏 response body。
		fields["error"] = err.Error()
	}
	// 完成日志保持原有 debug 级别和 message。
	logrus.WithFields(fields).Debug("metadata sync finished")
	// 返回全部可观察错误，同时保留本轮已提交数据。
	return err
}
